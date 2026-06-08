import type { ReactNode, RefObject } from "react";
import { Button, Spinner, Textarea } from "@fluentui/react-components";
import {
  CircleCheck,
  CircleX,
  Copy,
  Crown,
  DoorOpen,
  Headphones,
  MessageSquare,
  Mic,
  Network,
  PlugZap,
  Power,
  Radio,
  Send,
  Trash2,
  UserMinus,
} from "lucide-react";
import { formatBytes, formatTime, initials, statusText, statusTone, userLabel } from "./client-ui";
import type { ChatMessage, ConnectionState, PublicUser, Room, RoomMember } from "./types";

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
  canManage,
  disabled,
  onKick,
  onTransfer,
}: {
  member: RoomMember;
  isSelf: boolean;
  p2pLabel?: string;
  canManage: boolean;
  disabled: boolean;
  onKick: () => void;
  onTransfer: () => void;
}) {
  return (
    <div className="member-row">
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
  onlineCount,
  memberCount,
  currentUser,
  isOwner,
  disabled,
  onCopyKey,
  onConnect,
  onDisconnect,
  onDeleteRoom,
  onLeaveRoom,
}: {
  room: Room;
  connection: ConnectionState;
  connectionRunning: boolean;
  onlineCount: number;
  memberCount: number;
  currentUser?: PublicUser;
  isOwner: boolean;
  disabled: boolean;
  onCopyKey: () => void;
  onConnect: () => void;
  onDisconnect: () => void;
  onDeleteRoom: () => void;
  onLeaveRoom: () => void;
}) {
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
            <span>{connection.message || "稳定连接"}</span>
          </div>
        </div>
        <div className="connection-ip">
          <Network size={15} />
          <span>{connection.ip || room.networkCIDR}</span>
        </div>
      </div>

      <div className="room-side-metrics">
        <Metric label="成员" value={`${room.memberCount} / ${room.maxMembers}`} />
        <Metric label="在线" value={`${onlineCount} / ${memberCount}`} />
        <Metric label="节点" value={room.nodeName || "未分配"} />
        <Metric label="中继剩余" value={currentUser ? formatBytes(currentUser.relayTrafficRemainingBytes) : "-"} />
      </div>

      <div className="room-key compact">
        <span>房间密钥</span>
        <code>{room.roomPublicKey}</code>
        <Button icon={<Copy size={16} />} onClick={onCopyKey}>
          复制
        </Button>
      </div>

      <div className="command-actions room-control-actions">
        {connectionRunning ? (
          <Button icon={<Power size={16} />} disabled={disabled} onClick={onDisconnect}>
            断开虚拟网络
          </Button>
        ) : (
          <Button appearance="primary" icon={<PlugZap size={16} />} disabled={disabled} onClick={onConnect}>
            启动虚拟网络
          </Button>
        )}
        {isOwner ? (
          <Button icon={<Trash2 size={16} />} disabled={disabled} onClick={onDeleteRoom}>
            删除房间
          </Button>
        ) : (
          <Button icon={<DoorOpen size={16} />} disabled={disabled} onClick={onLeaveRoom}>
            退出房间
          </Button>
        )}
      </div>
    </aside>
  );
}

export function RoomPlayersPanel({
  room,
  members,
  currentUserID,
  onlineCount,
  connectionRunning,
  directPeerNames,
  directPeerIPSet,
  isOwner,
  disabled,
  onKick,
  onTransfer,
}: {
  room: Room;
  members: RoomMember[];
  currentUserID?: number;
  onlineCount: number;
  connectionRunning: boolean;
  directPeerNames: string[];
  directPeerIPSet: Set<string>;
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
          <h2>玩家与语音</h2>
        </div>
        <span>{onlineCount} 在线</span>
      </div>

      <div className="player-summary-strip">
        <div>
          <Radio size={16} />
          <span>{directPeerNames.length > 0 ? `${directPeerNames.length} 条 P2P 直连` : "TCP relay 就绪"}</span>
        </div>
        <div>
          <Network size={16} />
          <span>{room.networkCIDR}</span>
        </div>
      </div>

      <div className="member-table">
        {members.map((member) => (
          <MemberRow
            key={member.userID}
            member={member}
            isSelf={member.userID === currentUserID}
            p2pLabel={memberP2PLabel(member, currentUserID, connectionRunning, directPeerNames, directPeerIPSet)}
            canManage={isOwner && member.userID !== currentUserID}
            disabled={disabled}
            onKick={() => onKick(member.userID)}
            onTransfer={() => onTransfer(member.userID)}
          />
        ))}
      </div>

      <VoiceDock />
    </section>
  );
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

function VoiceDock() {
  return (
    <div className="voice-dock">
      <div className="voice-dock-title">
        <Headphones size={16} />
        <span>房间语音</span>
      </div>
      <div className="voice-actions">
        <button type="button" disabled title="麦克风">
          <Mic size={16} />
          <span>麦克风</span>
        </button>
        <button type="button" disabled title="耳机">
          <Headphones size={16} />
          <span>耳机</span>
        </button>
        <button type="button" disabled title="按键说话">
          <Radio size={16} />
          <span>按键说话</span>
        </button>
      </div>
    </div>
  );
}

export function RoomChatPanel({
  chatMessages,
  pendingChats,
  currentUserID,
  chatListRef,
  chatText,
  chatSending,
  onChatTextChange,
  onSendChat,
}: {
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
    <aside className="surface chat-panel">
      <div className="surface-head">
        <div>
          <span className="eyebrow">Chat</span>
          <h2>聊天</h2>
        </div>
        <MessageSquare size={18} />
      </div>
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
        <Textarea className="pretty-textarea chat-input" rows={2} maxLength={500} value={chatText} onChange={(_, data) => onChatTextChange(data.value)} />
        <Button appearance="primary" icon={chatSending ? <Spinner size="tiny" /> : <Send size={16} />} type="button" disabled={chatSending || chatText.trim() === ""} onClick={onSendChat}>
          {chatSending ? "发送中" : "发送"}
        </Button>
      </div>
    </aside>
  );
}
