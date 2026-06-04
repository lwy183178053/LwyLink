import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import {
  Badge,
  Button,
  Divider,
  Field,
  Input,
  Spinner,
  Textarea,
} from "@fluentui/react-components";
import {
  CircleCheck,
  CircleX,
  Copy,
  Crown,
  DoorOpen,
  KeyRound,
  LogIn,
  LogOut,
  Network,
  PlugZap,
  Plus,
  Power,
  RefreshCw,
  Server,
  Settings,
  ShieldCheck,
  Trash2,
  UserMinus,
  UserPlus,
  Users,
  Wifi,
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
  SetSettings,
  TransferOwner,
} from "../wailsjs/go/main/App";
import { EventsOn } from "../wailsjs/runtime/runtime";
import type { AppState, Room, RoomMember } from "./types";
import { emptyState } from "./types";

const remoteServerHost = "64.83.34.53";
const remoteAPIBaseURL = `http://${remoteServerHost}:8080`;
const localAPIBaseURL = "http://127.0.0.1:8080";
const usernameMinLength = 3;
const usernameMaxLength = 32;
const passwordMinLength = 6;
const passwordMaxLength = 128;
const usernamePattern = /^[a-z0-9_-]+$/;

type LoginForm = {
  username: string;
  password: string;
  displayName: string;
};

type RoomForm = {
  name: string;
  maxMembers: string;
  joinKey: string;
};

type SettingsForm = {
  serverHost: string;
  apiBaseURL: string;
  interfaceName: string;
};

