import type { CSSProperties, FormEvent, ReactNode, RefObject } from "react";
import { Button, Field, Input, Select, Slider, Spinner, Textarea } from "@fluentui/react-components";
import {
  CircleCheck,
  CircleX,
  Copy,
  Crown,
  DoorOpen,
  Gamepad2,
  Headphones,
  KeyRound,
  LogIn,
  LogOut,
  MessageSquare,
  Mic,
  MicOff,
  Megaphone,
  Network,
  Power,
  PlugZap,
  Plus,
  Radio,
  RefreshCw,
  Send,
  SlidersHorizontal,
  Trash2,
  UserMinus,
  UserPlus,
  Volume2,
} from "lucide-react";
import {
  emailCodeInputValue,
  emailMaxLength,
  emailMinLength,
  formatBytes,
  formatTime,
  initials,
  membershipLabel,
  membershipTone,
  passwordMaxLength,
  passwordMinLength,
  serverHostFromAPI,
  statusText,
  statusTone,
  updateStatusText,
  updateStatusTone,
  userLabel,
  type AuthMode,
  type AuthRules,
  type LoginForm,
  type RoomForm,
  type SettingsForm,
  type VoiceDevice,
} from "./client-ui";
import type { WebRTCVoiceStats } from "./webrtcVoice";
import type { ChatMessage, ConnectionState, PublicUser, Room, RoomMember, UpdateState, VoiceSettings, VoiceState } from "./types";

type VoiceLevelStyle = CSSProperties & {
  "--voice-level": string;
};

export function NavButton({ active, icon, label, detail, onClick }: { active: boolean; icon: ReactNode; label: string; detail: string; onClick: () => void }) {
  return (
    <button className={active ? "nav-button active" : "nav-button"} onClick={onClick} type="button">
      {icon}
      <span>{label}</span>
      <small>{detail}</small>
    </button>
  );
}

export function Avatar({ user, size = "normal" }: { user?: PublicUser; size?: "normal" | "large" | "hero" }) {
  return <div className={`avatar ${size}`}>{initials(user)}</div>;
}

export function Metric({ label, value, wide = false }: { label: string; value: string; wide?: boolean }) {
  return (
    <div className={wide ? "metric wide" : "metric"}>
      <span>{label}</span>
      <strong>{value || "-"}</strong>
    </div>
  );
}

export function Rule({ ok, text }: { ok: boolean; text: string }) {
  return (
    <div className={ok ? "rule ok" : "rule"}>
      {ok ? <CircleCheck size={15} /> : <CircleX size={15} />}
      <span>{text}</span>
    </div>
  );
}

