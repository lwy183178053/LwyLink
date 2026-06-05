import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Button, Field, Input, Spinner, Textarea } from "@fluentui/react-components";
import {
  BadgeCheck,
  CircleX,
  Copy,
  DoorOpen,
  Gamepad2,
  KeyRound,
  LogIn,
  LogOut,
  MessageSquare,
  Minus,
  Network,
  PlugZap,
  Plus,
  Power,
  RefreshCw,
  Send,
  Settings,
  ShieldCheck,
  SlidersHorizontal,
  Square,
  Trash2,
  UserCircle,
  UserPlus,
  Users,
  X,
} from "lucide-react";
import {
  ConnectRoom,
  CreateRoom,
  CurrentState,
  DeleteRoom,
  Disconnect,
  JoinRoom,
  KickMember,
  LeaveCurrentRoom,
  Login,
  Logout,
  RefreshRooms,
  Register,
  SendRoomChat,
  SetSettings,
  TransferOwner,
} from "../wailsjs/go/main/App";
import { EventsOn, Quit, WindowMinimise, WindowToggleMaximise } from "../wailsjs/runtime/runtime";
import {
  authRuleState,
  errorText,
  formatDate,
  membershipLabel,
  membershipTone,
  pageEyebrow,
  pageSubtitle,
  pageTitle,
  passwordMaxLength,
  passwordMinLength,
  serverHostFromAPI,
  statusText,
  statusTone,
  type LoginForm,
  type PageKey,
  type RoomForm,
  type SettingsForm,
  usernameMaxLength,
  usernameMinLength,
  userLabel,
} from "./client-ui";
import { Avatar, ChatBubble, MemberRow, Metric, NavButton, Rule } from "./components";
import type { AppState } from "./types";
import { emptyState } from "./types";

