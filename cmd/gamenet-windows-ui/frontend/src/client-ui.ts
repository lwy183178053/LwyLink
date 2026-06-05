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
  switch (page) {
    case "account":
      return "账号中心";
    case "room":
      return "房间";
    case "settings":
      return "设置";
  }
}

export function pageEyebrow(page: PageKey) {
  switch (page) {
    case "account":
      return "Profile";
    case "room":
      return "Lobby";
    case "settings":
      return "Preferences";
  }
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
  switch (status) {
    case "connected":
      return "已连接";
    case "connecting":
      return "连接中";
    case "disconnecting":
      return "断开中";
    case "error":
      return "错误";
    default:
      return "未连接";
  }
}

export function statusTone(status: string) {
  switch (status) {
    case "connected":
      return "good";
    case "connecting":
    case "disconnecting":
      return "warn";
    case "error":
      return "bad";
    default:
      return "neutral";
  }
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