export function MemberRow({
  member,
  isSelf,
  transportLabel,
  transportTone,
  speaking,
  canManage,
  disabled,
  onKick,
  onTransfer,
}: {
  member: RoomMember;
  isSelf: boolean;
  transportLabel?: string;
  transportTone?: "direct" | "relay" | "probing";
  speaking: boolean;
  canManage: boolean;
  disabled: boolean;
  onKick: () => void;
  onTransfer: () => void;
}) {
  return (
    <div className={speaking ? "member-row speaking" : "member-row"}>
      <div className="member-identity">
        <span className={`presence ${member.online ? "online" : ""}`} />
        <div>
          <strong>
            {userLabel(member.displayName, member.username)}
            {isSelf ? " · 我" : ""}
          </strong>
          <span>{member.virtualIP || member.username}</span>
        </div>
      </div>
      <div className="member-role">
        {transportLabel && <span className={`member-transport ${transportTone || "relay"}`}>{transportLabel}</span>}
        {speaking && (
          <span className="role speaking">
            <Mic size={13} />
            说话中
          </span>
        )}
        {member.role === "owner" ? (
          <span className="role owner">
            <Crown size={13} />
            房主
          </span>
        ) : (
          <span className="role">成员</span>
        )}
        {canManage && (
          <div className="member-actions">
            <Button size="small" icon={<Crown size={13} />} disabled={disabled} onClick={onTransfer}>
              转让
            </Button>
            <Button size="small" icon={<UserMinus size={13} />} disabled={disabled} onClick={onKick}>
              移出
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

export function ChatBubble({ message, mine }: { message: ChatMessage; mine: boolean }) {
  return (
    <div className={mine ? "chat-bubble mine" : "chat-bubble"}>
      <div className="chat-meta">
        <strong>{userLabel(message.displayName, message.username)}</strong>
        <span>{formatTime(message.createdAt)}</span>
      </div>
      <p>{message.message}</p>
    </div>
  );
}

export function RoomControlPanel({
  room,
  connection,
  connectionRunning,
  isOwner,
  disabled,
  onCopyKey,
  onConnect,
  onDeleteRoom,
  onLeaveRoom,
}: {
  room: Room;
  connection: ConnectionState;
  connectionRunning: boolean;
  isOwner: boolean;
  disabled: boolean;
  onCopyKey: () => void;
  onConnect: () => void;
  onDeleteRoom: () => void;
  onLeaveRoom: () => void;
}) {
  const roomIP = connection.ip || room.networkCIDR;
  return (
    <aside className="surface room-control-panel">
      <div className="room-title-block">
        <div>
          <span className="eyebrow">当前房间</span>
          <h2>{room.name}</h2>
        </div>
        <span className={`status-pill ${room.status === "open" ? "good" : "bad"}`}>{room.status}</span>
      </div>

      <div className="connection-card">
        <div className="connection-card-head">
          <span className={`status-light ${statusTone(connection.status)}`} />
          <div>
            <strong>{statusText(connection.status)}</strong>
          </div>
        </div>
        {roomIP && (
          <div className="connection-ip">
            <Network size={15} />
            <span>{roomIP}</span>
          </div>
        )}
      </div>

      <div className="room-side-metrics">
        <Metric label="成员" value={`${room.memberCount} / ${room.maxMembers}`} />
        <Metric label="节点" value={room.nodeName || "未分配"} />
      </div>

      <div className="room-key compact">
        <span>房间密钥</span>
        <code>{room.roomPublicKey}</code>
        <Button icon={<Copy size={16} />} onClick={onCopyKey}>
          复制
        </Button>
      </div>

      <div className="command-actions room-control-actions">
        {!connectionRunning && (
          <button className="room-icon-action primary" type="button" disabled={disabled} title="启动虚拟网络" onClick={onConnect} aria-label="启动虚拟网络">
            <PlugZap size={17} />
          </button>
        )}
        {isOwner ? (
          <button className="room-icon-action danger" type="button" disabled={disabled} title="删除房间" onClick={onDeleteRoom} aria-label="删除房间">
            <Trash2 size={17} />
          </button>
        ) : (
          <button className="room-icon-action" type="button" disabled={disabled} title="退出房间" onClick={onLeaveRoom} aria-label="退出房间">
            <DoorOpen size={17} />
          </button>
        )}
      </div>
    </aside>
  );
}

export function RoomPlayersPanel({
  members,
  currentUserID,
  onlineCount,
  connectionRunning,
  directPeerIPSet,
  connectionMessage,
  voiceState,
  isOwner,
  disabled,
  onKick,
  onTransfer,
}: {
  members: RoomMember[];
  currentUserID?: number;
  onlineCount: number;
  connectionRunning: boolean;
  directPeerIPSet: Set<string>;
  connectionMessage: string;
  voiceState: VoiceState;
  isOwner: boolean;
  disabled: boolean;
  onKick: (userID: number) => void;
  onTransfer: (userID: number) => void;
}) {
  return (
    <section className="surface room-players-panel">
      <div className="surface-head">
        <div>
          <span className="eyebrow">Players</span>
          <h2>玩家列表</h2>
        </div>
        <span>{onlineCount} 在线</span>
      </div>

      <div className="member-table">
        {members.map((member) => (
          <MemberRow
            key={member.userID}
            member={member}
            isSelf={member.userID === currentUserID}
            transportLabel={memberTransportLabel(member, currentUserID, connectionRunning, directPeerIPSet, connectionMessage)}
            transportTone={memberTransportTone(member, currentUserID, connectionRunning, directPeerIPSet, connectionMessage)}
            speaking={isVoiceSpeaking(voiceState, member.userID)}
            canManage={isOwner && member.userID !== currentUserID}
            disabled={disabled}
            onKick={() => onKick(member.userID)}
            onTransfer={() => onTransfer(member.userID)}
          />
        ))}
      </div>
    </section>
  );
}

export function RoomEntryPage({
  form,
  defaultName,
  roomSizePicker,
  roomSizeAllowed,
  capacityLimit,
  disabled,
  onCreateRoom,
  onJoinRoom,
  onRoomNameChange,
  onJoinKeyChange,
}: {
  form: RoomForm;
  defaultName: string;
  roomSizePicker: ReactNode;
  roomSizeAllowed: boolean;
  capacityLimit: number;
  disabled: boolean;
  onCreateRoom: (event: FormEvent) => void;
  onJoinRoom: (event: FormEvent) => void;
  onRoomNameChange: (value: string) => void;
  onJoinKeyChange: (value: string) => void;
}) {
  return (
    <section className="room-entry">
      <form className="surface command-panel" onSubmit={onCreateRoom}>
        <div className="surface-head">
          <div>
            <span className="eyebrow">房主</span>
            <h2>创建房间</h2>
          </div>
          <Plus size={18} />
        </div>
        <div className="form-stack">
          <Field label="房间名">
            <Input className="pretty-input" placeholder={defaultName} value={form.name} onChange={(_, data) => onRoomNameChange(data.value)} />
          </Field>
          <Field label="人数上限">{roomSizePicker}</Field>
          {!roomSizeAllowed && <span className="form-hint">当前账号最多可创建 {capacityLimit} 人房间。</span>}
          <Button appearance="primary" icon={<Plus size={16} />} type="submit" disabled={disabled || form.name.trim() === "" || !roomSizeAllowed}>
            创建
          </Button>
        </div>
      </form>

      <form className="surface command-panel" onSubmit={onJoinRoom}>
        <div className="surface-head">
          <div>
            <span className="eyebrow">成员</span>
            <h2>加入房间</h2>
          </div>
          <KeyRound size={18} />
        </div>
        <div className="form-stack">
          <Field label="房间密钥">
            <Textarea className="pretty-textarea" rows={8} value={form.joinKey} onChange={(_, data) => onJoinKeyChange(data.value)} />
          </Field>
          <Button appearance="primary" icon={<UserPlus size={16} />} type="submit" disabled={disabled || form.joinKey.trim() === ""}>
            加入
          </Button>
        </div>
      </form>
    </section>
  );
}

export function AccountPage({
  user,
  authMode,
  loginForm,
  authRules,
  apiBaseURL,
  interfaceName,
  connection,
  currentRoom,
  currentMember,
  canSubmitAuth,
  isBusy,
  onAuthModeChange,
  onAuthSubmit,
  onEmailChange,
  onLoginFormChange,
  onSendEmailCode,
  onChangePassword,
  onLogout,
}: {
  user?: PublicUser;
  authMode: AuthMode;
  loginForm: LoginForm;
  authRules: AuthRules;
  apiBaseURL: string;
  interfaceName: string;
  connection: ConnectionState;
  currentRoom?: Room;
  currentMember?: RoomMember;
  canSubmitAuth: boolean;
  isBusy: boolean;
  onAuthModeChange: (mode: AuthMode) => void;
  onAuthSubmit: (event: FormEvent) => void;
  onEmailChange: (value: string) => void;
  onLoginFormChange: (patch: Partial<LoginForm>) => void;
  onSendEmailCode: (purpose: "register" | "password_reset") => void;
  onChangePassword: (event: FormEvent) => void;
  onLogout: () => void;
}) {
  if (!user) {
    return (
      <section className="auth-layout">
        <div className="hero-panel">
          <div className="hero-symbol">
            <Gamepad2 size={30} />
          </div>
          <div className="hero-copy">
            <span>联微互联</span>
            <strong>LwyLink</strong>
          </div>
          <div className="hero-status-grid">
            <Metric label="服务器" value={serverHostFromAPI(apiBaseURL)} />
            <Metric label="网卡" value={interfaceName} />
            <Metric label="状态" value={statusText(connection.status)} />
          </div>
        </div>

        <form className="surface auth-form" onSubmit={onAuthSubmit}>
          <div className="surface-head">
            <div>
              <span className="eyebrow">账号</span>
              <h2>{authMode === "login" ? "登录" : authMode === "register" ? "注册" : "找回密码"}</h2>
            </div>
            {authMode === "reset" ? <KeyRound size={18} /> : <LogIn size={18} />}
          </div>
          <div className="form-stack">
            <div className="segmented">
              <button type="button" className={authMode === "login" ? "active" : ""} onClick={() => onAuthModeChange("login")}>
                登录
              </button>
              <button type="button" className={authMode === "register" ? "active" : ""} onClick={() => onAuthModeChange("register")}>
                注册
              </button>
            </div>
            <Field label="邮箱">
              <Input className="pretty-input" type="email" value={loginForm.email} onChange={(_, data) => onEmailChange(data.value)} />
            </Field>
            {authMode === "reset" ? (
              <>
                <div className="inline-action-field">
                  <Field label="邮箱验证码">
                    <Input
                      className="pretty-input"
                      inputMode="numeric"
                      maxLength={6}
                      value={loginForm.resetCode}
                      onChange={(_, data) => onLoginFormChange({ resetCode: emailCodeInputValue(data.value) })}
                    />
                  </Field>
                  <Button type="button" icon={<Send size={16} />} disabled={isBusy || !authRules.emailFormat} onClick={() => onSendEmailCode("password_reset")}>
                    发送
                  </Button>
                </div>
                <Field label="新密码">
                  <Input
                    className="pretty-input"
                    type="password"
                    value={loginForm.resetPassword}
                    onChange={(_, data) => onLoginFormChange({ resetPassword: data.value })}
                  />
                </Field>
                <div className="rules compact">
                  <Rule ok={authRules.emailFormat} text="请输入有效邮箱格式" />
                  <Rule ok={loginForm.resetCode.trim().length === 6} text="6 位邮箱验证码" />
                  <Rule ok={loginForm.resetPassword.length >= passwordMinLength} text={`新密码至少 ${passwordMinLength} 位`} />
                </div>
              </>
            ) : authMode === "register" && (
              <Field label="昵称">
                <Input className="pretty-input" value={loginForm.displayName} onChange={(_, data) => onLoginFormChange({ displayName: data.value })} />
              </Field>
            )}
            {authMode === "register" && (
              <div className="inline-action-field">
                <Field label="邮箱验证码">
                  <Input
                    className="pretty-input"
                    inputMode="numeric"
                    maxLength={6}
                    value={loginForm.emailCode}
                    onChange={(_, data) => onLoginFormChange({ emailCode: emailCodeInputValue(data.value) })}
                  />
                </Field>
                <Button type="button" icon={<Send size={16} />} disabled={isBusy || !authRules.emailFormat} onClick={() => onSendEmailCode("register")}>
                  发送
                </Button>
              </div>
            )}
            {authMode !== "reset" && (
              <Field label="密码">
                <Input className="pretty-input" type="password" value={loginForm.password} onChange={(_, data) => onLoginFormChange({ password: data.value })} />
              </Field>
            )}
            {authMode === "register" && (
              <div className="rules">
                <Rule ok={authRules.emailLength} text={`${emailMinLength}-${emailMaxLength} 位邮箱`} />
                <Rule ok={authRules.emailFormat} text="请输入有效邮箱格式" />
                <Rule ok={authRules.passwordLength} text={`${passwordMinLength}-${passwordMaxLength} 位密码`} />
                <Rule ok={loginForm.emailCode.trim().length === 6} text="6 位邮箱验证码" />
              </div>
            )}
            <Button appearance="primary" icon={authMode === "reset" ? <RefreshCw size={16} /> : <LogIn size={16} />} type="submit" disabled={isBusy || !canSubmitAuth}>
              {authMode === "login" ? "进入客户端" : authMode === "register" ? "创建账号" : "重置密码"}
            </Button>
            {authMode === "login" && (
              <button type="button" className="auth-link-button" onClick={() => onAuthModeChange("reset")}>
                <span>
                  <KeyRound size={15} />
                  忘记密码，使用邮箱验证码找回
                </span>
              </button>
            )}
            {authMode === "reset" && (
              <button type="button" className="auth-link-button" onClick={() => onAuthModeChange("login")}>
                返回登录
              </button>
            )}
          </div>
        </form>
      </section>
    );
  }

  return (
    <section className="account-grid">
      <div className="surface account-session">
        <Avatar user={user} size="hero" />
        <div className="account-main">
          <span>当前会话</span>
          <h2>{userLabel(user.displayName, user.username)}</h2>
          <div className="account-tags">
            <span className={`member-chip ${membershipTone(user.membership)}`}>{membershipLabel(user.membership)}</span>
            <span className="member-chip neutral">{user.disabled ? "已禁用" : "账号正常"}</span>
          </div>
        </div>
        <div className="account-actions">
          <Button icon={<LogOut size={16} />} disabled={isBusy} onClick={onLogout}>
            退出
          </Button>
        </div>
      </div>

      <div className="surface data-panel">
        <div className="surface-head">
          <h2>网络状态</h2>
          <Network size={18} />
        </div>
        <div className="data-grid">
          <Metric label="连接" value={statusText(connection.status)} />
          <Metric label="虚拟 IP" value={connection.ip || "-"} />
          <Metric label="房间在线" value={currentMember?.online ? "在线" : "未连接"} />
          <Metric label="当前房间" value={currentRoom?.name ?? "未加入"} wide />
        </div>
      </div>

      <form className="surface password-panel" onSubmit={onChangePassword}>
        <div className="surface-head">
          <div>
            <span className="eyebrow">安全</span>
            <h2>修改密码</h2>
          </div>
          <KeyRound size={18} />
        </div>
        <div className="form-stack">
          <Field label="当前密码">
            <Input className="pretty-input" type="password" value={loginForm.oldPassword} onChange={(_, data) => onLoginFormChange({ oldPassword: data.value })} />
          </Field>
          <Field label="新密码">
            <Input className="pretty-input" type="password" value={loginForm.newPassword} onChange={(_, data) => onLoginFormChange({ newPassword: data.value })} />
          </Field>
          <Button appearance="primary" icon={<KeyRound size={16} />} type="submit" disabled={isBusy || loginForm.newPassword.length < passwordMinLength}>
            保存新密码
          </Button>
        </div>
      </form>
    </section>
  );
}

function isVoiceSpeaking(voiceState: VoiceState, userID: number) {
  return (voiceState.participants || []).some((item) => item.userID === userID && item.speaking);
}

function memberTransportLabel(member: RoomMember, currentUserID: number | undefined, connectionRunning: boolean, directPeerIPSet: Set<string>, connectionMessage: string) {
  if (!connectionRunning) {
    return undefined;
  }
  if (member.userID === currentUserID) {
    return connectionMessage.includes("探测") ? "探测中" : directPeerIPSet.size > 0 ? "P2P 直连" : "中继";
  }
  if (member.virtualIP && directPeerIPSet.has(member.virtualIP)) {
    return "P2P 直连";
  }
  return connectionMessage.includes("探测") ? "探测中" : "中继";
}

function memberTransportTone(member: RoomMember, currentUserID: number | undefined, connectionRunning: boolean, directPeerIPSet: Set<string>, connectionMessage: string) {
  const label = memberTransportLabel(member, currentUserID, connectionRunning, directPeerIPSet, connectionMessage);
  if (label === "P2P 直连") {
    return "direct";
  }
  if (label === "探测中") {
    return "probing";
  }
  return label ? "relay" : undefined;
}

function voiceRTCDiagnostic(stats: WebRTCVoiceStats[], voiceState: VoiceState) {
  if (stats.length === 0) {
    return (voiceState.participantCount || 0) > 1
      ? { label: "高清语音", detail: "等待媒体路径", tone: "" }
      : { label: "高清语音", detail: "等待其他成员", tone: "" };
  }
  const connected = stats.filter(isWebRTCConnected);
  const reconnecting = stats.filter((item) => item.connectionState === "disconnected" || item.connectionState === "failed" || item.iceConnectionState === "disconnected" || item.iceConnectionState === "failed");
  const candidateSource = connected.length > 0 ? connected : stats;
  const candidates = candidateSource.map((item) => item.candidateType).filter(Boolean);
  const usingRelay = candidates.includes("relay");
  const usingDirect = candidates.some((item) => item === "host" || item === "srflx" || item === "prflx");
  const qualityStats = connected.length > 0 ? connected : stats;
  const avgRTT = averageNumber(qualityStats.map((item) => item.roundTripTimeMS).filter((item) => item > 0));
  const avgJitter = averageNumber(qualityStats.map((item) => item.jitterMS).filter((item) => item > 0));
  const lost = qualityStats.reduce((sum, item) => sum + Math.max(0, item.packetsLost), 0);
  const label =
    connected.length === 0 && reconnecting.length > 0
      ? "语音重连中"
      : usingRelay
        ? "TURN 语音"
        : usingDirect
          ? "P2P 语音"
          : connected.length > 0
            ? "高清语音"
            : "语音连接中";
  const tone = usingRelay ? "warn" : connected.length > 0 ? "good" : "";
  const detail = [
    avgRTT > 0 ? `RTT ${avgRTT}ms` : "",
    avgJitter > 0 ? `抖动 ${avgJitter}ms` : "",
    lost > 0 ? `丢包 ${lost}` : "",
  ].filter(Boolean).join(" · ");
  return { label, detail: detail || `${connected.length}/${stats.length} 路连接`, tone };
}

function isWebRTCConnected(item: WebRTCVoiceStats) {
  return item.connectionState === "connected" || item.iceConnectionState === "connected" || item.iceConnectionState === "completed";
}

function averageNumber(values: number[]) {
  if (values.length === 0) {
    return 0;
  }
  return Math.round(values.reduce((sum, value) => sum + value, 0) / values.length);
}

function VoiceDock({
  room,
  voiceState,
  voiceActive,
  voiceMicActive,
  voiceRTCStats,
  voiceInputLevel,
  disabled,
  onJoinVoice,
  onToggleVoiceMic,
  onLeaveVoice,
}: {
  room: Room;
  voiceState: VoiceState;
  voiceActive: boolean;
  voiceMicActive: boolean;
  voiceRTCStats: WebRTCVoiceStats[];
  voiceInputLevel: number;
  disabled: boolean;
  onJoinVoice: () => void;
  onToggleVoiceMic: () => void;
  onLeaveVoice: () => void;
}) {
  const voiceMaxMembers = room.voiceMaxMembers || room.maxMembers;
  const voiceMaxSpeakers = room.voiceMaxSpeakers || Math.min(2, voiceMaxMembers);
  const diagnostic = voiceRTCDiagnostic(voiceRTCStats, voiceState);
  const transportText = voiceActive ? diagnostic.label : "未加入语音";
  const transportClass = voiceActive ? diagnostic.tone : "";
  return (
    <div className="voice-dock">
      <div className="voice-dock-title">
        <Headphones size={15} />
        <span>语音</span>
        <small>
          {voiceState.participantCount || 0}/{voiceMaxMembers} 人语音 · {voiceState.speakerCount || 0}/{voiceMaxSpeakers} 个麦位
        </small>
      </div>
      <div className={`voice-transport ${transportClass}`}>
        <Radio size={14} />
        <span>{transportText}</span>
      </div>
      {voiceActive && diagnostic.detail && <div className="voice-quality">{diagnostic.detail}</div>}
      <div className="voice-actions">
        <button className={voiceActive ? "voice-icon-button active" : "voice-icon-button"} type="button" disabled={disabled} title={voiceActive ? "退出语音" : "加入语音"} onClick={voiceActive ? onLeaveVoice : onJoinVoice}>
          <Headphones size={16} />
        </button>
        <button
          className={voiceMicActive ? "voice-icon-button mic-on" : "voice-icon-button"}
          type="button"
          disabled={disabled || !voiceActive}
          title={voiceMicActive ? "关闭麦克风" : "打开麦克风"}
          style={{ "--voice-level": String(voiceMicActive ? Math.max(0, Math.min(1, voiceInputLevel)) : 0) } as VoiceLevelStyle}
          onClick={onToggleVoiceMic}
        >
          {voiceMicActive ? <Mic size={16} /> : <MicOff size={16} />}
        </button>
      </div>
    </div>
  );
}

export function RoomCommsPanel({
  room,
  voiceState,
  voiceActive,
  voiceMicActive,
  voiceRTCStats,
  voiceInputLevel,
  disabled,
  onJoinVoice,
  onToggleVoiceMic,
  onLeaveVoice,
  chatMessages,
  pendingChats,
  currentUserID,
  chatListRef,
  chatText,
  chatSending,
  onChatTextChange,
  onSendChat,
}: {
  room: Room;
  voiceState: VoiceState;
  voiceActive: boolean;
  voiceMicActive: boolean;
  voiceRTCStats: WebRTCVoiceStats[];
  voiceInputLevel: number;
  disabled: boolean;
  onJoinVoice: () => void;
  onToggleVoiceMic: () => void;
  onLeaveVoice: () => void;
  chatMessages: ChatMessage[];
  pendingChats: Array<{ id: string; message: string }>;
  currentUserID?: number;
  chatListRef: RefObject<HTMLDivElement>;
  chatText: string;
  chatSending: boolean;
  onChatTextChange: (value: string) => void;
  onSendChat: () => void;
}) {
  return (
    <aside className="surface chat-panel comms-panel">
      <div className="surface-head">
        <div>
          <span className="eyebrow">Comms</span>
          <h2>交流</h2>
        </div>
        <MessageSquare size={18} />
      </div>
      <VoiceDock
        room={room}
        voiceState={voiceState}
        voiceActive={voiceActive}
        voiceMicActive={voiceMicActive}
        voiceRTCStats={voiceRTCStats}
        voiceInputLevel={voiceInputLevel}
        disabled={disabled}
        onJoinVoice={onJoinVoice}
        onToggleVoiceMic={onToggleVoiceMic}
        onLeaveVoice={onLeaveVoice}
      />
      <div className="chat-list" ref={chatListRef}>
        {chatMessages.map((message) => (
          <ChatBubble key={message.id} message={message} mine={message.userID === currentUserID} />
        ))}
        {pendingChats.map((message) => (
          <div className="chat-bubble mine pending" key={message.id}>
            <div className="pending-chat-line">
              <span className="chat-pending-spinner" />
              <p>{message.message}</p>
            </div>
          </div>
        ))}
        {chatMessages.length === 0 && pendingChats.length === 0 && <div className="empty-chat">暂无消息</div>}
      </div>
      <div className="chat-form">
        <Input
          className="pretty-input chat-input"
          maxLength={500}
          value={chatText}
          onChange={(_, data) => onChatTextChange(data.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.nativeEvent.isComposing) {
              event.preventDefault();
              onSendChat();
            }
          }}
        />
        <Button appearance="primary" icon={chatSending ? <Spinner size="tiny" /> : <Send size={16} />} type="button" disabled={chatSending || chatText.trim() === ""} onClick={onSendChat}>
          {chatSending ? "发送中" : "发送"}
        </Button>
      </div>
    </aside>
  );
}

export function SettingsPage({
  form,
  update,
  announcement,
  voiceDevices,
  disabled,
  onSubmit,
  onAPIBaseURLChange,
  onVoiceSettingsChange,
  onCheckUpdates,
  onInstallUpdate,
  onCopyAnnouncement,
}: {
  form: SettingsForm;
  update: UpdateState;
  announcement: string;
  voiceDevices: { inputs: VoiceDevice[]; outputs: VoiceDevice[] };
  disabled: boolean;
  onSubmit: (event: FormEvent) => void;
  onAPIBaseURLChange: (value: string) => void;
  onVoiceSettingsChange: (patch: Partial<VoiceSettings>) => void;
  onCheckUpdates: () => void;
  onInstallUpdate: () => void;
  onCopyAnnouncement: () => void;
}) {
  const updateTone = updateStatusTone(update.status);
  return (
    <section className="settings-grid">
      <div className="settings-column">
        <form className="surface settings-panel" onSubmit={onSubmit}>
          <div className="surface-head">
            <div>
              <span className="eyebrow">客户端</span>
              <h2>服务器设置</h2>
            </div>
            <SlidersHorizontal size={18} />
          </div>
          <div className="form-stack">
            <Field label="接口地址">
              <Input className="pretty-input" value={form.apiBaseURL} onChange={(_, data) => onAPIBaseURLChange(data.value)} />
            </Field>
            <div className="command-actions">
              <Button appearance="primary" type="submit" disabled={disabled}>
                保存
              </Button>
            </div>
          </div>
        </form>

        <form className="surface voice-settings-panel" onSubmit={onSubmit}>
          <div className="surface-head">
            <div>
              <span className="eyebrow">Voice</span>
              <h2>语音设置</h2>
            </div>
            <Volume2 size={18} />
          </div>
          <div className="voice-settings-form">
            <Field label="输入设备">
              <Select className="pretty-input" value={form.voiceSettings.inputDeviceID} onChange={(event) => onVoiceSettingsChange({ inputDeviceID: event.target.value })}>
                <option value="">默认麦克风</option>
                {voiceDevices.inputs.map((device) => (
                  <option key={device.deviceID} value={device.deviceID}>
                    {device.label}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="输出设备">
              <Select className="pretty-input" value={form.voiceSettings.outputDeviceID} onChange={(event) => onVoiceSettingsChange({ outputDeviceID: event.target.value })}>
                <option value="">默认扬声器</option>
                {voiceDevices.outputs.map((device) => (
                  <option key={device.deviceID} value={device.deviceID}>
                    {device.label}
                  </option>
                ))}
              </Select>
            </Field>
            <div className="voice-slider-row">
              <span>麦克风音量</span>
              <Slider min={0.2} max={2} step={0.05} value={form.voiceSettings.micVolume} onChange={(_, data) => onVoiceSettingsChange({ micVolume: data.value })} />
              <strong>{Math.round(form.voiceSettings.micVolume * 100)}%</strong>
            </div>
            <div className="voice-slider-row">
              <span>播放音量</span>
              <Slider min={0.2} max={2} step={0.05} value={form.voiceSettings.outputVolume} onChange={(_, data) => onVoiceSettingsChange({ outputVolume: data.value })} />
              <strong>{Math.round(form.voiceSettings.outputVolume * 100)}%</strong>
            </div>
          </div>
        </form>
      </div>

      <div className="settings-column">
        <div className="surface update-panel">
          <div className="surface-head">
            <div>
              <span className="eyebrow">更新</span>
              <h2>客户端版本</h2>
            </div>
            <span className={`status-pill ${updateTone}`}>{updateStatusText(update.status)}</span>
          </div>
          <div className="data-grid">
            <Metric label="当前版本" value={update.currentVersion || "-"} />
            <Metric label="最新版本" value={update.latestVersion || "-"} />
            <Metric label="平台" value={update.platform || "windows-amd64"} />
            <Metric label="大小" value={update.sizeBytes ? formatBytes(update.sizeBytes) : "-"} />
            <Metric label="状态" value={update.message || updateStatusText(update.status)} wide />
          </div>
          <div className="settings-actions">
            <Button icon={<RefreshCw size={16} />} disabled={disabled || update.status === "downloading"} onClick={onCheckUpdates}>
              检查更新
            </Button>
            <Button appearance="primary" icon={<Power size={16} />} disabled={disabled || update.status !== "ready"} onClick={onInstallUpdate}>
              重启更新
            </Button>
          </div>
        </div>

        <div className="surface announcement-panel">
          <div className="surface-head">
            <div>
              <span className="eyebrow">公告</span>
              <h2>官方信息</h2>
            </div>
            <Megaphone size={18} />
          </div>
          <div className="announcement-body">
            <p>{announcement || "官方 QQ 群：715516422"}</p>
          </div>
          <div className="settings-actions">
            <Button icon={<Copy size={16} />} onClick={onCopyAnnouncement}>
              复制
            </Button>
          </div>
        </div>
      </div>
    </section>
  );
}
