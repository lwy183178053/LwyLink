import { SendVoiceAudioFrame } from "../wailsjs/go/main/App";
import { RnnoiseWorkletNode, loadRnnoise } from "@sapphi-red/web-noise-suppressor";
import rnnoiseWasmPath from "@sapphi-red/web-noise-suppressor/rnnoise.wasm?url";
import rnnoiseSIMDWasmPath from "@sapphi-red/web-noise-suppressor/rnnoise_simd.wasm?url";
import rnnoiseWorkletPath from "@sapphi-red/web-noise-suppressor/rnnoiseWorklet.js?url";
import type { Room } from "./types";
import type { VoiceSettings } from "./types";

type VoiceSessionCallbacks = {
  onError: (message: string) => void;
  onInputLevel?: (level: number) => void;
};

type EncodedVoiceFrame = {
  codec: VoiceCodec;
  sampleRate: number;
  timestampMS: number;
  payload: Uint8Array;
};

type VoiceCodec = "opus" | "pcm16";

export type IncomingVoiceFrame = {
  roomID: number;
  fromUserID: number;
  toUserID: number;
  seq: number;
  timestampMS: number;
  codec: string;
  sampleRate: number;
  payload: number[] | string;
};

const targetSampleRate = 48000;
const pcmFallbackSampleRate = 16000;
const frameDurationMS = 20;
const frameSamples = (targetSampleRate * frameDurationMS) / 1000;
const opusBitrate = 48000;
const jitterBufferDelayMS = 800;
const maxJitterFrames = 24;
const targetRMS = 0.075;
const autoGainMax = 1.8;
const softLimiterDrive = 1.15;
const smoothingBlend = 0.18;
const gainAttack = 0.28;
const gainRelease = 0.08;
const hangoverFrameCount = 14;
const speechHoldRMS = 0.006;
const speechFloorRMS = 0.0015;
const hangoverGainMax = 2.05;
const hangoverGainRelease = 0.025;
const voicePlaybackWorkletSource = `
class LwyLinkVoicePlaybackProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.volume = 1;
    this.users = new Map();
    this.port.onmessage = (event) => this.handleMessage(event.data || {});
  }

  handleMessage(message) {
    if (message.type === "setVolume") {
      const volume = Number(message.volume);
      this.volume = Number.isFinite(volume) ? Math.max(0.05, Math.min(2, volume)) : 1;
      return;
    }
    if (message.type === "resetAll") {
      this.users.clear();
      return;
    }
    if (message.type === "resetUser") {
      this.users.delete(Number(message.userID));
      return;
    }
    if (message.type === "pushFrame") {
      const userID = Number(message.userID);
      const samples = message.samples;
      if (!Number.isFinite(userID) || !(samples instanceof Float32Array) || samples.length === 0) {
        return;
      }
      let queue = this.users.get(userID);
      if (!queue) {
        queue = [];
        this.users.set(userID, queue);
      }
      queue.push(samples);
      let queuedSamples = 0;
      for (let i = queue.length - 1; i >= 0; i--) {
        queuedSamples += queue[i].length;
        if (queuedSamples > sampleRate * 0.7) {
          queue.splice(0, i);
          break;
        }
      }
    }
  }

  process(_inputs, outputs) {
    const output = outputs[0]?.[0];
    if (!output) {
      return true;
    }
    output.fill(0);
    let activeUsers = 0;
    for (const [userID, queue] of this.users) {
      if (queue.length === 0) {
        this.users.delete(userID);
        continue;
      }
      activeUsers++;
      let offset = 0;
      while (offset < output.length && queue.length > 0) {
        const head = queue[0];
        const take = Math.min(output.length - offset, head.length);
        for (let i = 0; i < take; i++) {
          output[offset + i] += head[i];
        }
        offset += take;
        if (take === head.length) {
          queue.shift();
        } else {
          queue[0] = head.subarray(take);
        }
      }
    }
    const mixScale = activeUsers > 1 ? 1 / Math.sqrt(activeUsers) : 1;
    for (let i = 0; i < output.length; i++) {
      output[i] = Math.tanh(output[i] * this.volume * mixScale);
    }
    return true;
  }
}

registerProcessor("lwylink-voice-playback", LwyLinkVoicePlaybackProcessor);
`;

