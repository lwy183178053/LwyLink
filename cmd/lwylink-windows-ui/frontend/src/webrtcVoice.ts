import { SendVoiceWebRTCSignal } from "../wailsjs/go/main/App";
import type { Room, VoiceSettings } from "./types";
import { buildDataChatPayload, chatSignalType, decodeDataChatMessage, type DataChatSender } from "./webrtcChatLogic";
import { hasRelayICEServer, nextVoiceDirectRetryState, nextVoiceRelayFallbackState } from "./webrtcPathPolicy";

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
  localCandidateType: string;
  remoteCandidateType: string;
  connectionState: RTCPeerConnectionState;
  iceConnectionState: RTCIceConnectionState;
  roundTripTimeMS: number;
  jitterMS: number;
  packetsLost: number;
  bytesSent: number;
  bytesReceived: number;
};

export type WebRTCDataChatMessage = {
  id: string;
  roomID: number;
  userID: number;
  username: string;
  displayName: string;
  message: string;
  createdAt: string;
};

type WebRTCVoiceCallbacks = {
  onError: (message: string) => void;
  onStats?: (stats: WebRTCVoiceStats[]) => void;
  onInputLevel?: (level: number) => void;
  onRelayTraffic?: (bytes: number) => void;
};

type VoiceMemberRef = {
  userID: number;
};

type ForcedVoiceRelay = {
  directRTTAtFallbackMS: number;
  relayWorseSamples: number;
};

const defaultICEServers: RTCIceServer[] = [{ urls: "stun:stun.lwylink.xyz:3478" }];
const reconnectDelayMS = 2500;
const disconnectGraceMS = 5000;

export class WebRTCVoiceSession {
  private readonly room: Room;
  private readonly selfUserID: number;
  private settings: VoiceSettings;
  private readonly iceServers: RTCIceServer[];
  private readonly relayAvailable: boolean;
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
  private lastTrafficTotals = new Map<number, number>();
  private forcedRelayPeers = new Map<number, ForcedVoiceRelay>();
  private highDirectRTTSamples = new Map<number, number>();
  private statsTimer: number | null = null;

  constructor(room: Room, selfUserID: number, settings: VoiceSettings, iceServers: RTCIceServer[], callbacks: WebRTCVoiceCallbacks) {
    this.room = room;
    this.selfUserID = selfUserID;
    this.settings = settings;
    this.iceServers = normalizeICEServers(iceServers);
    this.relayAvailable = hasRelayICEServer(this.iceServers);
    this.callbacks = callbacks;
  }

