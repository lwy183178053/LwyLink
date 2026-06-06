import type { PublicUser } from "./types";

export const usernameMinLength = 3;
export const usernameMaxLength = 32;
export const passwordMinLength = 6;
export const passwordMaxLength = 128;

const usernamePattern = /^[a-z0-9_-]+$/;

export type PageKey = "account" | "room" | "settings";

export type LoginForm = {
  username: string;
  password: string;
  displayName: string;
};

export type RoomForm = {
  name: string;
  maxMembers: string;
  joinKey: string;
};

export type SettingsForm = {
  apiBaseURL: string;
  p2pEnabled: boolean;
};

const pageTitles: Record<PageKey, string> = {
  account: "账号中心",
  room: "房间",
  settings: "设置",
};

const pageEyebrows: Record<PageKey, string> = {
  account: "Profile",
  room: "Lobby",
  settings: "Preferences",
};

const statusTexts: Record<string, string> = {
  connected: "已连接",
  connecting: "连接中",
  disconnecting: "断开中",
  error: "错误",
};

const statusTones: Record<string, string> = {
  connected: "good",
  connecting: "warn",
  disconnecting: "warn",
  error: "bad",
};

const updateStatusTexts: Record<string, string> = {
  checking: "检查中",
  downloading: "下载中",
  ready: "可更新",
  installing: "更新中",
  error: "更新失败",
};

const updateStatusTones: Record<string, string> = {
  ready: "good",
  checking: "warn",
  downloading: "warn",
  installing: "warn",
  error: "bad",
};

export function authRuleState(username: string, password: string) {
  const name = username.trim();
  return {
    usernameLength: name.length >= usernameMinLength && name.length <= usernameMaxLength,
    usernameCharset: name === "" || usernamePattern.test(name),
    passwordLength: password.length >= passwordMinLength && password.length <= passwordMaxLength,
  };
}

export function pageTitle(page: PageKey) {
  return pageTitles[page];
}

export function pageEyebrow(page: PageKey) {
  return pageEyebrows[page];
}

export function pageSubtitle(page: PageKey, user?: PublicUser, roomName?: string) {
  switch (page) {
    case "account":
      return user ? `${userLabel(user.displayName, user.username)} · ${membershipLabel(user.membership)}` : "账号";
    case "room":
      return roomName || "房间";
    case "settings":
      return "设置";
  }
}

export function statusText(status: string) {
  return statusTexts[status] ?? "未连接";
}

export function statusTone(status: string) {
  return statusTones[status] ?? "neutral";
}

export function isConnectionRunning(status: string) {
  return status === "connected" || status === "connecting";
}

export function transportText(mode?: string, message?: string, directPeers?: number) {
  if (message && !/tcp|udp|打洞|中继/i.test(message)) {
    return message;
  }
  switch (mode) {
    case "udp-direct":
      return `智能加速已开启${directPeers ? ` · ${directPeers} 人` : ""}`;
    case "udp-probing":
      return "正在优化连接";
    case "udp-disabled":
      return "稳定连接";
    default:
      return "稳定连接";
  }
}

export function transportTone(mode?: string) {
  switch (mode) {
    case "udp-direct":
      return "good";
    case "udp-probing":
      return "warn";
    case "udp-disabled":
      return "neutral";
    default:
      return "neutral";
  }
}

export const roomMemberOptions = [2, 4, 6, 8, 16, 32, 50, 100];
export const freeRoomMemberLimit = 8;
export const defaultRoomMemberLimit = freeRoomMemberLimit;

export function isVIP(user?: PublicUser) {
  return user?.membership === "vip";
}

export function canCreateRoomSize(maxMembers: number, user?: PublicUser) {
  return maxMembers <= freeRoomMemberLimit || isVIP(user);
}

export function defaultRoomName(user?: PublicUser) {
  return `${user ? userLabel(user.displayName, user.username) : "玩家"}的房间`;
}

export function updateStatusText(status: string) {
  return updateStatusTexts[status] ?? "无更新";
}

export function updateStatusTone(status: string) {
  return updateStatusTones[status] ?? "neutral";
}

export function membershipLabel(value?: string) {
  return value === "vip" ? "会员" : "普通用户";
}

export function membershipTone(value?: string) {
  return value === "vip" ? "vip" : "neutral";
}

export function userLabel(displayName: string, username: string) {
  return displayName || username || "-";
}

export function initials(user?: PublicUser) {
  const name = user ? userLabel(user.displayName, user.username) : "游";
  return name.slice(0, 2).toUpperCase();
}

export function formatDate(value?: string) {
  if (!value) {
    return "-";
  }
  return new Date(value).toLocaleString();
}

export function formatTime(value?: string) {
  if (!value) {
    return "";
  }
  return new Date(value).toLocaleTimeString();
}

export function formatBytes(size?: number) {
  const value = Number(size || 0);
  if (value < 1024) {
    return `${value} B`;
  }
  const units = ["KB", "MB", "GB"];
  let current = value;
  for (const unit of units) {
    current /= 1024;
    if (current < 1024) {
      return `${current.toFixed(1)} ${unit}`;
    }
  }
  return `${(current / 1024).toFixed(1)} TB`;
}

export function serverHostFromAPI(apiBaseURL: string) {
  try {
    const parsed = new URL(apiBaseURL);
    return parsed.hostname;
  } catch {
    return apiBaseURL.replace(/^https?:\/\//, "").replace(/:\d+.*$/, "");
  }
}

export function errorText(err: unknown) {
  if (err instanceof Error) {
    return err.message;
  }
  if (typeof err === "string") {
    return err;
  }
  return "操作失败";
}