export class VoiceSession {
  private room: Room;
  private callbacks: VoiceSessionCallbacks;
  private settings: VoiceSettings;
  private audioContext?: AudioContext;
  private stream?: MediaStream;
  private source?: MediaStreamAudioSourceNode;
  private noiseSuppressor?: RnnoiseWorkletNode;
  private processor?: ScriptProcessorNode;
  private monitorGain?: GainNode;
  private playback?: VoicePlaybackEngine;
  private encoder?: VoiceEncoder;
  private decoder?: VoiceDecoder;
  private micBuffer = new Float32Array(0);
  private micEnabled = false;
  private seq = 0;
  private started = false;
  private inputProcessor = new VoiceInputProcessor();
  private playbackByUser = new Map<number, VoicePlayoutBuffer>();

  constructor(room: Room, settings: VoiceSettings, callbacks: VoiceSessionCallbacks) {
    this.room = room;
    this.settings = settings;
    this.callbacks = callbacks;
  }

  updateSettings(settings: VoiceSettings) {
    this.settings = settings;
    this.playback?.setVolume(settings.outputVolume);
  }

  async start() {
    if (this.started) {
      return;
    }
    this.stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        deviceId: this.settings.inputDeviceID ? { exact: this.settings.inputDeviceID } : undefined,
        echoCancellation: this.settings.echoCancellation,
        noiseSuppression: this.settings.noiseSuppression,
        autoGainControl: this.settings.autoGainControl,
        channelCount: 1,
      },
      video: false,
    });
    this.audioContext = new AudioContext({ sampleRate: targetSampleRate });
    this.playback = await VoicePlaybackEngine.create(this.audioContext, this.settings.outputVolume);
    this.encoder = await VoiceEncoder.create((frame) => this.sendEncodedFrame(frame));
    this.decoder = await VoiceDecoder.create((frame) => this.playPCMFrame(frame));
    this.source = this.audioContext.createMediaStreamSource(this.stream);
    this.noiseSuppressor = await createRnnoiseNode(this.audioContext);
    this.processor = this.audioContext.createScriptProcessor(1024, 1, 1);
    this.monitorGain = this.audioContext.createGain();
    this.monitorGain.gain.value = 0;
    this.processor.onaudioprocess = (event) => this.captureAudio(event.inputBuffer.getChannelData(0));
    if (this.noiseSuppressor) {
      this.source.connect(this.noiseSuppressor);
      this.noiseSuppressor.connect(this.processor);
    } else {
      this.source.connect(this.processor);
    }
    this.processor.connect(this.monitorGain);
    this.monitorGain.connect(this.audioContext.destination);
    this.started = true;
  }

  setMicEnabled(enabled: boolean) {
    this.micEnabled = enabled;
    if (!enabled) {
      this.micBuffer = new Float32Array(0);
    }
  }

  playFrame(frame: IncomingVoiceFrame) {
    if (!this.audioContext || frame.roomID !== this.room.id || frame.timestampMS <= 0) {
      return;
    }
    const bytes = payloadBytes(frame.payload);
    if (bytes.length === 0) {
      return;
    }
    if (frame.codec === "opus") {
      this.decoder?.decodeOpus(frame.fromUserID, frame.timestampMS, bytes);
      return;
    }
    if (frame.codec === "pcm16") {
      this.playPCMFrame({
        fromUserID: frame.fromUserID,
        timestampMS: frame.timestampMS,
        sampleRate: frame.sampleRate || targetSampleRate,
        samples: pcm16ToFloat(bytes),
      });
    }
  }

  async close() {
    this.micEnabled = false;
    this.started = false;
    this.processor?.disconnect();
    this.noiseSuppressor?.disconnect();
    this.noiseSuppressor?.destroy();
    this.monitorGain?.disconnect();
    this.source?.disconnect();
    this.stream?.getTracks().forEach((track) => track.stop());
    this.encoder?.close();
    this.decoder?.close();
    this.playback?.close();
    await this.audioContext?.close().catch(() => undefined);
    this.processor = undefined;
    this.noiseSuppressor = undefined;
    this.monitorGain = undefined;
    this.source = undefined;
    this.stream = undefined;
    this.audioContext = undefined;
    this.encoder = undefined;
    this.decoder = undefined;
    this.playback = undefined;
    this.micBuffer = new Float32Array(0);
    this.inputProcessor.reset();
    this.callbacks.onInputLevel?.(0);
    this.playbackByUser.forEach((playback) => playback.close());
    this.playbackByUser.clear();
  }

  private captureAudio(input: Float32Array) {
    if (!this.micEnabled || !this.encoder || !this.audioContext) {
      return;
    }
    const sampleRate = this.audioContext.sampleRate || targetSampleRate;
    const resampled = resampleMono(input, sampleRate, targetSampleRate);
    this.micBuffer = appendSamples(this.micBuffer, resampled);
    while (this.micBuffer.length >= frameSamples) {
      const frame = this.micBuffer.slice(0, frameSamples);
      this.micBuffer = this.micBuffer.slice(frameSamples);
      const gain = clampNumber(this.settings.micVolume, 0.05, 2, 1);
      const processed = this.inputProcessor.process(frame, gain);
      this.callbacks.onInputLevel?.(voiceLevel(processed));
      this.encoder.encode(processed, Date.now());
    }
  }

  private sendEncodedFrame(frame: EncodedVoiceFrame) {
    void SendVoiceAudioFrame({
      roomID: this.room.id,
      seq: ++this.seq,
      timestampMS: frame.timestampMS,
      codec: frame.codec,
      sampleRate: frame.sampleRate,
      payload: Array.from(frame.payload),
    }).catch((err) => this.callbacks.onError(errorMessage(err)));
  }

  private playPCMFrame(frame: DecodedVoiceFrame) {
    if (!this.audioContext || frame.samples.length === 0) {
      return;
    }
    if (!this.playback) {
      return;
    }
    let buffer = this.playbackByUser.get(frame.fromUserID);
    if (!buffer) {
      buffer = new VoicePlayoutBuffer(frame.fromUserID, (ready) => this.playback?.pushFrame(ready));
      this.playbackByUser.set(frame.fromUserID, buffer);
    }
    buffer.push(frame);
  }
}

