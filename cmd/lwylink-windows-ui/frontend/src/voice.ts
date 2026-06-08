import { SendVoiceAudioFrame } from "../wailsjs/go/main/App";
import type { Room, VoiceState } from "./types";

type VoiceSessionCallbacks = {
  onError: (message: string) => void;
};

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

const targetSampleRate = 16000;
const silenceThreshold = 0.012;
const playoutDelaySeconds = 0.08;
const maxBufferedSeconds = 0.45;
const maxIncomingAgeMS = 2000;

export class VoiceSession {
  private room: Room;
  private callbacks: VoiceSessionCallbacks;
  private audioContext?: AudioContext;
  private stream?: MediaStream;
  private source?: MediaStreamAudioSourceNode;
  private processor?: ScriptProcessorNode;
  private monitorGain?: GainNode;
  private micEnabled = false;
  private seq = 0;
  private started = false;
  private playbackAtByUser = new Map<number, number>();

  constructor(room: Room, _userID: number, callbacks: VoiceSessionCallbacks) {
    this.room = room;
    this.callbacks = callbacks;
  }

  async start() {
    if (this.started) {
      return;
    }
    this.stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
        channelCount: 1,
      },
      video: false,
    });
    this.audioContext = new AudioContext({ sampleRate: targetSampleRate });
    this.source = this.audioContext.createMediaStreamSource(this.stream);
    this.processor = this.audioContext.createScriptProcessor(1024, 1, 1);
    this.monitorGain = this.audioContext.createGain();
    this.monitorGain.gain.value = 0;
    this.processor.onaudioprocess = (event) => {
      if (!this.micEnabled) {
        return;
      }
      const input = event.inputBuffer.getChannelData(0);
      if (rms(input) < silenceThreshold) {
        return;
      }
      const sampleRate = this.audioContext?.sampleRate || targetSampleRate;
      const resampled = resampleMono(input, sampleRate, targetSampleRate);
      const pcm = floatToPCM16(resampled);
      void SendVoiceAudioFrame({
        roomID: this.room.id,
        seq: ++this.seq,
        timestampMS: Date.now(),
        sampleRate: targetSampleRate,
        payload: Array.from(new Uint8Array(pcm.buffer)),
      }).catch((err) => this.callbacks.onError(errorMessage(err)));
    };
    this.source.connect(this.processor);
    this.processor.connect(this.monitorGain);
    this.monitorGain.connect(this.audioContext.destination);
    this.started = true;
  }

  setMicEnabled(enabled: boolean) {
    this.micEnabled = enabled;
  }

  async applyState(_state: VoiceState) {
    return;
  }

  playFrame(frame: IncomingVoiceFrame) {
    if (!this.audioContext || frame.roomID !== this.room.id || frame.codec !== "pcm16") {
      return;
    }
    if (Math.abs(Date.now() - frame.timestampMS) > maxIncomingAgeMS) {
      return;
    }
    const bytes = payloadBytes(frame.payload);
    if (bytes.length === 0) {
      return;
    }
    const pcm = new Int16Array(bytes.buffer, bytes.byteOffset, Math.floor(bytes.byteLength / 2));
    const buffer = this.audioContext.createBuffer(1, pcm.length, frame.sampleRate || targetSampleRate);
    const out = buffer.getChannelData(0);
    for (let i = 0; i < pcm.length; i++) {
      out[i] = Math.max(-1, Math.min(1, pcm[i] / 32768));
    }
    const source = this.audioContext.createBufferSource();
    source.buffer = buffer;
    source.connect(this.audioContext.destination);
    const now = this.audioContext.currentTime;
    let startAt = this.playbackAtByUser.get(frame.fromUserID) || now + playoutDelaySeconds;
    if (startAt < now || startAt - now > maxBufferedSeconds) {
      startAt = now + playoutDelaySeconds;
    }
    source.start(startAt);
    this.playbackAtByUser.set(frame.fromUserID, startAt + buffer.duration);
  }

  async close() {
    this.micEnabled = false;
    this.started = false;
    this.processor?.disconnect();
    this.monitorGain?.disconnect();
    this.source?.disconnect();
    this.stream?.getTracks().forEach((track) => track.stop());
    await this.audioContext?.close().catch(() => undefined);
    this.processor = undefined;
    this.monitorGain = undefined;
    this.source = undefined;
    this.stream = undefined;
    this.audioContext = undefined;
    this.playbackAtByUser.clear();
  }
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

function rms(input: Float32Array) {
  let sum = 0;
  for (let i = 0; i < input.length; i++) {
    sum += input[i] * input[i];
  }
  return Math.sqrt(sum / Math.max(1, input.length));
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