  async start(members: VoiceMemberRef[]) {
    if (typeof RTCPeerConnection === "undefined") {
      throw new Error("当前系统不支持高清语音");
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
        this.forcedRelayPeers.delete(peerUserID);
        this.highDirectRTTSamples.delete(peerUserID);
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
      this.scheduleReconnect(signal.fromUserID);
      return;
    }
    if (signal.type === "relay-fallback") {
      this.forceRelayForPeer(signal.fromUserID, relayFallbackRTTMS(payload));
      this.closePeer(signal.fromUserID);
      if (this.shouldInitiate(signal.fromUserID)) {
        window.setTimeout(() => {
          if (this.voiceMemberIDs.has(signal.fromUserID) && !this.peers.has(signal.fromUserID)) {
            void this.ensurePeer(signal.fromUserID, true).catch((err) => this.callbacks.onError(errorMessage(err)));
          }
        }, 150);
      }
      return;
    }
    if (signal.type === "direct-retry") {
      this.clearForcedRelayForPeer(signal.fromUserID);
      this.closePeer(signal.fromUserID);
      if (this.shouldInitiate(signal.fromUserID)) {
        window.setTimeout(() => {
          if (this.voiceMemberIDs.has(signal.fromUserID) && !this.peers.has(signal.fromUserID)) {
            void this.ensurePeer(signal.fromUserID, true).catch((err) => this.callbacks.onError(errorMessage(err)));
          }
        }, 150);
      }
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
    this.lastTrafficTotals.clear();
    this.forcedRelayPeers.clear();
    this.highDirectRTTSamples.clear();
    for (const peerUserID of [...this.peers.keys()]) {
      this.closePeer(peerUserID);
    }
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
    const peer = new RTCPeerConnection(this.peerConnectionConfig(peerUserID));
    this.peers.set(peerUserID, peer);
    const sendStream = this.destination?.stream ?? this.stream;
    sendStream?.getTracks().forEach((track) => peer.addTrack(track, sendStream));
    peer.onicecandidate = (event) => {
      if (event.candidate) {
        const candidate = event.candidate.toJSON();
        void this.sendSignal(peerUserID, "ice", candidate).catch((err) => this.callbacks.onError(errorMessage(err)));
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
    this.lastTrafficTotals.delete(peerUserID);
    this.highDirectRTTSamples.delete(peerUserID);
    this.clearDisconnectTimer(peerUserID);
    peer.close();
    removeRemoteAudio(peerUserID);
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
    const out: WebRTCVoiceStats[] = [];
    let relayTrafficBytes = 0;
    for (const [peerUserID, peer] of this.peers) {
      const report = await peer.getStats();
      const stats = viewPeerStats(peerUserID, peer, report);
      if (stats) {
        out.push(stats);
        relayTrafficBytes += this.relayTrafficDelta(stats);
        this.evaluatePathQuality(stats);
      }
    }
    this.callbacks.onStats?.(out);
    if (relayTrafficBytes > 0) {
      this.callbacks.onRelayTraffic?.(relayTrafficBytes);
    }
  }

  private relayTrafficDelta(stats: WebRTCVoiceStats) {
    const total = Math.max(0, Math.floor(stats.bytesSent + stats.bytesReceived));
    const previous = this.lastTrafficTotals.get(stats.peerUserID);
    this.lastTrafficTotals.set(stats.peerUserID, total);
    if (previous === undefined || stats.localCandidateType !== "relay" || total <= previous) {
      return 0;
    }
    return total - previous;
  }

  private peerConnectionConfig(peerUserID: number): RTCConfiguration {
    return {
      iceServers: this.iceServers,
      iceTransportPolicy: this.forcedRelayPeers.has(peerUserID) && this.relayAvailable ? "relay" : "all",
    };
  }

  private forceRelayForPeer(peerUserID: number, directRTTAtFallbackMS: number) {
    if (!this.relayAvailable || peerUserID <= 0 || !this.voiceMemberIDs.has(peerUserID)) {
      return false;
    }
    this.forcedRelayPeers.set(peerUserID, { directRTTAtFallbackMS, relayWorseSamples: 0 });
    this.highDirectRTTSamples.delete(peerUserID);
    return true;
  }

  private clearForcedRelayForPeer(peerUserID: number) {
    this.forcedRelayPeers.delete(peerUserID);
    this.highDirectRTTSamples.delete(peerUserID);
  }

  private evaluatePathQuality(stats: WebRTCVoiceStats) {
    const forcedRelay = this.forcedRelayPeers.get(stats.peerUserID);
    if (forcedRelay) {
      const retryState = nextVoiceDirectRetryState({
        candidateType: stats.candidateType,
        roundTripTimeMS: stats.roundTripTimeMS,
        directRTTAtFallbackMS: forcedRelay.directRTTAtFallbackMS,
        currentSamples: forcedRelay.relayWorseSamples,
        relayForced: true,
      });
      forcedRelay.relayWorseSamples = retryState.samples;
      if (retryState.shouldRetryDirect) {
        this.clearForcedRelayForPeer(stats.peerUserID);
        void this.sendSignal(stats.peerUserID, "direct-retry", {
          reason: "relay-rtt-worse",
          roundTripTimeMS: stats.roundTripTimeMS,
        }).catch(() => undefined);
        this.closePeer(stats.peerUserID);
        if (this.shouldInitiate(stats.peerUserID)) {
          window.setTimeout(() => {
            if (this.voiceMemberIDs.has(stats.peerUserID) && !this.peers.has(stats.peerUserID)) {
              void this.ensurePeer(stats.peerUserID, true).catch((err) => this.callbacks.onError(errorMessage(err)));
            }
          }, 150);
        }
      }
      return;
    }
    const state = nextVoiceRelayFallbackState({
      candidateType: stats.candidateType,
      roundTripTimeMS: stats.roundTripTimeMS,
      currentSamples: this.highDirectRTTSamples.get(stats.peerUserID) ?? 0,
      relayAvailable: this.relayAvailable,
      relayForced: false,
    });
    this.highDirectRTTSamples.set(stats.peerUserID, state.samples);
    if (!state.shouldFallback || !this.forceRelayForPeer(stats.peerUserID, stats.roundTripTimeMS)) {
      return;
    }
    void this.sendSignal(stats.peerUserID, "relay-fallback", {
      reason: "high-direct-rtt",
      roundTripTimeMS: stats.roundTripTimeMS,
    }).catch(() => undefined);
    this.closePeer(stats.peerUserID);
    if (this.shouldInitiate(stats.peerUserID)) {
      window.setTimeout(() => {
        if (this.voiceMemberIDs.has(stats.peerUserID) && !this.peers.has(stats.peerUserID)) {
          void this.ensurePeer(stats.peerUserID, true).catch((err) => this.callbacks.onError(errorMessage(err)));
        }
      }, 150);
    }
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

type WebRTCChatCallbacks = {
  onError: (message: string) => void;
  onDataChat: (message: WebRTCDataChatMessage) => void;
};

export class WebRTCChatSession {
  private readonly room: Room;
  private readonly selfUserID: number;
  private readonly iceServers: RTCIceServer[];
  private readonly callbacks: WebRTCChatCallbacks;
  private peers = new Map<number, RTCPeerConnection>();
  private channels = new Map<number, RTCDataChannel>();
  private pendingICE = new Map<number, RTCIceCandidateInit[]>();
  private reconnectTimers = new Map<number, number>();
  private memberIDs = new Set<number>();
  private closing = false;

  constructor(room: Room, selfUserID: number, iceServers: RTCIceServer[], callbacks: WebRTCChatCallbacks) {
    this.room = room;
    this.selfUserID = selfUserID;
    this.iceServers = normalizeICEServers(iceServers);
    this.callbacks = callbacks;
  }

  get roomID() {
    return this.room.id;
  }

  async start(members: VoiceMemberRef[]) {
    if (typeof RTCPeerConnection === "undefined") {
      return;
    }
    await this.syncMembers(members);
  }

  async syncMembers(members: VoiceMemberRef[]) {
    const nextIDs = new Set(
      members
        .map((member) => member.userID)
        .filter((userID) => userID > 0 && userID !== this.selfUserID),
    );
    this.memberIDs = nextIDs;
    for (const peerUserID of [...this.peers.keys()]) {
      if (!nextIDs.has(peerUserID)) {
        this.closePeer(peerUserID);
      }
    }
    for (const peerUserID of nextIDs) {
      if (!this.peers.has(peerUserID) && this.selfUserID < peerUserID) {
        await this.ensurePeer(peerUserID, true);
      }
    }
  }

  sendChat(message: string, sender: DataChatSender = {}) {
    if (this.memberIDs.size === 0 || this.channels.size < this.memberIDs.size) {
      return undefined;
    }
    for (const peerUserID of this.memberIDs) {
      if (this.channels.get(peerUserID)?.readyState !== "open") {
        return undefined;
      }
    }
    const payload = buildDataChatPayload({
      roomID: this.room.id,
      selfUserID: this.selfUserID,
      message,
      sender,
    });
    if (!payload) {
      return undefined;
    }
    const encoded = JSON.stringify(payload);
    for (const peerUserID of this.memberIDs) {
      try {
        this.channels.get(peerUserID)?.send(encoded);
      } catch (err) {
        this.callbacks.onError(errorMessage(err));
        return undefined;
      }
    }
    return payload;
  }

  async handleSignal(signal: WebRTCVoiceSignal) {
    if (signal.roomID !== this.room.id || signal.toUserID !== this.selfUserID || signal.fromUserID <= 0) {
      return;
    }
    const signalType = chatSignalType(signal.type);
    if (signalType === "") {
      return;
    }
    if (signalType === "hangup") {
      this.closePeer(signal.fromUserID);
      this.scheduleReconnect(signal.fromUserID);
      return;
    }
    const payload = decodeSignalPayload(signal.payload);
    const peer = await this.ensurePeer(signal.fromUserID, false);
    if (signalType === "offer") {
      await peer.setRemoteDescription(payload as RTCSessionDescriptionInit);
      await this.flushPendingICE(signal.fromUserID, peer);
      const answer = await peer.createAnswer();
      await peer.setLocalDescription(answer);
      await this.sendSignal(signal.fromUserID, "answer", answer);
      return;
    }
    if (signalType === "answer") {
      await peer.setRemoteDescription(payload as RTCSessionDescriptionInit);
      await this.flushPendingICE(signal.fromUserID, peer);
      return;
    }
    if (signalType === "ice" && payload) {
      await this.addICECandidate(signal.fromUserID, peer, payload as RTCIceCandidateInit);
    }
  }

  async close() {
    this.closing = true;
    await Promise.all([...this.peers.keys()].map((peerUserID) => this.sendSignal(peerUserID, "hangup", {}).catch(() => undefined)));
    for (const peerUserID of [...this.peers.keys()]) {
      this.closePeer(peerUserID);
    }
    this.reconnectTimers.forEach((timer) => window.clearTimeout(timer));
    this.reconnectTimers.clear();
    this.pendingICE.clear();
    this.memberIDs.clear();
  }

  private async ensurePeer(peerUserID: number, initiator: boolean) {
    const existing = this.peers.get(peerUserID);
    if (existing) {
      return existing;
    }
    const peer = new RTCPeerConnection({ iceServers: this.iceServers });
    this.peers.set(peerUserID, peer);
    peer.ondatachannel = (event) => this.attachChannel(peerUserID, event.channel);
    peer.onicecandidate = (event) => {
      if (event.candidate) {
        const candidate = event.candidate.toJSON();
        void this.sendSignal(peerUserID, "ice", candidate).catch((err) => this.callbacks.onError(errorMessage(err)));
      }
    };
    peer.onconnectionstatechange = () => {
      if (peer.connectionState === "failed" || peer.connectionState === "closed") {
        this.closePeer(peerUserID);
        this.scheduleReconnect(peerUserID);
      }
    };
    if (initiator) {
      this.attachChannel(peerUserID, peer.createDataChannel("lwylink-chat", { ordered: true }));
      const offer = await peer.createOffer();
      await peer.setLocalDescription(offer);
      await this.sendSignal(peerUserID, "offer", offer);
    }
    return peer;
  }

  private attachChannel(peerUserID: number, channel: RTCDataChannel) {
    if (channel.label !== "lwylink-chat") {
      return;
    }
    this.channels.get(peerUserID)?.close();
    this.channels.set(peerUserID, channel);
    channel.onmessage = (event) => {
      const message = decodeDataChatMessage(event.data, this.room.id, peerUserID);
      if (message) {
        this.callbacks.onDataChat(message);
      }
    };
    channel.onclose = () => {
      if (this.channels.get(peerUserID) === channel) {
        this.channels.delete(peerUserID);
      }
    };
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

  private async sendSignal(toUserID: number, type: string, payload: unknown) {
    await SendVoiceWebRTCSignal({
      roomID: this.room.id,
      toUserID,
      type: `chat-${type}`,
      payload: encodeSignalPayload(payload),
    });
  }

  private scheduleReconnect(peerUserID: number) {
    if (this.closing || !this.memberIDs.has(peerUserID) || this.selfUserID >= peerUserID || this.reconnectTimers.has(peerUserID)) {
      return;
    }
    const timer = window.setTimeout(() => {
      this.reconnectTimers.delete(peerUserID);
      if (this.memberIDs.has(peerUserID) && !this.peers.has(peerUserID)) {
        void this.ensurePeer(peerUserID, true).catch((err) => this.callbacks.onError(errorMessage(err)));
      }
    }, reconnectDelayMS);
    this.reconnectTimers.set(peerUserID, timer);
  }

  private closePeer(peerUserID: number) {
    if (!this.memberIDs.has(peerUserID)) {
      const timer = this.reconnectTimers.get(peerUserID);
      if (timer) {
        window.clearTimeout(timer);
        this.reconnectTimers.delete(peerUserID);
      }
    }
    this.channels.get(peerUserID)?.close();
    this.channels.delete(peerUserID);
    this.pendingICE.delete(peerUserID);
    this.peers.get(peerUserID)?.close();
    this.peers.delete(peerUserID);
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
  let selectedPair: VoiceRTCStatsItem | undefined;
  let inbound: VoiceRTCStatsItem | undefined;
  let outbound: VoiceRTCStatsItem | undefined;
  let transport: VoiceRTCStatsItem | undefined;
  let localCandidateType = "";
  let remoteCandidateType = "";
  report.forEach((raw) => {
    const item = raw as VoiceRTCStatsItem;
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
    selectedPair = report.get(transport.selectedCandidatePairId) as VoiceRTCStatsItem | undefined;
  }
  if (selectedPair?.localCandidateId) {
    const local = report.get(selectedPair.localCandidateId) as VoiceRTCStatsItem | undefined;
    localCandidateType = local?.candidateType || "";
  }
  if (selectedPair?.remoteCandidateId) {
    const remote = report.get(selectedPair.remoteCandidateId) as VoiceRTCStatsItem | undefined;
    remoteCandidateType = remote?.candidateType || "";
  }
  if (!selectedPair && !inbound && !outbound) {
    return undefined;
  }
  const candidateType = localCandidateType === "relay" || remoteCandidateType === "relay" ? "relay" : localCandidateType || remoteCandidateType;
  return {
    peerUserID,
    candidateType,
    localCandidateType,
    remoteCandidateType,
    connectionState: peer.connectionState,
    iceConnectionState: peer.iceConnectionState,
    roundTripTimeMS: Math.round(Number(selectedPair?.currentRoundTripTime || 0) * 1000),
    jitterMS: Math.round(Number(inbound?.jitter || 0) * 1000),
    packetsLost: Number(inbound?.packetsLost || 0),
    bytesSent: Number(outbound?.bytesSent || 0),
    bytesReceived: Number(inbound?.bytesReceived || 0),
  };
}

type VoiceRTCStatsItem = RTCStats & {
  kind?: string;
  state?: string;
  selected?: boolean;
  selectedCandidatePairId?: string;
  localCandidateId?: string;
  remoteCandidateId?: string;
  candidateType?: string;
  currentRoundTripTime?: number;
  jitter?: number;
  packetsLost?: number;
  bytesSent?: number;
  bytesReceived?: number;
};

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

function removeRemoteAudio(peerUserID: number) {
  const audio = document.getElementById(remoteAudioID(peerUserID)) as HTMLAudioElement | null;
  if (!audio) {
    return;
  }
  audio.pause();
  audio.srcObject = null;
  audio.remove();
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

function relayFallbackRTTMS(payload: unknown) {
  if (!payload || typeof payload !== "object") {
    return 0;
  }
  const value = (payload as { roundTripTimeMS?: unknown }).roundTripTimeMS;
  return typeof value === "number" && Number.isFinite(value) ? Math.max(0, Math.round(value)) : 0;
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
    .map((server) => {
      return {
        urls: normalizeICEURLs(server.urls),
        username: typeof server.username === "string" ? server.username.trim() : server.username,
        credential: typeof server.credential === "string" ? server.credential.trim() : server.credential,
      };
    })
    .filter((server) => {
      if (Array.isArray(server.urls)) {
        return server.urls.length > 0;
      }
      return server.urls !== "";
    });
  return out.length > 0 ? out : defaultICEServers;
}

function normalizeICEURLs(urls: RTCIceServer["urls"]) {
  if (Array.isArray(urls)) {
    return urls.map((url) => (typeof url === "string" ? url.trim() : "")).filter(Boolean);
  }
  return typeof urls === "string" ? urls.trim() : "";
}