async function createRnnoiseNode(context: AudioContext) {
  if (!context.audioWorklet) {
    return undefined;
  }
  try {
    const wasmBinary = await loadRnnoise({ url: rnnoiseWasmPath, simdUrl: rnnoiseSIMDWasmPath });
    await context.audioWorklet.addModule(rnnoiseWorkletPath);
    return new RnnoiseWorkletNode(context, { wasmBinary, maxChannels: 1 });
  } catch {
    return undefined;
  }
}

export async function listVoiceDevices() {
  if (!navigator.mediaDevices?.enumerateDevices) {
    return { inputs: [], outputs: [] };
  }
  const devices = await navigator.mediaDevices.enumerateDevices();
  return {
    inputs: devices.filter((item) => item.kind === "audioinput").map(viewDevice),
    outputs: devices.filter((item) => item.kind === "audiooutput").map(viewDevice),
  };
}

function viewDevice(device: MediaDeviceInfo) {
  return {
    deviceID: device.deviceId,
    label: device.label || "默认设备",
  };
}

type DecodedVoiceFrame = {
  fromUserID: number;
  timestampMS: number;
  sampleRate: number;
  samples: Float32Array<ArrayBuffer>;
};

class VoiceEncoder {
  private encoder?: AudioEncoder;
  private readonly output: (frame: EncodedVoiceFrame) => void;
  private readonly codec: VoiceCodec;

  private constructor(codec: VoiceCodec, output: (frame: EncodedVoiceFrame) => void, encoder?: AudioEncoder) {
    this.codec = codec;
    this.output = output;
    this.encoder = encoder;
  }

  static async create(output: (frame: EncodedVoiceFrame) => void) {
    const config: AudioEncoderConfig = {
      codec: "opus",
      sampleRate: targetSampleRate,
      numberOfChannels: 1,
      bitrate: opusBitrate,
      bitrateMode: "constant",
    };
    if (typeof AudioEncoder !== "undefined" && typeof AudioData !== "undefined") {
      const support = await AudioEncoder.isConfigSupported(config).catch(() => ({ supported: false }));
      if (support.supported) {
        const encoder = new AudioEncoder({
          output: (chunk) => {
            const payload = new Uint8Array(chunk.byteLength);
            chunk.copyTo(payload);
            output({
              codec: "opus",
              sampleRate: targetSampleRate,
              timestampMS: Math.round(chunk.timestamp / 1000),
              payload,
            });
          },
          error: () => undefined,
        });
        encoder.configure(config);
        return new VoiceEncoder("opus", output, encoder);
      }
    }
    return new VoiceEncoder("pcm16", output);
  }