export default function App() {
  const [state, setState] = useState<AppState>(emptyState);
  const [authMode, setAuthMode] = useState<"login" | "register">("login");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [logs, setLogs] = useState<string[]>([]);
  const [loginForm, setLoginForm] = useState<LoginForm>({ username: "", password: "", displayName: "" });
  const [roomForm, setRoomForm] = useState<RoomForm>({
    name: "",
    maxMembers: "8",
    joinKey: "",
  });
  const [settingsForm, setSettingsForm] = useState<SettingsForm>({
    serverHost: serverHostFromAPI(emptyState.apiBaseURL),
    apiBaseURL: emptyState.apiBaseURL,
    interfaceName: emptyState.interfaceName,
  });
  const connectionSignature = useRef("");

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
  const onlineCount = members.filter((member) => member.online).length;
  const currentMember = members.find((member) => member.userID === state.user?.id);
  const isOwner = !!currentRoom && currentRoom.ownerUserID === state.user?.id;
  const canEnterRoom = !!state.user && !currentRoom;
  const connectionTone = statusTone(state.connection.status);
  const isBusy = busy !== "";
  const authRules = authRuleState(loginForm.username, loginForm.password);
  const canSubmitAuth = authMode === "login"
    ? loginForm.username.trim() !== "" && loginForm.password !== ""
    : authRules.usernameLength && authRules.usernameCharset && authRules.passwordLength;

  function applyState(next: AppState) {
    setState(next);
    setSettingsForm({
      serverHost: serverHostFromAPI(next.apiBaseURL),
      apiBaseURL: next.apiBaseURL,
      interfaceName: next.interfaceName,
    });

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
    } catch (err) {
      const text = errorText(err);
      setError(text);
      pushLog(`${label}失败: ${text}`);
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
    if (authMode === "login") {
      await run("登录", () => Login({ username: loginForm.username, password: loginForm.password }));
      return;
    }
    await run("注册", () =>
      Register({
        username: loginForm.username,
        password: loginForm.password,
        displayName: loginForm.displayName,
      }),
    );
  }

  function handleUsernameChange(value: string) {
    setLoginForm((current) => ({ ...current, username: value.trim().toLowerCase() }));
  }

  async function handleCreateRoom(event: FormEvent) {
    event.preventDefault();
    await run("创建房间", () =>
      CreateRoom({
        name: roomForm.name,
        maxMembers: Number.parseInt(roomForm.maxMembers, 10) || 8,
      }),
    );
    setRoomForm((value) => ({ ...value, name: "" }));
  }

  async function handleJoinRoom(event: FormEvent) {
    event.preventDefault();
    await run("加入房间", () => JoinRoom({ roomPublicKey: roomForm.joinKey }));
    setRoomForm((value) => ({ ...value, joinKey: "" }));
  }

  async function handleSaveSettings(event: FormEvent) {
    event.preventDefault();
    await run("保存设置", () => SetSettings({ apiBaseURL: settingsForm.apiBaseURL, interfaceName: settingsForm.interfaceName }));
  }

  function handleServerHostChange(value: string) {
    setSettingsForm((current) => ({
      ...current,
      serverHost: value,
      apiBaseURL: apiBaseURLFromHost(value),
    }));
  }

  function setRemoteServer() {
    setSettingsForm((current) => ({
      ...current,
      serverHost: remoteServerHost,
      apiBaseURL: remoteAPIBaseURL,
    }));
  }

  function setLocalServer() {
    setSettingsForm((current) => ({
      ...current,
      serverHost: "127.0.0.1",
      apiBaseURL: localAPIBaseURL,
    }));
  }

  async function copyKey(room: Room) {
    try {
      await navigator.clipboard.writeText(room.roomPublicKey);
      pushLog(`已复制房间 ${room.id} 公钥`);
    } catch (err) {
      setError(errorText(err));
    }
  }

  async function handleDeleteRoom(room: Room) {
    if (!window.confirm(`确认删除房间「${room.name}」？房间公钥会立即失效。`)) {
      return;
    }
    await run("删除房间", () => DeleteRoom(room.id));
  }

  async function handleLeaveRoom(room: Room) {
    if (!window.confirm(`确认退出房间「${room.name}」？`)) {
      return;
    }
    await run("退出房间", LeaveCurrentRoom);
  }

  async function handleKick(member: RoomMember) {
    if (!window.confirm(`确认踢出 ${member.displayName || member.username}？`)) {
      return;
    }
    await run("踢出成员", () => KickMember({ userID: member.userID }));
  }

  async function handleTransfer(member: RoomMember) {
    if (!window.confirm(`确认把房主转让给 ${member.displayName || member.username}？`)) {
      return;
    }
    await run("转让房主", () => TransferOwner({ userID: member.userID }));
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <strong>LwyV-GameNet</strong>
          <span>Windows Client</span>
        </div>

        <nav className="step-nav">
          <a href="#account-step">
            <LogIn size={16} />
            <span>1. 账号</span>
            <Badge color={state.user ? "success" : "warning"}>{state.user ? "已登录" : "未登录"}</Badge>
          </a>
          <a href="#room-step">
            <Network size={16} />
            <span>2. 房间入口</span>
            <Badge color={currentRoom ? "success" : "subtle"}>{currentRoom ? "已加入" : "待进入"}</Badge>
          </a>
          <a href="#status-step">
            <Users size={16} />
            <span>3. 房间状态</span>
            <Badge color={currentMember?.role === "owner" ? "important" : currentMember ? "informative" : "subtle"}>
              {currentRoom ? roleText(currentMember?.role || (isOwner ? "owner" : "member")) : "无房间"}
            </Badge>
          </a>
          <a href="#settings-step">
            <Settings size={16} />
            <span>4. 设置</span>
            <Badge color="informative">{serverHostFromAPI(state.apiBaseURL)}</Badge>
          </a>
        </nav>

        <div className="sidebar-status">
          <Badge appearance="filled" color={connectionTone}>{statusText(state.connection.status)}</Badge>
          <span>{currentRoom?.name || state.connection.message}</span>
        </div>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div>
            <h1>游戏虚拟组网</h1>
            <div className="muted">
              {state.user ? `${state.user.displayName || state.user.username} @ ${state.apiBaseURL}` : state.apiBaseURL}
            </div>
          </div>
          <div className="top-actions">
            {isBusy && <Spinner size="tiny" label={busy} />}
            {state.user && (
              <Button icon={<RefreshCw size={16} />} onClick={() => run("刷新状态", RefreshRooms)} disabled={isBusy}>
                刷新
              </Button>
            )}
          </div>
        </header>

        {error && <div className="error-bar">{error}</div>}

        <section id="account-step" className="workflow-section">
          <StepHeading
            number="1"
            icon={<LogIn size={18} />}
            title="账号"
            status={state.user ? "已登录" : authMode === "login" ? "登录" : "注册"}
          />
          <div className="account-layout">
            <div className="panel">
              <div className="panel-head">
                <h2>{state.user ? "当前账号" : authMode === "login" ? "登录账号" : "注册账号"}</h2>
                {!state.user && (
                  <div className="auth-tabs">
                    <Button appearance={authMode === "login" ? "primary" : "subtle"} icon={<LogIn size={16} />} onClick={() => setAuthMode("login")}>
                      登录
                    </Button>
                    <Button appearance={authMode === "register" ? "primary" : "subtle"} icon={<UserPlus size={16} />} onClick={() => setAuthMode("register")}>
                      注册
                    </Button>
                  </div>
                )}
              </div>
              {state.user ? (
                <div className="account-card">
                  <div className="avatar">{avatarText(state.user.displayName || state.user.username)}</div>
                  <div>
                    <strong>{state.user.displayName || state.user.username}</strong>
                    <span>@{state.user.username}</span>
                  </div>
                  <Button icon={<LogOut size={16} />} onClick={() => run("退出登录", Logout)} disabled={isBusy}>
                    退出
                  </Button>
                </div>
              ) : (
                <form className="form-stack" onSubmit={handleAuthSubmit}>
                  <Field
                    label="用户名"
                    validationState={authMode === "register" && loginForm.username ? fieldState(authRules.usernameLength && authRules.usernameCharset) : undefined}
                    validationMessage={authMode === "register" && loginForm.username ? usernameValidationText(authRules) : undefined}
                  >
                    <Input
                      value={loginForm.username}
                      maxLength={usernameMaxLength}
                      onChange={(_, data) => handleUsernameChange(data.value)}
                    />
                  </Field>
                  {authMode === "register" && (
                    <Field label="昵称">
                      <Input
                        value={loginForm.displayName}
                        onChange={(_, data) => setLoginForm({ ...loginForm, displayName: data.value })}
                      />
                    </Field>
                  )}
                  <Field
                    label="密码"
                    validationState={authMode === "register" && loginForm.password ? fieldState(authRules.passwordLength) : undefined}
                    validationMessage={authMode === "register" && loginForm.password ? passwordValidationText(authRules) : undefined}
                  >
                    <Input
                      type="password"
                      maxLength={passwordMaxLength}
                      value={loginForm.password}
                      onChange={(_, data) => setLoginForm({ ...loginForm, password: data.value })}
                    />
                  </Field>
                  {authMode === "register" && (
                    <div className="rule-grid">
                      <RuleItem ok={authRules.usernameLength} text={`${usernameMinLength}-${usernameMaxLength} 位用户名`} />
                      <RuleItem ok={authRules.usernameCharset} text="小写字母 / 数字 / _ / -" />
                      <RuleItem ok={authRules.passwordLength} text={`${passwordMinLength}-${passwordMaxLength} 位密码`} />
                      <RuleItem ok={true} text="不要求复杂度" />
                    </div>
                  )}
                  <Button appearance="primary" icon={authMode === "login" ? <LogIn size={16} /> : <UserPlus size={16} />} type="submit" disabled={isBusy || !canSubmitAuth}>
                    {authMode === "login" ? "登录" : "注册并登录"}
                  </Button>
                </form>
              )}
            </div>

            <div className="panel">
              <div className="panel-head">
                <h2>账号状态</h2>
                <Badge color={state.user ? "success" : "warning"}>{state.user ? "在线" : "离线"}</Badge>
              </div>
              <div className="metrics">
                <Metric label="当前房间" value={currentRoom?.name || "-"} />
                <Metric label="我的身份" value={currentRoom ? roleText(currentMember?.role || (isOwner ? "owner" : "member")) : "-"} />
                <Metric label="虚拟 IP" value={state.connection.ip || "-"} />
              </div>
              {!state.user && (
                <div className="account-note">
                  <ShieldCheck size={18} />
                  <span>同一账号只保留最后一次登录会话，用户名注册后不可重复。</span>
                </div>
              )}
            </div>
          </div>
        </section>

        <section id="room-step" className="workflow-section">
          <StepHeading
            number="2"
            icon={<Network size={18} />}
            title="创建或加入房间"
            status={currentRoom ? "已在房间中" : state.user ? "可进入房间" : "请先登录"}
          />

          {!state.user && (
            <div className="panel empty-panel">
              <ShieldCheck size={22} />
              <strong>请先登录账号</strong>
            </div>
          )}

          {state.user && currentRoom && (
            <div className="panel joined-panel">
              <div className="joined-icon"><Network size={22} /></div>
              <div>
                <strong>你已经在「{currentRoom.name}」中</strong>
                <span>同一账号同一时间只能处在一个房间里，需要联机时再手动启动虚拟网卡。</span>
              </div>
              <Button icon={<RefreshCw size={16} />} onClick={() => run("刷新状态", RefreshRooms)} disabled={isBusy}>
                刷新
              </Button>
            </div>
          )}

          {state.user && !currentRoom && (
            <div className="room-entry-layout">
              <div className="panel">
                <div className="panel-head">
                  <h2>创建房间</h2>
                  <Badge color="informative">自动成为房主</Badge>
                </div>
                <form className="form-stack" onSubmit={handleCreateRoom}>
                  <Field label="房间名">
                    <Input value={roomForm.name} onChange={(_, data) => setRoomForm({ ...roomForm, name: data.value })} />
                  </Field>
                  <Field label="最大人数">
                    <Input
                      type="number"
                      min={2}
                      max={64}
                      value={roomForm.maxMembers}
                      onChange={(_, data) => setRoomForm({ ...roomForm, maxMembers: data.value })}
                    />
                  </Field>
                  <Button appearance="primary" icon={<Plus size={16} />} type="submit" disabled={!canEnterRoom || isBusy}>
                    创建房间
                  </Button>
                </form>
              </div>

              <div className="panel">
                <div className="panel-head">
                  <h2>加入房间</h2>
                  <Badge color="subtle">使用房间公钥</Badge>
                </div>
                <form className="form-stack" onSubmit={handleJoinRoom}>
                  <Field label="房间公钥">
                    <Textarea
                      value={roomForm.joinKey}
                      onChange={(_, data) => setRoomForm({ ...roomForm, joinKey: data.value })}
                      resize="vertical"
                    />
                  </Field>
                  <Button icon={<KeyRound size={16} />} type="submit" disabled={!canEnterRoom || isBusy}>
                    加入房间
                  </Button>
                </form>
              </div>
            </div>
          )}
        </section>

        <section id="status-step" className="workflow-section">
          <StepHeading
            number="3"
            icon={<Users size={18} />}
            title="当前房间状态"
            status={currentRoom ? `${roleText(currentMember?.role || (isOwner ? "owner" : "member"))} · ${statusText(state.connection.status)}` : "未加入房间"}
          />

          {!currentRoom && (
            <div className="panel empty-panel">
              <Users size={22} />
              <strong>暂无当前房间</strong>
            </div>
          )}

          {currentRoom && (
            <div className="status-layout">
              <div className="panel room-panel">
                <div className="panel-head">
                  <h2>{currentRoom.name}</h2>
                  <Badge appearance="filled" color={isOwner ? "important" : "informative"}>
                    {roleText(currentMember?.role || (isOwner ? "owner" : "member"))}
                  </Badge>
                </div>
                <div className="room-summary">
                  <Metric label="人数" value={`${currentRoom.memberCount} / ${currentRoom.maxMembers}`} />
                  <Metric label="在线" value={`${onlineCount} / ${members.length || currentRoom.memberCount}`} />
                  <Metric label="虚拟网段" value={currentRoom.networkCIDR} />
                  <Metric label="节点" value={currentRoom.nodeName || currentRoom.nodeOverlayAddr || "-"} />
                  <Metric label="网络状态" value={statusText(state.connection.status)} />
                </div>
                <Divider />
                <div className="key-block">
                  <Field label="房间公钥">
                    <Textarea value={currentRoom.roomPublicKey} readOnly resize="vertical" />
                  </Field>
                </div>
                <div className="actions padded-actions">
                  {state.connection.status === "disconnected" ? (
                    <Button icon={<PlugZap size={16} />} onClick={() => run("启动虚拟网卡", () => ConnectRoom(currentRoom.id))} disabled={isBusy}>
                      启动虚拟网卡
                    </Button>
                  ) : (
                    <Button icon={<Power size={16} />} onClick={() => run("断开虚拟网卡", Disconnect)} disabled={isBusy}>
                      断开虚拟网卡
                    </Button>
                  )}
                  <Button icon={<Copy size={16} />} onClick={() => copyKey(currentRoom)}>
                    复制公钥
                  </Button>
                  {isOwner ? (
                    <Button icon={<Trash2 size={16} />} onClick={() => handleDeleteRoom(currentRoom)} disabled={isBusy}>
                      删除房间
                    </Button>
                  ) : (
                    <Button icon={<DoorOpen size={16} />} onClick={() => handleLeaveRoom(currentRoom)} disabled={isBusy}>
                      退出房间
                    </Button>
                  )}
                </div>
              </div>

              <div className="panel members-panel">
                <div className="panel-head">
                  <h2>玩家列表</h2>
                  <Badge color={onlineCount > 0 ? "success" : "subtle"}>{onlineCount} / {members.length} 在线</Badge>
                </div>
                <div className="member-list">
                  {members.map((member) => (
                    <div className="member-row" key={member.userID}>
                      <div className="member-avatar">{avatarText(member.displayName || member.username)}</div>
                      <div className="member-main">
                        <strong>{member.displayName || member.username}</strong>
                        <span>@{member.username} · ID {member.userID}</span>
                      </div>
                      <Badge className="role-badge" appearance="filled" color={member.role === "owner" ? "important" : "informative"}>
                        {roleText(member.role)}
                      </Badge>
                      <Badge className="online-badge" appearance="filled" color={member.online ? "success" : "subtle"}>
                        {member.online ? "在线" : "离线"}
                      </Badge>
                      <span className="member-ip">{member.online ? member.virtualIP || "未启动网卡" : formatTime(member.createdAt)}</span>
                      <div className="member-actions">
                        {isOwner && member.userID !== state.user?.id ? (
                          <>
                            <Button icon={<Crown size={15} />} onClick={() => handleTransfer(member)} disabled={isBusy}>
                              转让
                            </Button>
                            <Button icon={<UserMinus size={15} />} onClick={() => handleKick(member)} disabled={isBusy}>
                              踢出
                            </Button>
                          </>
                        ) : (
                          <span className="muted">-</span>
                        )}
                      </div>
                    </div>
                  ))}
                  {members.length === 0 && <div className="empty-cell">暂无成员</div>}
                </div>
              </div>
            </div>
          )}
        </section>

        <section id="settings-step" className="workflow-section">
          <StepHeading
            number="4"
            icon={<Settings size={18} />}
            title="设置"
            status={serverHostFromAPI(state.apiBaseURL)}
          />
          <div className="settings-layout">
            <div className="panel">
              <div className="panel-head">
                <h2>服务器与网卡</h2>
                <Server size={18} />
              </div>
              <form className="form-stack" onSubmit={handleSaveSettings}>
                <Field label="服务器 IP">
                  <Input value={settingsForm.serverHost} onChange={(_, data) => handleServerHostChange(data.value)} />
                </Field>
                <Field label="API 地址">
                  <Input
                    value={settingsForm.apiBaseURL}
                    onChange={(_, data) => setSettingsForm({ ...settingsForm, apiBaseURL: data.value })}
                  />
                </Field>
                <Field label="虚拟网卡名">
                  <Input
                    value={settingsForm.interfaceName}
                    onChange={(_, data) => setSettingsForm({ ...settingsForm, interfaceName: data.value })}
                  />
                </Field>
                <div className="actions no-pad">
                  <Button type="button" icon={<Server size={16} />} onClick={setRemoteServer}>
                    远程服务器
                  </Button>
                  <Button type="button" icon={<Network size={16} />} onClick={setLocalServer}>
                    本机测试
                  </Button>
                  <Button appearance="primary" icon={<Settings size={16} />} type="submit" disabled={isBusy}>
                    保存设置
                  </Button>
                </div>
              </form>
            </div>

            <div className="panel">
              <div className="panel-head">
                <h2>虚拟网络</h2>
                <Badge appearance="filled" color={connectionTone}>{statusText(state.connection.status)}</Badge>
              </div>
              <div className="connection-box">
                <Metric label="房间" value={state.connection.roomName || currentRoom?.name || "-"} />
                <Metric label="虚拟 IP" value={state.connection.ip || "-"} />
                <Metric label="网段" value={state.connection.networkCIDR || currentRoom?.networkCIDR || "-"} />
                <Metric label="节点" value={state.connection.overlayAddr || currentRoom?.nodeOverlayAddr || "-"} />
                <Metric label="网关" value={state.connection.gateway || "-"} />
                <Metric label="MTU" value={state.connection.mtu ? String(state.connection.mtu) : "-"} />
              </div>
            </div>

            <div className="panel logs-panel">
              <div className="panel-head">
                <h2>日志</h2>
                <Wifi size={18} />
              </div>
              <div className="log-list">
                {logs.map((item, index) => <div key={`${item}-${index}`}>{item}</div>)}
                {logs.length === 0 && <div className="muted">暂无日志</div>}
              </div>
            </div>
          </div>
        </section>
      </main>
    </div>
  );
}

