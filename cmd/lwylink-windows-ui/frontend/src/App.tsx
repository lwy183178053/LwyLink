import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Button, Field, Input, Spinner, Switch, Textarea } from "@fluentui/react-components";
import {
  Copy,
  DoorOpen,
  Gamepad2,
  KeyRound,
  LogIn,
  LogOut,
  Megaphone,
  MessageSquare,
  Minus,
  Network,
  PlugZap,
  Plus,
  Power,
  RefreshCw,
  Send,
  Settings,
  SlidersHorizontal,
  Square,
  Trash2,
  UserCircle,
  UserPlus,
  X,
} from "lucide-react";
import {
  CheckForUpdates,
  ConnectRoom,
  CreateRoom,
  CurrentState,
  DeleteRoom,
  Disconnect,
  HideToTray,
  InstallPreparedUpdate,
  JoinRoom,
  KickMember,
  LeaveCurrentRoom,
  Login,
  Logout,
  Register,
  SendRoomChat,
  SetSettings,
  TransferOwner,
} from "../wailsjs/go/main/App";
import { EventsOn, WindowMinimise, WindowToggleMaximise } from "../wailsjs/runtime/runtime";
import {
  authRuleState,
  canCreateRoomSize,
  defaultRoomMemberLimit,
  defaultRoomName,
  errorText,
  formatBytes,
  freeRoomMemberLimit,
  isConnectionRunning,
  membershipLabel,
  membershipTone,
  pageEyebrow,
  pageSubtitle,
  pageTitle,
  emailMaxLength,
  emailMinLength,
  passwordMaxLength,
  passwordMinLength,
  roomMemberOptions,
  serverHostFromAPI,
  statusText,
  statusTone,
  trafficPeriodLabel,
  trafficUsedPercent,
  updateStatusText,
  updateStatusTone,
  type LoginForm,
  type PageKey,
  type RoomForm,
  type SettingsForm,
  userLabel,
} from "./client-ui";
import { Avatar, ChatBubble, MemberRow, Metric, NavButton, Rule } from "./components";
import appIcon from "./assets/lwylink-icon.png";
import type { AppState } from "./types";
import { emptyState } from "./types";

type NoticeTone = "info" | "success" | "error";
type PendingChat = {
  id: string;
  message: string;
};

