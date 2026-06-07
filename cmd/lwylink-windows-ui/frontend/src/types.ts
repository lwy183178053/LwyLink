export type PublicUser = {
  id: number;
  username: string;
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
  connection: ConnectionState;
  update: UpdateState;
  announcement: string;
};

export const emptyState: AppState = {
  apiBaseURL: "http://64.83.34.53:8080",
  interfaceName: "LwyLink",
  p2pEnabled: false,
  rooms: [],
  members: [],
  chatMessages: [],
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
    currentVersion: "0.1.15",
    latestVersion: "",
    platform: "windows-amd64",
    notes: "",
    mandatory: false,
    installArgs: "",
    fileName: "",
    sizeBytes: 0,
    sha256: "",
  },
};