  encode(samples: Float32Array, timestampMS: number) {
    if (this.encoder && this.codec === "opus") {
      if (this.encoder.encodeQueueSize > 3) {
        return;
      }
      const ownedSamples = new Float32Array(samples);
      const data = new AudioData({
        format: "f32",
        sampleRate: targetSampleRate,
        numberOfChannels: 1,
        numberOfFrames: ownedSamples.length,
        timestamp: timestampMS * 1000,
        data: ownedSamples.buffer,
      });
      this.encoder.encode(data);
      data.close();
      return;
    }
    const pcmSamples = resampleMono(samples, targetSampleRate, pcmFallbackSampleRate);
    const pcm = floatToPCM16(pcmSamples);
    this.output({
      codec: "pcm16",
      sampleRate: pcmFallbackSampleRate,
      timestampMS,
      payload: new Uint8Array(pcm.buffer),
    });
  }

  close() {
    this.encoder?.close();
    this.encoder = undefined;
  }
}

class VoiceDecoder {
  private decoder?: AudioDecoder;
  private readonly output: (frame: DecodedVoiceFrame) => void;
  private pendingByTimestamp = new Map<number, number[]>();

  private constructor(output: (frame: DecodedVoiceFrame) => void, decoder?: AudioDecoder) {
    this.output = output;
    this.decoder = decoder;
  }

  static async create(output: (frame: DecodedVoiceFrame) => void) {
    const config: AudioDecoderConfig = {
      codec: "opus",
      sampleRate: targetSampleRate,
      numberOfChannels: 1,
    };
    if (typeof AudioDecoder !== "undefined" && typeof EncodedAudioChunk !== "undefined") {
      const support = await AudioDecoder.isConfigSupported(config).catch(() => ({ supported: false }));
      if (support.supported) {
        const decoderOwner = new VoiceDecoder(output);
        const decoder = new AudioDecoder({
          output: (audio) => decoderOwner.handleDecodedAudio(audio),
          error: () => undefined,
        });
        decoderOwner.decoder = decoder;
        decoder.configure(config);
        return decoderOwner;
      }
    }
    return new VoiceDecoder(output);
  }

  decodeOpus(fromUserID: number, timestampMS: number, payload: Uint8Array) {
    if (!this.decoder || this.decoder.state !== "configured") {
      return;
    }
    if (this.decoder.decodeQueueSize > 6) {
      return;
    }
    const timestamp = timestampMS * 1000;
    pushPendingUser(this.pendingByTimestamp, timestamp, fromUserID);
    this.decoder.decode(
      new EncodedAudioChunk({
        type: "key",
        timestamp,
        duration: frameDurationMS * 1000,
        data: payload,
      }),
    );
  }

  close() {
    this.decoder?.close();
    this.decoder = undefined;
    this.pendingByTimestamp.clear();
  }

  private handleDecodedAudio(audio: AudioData) {
    const fromUserID = thisFromTimestamp(audio.timestamp, this.pendingByTimestamp);
    if (fromUserID <= 0) {
      audio.close();
      return;
    }
    const samples = audioDataToFloat32(audio);
    this.output({ fromUserID, timestampMS: Math.round(audio.timestamp / 1000), sampleRate: audio.sampleRate || targetSampleRate, samples });
    audio.close();
  }
}

class VoicePlayoutBuffer {
  private readonly fromUserID: number;
  private readonly output: (frame: DecodedVoiceFrame) => void;
  private frames: DecodedVoiceFrame[] = [];
  private timer: number | null = null;
  private lastTimestampMS = 0;

  constructor(fromUserID: number, output: (frame: DecodedVoiceFrame) => void) {
    this.fromUserID = fromUserID;
    this.output = output;
  }

  push(frame: DecodedVoiceFrame) {
    if (frame.timestampMS <= this.lastTimestampMS) {
      return;
    }
    this.frames.push(frame);
    this.frames.sort((left, right) => left.timestampMS - right.timestampMS);
    if (this.frames.length > maxJitterFrames) {
      const dropped = this.frames.splice(0, this.frames.length - maxJitterFrames);
      const newestDropped = dropped[dropped.length - 1];
      if (newestDropped) {
        this.lastTimestampMS = Math.max(this.lastTimestampMS, newestDropped.timestampMS);
      }
    }
    if (this.timer === null) {
      this.timer = window.setTimeout(() => this.flush(), jitterBufferDelayMS);
    }
  }

