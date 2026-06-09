import type { PublicUser, VoiceSettings } from "./types";

export const emailMinLength = 6;
export const emailMaxLength = 254;
export const passwordMinLength = 6;
export const passwordMaxLength = 128;

const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export type PageKey = "account" | "room" | "settings";
export type AuthMode = "login" | "register" | "reset";

export type AuthRules = {
  emailLength: boolean;
  emailFormat: boolean;
  passwordLength: boolean;
};

export type LoginForm = {
  email: string;
  password: string;
  displayName: string;
  emailCode: string;
  resetCode: string;
  resetPassword: string;
  oldPassword: string;
  newPassword: string;
};

export type RoomForm = {
  name: string;
  maxMembers: string;
  joinKey: string;
};

export type SettingsForm = {
  apiBaseURL: string;
  p2pEnabled: boolean;
  voiceSettings: VoiceSettings;
};

export type VoiceDevice = {
  deviceID: string;
  label: string;
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
  connecting: "未连接",
  disconnecting: "未连接",
  error: "未连接",
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

export function authRuleState(email: string, password: string): AuthRules {
  const value = email.trim();
  return {
    emailLength: value.length >= emailMinLength && value.length <= emailMaxLength,
    emailFormat: value === "" || emailPattern.test(value),
    passwordLength: password.length >= passwordMinLength && password.length <= passwordMaxLength,
  };
}

export function emailCodeInputValue(value: string) {
  return value.replace(/\D/g, "").slice(0, 6);
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

export const roomMemberOptions = [2, 4, 8, 16, 32, 64];
export const freeRoomMemberLimit = 4;
export const defaultRoomMemberLimit = freeRoomMemberLimit;

function isAdmin(user?: PublicUser) {
  return user?.role === "admin";
}

function isVIP(user?: PublicUser) {
  return user?.membership === "vip";
}

export function roomCapacityLimit(user?: PublicUser) {
  if (isAdmin(user)) {
    return 64;
  }
  if (isVIP(user)) {
    return 16;
  }
  return freeRoomMemberLimit;
}

export function canCreateRoomSize(maxMembers: number, user?: PublicUser) {
  return roomMemberOptions.includes(maxMembers) && maxMembers <= roomCapacityLimit(user);
}

export function roomSizeTierLabel(maxMembers: number) {
  if (maxMembers > 16) {
    return "管理";
  }
  if (maxMembers > freeRoomMemberLimit) {
    return "会员";
  }
  return "普通";
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

export function trafficUsedPercent(user?: PublicUser) {
  if (!user || user.relayTrafficQuotaBytes <= 0) {
    return 0;
  }
  return Math.max(0, Math.min(100, Math.round((user.relayTrafficUsedBytes / user.relayTrafficQuotaBytes) * 100)));
}

export function trafficPeriodLabel(value?: string) {
  if (!value) {
    return "本期";
  }
  const [year, month] = value.split("-");
  if (!year || !month) {
    return value;
  }
  return `${year}年${Number(month)}月`;
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
