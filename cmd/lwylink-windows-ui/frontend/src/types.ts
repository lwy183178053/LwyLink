export type PublicUser = {
  id: number;
  username: string;
  email: string;
  displayName: string;
  role: string;
  membership: "free" | "vip" | string;
  disabled: boolean;
  relayTrafficUsedBytes: number;
  relayTrafficExtraBytes: number;
  relayTrafficQuotaBytes: number;
  relayTrafficRemainingBytes: number;
  relayTrafficPeriod: string;
  createdAt: string;
};

export type Room = {
  id: number;
  name: string;
  ownerUserID: number;
  nodeID: number;
  roomPublicKey: string;
  networkCIDR: string;
  maxMembers: number;
  voiceMaxMembers?: number;
  voiceMaxSpeakers?: number;
  memberCount: number;
  status: string;
  nodeName?: string;
  nodeOverlayAddr?: string;
  nodeUDPAddr?: string;
  nodePublicKey?: string;
  createdAt: string;
  updatedAt: string;
};

export type RoomMember = {
  roomID: number;
  userID: number;
  username: string;
  displayName: string;
  role: "owner" | "member" | string;
  online?: boolean;
  virtualIP?: string;
  createdAt: string;
};

export type VoiceParticipant = {
  roomID: number;
  userID: number;
  username: string;
  displayName: string;
  speaking: boolean;
  muted: boolean;
  joinedAt: string;
  updatedAt: string;
};

export type VoiceState = {
  roomID: number;
  maxMembers: number;
  maxSpeakers: number;
  participantCount: number;
  speakerCount: number;
  participants: VoiceParticipant[];
  updatedAt: string;
};

export type ChatMessage = {
  id: string;
  roomID: number;
  userID: number;
  username: string;
  displayName: string;
  message: string;
  createdAt: string;
};

export type ConnectionState = {
  status: "disconnected" | "connecting" | "connected" | "disconnecting" | "error" | string;
  message: string;
  roomID: number;
  ip: string;
  transportMode?: string;
  directPeerIPs?: string[];
  transportDiagnostics?: TransportDiagnostic[];
};

export type TransportDiagnostic = {
  virtualIP: string;
  candidateCount: number;
  selectedType: string;
  lastProbeAt: string;
  failedProbes: number;
  lastObservedPeerAt: string;
};

export type UpdateState = {
  status: "idle" | "checking" | "downloading" | "ready" | "installing" | "error" | string;
  message: string;
  currentVersion: string;
  latestVersion: string;
  platform: string;
  notes: string;
  mandatory: boolean;
  installArgs: string;
  fileName: string;
  sizeBytes: number;
  sha256: string;
};

export type VoiceSettings = {
  inputDeviceID: string;
  outputDeviceID: string;
  micVolume: number;
  outputVolume: number;
  autoGainControl: boolean;
  noiseSuppression: boolean;
  echoCancellation: boolean;
};

export type AppState = {
  apiBaseURL: string;
  interfaceName: string;
  voiceSettings: VoiceSettings;
  user?: PublicUser;
  rooms: Room[];
  currentRoom?: Room;
  members: RoomMember[];
  chatMessages: ChatMessage[];
  voice: VoiceState;
  connection: ConnectionState;
  update: UpdateState;
  announcement: string;
};

export const emptyState: AppState = {
  apiBaseURL: "https://www.lwylink.xyz",
  interfaceName: "LwyLink",
  voiceSettings: {
    inputDeviceID: "",
    outputDeviceID: "",
    micVolume: 1.15,
    outputVolume: 1,
    autoGainControl: true,
    noiseSuppression: true,
    echoCancellation: true,
  },
  rooms: [],
  members: [],
  chatMessages: [],
  voice: {
    roomID: 0,
    maxMembers: 0,
    maxSpeakers: 0,
    participantCount: 0,
    speakerCount: 0,
    participants: [],
    updatedAt: "",
  },
  announcement: "官方 QQ 群：715516422",
    connection: {
      status: "disconnected",
      message: "未连接",
      roomID: 0,
      ip: "",
      transportMode: "",
      directPeerIPs: [],
      transportDiagnostics: [],
    },
  update: {
    status: "idle",
    message: "",
    currentVersion: "0.2.5",
    latestVersion: "",
    platform: "windows-amd64",
    notes: "",
    mandatory: true,
    installArgs: "",
    fileName: "",
    sizeBytes: 0,
    sha256: "",
  },
};
