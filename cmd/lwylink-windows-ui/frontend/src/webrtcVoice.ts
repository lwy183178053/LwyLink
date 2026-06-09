import { SendVoiceWebRTCSignal } from "../wailsjs/go/main/App";
import type { Room, VoiceSettings } from "./types";

export type WebRTCVoiceSignal = {
  roomID: number;
  fromUserID: number;
  toUserID: number;
  type: "offer" | "answer" | "ice" | "hangup" | string;
  payload: unknown;
};

export type WebRTCVoiceStats = {
  peerUserID: number;
  candidateType: string;
  connectionState: RTCPeerConnectionState;
  iceConnectionState: RTCIceConnectionState;
  roundTripTimeMS: number;
  jitterMS: number;
  packetsLost: number;
  bytesSent: number;
  bytesReceived: number;
};

type WebRTCVoiceCallbacks = {
  onError: (message: string) => void;
  onStats?: (stats: WebRTCVoiceStats[]) => void;
  onInputLevel?: (level: number) => void;
};

type VoiceMemberRef = {
  userID: number;
};

const defaultICEServers: RTCIceServer[] = [{ urls: "stun:stun.l.google.com:19302" }];
const reconnectDelayMS = 2500;
const disconnectGraceMS = 5000;

export class WebRTCVoiceExperiment {
  private readonly room: Room;
  private readonly selfUserID: number;
  private settings: VoiceSettings;
  private readonly iceServers: RTCIceServer[];
  private readonly callbacks: WebRTCVoiceCallbacks;
  private stream?: MediaStream;
  private audioContext?: AudioContext;
  private source?: MediaStreamAudioSourceNode;
  private micGain?: GainNode;
  private destination?: MediaStreamAudioDestinationNode;
  private levelProcessor?: ScriptProcessorNode;
  private levelMonitorGain?: GainNode;
  private outputGainNodes = new Map<number, GainNode>();
  private peers = new Map<number, RTCPeerConnection>();
  private pendingICE = new Map<number, RTCIceCandidateInit[]>();
  private disconnectTimers = new Map<number, number>();
  private reconnectTimers = new Map<number, number>();
  private voiceMemberIDs = new Set<number>();
  private statsTimer: number | null = null;

  constructor(room: Room, selfUserID: number, settings: VoiceSettings, iceServers: RTCIceServer[], callbacks: WebRTCVoiceCallbacks) {
    this.room = room;
    this.selfUserID = selfUserID;
    this.settings = settings;
    this.iceServers = normalizeICEServers(iceServers);
    this.callbacks = callbacks;
  }

