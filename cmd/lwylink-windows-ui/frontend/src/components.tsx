import type { CSSProperties, ReactNode, RefObject } from "react";
import { Button, Input, Spinner } from "@fluentui/react-components";
import {
  CircleCheck,
  CircleX,
  Copy,
  Crown,
  DoorOpen,
  Headphones,
  MessageSquare,
  Mic,
  MicOff,
  Network,
  PlugZap,
  Radio,
  Send,
  Trash2,
  UserMinus,
} from "lucide-react";
import { formatTime, initials, statusText, statusTone, userLabel } from "./client-ui";
import type { WebRTCVoiceStats } from "./webrtcVoice";
import type { ChatMessage, ConnectionState, PublicUser, Room, RoomMember, VoiceState } from "./types";

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
  p2pLabel,
  speaking,
  canManage,
  disabled,
  onKick,
  onTransfer,
}: {
  member: RoomMember;
  isSelf: boolean;
  p2pLabel?: string;
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
          {p2pLabel && <small className="member-p2p">{p2pLabel}</small>}
        </div>
      </div>
      <div className="member-role">
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
  directPeerNames,
  directPeerIPSet,
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
  directPeerNames: string[];
  directPeerIPSet: Set<string>;
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
            p2pLabel={memberP2PLabel(member, currentUserID, connectionRunning, directPeerNames, directPeerIPSet)}
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

function isVoiceSpeaking(voiceState: VoiceState, userID: number) {
  return (voiceState.participants || []).some((item) => item.userID === userID && item.speaking);
}

function memberP2PLabel(member: RoomMember, currentUserID: number | undefined, connectionRunning: boolean, directPeerNames: string[], directPeerIPSet: Set<string>) {
  if (!connectionRunning) {
    return undefined;
  }
  if (member.userID === currentUserID && directPeerNames.length > 0) {
    return `已与 ${directPeerNames.join("、")} P2P 直连`;
  }
  if (member.virtualIP && directPeerIPSet.has(member.virtualIP)) {
    return "与我 P2P 直连";
  }
  return undefined;
}

function voiceRTCDiagnostic(stats: WebRTCVoiceStats[]) {
  if (stats.length === 0) {
    return { label: "高清语音", detail: "等待媒体路径", tone: "" };
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
  const diagnostic = voiceRTCDiagnostic(voiceRTCStats);
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
