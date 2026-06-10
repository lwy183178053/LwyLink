export type DataChatSender = {
  username?: string;
  displayName?: string;
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

export const maxDataChatMessageRunes = 500;
export const maxDataChatPayloadBytes = 4096;
export const maxDataChatHistoryItems = 200;

export function chatSignalType(value: string) {
  if (!value.startsWith("chat-")) {
    return "";
  }
  const signalType = value.slice("chat-".length);
  return signalType === "offer" || signalType === "answer" || signalType === "ice" || signalType === "hangup" ? signalType : "";
}

export function buildDataChatPayload(input: {
  roomID: number;
  selfUserID: number;
  message: string;
  sender?: DataChatSender;
  now?: Date;
  randomID?: string;
}): WebRTCDataChatMessage | undefined {
  const text = input.message.trim();
  if (!text || Array.from(text).length > maxDataChatMessageRunes) {
    return undefined;
  }
  const payload: WebRTCDataChatMessage = {
    id: `p2p-${input.selfUserID}-${(input.now ?? new Date()).getTime()}-${input.randomID ?? Math.random().toString(36).slice(2, 8)}`,
    roomID: input.roomID,
    userID: input.selfUserID,
    username: input.sender?.username ?? "",
    displayName: input.sender?.displayName ?? "",
    message: text,
    createdAt: (input.now ?? new Date()).toISOString(),
  };
  if (encodedBytes(JSON.stringify(payload)) > maxDataChatPayloadBytes) {
    return undefined;
  }
  return payload;
}

export function decodeDataChatMessage(payload: unknown, roomID: number, peerUserID: number): WebRTCDataChatMessage | undefined {
  if (typeof payload !== "string") {
    return undefined;
  }
  if (encodedBytes(payload) > maxDataChatPayloadBytes) {
    return undefined;
  }
  try {
    const parsed = JSON.parse(payload) as Partial<WebRTCDataChatMessage>;
    const message = typeof parsed.message === "string" ? parsed.message.trim() : "";
    if (parsed.roomID !== roomID || parsed.userID !== peerUserID || !message || Array.from(message).length > maxDataChatMessageRunes) {
      return undefined;
    }
    return {
      id: typeof parsed.id === "string" && parsed.id !== "" ? parsed.id : `p2p-${peerUserID}-${Date.now()}`,
      roomID,
      userID: peerUserID,
      username: typeof parsed.username === "string" ? parsed.username : "",
      displayName: typeof parsed.displayName === "string" ? parsed.displayName : "",
      message,
      createdAt: typeof parsed.createdAt === "string" && parsed.createdAt !== "" ? parsed.createdAt : new Date().toISOString(),
    };
  } catch {
    return undefined;
  }
}

export function shouldFallbackToServerChat(input: { currentRoomID?: number; message: string; p2pSent: boolean }) {
  return Boolean(input.currentRoomID && input.message.trim() && !input.p2pSent);
}

export function isCurrentDataChatMessage(input: { message: WebRTCDataChatMessage; currentRoomID?: number; sessionRoomID?: number }) {
  return Boolean(input.currentRoomID && input.sessionRoomID === input.currentRoomID && input.message.roomID === input.currentRoomID);
}

export function appendDedupedDataChatMessage(
  items: WebRTCDataChatMessage[],
  message: WebRTCDataChatMessage,
  maxItems = maxDataChatHistoryItems,
) {
  if (items.some((item) => item.id === message.id)) {
    return items;
  }
  return [...items, message].slice(-Math.max(1, Math.floor(maxItems)));
}

function encodedBytes(value: string) {
  return new TextEncoder().encode(value).byteLength;
}
