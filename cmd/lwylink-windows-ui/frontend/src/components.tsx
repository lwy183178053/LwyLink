import type { ReactNode } from "react";
import { Button } from "@fluentui/react-components";
import { CircleCheck, CircleX, Crown, UserMinus } from "lucide-react";
import { formatTime, initials, userLabel } from "./client-ui";
import type { ChatMessage, PublicUser, RoomMember } from "./types";

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
  canManage,
  disabled,
  onKick,
  onTransfer,
}: {
  member: RoomMember;
  isSelf: boolean;
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