  close() {
    if (this.timer !== null) {
      window.clearTimeout(this.timer);
      this.timer = null;
    }
    this.frames = [];
    this.lastTimestampMS = 0;
  }

  private flush() {
    this.timer = null;
    const readyBefore = Date.now() - jitterBufferDelayMS;
    const ready = this.frames.filter((frame) => frame.timestampMS <= readyBefore);
    this.frames = this.frames.filter((frame) => frame.timestampMS > readyBefore);
    for (const frame of ready) {
      this.emit(frame);
    }
    if (this.frames.length > 0) {
      this.timer = window.setTimeout(() => this.flush(), Math.max(8, jitterBufferDelayMS / 3));
    }
  }

  private emit(frame: DecodedVoiceFrame) {
    if (frame.timestampMS <= this.lastTimestampMS) {
      return;
    }
    this.lastTimestampMS = frame.timestampMS;
    this.output({ ...frame, fromUserID: this.fromUserID });
  }
}

class VoicePlaybackEngine {
  private readonly node: AudioWorkletNode;
  private readonly moduleURL: string;

  private constructor(node: AudioWorkletNode, moduleURL: string) {
    this.node = node;
    this.moduleURL = moduleURL;
  }

  static async create(context: AudioContext, volume: number) {
    if (!context.audioWorklet || typeof AudioWorkletNode === "undefined") {
      throw new Error("当前系统不支持低延迟语音播放引擎");
    }
    const moduleURL = URL.createObjectURL(new Blob([voicePlaybackWorkletSource], { type: "text/javascript" }));
    try {
      await context.audioWorklet.addModule(moduleURL);
      const node = new AudioWorkletNode(context, "lwylink-voice-playback", {
        numberOfInputs: 0,
        numberOfOutputs: 1,
        outputChannelCount: [1],
      });
      node.connect(context.destination);
      const engine = new VoicePlaybackEngine(node, moduleURL);
      engine.setVolume(volume);
      return engine;
    } catch (err) {
      URL.revokeObjectURL(moduleURL);
      throw err;
    }
  }

  pushFrame(frame: DecodedVoiceFrame) {
    const samples = copyFloat32(frame.samples);
    this.node.port.postMessage(
      {
        type: "pushFrame",
        userID: frame.fromUserID,
        timestampMS: frame.timestampMS,
        samples,
      },
      [samples.buffer],
    );
  }

  setVolume(volume: number) {
    this.node.port.postMessage({
      type: "setVolume",
      volume: clampNumber(volume, 0.05, 2, 1),
    });
  }

  close() {
    this.node.port.postMessage({ type: "resetAll" });
    this.node.disconnect();
    URL.revokeObjectURL(this.moduleURL);
  }
}

function pushPendingUser(pending: Map<number, number[]>, timestamp: number, userID: number) {
  const users = pending.get(timestamp);
  if (users) {
    users.push(userID);
    return;
  }
  pending.set(timestamp, [userID]);
}

function thisFromTimestamp(timestamp: number, pending: Map<number, number[]>) {
  const users = pending.get(timestamp);
  if (!users || users.length === 0) {
    return 0;
  }
  const userID = users.shift() || 0;
  if (users.length === 0) {
    pending.delete(timestamp);
  }
  return userID;
}

function audioDataToFloat32(audio: AudioData) {
  const samples = new Float32Array(audio.numberOfFrames);
  audio.copyTo(samples, { planeIndex: 0, format: "f32" });
  return samples;
}

function appendSamples(current: Float32Array, next: Float32Array) {
  if (current.length === 0) {
    return next.slice();
  }
  const out = new Float32Array(current.length + next.length);
  out.set(current, 0);
  out.set(next, current.length);
  return out;
}

function copyFloat32(input: Float32Array) {
  const out = new Float32Array(input.length);
  out.set(input);
  return out;
}