  async start(members: VoiceMemberRef[]) {
    if (typeof RTCPeerConnection === "undefined") {
      throw new Error("当前系统不支持高清语音");
    }
    this.stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        deviceId: this.settings.inputDeviceID ? { exact: this.settings.inputDeviceID } : undefined,
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
        channelCount: 1,
      },
      video: false,
    });
    this.audioContext = new AudioContext();
    await this.setOutputDevice(this.settings.outputDeviceID);
    this.source = this.audioContext.createMediaStreamSource(this.stream);
    this.micGain = this.audioContext.createGain();
    this.destination = this.audioContext.createMediaStreamDestination();
    this.levelProcessor = this.audioContext.createScriptProcessor(1024, 1, 1);
    this.levelMonitorGain = this.audioContext.createGain();
    this.micGain.gain.value = clampNumber(this.settings.micVolume, 0.05, 2, 1);
    this.levelMonitorGain.gain.value = 0;
    this.levelProcessor.onaudioprocess = (event) => this.callbacks.onInputLevel?.(voiceLevel(event.inputBuffer.getChannelData(0)));
    this.source.connect(this.micGain);
    this.micGain.connect(this.destination);
    this.micGain.connect(this.levelProcessor);
    this.levelProcessor.connect(this.levelMonitorGain);
    this.levelMonitorGain.connect(this.audioContext.destination);
    this.setMicEnabled(false);
    await this.syncMembers(members);
    this.statsTimer = window.setInterval(() => void this.collectStats(), 1500);
  }

  setMicEnabled(enabled: boolean) {
    this.stream?.getAudioTracks().forEach((track) => {
      track.enabled = enabled;
    });
  }

  updateSettings(settings: VoiceSettings) {
    const outputDeviceChanged = settings.outputDeviceID !== this.settings.outputDeviceID;
    this.settings = settings;
    this.micGain?.gain.setTargetAtTime(clampNumber(settings.micVolume, 0.05, 2, 1), this.audioContext?.currentTime ?? 0, 0.015);
    for (const gain of this.outputGainNodes.values()) {
      gain.gain.setTargetAtTime(clampNumber(settings.outputVolume, 0.05, 2, 1), this.audioContext?.currentTime ?? 0, 0.015);
    }
    if (outputDeviceChanged) {
      void this.setOutputDevice(settings.outputDeviceID);
    }
  }

  async syncMembers(members: VoiceMemberRef[]) {
    const nextIDs = new Set(
      members
        .map((member) => member.userID)
        .filter((userID) => userID > 0 && userID !== this.selfUserID),
    );
    this.voiceMemberIDs = nextIDs;
    for (const peerUserID of [...this.peers.keys()]) {
      if (!nextIDs.has(peerUserID)) {
        this.closePeer(peerUserID);
      }
    }
    for (const peerUserID of nextIDs) {
      if (!this.peers.has(peerUserID) && this.shouldInitiate(peerUserID)) {
        await this.ensurePeer(peerUserID, true);
      }
    }
  }

  async handleSignal(signal: WebRTCVoiceSignal) {
    if (signal.roomID !== this.room.id || signal.toUserID !== this.selfUserID || signal.fromUserID <= 0) {
      return;
    }
    const payload = decodeSignalPayload(signal.payload);
    if (signal.type === "hangup") {
      this.closePeer(signal.fromUserID);
      return;
    }
    const peer = await this.ensurePeer(signal.fromUserID, false);
    if (signal.type === "offer") {
      await peer.setRemoteDescription(payload as RTCSessionDescriptionInit);
      await this.flushPendingICE(signal.fromUserID, peer);
      const answer = await peer.createAnswer();
      await peer.setLocalDescription(answer);
      await this.sendSignal(signal.fromUserID, "answer", answer);
      return;
    }
    if (signal.type === "answer") {
      await peer.setRemoteDescription(payload as RTCSessionDescriptionInit);
      await this.flushPendingICE(signal.fromUserID, peer);
      return;
    }
    if (signal.type === "ice" && payload) {
      await this.addICECandidate(signal.fromUserID, peer, payload as RTCIceCandidateInit);
    }
  }

  async close() {
    if (this.statsTimer !== null) {
      window.clearInterval(this.statsTimer);
      this.statsTimer = null;
    }
    await Promise.all([...this.peers.keys()].map((peerUserID) => this.sendSignal(peerUserID, "hangup", {}).catch(() => undefined)));
    this.reconnectTimers.forEach((timer) => window.clearTimeout(timer));
    this.disconnectTimers.forEach((timer) => window.clearTimeout(timer));
    this.reconnectTimers.clear();
    this.disconnectTimers.clear();
    this.pendingICE.clear();
    this.voiceMemberIDs.clear();
    this.peers.forEach((peer) => peer.close());
    this.peers.clear();
    this.outputGainNodes.clear();
    this.source?.disconnect();
    this.micGain?.disconnect();
    this.destination?.disconnect();
    this.levelProcessor?.disconnect();
    this.levelMonitorGain?.disconnect();
    this.stream?.getTracks().forEach((track) => track.stop());
    await this.audioContext?.close().catch(() => undefined);
    this.source = undefined;
    this.micGain = undefined;
    this.destination = undefined;
    this.levelProcessor = undefined;
    this.levelMonitorGain = undefined;
    this.audioContext = undefined;
    this.stream = undefined;
    this.callbacks.onInputLevel?.(0);
  }

  private async ensurePeer(peerUserID: number, initiator: boolean) {
    const existing = this.peers.get(peerUserID);
    if (existing) {
      return existing;
    }
    const peer = new RTCPeerConnection({ iceServers: this.iceServers });
    this.peers.set(peerUserID, peer);
    const sendStream = this.destination?.stream ?? this.stream;
    sendStream?.getTracks().forEach((track) => peer.addTrack(track, sendStream));
    peer.onicecandidate = (event) => {
      if (event.candidate) {
        void this.sendSignal(peerUserID, "ice", event.candidate.toJSON()).catch((err) => this.callbacks.onError(errorMessage(err)));
      }
    };
    peer.ontrack = (event) => {
      const [stream] = event.streams;
      if (stream) {
        this.playRemoteStream(peerUserID, stream);
      }
    };
    peer.onconnectionstatechange = () => {
      this.handlePeerStateChange(peerUserID, peer);
    };
    peer.oniceconnectionstatechange = () => {
      this.handlePeerStateChange(peerUserID, peer);
    };
    if (initiator) {
      const offer = await peer.createOffer({ offerToReceiveAudio: true });
      await peer.setLocalDescription(offer);
      await this.sendSignal(peerUserID, "offer", offer);
    }
    return peer;
  }

  private async addICECandidate(peerUserID: number, peer: RTCPeerConnection, candidate: RTCIceCandidateInit) {
    if (!peer.remoteDescription) {
      const pending = this.pendingICE.get(peerUserID) ?? [];
      pending.push(candidate);
      this.pendingICE.set(peerUserID, pending.slice(-32));
      return;
    }
    await peer.addIceCandidate(candidate).catch(() => undefined);
  }

  private async flushPendingICE(peerUserID: number, peer: RTCPeerConnection) {
    const pending = this.pendingICE.get(peerUserID);
    if (!pending || !peer.remoteDescription) {
      return;
    }
    this.pendingICE.delete(peerUserID);
    for (const candidate of pending) {
      await peer.addIceCandidate(candidate).catch(() => undefined);
    }
  }

  private shouldInitiate(peerUserID: number) {
    return this.selfUserID < peerUserID;
  }

  private scheduleReconnect(peerUserID: number) {
    if (!this.voiceMemberIDs.has(peerUserID) || !this.shouldInitiate(peerUserID) || this.reconnectTimers.has(peerUserID)) {
      return;
    }
    const timer = window.setTimeout(() => {
      this.reconnectTimers.delete(peerUserID);
      if (this.voiceMemberIDs.has(peerUserID) && !this.peers.has(peerUserID)) {
        void this.ensurePeer(peerUserID, true).catch((err) => this.callbacks.onError(errorMessage(err)));
      }
    }, reconnectDelayMS);
    this.reconnectTimers.set(peerUserID, timer);
  }

  private scheduleDisconnectRecovery(peerUserID: number, peer: RTCPeerConnection) {
    if (this.disconnectTimers.has(peerUserID)) {
      return;
    }
    const timer = window.setTimeout(() => {
      this.disconnectTimers.delete(peerUserID);
      if (this.peers.get(peerUserID) !== peer || !this.voiceMemberIDs.has(peerUserID)) {
        return;
      }
      if (peer.connectionState === "disconnected" || peer.iceConnectionState === "disconnected") {
        this.closePeer(peerUserID);
        this.scheduleReconnect(peerUserID);
      }
    }, disconnectGraceMS);
    this.disconnectTimers.set(peerUserID, timer);
  }

  private clearDisconnectTimer(peerUserID: number) {
    const timer = this.disconnectTimers.get(peerUserID);
    if (!timer) {
      return;
    }
    window.clearTimeout(timer);
    this.disconnectTimers.delete(peerUserID);
  }

  private handlePeerStateChange(peerUserID: number, peer: RTCPeerConnection) {
    if (peer.connectionState === "connected" || peer.iceConnectionState === "connected" || peer.iceConnectionState === "completed") {
      this.clearDisconnectTimer(peerUserID);
    }
    if (peer.connectionState === "disconnected" || peer.iceConnectionState === "disconnected") {
      this.scheduleDisconnectRecovery(peerUserID, peer);
    }
    if (peer.connectionState === "failed" || peer.iceConnectionState === "failed") {
      this.closePeer(peerUserID);
      this.scheduleReconnect(peerUserID);
    }
    if (peer.connectionState === "closed" || peer.iceConnectionState === "closed") {
      this.closePeer(peerUserID);
    }
  }

  private closePeer(peerUserID: number) {
    const peer = this.peers.get(peerUserID);
    if (!peer) {
      return;
    }
    this.peers.delete(peerUserID);
    this.pendingICE.delete(peerUserID);
    this.clearDisconnectTimer(peerUserID);
    peer.close();
    removeRemoteStream(peerUserID);
    const gain = this.outputGainNodes.get(peerUserID);
    gain?.disconnect();
    this.outputGainNodes.delete(peerUserID);
    const timer = this.reconnectTimers.get(peerUserID);
    if (timer && !this.voiceMemberIDs.has(peerUserID)) {
      window.clearTimeout(timer);
      this.reconnectTimers.delete(peerUserID);
    }
  }

  private async sendSignal(toUserID: number, type: string, payload: unknown) {
    await SendVoiceWebRTCSignal({
      roomID: this.room.id,
      toUserID,
      type,
      payload: encodeSignalPayload(payload),
    });
  }

  private async collectStats() {
    if (!this.callbacks.onStats) {
      return;
    }
    const out: WebRTCVoiceStats[] = [];
    for (const [peerUserID, peer] of this.peers) {
      const report = await peer.getStats();
      const stats = viewPeerStats(peerUserID, peer, report);
      if (stats) {
        out.push(stats);
      }
    }
    this.callbacks.onStats(out);
  }

  private playRemoteStream(peerUserID: number, stream: MediaStream) {
    if (!this.audioContext) {
      playRemoteStream(peerUserID, stream);
      return;
    }
    const id = remoteAudioID(peerUserID);
    let audio = document.getElementById(id) as HTMLAudioElement | null;
    if (!audio) {
      audio = document.createElement("audio");
      audio.id = id;
      audio.autoplay = true;
      audio.muted = true;
      audio.dataset.lwylinkWebrtcVoice = "true";
      document.body.appendChild(audio);
    }
    audio.srcObject = stream;
    void audio.play().catch(() => undefined);
    const source = this.audioContext.createMediaStreamSource(stream);
    const gain = this.audioContext.createGain();
    gain.gain.value = clampNumber(this.settings.outputVolume, 0.05, 2, 1);
    source.connect(gain);
    gain.connect(this.audioContext.destination);
    this.outputGainNodes.set(peerUserID, gain);
  }

  private async setOutputDevice(deviceID: string) {
    const context = this.audioContext as (AudioContext & { setSinkId?: (sinkID: string) => Promise<void> }) | undefined;
    if (!context?.setSinkId) {
      return;
    }
    await context.setSinkId(deviceID || "").catch(() => undefined);
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

function viewPeerStats(peerUserID: number, peer: RTCPeerConnection, report: RTCStatsReport): WebRTCVoiceStats | undefined {
  let selectedPair: any;
  let inbound: any;
  let outbound: any;
  let transport: any;
  let candidateType = "";
  report.forEach((item: any) => {
    if (item.type === "candidate-pair" && item.state === "succeeded" && item.selected) {
      selectedPair = item;
    }
    if (item.type === "inbound-rtp" && item.kind === "audio") {
      inbound = item;
    }
    if (item.type === "outbound-rtp" && item.kind === "audio") {
      outbound = item;
    }
    if (item.type === "transport") {
      transport = item;
    }
  });
  if (!selectedPair && transport?.selectedCandidatePairId) {
    selectedPair = report.get(transport.selectedCandidatePairId) as any;
  }
  if (selectedPair?.localCandidateId) {
    const local = report.get(selectedPair.localCandidateId) as any;
    candidateType = local?.candidateType || "";
  }
  if (!selectedPair && !inbound && !outbound) {
    return undefined;
  }
  return {
    peerUserID,
    candidateType,
    connectionState: peer.connectionState,
    iceConnectionState: peer.iceConnectionState,
    roundTripTimeMS: Math.round(Number(selectedPair?.currentRoundTripTime || 0) * 1000),
    jitterMS: Math.round(Number(inbound?.jitter || 0) * 1000),
    packetsLost: Number(inbound?.packetsLost || 0),
    bytesSent: Number(outbound?.bytesSent || 0),
    bytesReceived: Number(inbound?.bytesReceived || 0),
  };
}

function playRemoteStream(peerUserID: number, stream: MediaStream) {
  const id = remoteAudioID(peerUserID);
  let audio = document.getElementById(id) as HTMLAudioElement | null;
  if (!audio) {
    audio = document.createElement("audio");
    audio.id = id;
    audio.autoplay = true;
    audio.dataset.lwylinkWebrtcVoice = "true";
    document.body.appendChild(audio);
  }
  audio.srcObject = stream;
  void audio.play().catch(() => undefined);
}

function removeRemoteStream(peerUserID: number) {
  document.getElementById(remoteAudioID(peerUserID))?.remove();
}

function remoteAudioID(peerUserID: number) {
  return `lwylink-webrtc-voice-${peerUserID}`;
}

function encodeSignalPayload(payload: unknown) {
  return Array.from(new TextEncoder().encode(JSON.stringify(payload ?? {})));
}

function decodeSignalPayload(payload: unknown) {
  if (Array.isArray(payload)) {
    const bytes = new Uint8Array(payload);
    const text = new TextDecoder().decode(bytes);
    return text ? JSON.parse(text) : {};
  }
  return payload;
}

function errorMessage(err: unknown) {
  if (err instanceof Error) {
    return err.message;
  }
  return String(err || "高清语音连接失败");
}

function clampNumber(value: number, min: number, max: number, fallback: number) {
  if (!Number.isFinite(value)) {
    return fallback;
  }
  return Math.max(min, Math.min(max, value));
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

function normalizeICEServers(servers: RTCIceServer[]) {
  const out = servers
    .map((server) => ({
      urls: server.urls,
      username: server.username,
      credential: server.credential,
    }))
    .filter((server) => {
      if (Array.isArray(server.urls)) {
        return server.urls.some((url) => typeof url === "string" && url.trim() !== "");
      }
      return typeof server.urls === "string" && server.urls.trim() !== "";
    });
  return out.length > 0 ? out : defaultICEServers;
}