function StepHeading(props: { number: string; icon: JSX.Element; title: string; status: string }) {
  return (
    <div className="step-heading">
      <div className="step-mark">
        <span>{props.number}</span>
        {props.icon}
      </div>
      <div>
        <h2>{props.title}</h2>
        <span>{props.status}</span>
      </div>
    </div>
  );
}

function RuleItem(props: { ok: boolean; text: string }) {
  return (
    <div className={`rule-item ${props.ok ? "ok" : "bad"}`}>
      {props.ok ? <CircleCheck size={15} /> : <CircleX size={15} />}
      <span>{props.text}</span>
    </div>
  );
}

function Metric(props: { label: string; value: string }) {
  return (
    <div className="metric">
      <span>{props.label}</span>
      <strong>{props.value}</strong>
    </div>
  );
}

function authRuleState(username: string, password: string) {
  const normalized = username.trim().toLowerCase();
  return {
    usernameLength: normalized.length >= usernameMinLength && normalized.length <= usernameMaxLength,
    usernameCharset: normalized === "" || usernamePattern.test(normalized),
    passwordLength: password.length >= passwordMinLength && password.length <= passwordMaxLength,
  };
}

function fieldState(ok: boolean): "success" | "error" {
  return ok ? "success" : "error";
}

function usernameValidationText(rules: ReturnType<typeof authRuleState>) {
  if (!rules.usernameLength) {
    return `用户名长度必须是 ${usernameMinLength}-${usernameMaxLength} 位`;
  }
  if (!rules.usernameCharset) {
    return "用户名只能包含小写字母、数字、下划线和短横线";
  }
  return "用户名格式可用";
}