export default function App() {
  const [page, setPage] = useState<PageKey>("account");
  const [state, setState] = useState<AppState>(emptyState);
  const [authMode, setAuthMode] = useState<"login" | "register">("login");
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState<{ id: number; text: string; tone: NoticeTone } | null>(null);
  const [chatText, setChatText] = useState("");
  const [pendingChats, setPendingChats] = useState<PendingChat[]>([]);
  const [chatSending, setChatSending] = useState(false);
  const [dismissedUpdate, setDismissedUpdate] = useState("");
  const [loginForm, setLoginForm] = useState<LoginForm>({ email: "", password: "", displayName: "" });
  const [roomForm, setRoomForm] = useState<RoomForm>({ name: "", maxMembers: String(defaultRoomMemberLimit), joinKey: "" });
  const [settingsForm, setSettingsForm] = useState<SettingsForm>({
    apiBaseURL: emptyState.apiBaseURL,
    p2pEnabled: emptyState.p2pEnabled,
  });
  const connectionSignature = useRef("");
  const settingsSignature = useRef("");
  const chatListRef = useRef<HTMLDivElement | null>(null);
  const noticeTimer = useRef<number | null>(null);

  useEffect(() => {
    CurrentState()
      .then((next: AppState) => applyState(next))
      .catch((err: unknown) => showNotice(errorText(err), "error"));
    const off = EventsOn("state-changed", (next: AppState) => applyState(next));
    return () => {
      if (typeof off === "function") {
        off();
      }
    };
  }, []);

  useEffect(() => {
    return () => {
      if (noticeTimer.current !== null) {
        window.clearTimeout(noticeTimer.current);
      }
    };
  }, []);

  const currentRoom = useMemo(() => state.currentRoom ?? state.rooms[0], [state.currentRoom, state.rooms]);
  const members = state.members ?? [];
  const chatMessages = useMemo(
    () => (currentRoom ? (state.chatMessages ?? []).filter((message) => message.roomID === currentRoom.id) : []),
    [currentRoom, state.chatMessages],
  );
  const onlineCount = members.filter((member) => member.online).length;
  const currentMember = members.find((member) => member.userID === state.user?.id);
  const isOwner = !!currentRoom && currentRoom.ownerUserID === state.user?.id;
  const connectionTone = statusTone(state.connection.status);
  const connectionRunning = isConnectionRunning(state.connection.status);
  const directPeerIPSet = useMemo(() => new Set(state.connection.directPeerIPs ?? []), [state.connection.directPeerIPs]);
  const directPeerNames = useMemo(
    () =>
      members
        .filter((member) => member.virtualIP && directPeerIPSet.has(member.virtualIP))
        .map((member) => userLabel(member.displayName, member.username)),
    [members, directPeerIPSet],
  );
  const updateTone = updateStatusTone(state.update.status);
  const showUpdatePrompt =
    state.update.status === "ready" &&
    state.update.latestVersion !== "" &&
    (state.update.mandatory || dismissedUpdate !== state.update.latestVersion);
  const isBusy = busy !== "";
  const authRules = authRuleState(loginForm.email, loginForm.password);
  const canSubmitAuth =
    authMode === "login"
      ? loginForm.email.trim() !== "" && loginForm.password !== ""
      : authRules.emailLength && authRules.emailFormat && authRules.passwordLength;
  const selectedMaxMembers = Number.parseInt(roomForm.maxMembers, 10) || defaultRoomMemberLimit;
  const roomSizeAllowed = canCreateRoomSize(selectedMaxMembers, state.user);
  const trafficPercent = trafficUsedPercent(state.user);
  const trafficCritical = !!state.user && state.user.relayTrafficRemainingBytes <= 0;

  useEffect(() => {
    if (page !== "room" || !chatListRef.current) {
      return;
    }
    chatListRef.current.scrollTop = chatListRef.current.scrollHeight;
  }, [chatMessages.length, pendingChats.length]);

  useEffect(() => {
    setPendingChats([]);
  }, [currentRoom?.id]);

  useEffect(() => {
    if (!state.user) {
      return;
    }
    setRoomForm((current) => {
      const nextName = current.name.trim() === "" ? defaultRoomName(state.user) : current.name;
      const currentMaxMembers = Number.parseInt(current.maxMembers, 10) || defaultRoomMemberLimit;
      const nextMaxMembers = canCreateRoomSize(currentMaxMembers, state.user) ? current.maxMembers : String(defaultRoomMemberLimit);
      if (nextName === current.name && nextMaxMembers === current.maxMembers) {
        return current;
      }
      return { ...current, name: nextName, maxMembers: nextMaxMembers };
    });
  }, [state.user?.id, state.user?.username, state.user?.displayName, state.user?.membership]);

  function applyState(next: AppState) {
    setState(next);
    const nextSettingsSignature = `${next.apiBaseURL}|${next.interfaceName}|${next.p2pEnabled}`;
    if (settingsSignature.current !== nextSettingsSignature) {
      settingsSignature.current = nextSettingsSignature;
      setSettingsForm({
        apiBaseURL: next.apiBaseURL,
        p2pEnabled: next.p2pEnabled,
      });
    }

    const signature = `${next.connection.status}|${next.connection.message}|${next.connection.ip}`;
    if (connectionSignature.current && signature !== connectionSignature.current) {
      showNotice(
        `网络状态: ${statusText(next.connection.status)}${next.connection.message ? ` - ${next.connection.message}` : ""}`,
        next.connection.status === "error" ? "error" : "info",
      );
    }
    connectionSignature.current = signature;
  }

  function showNotice(text: string, tone: NoticeTone = "info") {
    if (noticeTimer.current !== null) {
      window.clearTimeout(noticeTimer.current);
    }
    setNotice({ id: Date.now(), text, tone });
    noticeTimer.current = window.setTimeout(() => setNotice(null), tone === "error" ? 4600 : 2600);
  }

  async function run(label: string, task: () => Promise<AppState>) {
    setBusy(label);
    try {
      const next = await task();
      applyState(next);
      showNotice(`${label}完成`, "success");
      return true;
    } catch (err) {
      const text = errorText(err);
      showNotice(text, "error");
      return false;
    } finally {
      setBusy("");
    }
  }

  async function handleAuthSubmit(event: FormEvent) {
    event.preventDefault();
    if (!canSubmitAuth) {
      showNotice(authMode === "login" ? "请输入邮箱和密码" : "请先完成账号规则", "error");
      return;
    }
    const ok =
      authMode === "login"
        ? await run("登录", () => Login({ email: loginForm.email, username: loginForm.email, password: loginForm.password }))
        : await run("注册", () =>
            Register({
              email: loginForm.email,
              username: loginForm.email,
              password: loginForm.password,
              displayName: loginForm.displayName,
            }),
          );
    if (ok) {
      setPage("room");
    }
  }

  async function handleCreateRoom(event: FormEvent) {
    event.preventDefault();
    if (!roomSizeAllowed) {
      showNotice("16人及以上房间需要会员账号", "error");
      return;
    }
    const ok = await run("创建房间", () =>
      CreateRoom({
        name: roomForm.name.trim(),
        maxMembers: selectedMaxMembers,
      }),
    );
    if (ok) {
      setRoomForm((value) => ({ ...value, name: state.user ? defaultRoomName(state.user) : "" }));
    }
  }

  async function handleJoinRoom(event: FormEvent) {
    event.preventDefault();
    const ok = await run("加入房间", () => JoinRoom({ roomPublicKey: roomForm.joinKey }));
    if (ok) {
      setRoomForm((value) => ({ ...value, joinKey: "" }));
    }
  }

  async function handleSendChat() {
    const message = chatText.trim();
    if (!currentRoom || !message) {
      return;
    }
    const pendingID = `pending-${Date.now()}`;
    setPendingChats((items) => [...items, { id: pendingID, message }]);
    setChatText("");
    setChatSending(true);
    try {
      const next = await SendRoomChat({ roomID: currentRoom.id, message });
      applyState(next);
    } catch (err) {
      const text = errorText(err);
      showNotice(text, "error");
    } finally {
      setPendingChats((items) => items.filter((item) => item.id !== pendingID));
      setChatSending(false);
    }
  }

  async function handleSaveSettings(event: FormEvent) {
    event.preventDefault();
    await run("保存设置", () => SetSettings({ apiBaseURL: settingsForm.apiBaseURL, interfaceName: state.interfaceName, p2pEnabled: settingsForm.p2pEnabled }));
  }

  async function handleCheckUpdates() {
    try {
      const next = await CheckForUpdates();
      applyState(next);
      showNotice("已开始检查更新", "info");
    } catch (err) {
      const text = errorText(err);
      showNotice(text, "error");
    }
  }

  function handleEmailChange(value: string) {
    setLoginForm((current) => ({ ...current, email: value.trim().toLowerCase() }));
  }

  async function copyText(text: string, label: string) {
    if (!text) {
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      showNotice(`${label}已复制`, "success");
    } catch {
      showNotice("复制失败", "error");
    }
  }

  function renderRoomSizePicker() {
    return (
      <div className="room-size-picker">
        {roomMemberOptions.map((value) => {
          const locked = !canCreateRoomSize(value, state.user);
          return (
            <button
              type="button"
              key={value}
              className={`room-size-option ${roomForm.maxMembers === String(value) ? "active" : ""}`}
              disabled={locked}
              onClick={() => setRoomForm((current) => ({ ...current, maxMembers: String(value) }))}
            >
              <strong>{value}</strong>
              <span>{locked ? "会员" : value <= freeRoomMemberLimit ? "普通" : "会员"}</span>
            </button>
          );
        })}
      </div>
    );
  }

  return (
    <div className="client-shell">
      <aside className="rail">
        <div className="player-card">
          <Avatar user={state.user} size="large" />
          <div className="player-meta">
            <span>{state.user ? `@${state.user.username}` : "未登录"}</span>
            <strong>{state.user ? userLabel(state.user.displayName, state.user.username) : "游客"}</strong>
          </div>
          <span className={`member-chip ${state.user ? membershipTone(state.user.membership) : "neutral"}`}>
            {state.user ? membershipLabel(state.user.membership) : "访客"}
          </span>
        </div>

        {state.user && (
          <div className="traffic-card">
            <div className="traffic-card-head">
              <span>中继流量</span>
              <strong>{trafficPeriodLabel(state.user.relayTrafficPeriod)}</strong>
            </div>
            <div className={`traffic-bar ${trafficCritical ? "bad" : ""}`}>
              <span style={{ width: `${trafficPercent}%` }} />
            </div>
            <div className="traffic-card-stats">
              <span>
                {formatBytes(state.user.relayTrafficUsedBytes)} / {formatBytes(state.user.relayTrafficQuotaBytes)}
              </span>
              <strong>剩余 {formatBytes(state.user.relayTrafficRemainingBytes)}</strong>
            </div>
            {state.user.relayTrafficExtraBytes > 0 && <small>加购 {formatBytes(state.user.relayTrafficExtraBytes)}</small>}
          </div>
        )}

        <nav className="rail-nav">
          <NavButton active={page === "account"} icon={<UserCircle size={18} />} label="账号" detail={state.user ? "已登录" : "未登录"} onClick={() => setPage("account")} />
          <NavButton active={page === "room"} icon={<Gamepad2 size={18} />} label="房间" detail={currentRoom?.name ?? "未加入"} onClick={() => setPage("room")} />
          <NavButton active={page === "settings"} icon={<Settings size={18} />} label="设置" detail={state.interfaceName} onClick={() => setPage("settings")} />
        </nav>

        <div className="rail-network">
          <div className="network-topline">
            <span className={`status-light ${connectionTone}`} />
            <strong>{statusText(state.connection.status)}</strong>
          </div>
          <span>{state.connection.ip || state.connection.message || "未连接"}</span>
          <div className="rail-actions">
            {currentRoom && connectionRunning ? (
              <button type="button" disabled={isBusy} onClick={() => run("断开虚拟网络", Disconnect)}>
                <Power size={15} />
                断开
              </button>
            ) : (
              <button type="button" disabled={isBusy || !currentRoom} onClick={() => currentRoom && run("连接虚拟网络", () => ConnectRoom(currentRoom.id))}>
                <PlugZap size={15} />
                连接
              </button>
            )}
          </div>
        </div>
      </aside>

      <section className="app-frame">
        <div className="integrated-chrome">
          <div className="chrome-drag">
            <div className="brand-lockup">
              <img className="brand-mark" src={appIcon} alt="" aria-hidden="true" />
              <div>
                <strong>LwyLink</strong>
                <span>{pageSubtitle(page, state.user, currentRoom?.name)}</span>
              </div>
            </div>
            <div className="chrome-status">
              <span className={`status-pill ${connectionTone}`}>{statusText(state.connection.status)}</span>
            </div>
          </div>
          <div className="window-controls">
            <button type="button" aria-label="最小化" onClick={WindowMinimise}>
              <Minus size={16} />
            </button>
            <button type="button" aria-label="最大化" onClick={WindowToggleMaximise}>
              <Square size={13} />
            </button>
            <button type="button" aria-label="关闭" className="close" onClick={() => void HideToTray()}>
              <X size={16} />
            </button>
          </div>
        </div>

        <main className="stage">
          <div className="stage-head">
            <div>
              <span className="eyebrow">{pageEyebrow(page)}</span>
              <h1>{pageTitle(page)}</h1>
            </div>
            <div className="stage-actions">
              {isBusy && (
                <span className="operation-busy">
                  <Spinner size="tiny" />
                  {busy}
                </span>
              )}
            </div>
          </div>

          {page === "account" && renderAccountPage()}
          {page === "room" && renderRoomPage()}
          {page === "settings" && renderSettingsPage()}
          {showUpdatePrompt && renderUpdatePrompt()}
          {notice && (
            <div className={`app-toast ${notice.tone}`} key={notice.id}>
              {notice.text}
            </div>
          )}
        </main>
      </section>
    </div>
  );

  function renderAccountPage() {
    if (!state.user) {
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
              <Metric label="服务器" value={serverHostFromAPI(settingsForm.apiBaseURL)} />
              <Metric label="网卡" value={state.interfaceName} />
              <Metric label="状态" value={statusText(state.connection.status)} />
            </div>
          </div>

          <form className="surface auth-form" onSubmit={handleAuthSubmit}>
            <div className="surface-head">
              <div>
                <span className="eyebrow">账号</span>
                <h2>{authMode === "login" ? "登录" : "注册"}</h2>
              </div>
              <LogIn size={18} />
            </div>
            <div className="form-stack">
              <div className="segmented">
                <button type="button" className={authMode === "login" ? "active" : ""} onClick={() => setAuthMode("login")}>
                  登录
                </button>
                <button type="button" className={authMode === "register" ? "active" : ""} onClick={() => setAuthMode("register")}>
                  注册
                </button>
              </div>
              <Field label="邮箱">
                <Input className="pretty-input" type="email" value={loginForm.email} onChange={(_, data) => handleEmailChange(data.value)} />
              </Field>
              {authMode === "register" && (
                <Field label="昵称">
                  <Input
                    className="pretty-input"
                    value={loginForm.displayName}
                    onChange={(_, data) => setLoginForm((current) => ({ ...current, displayName: data.value }))}
                  />
                </Field>
              )}
              <Field label="密码">
                <Input
                  className="pretty-input"
                  type="password"
                  value={loginForm.password}
                  onChange={(_, data) => setLoginForm((current) => ({ ...current, password: data.value }))}
                />
              </Field>
              {authMode === "register" && (
                <div className="rules">
                  <Rule ok={authRules.emailLength} text={`${emailMinLength}-${emailMaxLength} 位邮箱`} />
                  <Rule ok={authRules.emailFormat} text="请输入有效邮箱格式" />
                  <Rule ok={authRules.passwordLength} text={`${passwordMinLength}-${passwordMaxLength} 位密码`} />
                </div>
              )}
              <Button appearance="primary" icon={<LogIn size={16} />} type="submit" disabled={isBusy || !canSubmitAuth}>
                {authMode === "login" ? "进入客户端" : "创建账号"}
              </Button>
            </div>
          </form>
        </section>
      );
    }

    return (
      <section className="account-grid">
        <div className="surface account-session">
          <Avatar user={state.user} size="hero" />
          <div className="account-main">
            <span>当前会话</span>
            <h2>{userLabel(state.user.displayName, state.user.username)}</h2>
            <div className="account-tags">
              <span className={`member-chip ${membershipTone(state.user.membership)}`}>
                {membershipLabel(state.user.membership)}
              </span>
              <span className="member-chip neutral">{state.user.disabled ? "已禁用" : "账号正常"}</span>
            </div>
          </div>
          <div className="account-actions">
            <Button icon={<LogOut size={16} />} disabled={isBusy} onClick={() => run("退出登录", Logout)}>
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
            <Metric label="连接" value={statusText(state.connection.status)} />
            <Metric label="虚拟 IP" value={state.connection.ip || "-"} />
            <Metric label="房间在线" value={currentMember?.online ? "在线" : "未连接"} />
            <Metric label="当前房间" value={currentRoom?.name ?? "未加入"} wide />
          </div>
        </div>
      </section>
    );
  }

  function renderRoomPage() {
    if (!state.user) {
      return (
        <section className="empty-state">
          <UserCircle size={38} />
          <h2>请先登录</h2>
        </section>
      );
    }

    if (!currentRoom) {
      return (
        <section className="room-entry">
          <form className="surface command-panel" onSubmit={handleCreateRoom}>
            <div className="surface-head">
              <div>
                <span className="eyebrow">房主</span>
                <h2>创建房间</h2>
              </div>
              <Plus size={18} />
            </div>
            <div className="form-stack">
              <Field label="房间名">
                <Input
                  className="pretty-input"
                  placeholder={state.user ? defaultRoomName(state.user) : "玩家的房间"}
                  value={roomForm.name}
                  onChange={(_, data) => setRoomForm((current) => ({ ...current, name: data.value }))}
                />
              </Field>
              <Field label="人数上限">
                {renderRoomSizePicker()}
              </Field>
              {!roomSizeAllowed && <span className="form-hint">16 人及以上房间需要会员账号。</span>}
              <Button appearance="primary" icon={<Plus size={16} />} type="submit" disabled={isBusy || roomForm.name.trim() === "" || !roomSizeAllowed}>
                创建
              </Button>
            </div>
          </form>

          <form className="surface command-panel" onSubmit={handleJoinRoom}>
            <div className="surface-head">
              <div>
                <span className="eyebrow">成员</span>
                <h2>加入房间</h2>
              </div>
              <KeyRound size={18} />
            </div>
            <div className="form-stack">
              <Field label="房间密钥">
                <Textarea
                  className="pretty-textarea"
                  rows={8}
                  value={roomForm.joinKey}
                  onChange={(_, data) => setRoomForm((current) => ({ ...current, joinKey: data.value }))}
                />
              </Field>
              <Button appearance="primary" icon={<UserPlus size={16} />} type="submit" disabled={isBusy || roomForm.joinKey.trim() === ""}>
                加入
              </Button>
            </div>
          </form>
        </section>
      );
    }

    return (
      <section className="room-workspace">
        <div className="surface room-command">
          <div className="room-command-top">
            <div>
              <span className="eyebrow">当前房间</span>
              <h2>{currentRoom.name}</h2>
            </div>
            <span className={`status-pill ${currentRoom.status === "open" ? "good" : "bad"}`}>{currentRoom.status}</span>
          </div>

          <div className="room-metrics">
            <Metric label="成员" value={`${currentRoom.memberCount} / ${currentRoom.maxMembers}`} />
            <Metric label="在线" value={`${onlineCount} / ${members.length}`} />
            <Metric label="节点" value={currentRoom.nodeName || "未分配"} />
            <Metric label="中继剩余" value={state.user ? formatBytes(state.user.relayTrafficRemainingBytes) : "-"} />
          </div>

          <div className="room-network-line">
            <Network size={15} />
            <span>{currentRoom.networkCIDR}</span>
          </div>

          <div className="room-key">
            <span>房间密钥</span>
            <code>{currentRoom.roomPublicKey}</code>
            <Button icon={<Copy size={16} />} onClick={() => copyText(currentRoom.roomPublicKey, "房间密钥")}>
              复制
            </Button>
          </div>

          <div className="command-actions">
            {connectionRunning ? (
              <Button icon={<Power size={16} />} disabled={isBusy} onClick={() => run("断开虚拟网络", Disconnect)}>
                断开虚拟网络
              </Button>
            ) : (
              <Button appearance="primary" icon={<PlugZap size={16} />} disabled={isBusy} onClick={() => run("连接虚拟网络", () => ConnectRoom(currentRoom.id))}>
                启动虚拟网络
              </Button>
            )}
            {isOwner ? (
              <Button icon={<Trash2 size={16} />} disabled={isBusy} onClick={() => run("删除房间", () => DeleteRoom(currentRoom.id))}>
                删除房间
              </Button>
            ) : (
              <Button icon={<DoorOpen size={16} />} disabled={isBusy} onClick={() => run("退出房间", LeaveCurrentRoom)}>
                退出房间
              </Button>
            )}
          </div>
        </div>

        <div className="surface member-panel">
          <div className="surface-head">
            <h2>成员</h2>
            <span>{members.length} 人</span>
          </div>
          <div className="member-table">
            {members.map((member) => (
              <MemberRow
                key={member.userID}
                member={member}
                isSelf={member.userID === state.user?.id}
                p2pLabel={
                  connectionRunning && member.userID === state.user?.id && directPeerNames.length > 0
                    ? `已与 ${directPeerNames.join("、")} P2P 直连`
                    : connectionRunning && !!member.virtualIP && directPeerIPSet.has(member.virtualIP)
                      ? "与我 P2P 直连"
                      : undefined
                }
                canManage={isOwner && member.userID !== state.user?.id}
                disabled={isBusy}
                onKick={() => run("移出成员", () => KickMember({ userID: member.userID }))}
                onTransfer={() => run("转让房主", () => TransferOwner({ userID: member.userID }))}
              />
            ))}
          </div>
        </div>

        <div className="surface chat-panel">
          <div className="surface-head">
            <h2>聊天</h2>
            <MessageSquare size={18} />
          </div>
          <div className="chat-list" ref={chatListRef}>
            {chatMessages.map((message) => (
              <ChatBubble key={message.id} message={message} mine={message.userID === state.user?.id} />
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
            <Textarea
              className="pretty-textarea chat-input"
              rows={2}
              maxLength={500}
              value={chatText}
              onChange={(_, data) => setChatText(data.value)}
            />
            <Button
              appearance="primary"
              icon={chatSending ? <Spinner size="tiny" /> : <Send size={16} />}
              type="button"
              disabled={chatSending || chatText.trim() === ""}
              onClick={handleSendChat}
            >
              {chatSending ? "发送中" : "发送"}
            </Button>
          </div>
        </div>
      </section>
    );
  }

  function renderSettingsPage() {
    return (
      <section className="settings-grid">
        <form className="surface settings-panel" onSubmit={handleSaveSettings}>
          <div className="surface-head">
            <div>
              <span className="eyebrow">客户端</span>
              <h2>连接偏好</h2>
            </div>
            <SlidersHorizontal size={18} />
          </div>
          <div className="form-stack">
            <Field label="接口地址">
              <Input
                className="pretty-input"
                value={settingsForm.apiBaseURL}
                onChange={(_, data) => setSettingsForm((current) => ({ ...current, apiBaseURL: data.value }))}
              />
            </Field>
            <Switch
              label="启用 P2P 直连"
              checked={settingsForm.p2pEnabled}
              onChange={(_, data) => setSettingsForm((current) => ({ ...current, p2pEnabled: data.checked }))}
            />
            <div className="command-actions">
              <Button appearance="primary" type="submit" disabled={isBusy}>
                保存
              </Button>
            </div>
          </div>
        </form>

        <div className="surface announcement-panel">
          <div className="surface-head">
            <div>
              <span className="eyebrow">公告</span>
              <h2>官方信息</h2>
            </div>
            <Megaphone size={18} />
          </div>
          <div className="announcement-body">
            <p>{state.announcement || "官方 QQ 群：715516422"}</p>
          </div>
          <div className="settings-actions">
            <Button icon={<Copy size={16} />} onClick={() => copyText(state.announcement || "官方 QQ 群：715516422", "公告")}>
              复制
            </Button>
          </div>
        </div>

        <div className="surface update-panel">
          <div className="surface-head">
            <div>
              <span className="eyebrow">更新</span>
              <h2>客户端版本</h2>
            </div>
            <span className={`status-pill ${updateTone}`}>{updateStatusText(state.update.status)}</span>
          </div>
          <div className="data-grid">
            <Metric label="当前版本" value={state.update.currentVersion || "-"} />
            <Metric label="最新版本" value={state.update.latestVersion || "-"} />
            <Metric label="平台" value={state.update.platform || "windows-amd64"} />
            <Metric label="大小" value={state.update.sizeBytes ? formatBytes(state.update.sizeBytes) : "-"} />
            <Metric label="状态" value={state.update.message || updateStatusText(state.update.status)} wide />
          </div>
          <div className="settings-actions">
            <Button icon={<RefreshCw size={16} />} disabled={isBusy || state.update.status === "downloading"} onClick={handleCheckUpdates}>
              检查更新
            </Button>
            <Button
              appearance="primary"
              icon={<Power size={16} />}
              disabled={isBusy || state.update.status !== "ready"}
              onClick={() => run("启动更新", InstallPreparedUpdate)}
            >
              重启更新
            </Button>
          </div>
        </div>
      </section>
    );
  }

  function renderUpdatePrompt() {
    return (
      <div className="update-backdrop">
        <div className="update-dialog">
          <div className="surface-head">
            <div>
              <span className="eyebrow">Update Ready</span>
              <h2>新版本 {state.update.latestVersion} 已准备好</h2>
            </div>
            <span className={`status-pill ${state.update.mandatory ? "warn" : "good"}`}>{state.update.mandatory ? "强制" : "可选"}</span>
          </div>
          <div className="update-body">
            <div className="update-version-line">
              <span>当前 {state.update.currentVersion || "-"}</span>
              <span>目标 {state.update.latestVersion}</span>
              <span>{state.update.sizeBytes ? formatBytes(state.update.sizeBytes) : ""}</span>
            </div>
            {state.update.notes ? <p>{state.update.notes}</p> : <p>更新包已经在后台下载并完成校验。</p>}
          </div>
          <div className="update-actions">
            {!state.update.mandatory && (
              <Button disabled={isBusy} onClick={() => setDismissedUpdate(state.update.latestVersion)}>
                稍后
              </Button>
            )}
            <Button appearance="primary" icon={<Power size={16} />} disabled={isBusy} onClick={() => run("启动更新", InstallPreparedUpdate)}>
              立即重启更新
            </Button>
          </div>
        </div>
      </div>
    );
  }
}
