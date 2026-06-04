export type PublicUser = {
  id: number;
  username: string;
  displayName: string;
  role: string;
  disabled: boolean;
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

export type ConnectionState = {
  status: "disconnected" | "connecting" | "connected" | "disconnecting" | "error" | string;
  message: string;
  roomID: number;
  roomName: string;
  networkCIDR: string;
  overlayAddr: string;
  ip: string;
  gateway: string;
  mtu: number;
};

export type AppState = {
  apiBaseURL: string;
  interfaceName: string;
  user?: PublicUser;
  rooms: Room[];
  currentRoom?: Room;
  members: RoomMember[];
  connection: ConnectionState;
};

export const emptyState: AppState = {
  apiBaseURL: "http://64.83.34.53:8080",
  interfaceName: "LwyV-GameNet",
  rooms: [],
  members: [],
  connection: {
    status: "disconnected",
    message: "未连接",
    roomID: 0,
    roomName: "",
    networkCIDR: "",
    overlayAddr: "",
    ip: "",
    gateway: "",
    mtu: 0,
  },
};