export default function App() {
  const [page, setPage] = useState<PageKey>("account");
  const [state, setState] = useState<AppState>(emptyState);
  const [authMode, setAuthMode] = useState<"login" | "register">("login");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [logs, setLogs] = useState<string[]>([]);
  const [chatText, setChatText] = useState("");
  const [chatSending, setChatSending] = useState(false);
  const [loginForm, setLoginForm] = useState<LoginForm>({ username: "", password: "", displayName: "" });
  const [roomForm, setRoomForm] = useState<RoomForm>({ name: "", maxMembers: "8", joinKey: "" });
  const [settingsForm, setSettingsForm] = useState<SettingsForm>({
    apiBaseURL: emptyState.apiBaseURL,
  });
  const connectionSignature = useRef("");
  const settingsSignature = useRef("");
  const chatListRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    CurrentState()
      .then((next: AppState) => applyState(next))
      .catch((err: unknown) => setError(errorText(err)));
    const off = EventsOn("state-changed", (next: AppState) => applyState(next));
    return () => {
      if (typeof off === "function") {
        off();
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
  const isBusy = busy !== "";
  const authRules = authRuleState(loginForm.username, loginForm.password);
  const canSubmitAuth =
    authMode === "login"
      ? loginForm.username.trim() !== "" && loginForm.password !== ""
      : authRules.usernameLength && authRules.usernameCharset && authRules.passwordLength;

  useEffect(() => {
    if (page !== "room" || !chatListRef.current) {
      return;
    }
    chatListRef.current.scrollTop = chatListRef.current.scrollHeight;
  }, [chatMessages.length]);

  function applyState(next: AppState) {
    setState(next);
    const nextSettingsSignature = `${next.apiBaseURL}|${next.interfaceName}`;
    if (settingsSignature.current !== nextSettingsSignature) {
      settingsSignature.current = nextSettingsSignature;
      setSettingsForm({
        apiBaseURL: next.apiBaseURL,
      });
    }

    const signature = `${next.connection.status}|${next.connection.message}|${next.connection.ip}`;
    if (connectionSignature.current && signature !== connectionSignature.current) {
      pushLog(`网络状态: ${statusText(next.connection.status)}${next.connection.message ? ` - ${next.connection.message}` : ""}`);
    }
    connectionSignature.current = signature;
  }

  function pushLog(line: string) {
    const stamp = new Date().toLocaleTimeString();
    setLogs((items) => [`${stamp} ${line}`, ...items].slice(0, 80));
  }

  async function run(label: string, task: () => Promise<AppState>) {
    setBusy(label);
    setError("");
    try {
      const next = await task();
      applyState(next);
      pushLog(`${label}完成`);
      return true;
    } catch (err) {
      const text = errorText(err);
      setError(text);
      pushLog(`${label}失败: ${text}`);
      return false;
    } finally {
      setBusy("");
    }
  }

  async function handleAuthSubmit(event: FormEvent) {
    event.preventDefault();
    if (!canSubmitAuth) {
      setError(authMode === "login" ? "请输入用户名和密码" : "请先完成账号规则");
      return;
    }
    const ok =
      authMode === "login"
        ? await run("登录", () => Login({ username: loginForm.username, password: loginForm.password }))
        : await run("注册", () =>
            Register({
              username: loginForm.username,
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
    const ok = await run("创建房间", () =>
      CreateRoom({
        name: roomForm.name,
        maxMembers: Number.parseInt(roomForm.maxMembers, 10) || 8,
      }),
    );
    if (ok) {
      setRoomForm((value) => ({ ...value, name: "" }));
    }
  }

  async function handleJoinRoom(event: FormEvent) {
    event.preventDefault();
    const ok = await run("加入房间", () => JoinRoom({ roomPublicKey: roomForm.joinKey }));
    if (ok) {
      setRoomForm((value) => ({ ...value, joinKey: "" }));
    }
  }

  async function handleSendChat(event: FormEvent) {
    event.preventDefault();
    const message = chatText.trim();
    if (!currentRoom || !message) {
      return;
    }
    setChatSending(true);
    setError("");
    try {
      const next = await SendRoomChat({ roomID: currentRoom.id, message });
      applyState(next);
      setChatText("");
      pushLog("发送聊天完成");
    } catch (err) {
      const text = errorText(err);
      setError(text);
      pushLog(`发送聊天失败: ${text}`);
    } finally {
      setChatSending(false);
    }
  }

  async function handleSaveSettings(event: FormEvent) {
    event.preventDefault();
    await run("保存设置", () => SetSettings({ apiBaseURL: settingsForm.apiBaseURL, interfaceName: state.interfaceName }));
  }

  function handleUsernameChange(value: string) {
    setLoginForm((current) => ({ ...current, username: value.trim().toLowerCase() }));
  }

  async function copyText(text: string, label: string) {
    if (!text) {
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      pushLog(`${label}已复制`);
    } catch {
      setError("复制失败");
    }
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
            {currentRoom && (state.connection.status === "connected" || state.connection.status === "connecting") ? (
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
        <main className="stage">
          <div className="integrated-chrome">
            <div className="chrome-drag">
              <div className="brand-lockup">
                <span className="brand-mark">LG</span>
                <div>
                  <strong>LwyV GameNet</strong>
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
              <button type="button" aria-label="关闭" className="close" onClick={Quit}>
                <X size={16} />
              </button>
            </div>
          </div>

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
              {page !== "settings" && (
                <Button icon={<RefreshCw size={16} />} disabled={isBusy || !state.user} onClick={() => run("刷新房间", RefreshRooms)}>
                  刷新
                </Button>
              )}
            </div>
          </div>

          {error && (
            <div className="toast-error">
              <CircleX size={16} />
              <span>{error}</span>
            </div>
          )}

          {page === "account" && renderAccountPage()}
          {page === "room" && renderRoomPage()}
          {page === "settings" && renderSettingsPage()}
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
              <span>LwyV GameNet</span>
              <strong>玩家网络客户端</strong>
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
              <Field label="用户名">
                <Input value={loginForm.username} onChange={(_, data) => handleUsernameChange(data.value)} />
              </Field>
              {authMode === "register" && (
                <Field label="昵称">
                  <Input value={loginForm.displayName} onChange={(_, data) => setLoginForm((current) => ({ ...current, displayName: data.value }))} />
                </Field>
              )}
              <Field label="密码">
                <Input type="password" value={loginForm.password} onChange={(_, data) => setLoginForm((current) => ({ ...current, password: data.value }))} />
              </Field>
              {authMode === "register" && (
                <div className="rules">
                  <Rule ok={authRules.usernameLength} text={`${usernameMinLength}-${usernameMaxLength} 位用户名`} />
                  <Rule ok={authRules.usernameCharset} text="小写字母、数字、下划线或短横线" />
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
        <div className="surface account-hero">
          <Avatar user={state.user} size="hero" />
          <div className="account-main">
            <span>@{state.user.username}</span>
            <h2>{userLabel(state.user.displayName, state.user.username)}</h2>
            <div className="account-tags">
              <span className={`member-chip ${membershipTone(state.user.membership)}`}>
                <BadgeCheck size={14} />
                {membershipLabel(state.user.membership)}
              </span>
              <span className="member-chip neutral">{state.user.disabled ? "已禁用" : "账号正常"}</span>
            </div>
          </div>
          <div className="account-actions">
            <Button icon={<RefreshCw size={16} />} disabled={isBusy} onClick={() => run("刷新房间", RefreshRooms)}>
              刷新
            </Button>
            <Button icon={<LogOut size={16} />} disabled={isBusy} onClick={() => run("退出登录", Logout)}>
              退出
            </Button>
          </div>
        </div>

        <div className="surface data-panel">
          <div className="surface-head">
            <h2>账号信息</h2>
            <ShieldCheck size={18} />
          </div>
          <div className="data-grid">
            <Metric label="用户 ID" value={String(state.user.id)} />
            <Metric label="权限" value={state.user.role} />
            <Metric label="创建时间" value={formatDate(state.user.createdAt)} wide />
            <Metric label="当前房间" value={currentRoom?.name ?? "未加入"} wide />
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
                <Input value={roomForm.name} onChange={(_, data) => setRoomForm((current) => ({ ...current, name: data.value }))} />
              </Field>
              <Field label="人数上限">
                <Input type="number" min={2} max={64} value={roomForm.maxMembers} onChange={(_, data) => setRoomForm((current) => ({ ...current, maxMembers: data.value }))} />
              </Field>
              <Button appearance="primary" icon={<Plus size={16} />} type="submit" disabled={isBusy || roomForm.name.trim() === ""}>
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
                <Textarea rows={8} value={roomForm.joinKey} onChange={(_, data) => setRoomForm((current) => ({ ...current, joinKey: data.value }))} />
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
            {state.connection.status === "connected" || state.connection.status === "connecting" ? (
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
            {chatMessages.length === 0 && <div className="empty-chat">暂无消息</div>}
          </div>
          <form className="chat-form" onSubmit={handleSendChat}>
            <Textarea rows={2} maxLength={500} value={chatText} onChange={(_, data) => setChatText(data.value)} />
            <Button appearance="primary" icon={chatSending ? <Spinner size="tiny" /> : <Send size={16} />} type="submit" disabled={chatSending || chatText.trim() === ""}>
              {chatSending ? "发送中" : "发送"}
            </Button>
          </form>
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
                value={settingsForm.apiBaseURL}
                onChange={(_, data) => setSettingsForm((current) => ({ ...current, apiBaseURL: data.value }))}
              />
            </Field>
            <div className="command-actions">
              <Button appearance="primary" type="submit" disabled={isBusy}>
                保存
              </Button>
            </div>
          </div>
        </form>

        <div className="surface logs-panel">
          <div className="surface-head">
            <h2>本地日志</h2>
            <span>{logs.length} 条</span>
          </div>
          <div className="logs">
            {logs.length === 0 ? <span className="muted">暂无日志</span> : logs.map((line) => <code key={line}>{line}</code>)}
          </div>
        </div>
      </section>
    );
  }
}
