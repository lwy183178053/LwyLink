import { SendVoiceAudioFrame } from "../wailsjs/go/main/App";
import type { Room, VoiceState } from "./types";
import type { VoiceSettings } from "./types";

type VoiceSessionCallbacks = {
  onError: (message: string) => void;
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
const opusBitrate = 24000;
const playoutDelaySeconds = 0.08;
const maxBufferedSeconds = 0.45;
const maxIncomingAgeMS = 2000;

export class VoiceSession {
  private room: Room;
  private callbacks: VoiceSessionCallbacks;
  private settings: VoiceSettings;
  private audioContext?: AudioContext;
  private stream?: MediaStream;
  private source?: MediaStreamAudioSourceNode;
  private processor?: ScriptProcessorNode;
  private monitorGain?: GainNode;
  private encoder?: VoiceEncoder;
  private decoder?: VoiceDecoder;
  private micBuffer = new Float32Array(0);
  private micEnabled = false;
  private seq = 0;
  private started = false;
  private playbackAtByUser = new Map<number, number>();

  constructor(room: Room, _userID: number, settings: VoiceSettings, callbacks: VoiceSessionCallbacks) {
    this.room = room;
    this.settings = settings;
    this.callbacks = callbacks;
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
    this.encoder = await VoiceEncoder.create((frame) => this.sendEncodedFrame(frame));
    this.decoder = await VoiceDecoder.create((frame) => this.playPCMFrame(frame));
    this.source = this.audioContext.createMediaStreamSource(this.stream);
    this.processor = this.audioContext.createScriptProcessor(1024, 1, 1);
    this.monitorGain = this.audioContext.createGain();
    this.monitorGain.gain.value = 0;
    this.processor.onaudioprocess = (event) => this.captureAudio(event.inputBuffer.getChannelData(0));
    this.source.connect(this.processor);
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

  async applyState(_state: VoiceState) {
    return;
  }

  playFrame(frame: IncomingVoiceFrame) {
    if (!this.audioContext || frame.roomID !== this.room.id || Math.abs(Date.now() - frame.timestampMS) > maxIncomingAgeMS) {
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
        sampleRate: frame.sampleRate || targetSampleRate,
        samples: pcm16ToFloat(bytes),
      });
    }
  }

  async close() {
    this.micEnabled = false;
    this.started = false;
    this.processor?.disconnect();
    this.monitorGain?.disconnect();
    this.source?.disconnect();
    this.stream?.getTracks().forEach((track) => track.stop());
    this.encoder?.close();
    this.decoder?.close();
    await this.audioContext?.close().catch(() => undefined);
    this.processor = undefined;
    this.monitorGain = undefined;
    this.source = undefined;
    this.stream = undefined;
    this.audioContext = undefined;
    this.encoder = undefined;
    this.decoder = undefined;
    this.micBuffer = new Float32Array(0);
    this.playbackAtByUser.clear();
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
      const amplified = gain === 1 ? frame : scaleSamples(frame, gain);
      if (rms(amplified) < clampNumber(this.settings.silenceThreshold, 0, 0.2, 0.012)) {
        continue;
      }
      this.encoder.encode(amplified, Date.now());
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
    const samples = new Float32Array(frame.samples);
    const buffer = this.audioContext.createBuffer(1, samples.length, frame.sampleRate || targetSampleRate);
    buffer.copyToChannel(samples, 0);
    const source = this.audioContext.createBufferSource();
    const gain = this.audioContext.createGain();
    gain.gain.value = clampNumber(this.settings.outputVolume, 0.05, 2, 1);
    source.buffer = buffer;
    source.connect(gain);
    gain.connect(this.audioContext.destination);
    const now = this.audioContext.currentTime;
    let startAt = this.playbackAtByUser.get(frame.fromUserID) || now + playoutDelaySeconds;
    if (startAt < now || startAt - now > maxBufferedSeconds) {
      startAt = now + playoutDelaySeconds;
    }
    source.start(startAt);
    this.playbackAtByUser.set(frame.fromUserID, startAt + buffer.duration);
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

export async function runVoiceLocalTest(settings: VoiceSettings) {
  const stream = await navigator.mediaDevices.getUserMedia({
    audio: {
      deviceId: settings.inputDeviceID ? { exact: settings.inputDeviceID } : undefined,
      echoCancellation: settings.echoCancellation,
      noiseSuppression: settings.noiseSuppression,
      autoGainControl: settings.autoGainControl,
      channelCount: 1,
    },
    video: false,
  });
  try {
    const recorder = new MediaRecorder(stream);
    const chunks: Blob[] = [];
    recorder.ondataavailable = (event) => {
      if (event.data.size > 0) {
        chunks.push(event.data);
      }
    };
    recorder.start();
    await delay(1400);
    recorder.stop();
    await new Promise<void>((resolve) => {
      recorder.onstop = () => resolve();
    });
    const audio = new Audio(URL.createObjectURL(new Blob(chunks, { type: recorder.mimeType || "audio/webm" })));
    audio.volume = clampNumber(settings.outputVolume, 0.05, 1, 1);
    if (settings.outputDeviceID && "setSinkId" in audio) {
      await (audio as HTMLAudioElement & { setSinkId: (sinkId: string) => Promise<void> }).setSinkId(settings.outputDeviceID).catch(() => undefined);
    }
    await audio.play();
  } finally {
    stream.getTracks().forEach((track) => track.stop());
  }
}

function viewDevice(device: MediaDeviceInfo) {
  return {
    deviceID: device.deviceId,
    label: device.label || "默认设备",
  };
}

function delay(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

type DecodedVoiceFrame = {
  fromUserID: number;
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
    this.output({ fromUserID, sampleRate: audio.sampleRate || targetSampleRate, samples });
    audio.close();
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

function scaleSamples(input: Float32Array, gain: number) {
  const out = new Float32Array(input.length);
  for (let i = 0; i < input.length; i++) {
    out[i] = Math.max(-1, Math.min(1, input[i] * gain));
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