function resampleMono(input: Float32Array, sourceRate: number, targetRate: number) {
  if (!Number.isFinite(sourceRate) || sourceRate <= 0 || Math.abs(sourceRate - targetRate) < 1) {
    return input;
  }
  const outputLength = Math.max(1, Math.round((input.length * targetRate) / sourceRate));
  const output = new Float32Array(outputLength);
  const ratio = sourceRate / targetRate;
  for (let i = 0; i < output.length; i++) {
    const pos = i * ratio;
    const left = Math.floor(pos);
    const right = Math.min(input.length - 1, left + 1);
    const frac = pos - left;
    output[i] = input[left] * (1 - frac) + input[right] * frac;
  }
  return output;
}

function floatToPCM16(input: Float32Array) {
  const pcm = new Int16Array(input.length);
  for (let i = 0; i < input.length; i++) {
    const sample = Math.max(-1, Math.min(1, input[i]));
    pcm[i] = sample < 0 ? sample * 32768 : sample * 32767;
  }
  return pcm;
}

function pcm16ToFloat(bytes: Uint8Array) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const samples = new Float32Array(Math.floor(bytes.byteLength / 2));
  for (let i = 0; i < samples.length; i++) {
    samples[i] = view.getInt16(i * 2, true) / 32768;
  }
  return samples;
}

function rms(input: Float32Array) {
  let sum = 0;
  for (let i = 0; i < input.length; i++) {
    sum += input[i] * input[i];
  }
  return Math.sqrt(sum / Math.max(1, input.length));
}

function voiceLevel(input: Float32Array) {
  return clampNumber(rms(input) / 0.12, 0, 1, 0);
}

class VoiceInputProcessor {
  private automaticGain = 1;
  private lastSmoothedSample = 0;
  private hangoverFrames = 0;

  process(input: Float32Array, userGain: number) {
    const level = rms(input);
    if (level >= speechHoldRMS) {
      this.hangoverFrames = hangoverFrameCount;
    } else if (this.hangoverFrames > 0) {
      this.hangoverFrames--;
    }

    const holdingVoiceTail = this.hangoverFrames > 0 && level >= speechFloorRMS;
    const wantedGain = automaticGainForLevel(level, holdingVoiceTail ? hangoverGainMax : autoGainMax);
    const release = holdingVoiceTail ? hangoverGainRelease : gainRelease;
    const rate = wantedGain > this.automaticGain ? gainAttack : release;
    this.automaticGain += (wantedGain - this.automaticGain) * rate;
    const limited = softLimitSamples(scaleSamples(input, userGain * this.automaticGain));
    return this.smooth(limited);
  }

  reset() {
    this.automaticGain = 1;
    this.lastSmoothedSample = 0;
    this.hangoverFrames = 0;
  }

  private smooth(input: Float32Array) {
    if (input.length === 0) {
      return input;
    }
    const out = new Float32Array(input.length);
    let previous = this.lastSmoothedSample;
    for (let i = 0; i < input.length; i++) {
      const sample = input[i] * (1 - smoothingBlend) + previous * smoothingBlend;
      out[i] = sample;
      previous = sample;
    }
    this.lastSmoothedSample = previous;
    return out;
  }
}

function automaticGainForLevel(level: number, maxGain: number) {
  return level > 0.0001 ? Math.min(maxGain, Math.max(1, targetRMS / level)) : 1;
}

function scaleSamples(input: Float32Array, gain: number) {
  const out = new Float32Array(input.length);
  for (let i = 0; i < input.length; i++) {
    out[i] = input[i] * gain;
  }
  return out;
}

function softLimitSamples(input: Float32Array) {
  const out = new Float32Array(input.length);
  const norm = Math.tanh(softLimiterDrive);
  for (let i = 0; i < input.length; i++) {
    out[i] = Math.tanh(input[i] * softLimiterDrive) / norm;
  }
  return out;
}

function clampNumber(value: number, min: number, max: number, fallback: number) {
  if (!Number.isFinite(value)) {
    return fallback;
  }
  return Math.max(min, Math.min(max, value));
}

function payloadBytes(payload: number[] | string) {
  if (Array.isArray(payload)) {
    return new Uint8Array(payload);
  }
  try {
    const raw = atob(payload);
    const bytes = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) {
      bytes[i] = raw.charCodeAt(i);
    }
    return bytes;
  } catch {
    return new Uint8Array();
  }
}

function errorMessage(err: unknown) {
  if (err instanceof Error) {
    return err.message;
  }
  if (typeof err === "string") {
    return err;
  }
  return "语音连接失败";
}