function passwordValidationText(rules: ReturnType<typeof authRuleState>) {
  if (!rules.passwordLength) {
    return `密码长度必须是 ${passwordMinLength}-${passwordMaxLength} 位`;
  }
  return "密码长度可用";
}

function roleText(role: string) {
  if (role === "owner") {
    return "房主";
  }
  if (role === "member") {
    return "普通成员";
  }
  return "-";
}

function statusText(status: string) {
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

function statusTone(status: string): "success" | "warning" | "danger" | "informative" | "subtle" {
  switch (status) {
    case "connected":
      return "success";
    case "connecting":
    case "disconnecting":
      return "warning";
    case "error":
      return "danger";
    default:
      return "subtle";
  }
}

function formatTime(value: string) {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function avatarText(value: string) {
  const text = value.trim();
  return (text ? text.slice(0, 1) : "U").toUpperCase();
}

function apiBaseURLFromHost(value: string) {
  const raw = value.trim().replace(/\/+$/, "");
  if (!raw) {
    return remoteAPIBaseURL;
  }
  if (raw.startsWith("http://") || raw.startsWith("https://")) {
    return raw;
  }
  if (raw.includes(":")) {
    return `http://${raw}`;
  }
  return `http://${raw}:8080`;
}

function serverHostFromAPI(value: string) {
  try {
    return new URL(value).hostname || remoteServerHost;
  } catch {
    return remoteServerHost;
  }
}

function errorText(err: unknown) {
  if (err instanceof Error) {
    return err.message;
  }
  return String(err);
}
