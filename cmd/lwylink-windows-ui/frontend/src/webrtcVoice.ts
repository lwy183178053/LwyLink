import { SendVoiceWebRTCSignal } from "../wailsjs/go/main/App";
import type { Room, RoomMember, VoiceSettings } from "./types";

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
  roundTripTimeMS: number;
  jitterMS: number;
  packetsLost: number;
  bytesSent: number;
  bytesReceived: number;
};

type WebRTCVoiceCallbacks = {
  onError: (message: string) => void;
  onStats?: (stats: WebRTCVoiceStats[]) => void;
};

const defaultICEServers: RTCIceServer[] = [{ urls: "stun:stun.l.google.com:19302" }];

export class WebRTCVoiceExperiment {
  private readonly room: Room;
  private readonly selfUserID: number;
  private settings: VoiceSettings;
  private readonly callbacks: WebRTCVoiceCallbacks;
  private stream?: MediaStream;
  private audioContext?: AudioContext;
  private source?: MediaStreamAudioSourceNode;
  private micGain?: GainNode;
  private destination?: MediaStreamAudioDestinationNode;
  private outputGainNodes = new Map<number, GainNode>();
  private peers = new Map<number, RTCPeerConnection>();
  private statsTimer: number | null = null;

  constructor(room: Room, selfUserID: number, settings: VoiceSettings, callbacks: WebRTCVoiceCallbacks) {
    this.room = room;
    this.selfUserID = selfUserID;
    this.settings = settings;
    this.callbacks = callbacks;
  }

  async start(members: RoomMember[]) {
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
    this.source = this.audioContext.createMediaStreamSource(this.stream);
    this.micGain = this.audioContext.createGain();
    this.destination = this.audioContext.createMediaStreamDestination();
    this.micGain.gain.value = clampNumber(this.settings.micVolume, 0.05, 2, 1);
    this.source.connect(this.micGain);
    this.micGain.connect(this.destination);
    this.setMicEnabled(false);
    for (const member of members) {
      if (member.userID > 0 && member.userID !== this.selfUserID) {
        await this.ensurePeer(member.userID, true);
      }
    }
    this.statsTimer = window.setInterval(() => void this.collectStats(), 1500);
  }

  setMicEnabled(enabled: boolean) {
    this.stream?.getAudioTracks().forEach((track) => {
      track.enabled = enabled;
    });
  }

  updateSettings(settings: VoiceSettings) {
    this.settings = settings;
    this.micGain?.gain.setTargetAtTime(clampNumber(settings.micVolume, 0.05, 2, 1), this.audioContext?.currentTime ?? 0, 0.015);
    for (const gain of this.outputGainNodes.values()) {
      gain.gain.setTargetAtTime(clampNumber(settings.outputVolume, 0.05, 2, 1), this.audioContext?.currentTime ?? 0, 0.015);
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
      const answer = await peer.createAnswer();
      await peer.setLocalDescription(answer);
      await this.sendSignal(signal.fromUserID, "answer", answer);
      return;
    }
    if (signal.type === "answer") {
      await peer.setRemoteDescription(payload as RTCSessionDescriptionInit);
      return;
    }
    if (signal.type === "ice" && payload) {
      await peer.addIceCandidate(payload as RTCIceCandidateInit);
    }
  }

  async close() {
    if (this.statsTimer !== null) {
      window.clearInterval(this.statsTimer);
      this.statsTimer = null;
    }
    await Promise.all([...this.peers.keys()].map((peerUserID) => this.sendSignal(peerUserID, "hangup", {}).catch(() => undefined)));
    this.peers.forEach((peer) => peer.close());
    this.peers.clear();
    this.outputGainNodes.clear();
    this.source?.disconnect();
    this.micGain?.disconnect();
    this.destination?.disconnect();
    this.stream?.getTracks().forEach((track) => track.stop());
    await this.audioContext?.close().catch(() => undefined);
    this.source = undefined;
    this.micGain = undefined;
    this.destination = undefined;
    this.audioContext = undefined;
    this.stream = undefined;
  }

  private async ensurePeer(peerUserID: number, initiator: boolean) {
    const existing = this.peers.get(peerUserID);
    if (existing) {
      return existing;
    }
    const peer = new RTCPeerConnection({ iceServers: defaultICEServers });
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
      if (peer.connectionState === "failed" || peer.connectionState === "closed") {
        this.closePeer(peerUserID);
      }
    };
    if (initiator) {
      const offer = await peer.createOffer({ offerToReceiveAudio: true });
      await peer.setLocalDescription(offer);
      await this.sendSignal(peerUserID, "offer", offer);
    }
    return peer;
  }

  private closePeer(peerUserID: number) {
    const peer = this.peers.get(peerUserID);
    if (!peer) {
      return;
    }
    peer.close();
    this.peers.delete(peerUserID);
    removeRemoteStream(peerUserID);
    const gain = this.outputGainNodes.get(peerUserID);
    gain?.disconnect();
    this.outputGainNodes.delete(peerUserID);
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
      const stats = viewPeerStats(peerUserID, report);
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
}

function viewPeerStats(peerUserID: number, report: RTCStatsReport): WebRTCVoiceStats | undefined {
  let selectedPair: any;
  let inbound: any;
  let outbound: any;
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
  });
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
