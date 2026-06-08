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

export type RoomEvent = {
  type: string;
  roomID: number;
  actorUserID?: number;
  targetUserID?: number;
  voice?: VoiceState;
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
  directPeerIPs?: string[];
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

export type AppState = {
  apiBaseURL: string;
  interfaceName: string;
  p2pEnabled: boolean;
  user?: PublicUser;
  rooms: Room[];
  currentRoom?: Room;
  members: RoomMember[];
  chatMessages: ChatMessage[];
  voice: VoiceState;
  voiceMode: string;
  voiceMessage: string;
  connection: ConnectionState;
  update: UpdateState;
  announcement: string;
};

export const emptyState: AppState = {
  apiBaseURL: "https://www.lwylink.xyz",
  interfaceName: "LwyLink",
  p2pEnabled: true,
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
  voiceMode: "idle",
  voiceMessage: "未加入语音",
  announcement: "官方 QQ 群：715516422",
  connection: {
    status: "disconnected",
    message: "未连接",
    roomID: 0,
    ip: "",
    directPeerIPs: [],
  },
  update: {
    status: "idle",
    message: "",
    currentVersion: "0.1.20",
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
