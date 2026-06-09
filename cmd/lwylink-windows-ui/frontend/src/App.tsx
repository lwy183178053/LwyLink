import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Button, Spinner } from "@fluentui/react-components";
import {
  Copy,
  Gamepad2,
  Minus,
  PlugZap,
  Power,
  Settings,
  Square,
  UserCircle,
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
  JoinRoomVoice,
  KickMember,
  LeaveRoomVoice,
  LeaveCurrentRoom,
  Login,
  Logout,
  Register,
  ResetPassword,
  SendRoomChat,
  SendEmailVerification,
  SetSettings,
  SetRoomVoiceMic,
  ChangePassword,
  TransferOwner,
  VoiceWebRTCICE,
} from "../wailsjs/go/main/App";
import { main as goModels } from "../wailsjs/go/models";
import { EventsOn, WindowMinimise, WindowToggleMaximise } from "../wailsjs/runtime/runtime";
import {
  authRuleState,
  canCreateRoomSize,
  defaultRoomMemberLimit,
  defaultRoomName,
  errorText,
  formatBytes,
  isConnectionRunning,
  membershipLabel,
  membershipTone,
  pageEyebrow,
  pageSubtitle,
  pageTitle,
  passwordMinLength,
  roomCapacityLimit,
  roomMemberOptions,
  roomSizeTierLabel,
  statusText,
  statusTone,
  trafficPeriodLabel,
  trafficUsedPercent,
  updateStatusText,
  type LoginForm,
  type PageKey,
  type RoomForm,
  type SettingsForm,
  type VoiceDevice,
  userLabel,
} from "./client-ui";
import { AccountPage, Avatar, NavButton, RoomCommsPanel, RoomControlPanel, RoomEntryPage, RoomPlayersPanel, SettingsPage } from "./components";
import appIcon from "./assets/lwylink-icon.png";
import { WebRTCVoiceSession, listVoiceDevices, type WebRTCVoiceSignal, type WebRTCVoiceStats } from "./webrtcVoice";
import type { AppState, VoiceSettings } from "./types";
import { emptyState } from "./types";

type NoticeTone = "info" | "success" | "error";
type PendingChat = {
  id: string;
  message: string;
};
export default function App() {
  const [page, setPage] = useState<PageKey>("account");
  const [state, setState] = useState<AppState>(emptyState);
  const [authMode, setAuthMode] = useState<"login" | "register" | "reset">("login");
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState<{ id: number; text: string; tone: NoticeTone } | null>(null);
  const [chatText, setChatText] = useState("");
  const [pendingChats, setPendingChats] = useState<PendingChat[]>([]);
  const [chatSending, setChatSending] = useState(false);
  const [voiceActive, setVoiceActive] = useState(false);
  const [voiceMicActive, setVoiceMicActive] = useState(false);
  const [voiceConnectPrompt, setVoiceConnectPrompt] = useState(false);
  const [voiceRTCStats, setVoiceRTCStats] = useState<WebRTCVoiceStats[]>([]);
  const [voiceInputLevel, setVoiceInputLevel] = useState(0);
  const [voiceDevices, setVoiceDevices] = useState<{ inputs: VoiceDevice[]; outputs: VoiceDevice[] }>({ inputs: [], outputs: [] });
  const [loginForm, setLoginForm] = useState<LoginForm>({
    email: "",
    password: "",
    displayName: "",
    emailCode: "",
    resetCode: "",
    resetPassword: "",
    oldPassword: "",
    newPassword: "",
  });
  const [roomForm, setRoomForm] = useState<RoomForm>({ name: "", maxMembers: String(defaultRoomMemberLimit), joinKey: "" });
  const [settingsForm, setSettingsForm] = useState<SettingsForm>({
    apiBaseURL: emptyState.apiBaseURL,
    p2pEnabled: emptyState.p2pEnabled,
    voiceSettings: emptyState.voiceSettings,
  });
  const connectionSignature = useRef("");
  const settingsSignature = useRef("");
  const chatListRef = useRef<HTMLDivElement | null>(null);
  const noticeTimer = useRef<number | null>(null);
  const webrtcVoiceRef = useRef<WebRTCVoiceSession | null>(null);

  useEffect(() => {
    CurrentState()
      .then((next: AppState) => applyState(next))
      .catch((err: unknown) => showNotice(errorText(err), "error"));
    const off = EventsOn("state-changed", (next: AppState) => applyState(next));
    const offWebRTCVoice = EventsOn("voice-webrtc-signal", (signal: WebRTCVoiceSignal) => {
      void webrtcVoiceRef.current?.handleSignal(signal);
    });
    return () => {
      if (typeof off === "function") {
        off();
      }
      if (typeof offWebRTCVoice === "function") {
        offWebRTCVoice();
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
  const networkDisplayIP = state.connection.ip || currentRoom?.networkCIDR || "";
  const directPeerIPSet = useMemo(() => new Set(state.connection.directPeerIPs ?? []), [state.connection.directPeerIPs]);
  const directPeerNames = useMemo(
    () =>
      members
        .filter((member) => member.virtualIP && directPeerIPSet.has(member.virtualIP))
        .map((member) => userLabel(member.displayName, member.username)),
    [members, directPeerIPSet],
  );
  const showUpdatePrompt =
    state.update.status === "ready" &&
    state.update.latestVersion !== "";
  const isBusy = busy !== "";
  const authRules = authRuleState(loginForm.email, loginForm.password);
  const canSubmitReset = authRules.emailFormat && loginForm.resetCode.trim().length === 6 && loginForm.resetPassword.length >= passwordMinLength;
  const canSubmitAuth =
    authMode === "login"
      ? loginForm.email.trim() !== "" && loginForm.password !== ""
      : authMode === "register"
        ? authRules.emailLength && authRules.emailFormat && authRules.passwordLength && loginForm.emailCode.trim().length === 6
        : canSubmitReset;
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
    const selfInVoice = !!state.user && state.voice.participants.some((item) => item.userID === state.user?.id);
    if ((!selfInVoice || !connectionRunning) && voiceActive) {
      void stopLocalVoice(connectionRunning === false && selfInVoice);
    }
  }, [state.voice.updatedAt, state.voice.participantCount, state.user?.id, connectionRunning, voiceActive]);

  useEffect(() => {
    return () => {
      void stopLocalVoice(false);
    };
  }, [currentRoom?.id, state.user?.id]);

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
  }, [state.user?.id, state.user?.username, state.user?.displayName, state.user?.membership, state.user?.role]);

  useEffect(() => {
    if (page !== "settings") {
      return;
    }
    void refreshVoiceDevices(false);
  }, [page]);

  useEffect(() => {
    webrtcVoiceRef.current?.updateSettings(settingsForm.voiceSettings);
  }, [settingsForm.voiceSettings]);

  useEffect(() => {
    if (!voiceActive) {
      return;
    }
    void webrtcVoiceRef.current?.syncMembers(state.voice.participants);
  }, [state.voice.updatedAt, state.voice.participantCount, voiceActive]);

  function applyState(next: AppState) {
    const normalized = normalizeAppState(next);
    setState(normalized);
    const nextSettingsSignature = `${normalized.apiBaseURL}|${normalized.interfaceName}|${normalized.p2pEnabled}|${JSON.stringify(normalized.voiceSettings)}`;
    if (settingsSignature.current !== nextSettingsSignature) {
      settingsSignature.current = nextSettingsSignature;
      setSettingsForm({
        apiBaseURL: normalized.apiBaseURL,
        p2pEnabled: normalized.p2pEnabled,
        voiceSettings: normalized.voiceSettings,
      });
    }

    const message = visibleConnectionMessage(normalized.connection.message);
    const signature = `${normalized.connection.status}|${message}|${normalized.connection.ip}`;
    if (connectionSignature.current && signature !== connectionSignature.current && message) {
      showNotice(`网络状态: ${statusText(normalized.connection.status)} - ${message}`, normalized.connection.status === "error" ? "error" : "info");
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
      showNotice(authMode === "login" ? "请输入邮箱和密码" : authMode === "register" ? "请先完成账号规则" : "请填写邮箱、验证码和新密码", "error");
      return;
    }
    const ok =
      authMode === "login"
        ? await run("登录", () => Login({ email: loginForm.email, password: loginForm.password }))
        : authMode === "register"
          ? await run("注册", () =>
              Register({
                email: loginForm.email,
                password: loginForm.password,
                displayName: loginForm.displayName,
                emailCode: loginForm.emailCode,
              }),
            )
          : await run("重置密码", () => ResetPassword({ email: loginForm.email, emailCode: loginForm.resetCode, newPassword: loginForm.resetPassword }));
    if (ok) {
      setLoginForm((current) => ({
        ...current,
        password: authMode === "reset" ? current.resetPassword : current.password,
        emailCode: "",
        resetCode: "",
        resetPassword: "",
        displayName: authMode === "register" ? "" : current.displayName,
      }));
      setAuthMode("login");
      if (authMode !== "reset") {
        setPage("account");
      }
    }
  }

  async function handleCreateRoom(event: FormEvent) {
    event.preventDefault();
    if (!roomSizeAllowed) {
      showNotice(`当前账号最多可创建 ${roomCapacityLimit(state.user)} 人房间`, "error");
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

  async function connectCurrentRoom() {
    if (!currentRoom) {
      return false;
    }
    return run("连接虚拟网络", () => ConnectRoom(currentRoom.id));
  }

  async function disconnectCurrentRoom() {
    await stopLocalVoice(true);
    return run("断开虚拟网络", Disconnect);
  }

  async function handleJoinVoice() {
    if (!currentRoom || !state.user) {
      return;
    }
    if (voiceActive) {
      await handleLeaveVoice();
      return;
    }
    if (!connectionRunning) {
      setVoiceConnectPrompt(true);
      return;
    }
    await startVoiceSession();
  }

  async function startVoiceSession() {
    if (!currentRoom || !state.user) {
      return;
    }
    let joinedServerVoice = false;
    try {
      const next = await JoinRoomVoice(currentRoom.id);
      joinedServerVoice = true;
      applyState(next);
      setVoiceActive(true);
      setVoiceMicActive(false);
      await startWebRTCVoice(next);
      showNotice("已加入高清语音", "success");
    } catch (err) {
      await stopWebRTCVoice();
      setVoiceActive(false);
      setVoiceMicActive(false);
      if (joinedServerVoice && currentRoom) {
        try {
          const next = await LeaveRoomVoice(currentRoom.id);
          applyState(next);
        } catch {
          // The original startup error is more useful to the user than cleanup noise.
        }
      }
      showNotice(errorText(err), "error");
    }
  }

  async function handleConnectAndJoinVoice() {
    setVoiceConnectPrompt(false);
    const ok = await connectCurrentRoom();
    if (ok) {
      await startVoiceSession();
    }
  }

  async function handleLeaveVoice() {
    if (!currentRoom) {
      return;
    }
    await stopLocalVoice(true);
  }

  async function stopLocalVoice(notifyServer: boolean) {
    const roomID = currentRoom?.id;
    setVoiceActive(false);
    setVoiceMicActive(false);
    setVoiceRTCStats([]);
    setVoiceInputLevel(0);
    await stopWebRTCVoice();
    if (notifyServer && roomID) {
      try {
        const next = await LeaveRoomVoice(roomID);
        applyState(next);
      } catch (err) {
        showNotice(errorText(err), "error");
      }
    }
  }

  async function startWebRTCVoice(nextState: AppState) {
    if (!currentRoom || !state.user) {
      return;
    }
    let iceServers: RTCIceServer[] = [];
    try {
      iceServers = await VoiceWebRTCICE();
    } catch (err) {
      showNotice(`获取语音网络配置失败，已使用默认配置：${errorText(err)}`, "info");
    }
    const session = new WebRTCVoiceSession(currentRoom, state.user.id, state.voiceSettings, iceServers, {
      onError: (message) => showNotice(message, "error"),
      onStats: (stats) => {
        setVoiceRTCStats(stats);
        if (stats.length > 0) {
          (globalThis as any).__lwylinkWebRTCVoiceStats = stats;
        }
      },
      onInputLevel: setVoiceInputLevel,
    });
    try {
      webrtcVoiceRef.current = session;
      await session.start(nextState.voice.participants ?? []);
    } catch (err) {
      webrtcVoiceRef.current = null;
      await session.close();
      throw err;
    }
  }

  async function stopWebRTCVoice() {
    const session = webrtcVoiceRef.current;
    webrtcVoiceRef.current = null;
    await session?.close();
  }

  async function handleToggleVoiceMic() {
    if (!currentRoom || !webrtcVoiceRef.current) {
      showNotice("请先加入语音", "error");
      return;
    }
    const nextSpeaking = !voiceMicActive;
    try {
      const next = await SetRoomVoiceMic(currentRoom.id, nextSpeaking);
      webrtcVoiceRef.current?.setMicEnabled(nextSpeaking);
      setVoiceMicActive(nextSpeaking);
      if (!nextSpeaking) {
        setVoiceInputLevel(0);
      }
      applyState(next);
    } catch (err) {
      showNotice(errorText(err), "error");
    }
  }

  async function handleSaveSettings(event: FormEvent) {
    event.preventDefault();
    await run("保存设置", () =>
      SetSettings(goModels.SettingsInput.createFrom({
        apiBaseURL: settingsForm.apiBaseURL,
        interfaceName: state.interfaceName,
        p2pEnabled: settingsForm.p2pEnabled,
        voiceSettings: settingsForm.voiceSettings,
      })),
    );
  }

  async function refreshVoiceDevices(notify = true) {
    try {
      const devices = await listVoiceDevices();
      setVoiceDevices(devices);
      if (notify) {
        showNotice("语音设备已刷新", "success");
      }
    } catch (err) {
      showNotice(errorText(err), "error");
    }
  }

  function updateVoiceSettings(patch: Partial<VoiceSettings>) {
    setSettingsForm((current) => ({
      ...current,
      voiceSettings: {
        ...current.voiceSettings,
        ...patch,
      },
    }));
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

  async function handleSendEmailCode(purpose: "register" | "password_reset") {
    if (loginForm.email.trim() === "") {
      showNotice("请先填写邮箱", "error");
      return;
    }
    await run("发送验证码", () => SendEmailVerification({ email: loginForm.email, purpose }));
  }

  async function handleChangePassword(event: FormEvent) {
    event.preventDefault();
    if (!loginForm.oldPassword || !loginForm.newPassword) {
      showNotice("请填写当前密码和新密码", "error");
      return;
    }
    const ok = await run("修改密码", () => ChangePassword({ oldPassword: loginForm.oldPassword, newPassword: loginForm.newPassword }));
    if (ok) {
      setLoginForm((current) => ({ ...current, oldPassword: "", newPassword: "" }));
      setPage("account");
      setAuthMode("login");
      showNotice("密码已修改，请重新登录", "info");
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
              <span>{roomSizeTierLabel(value)}</span>
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
          {networkDisplayIP && <span>{networkDisplayIP}</span>}
          <div className="rail-actions">
            {currentRoom && connectionRunning ? (
              <button type="button" disabled={isBusy} onClick={disconnectCurrentRoom}>
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
          {voiceConnectPrompt && (
            <div className="modal-scrim" role="presentation">
              <div className="voice-connect-modal" role="dialog" aria-modal="true" aria-labelledby="voice-connect-title">
                <button className="modal-close" type="button" aria-label="关闭" onClick={() => setVoiceConnectPrompt(false)}>
                  <X size={16} />
                </button>
                <div className="modal-symbol">
                  <PlugZap size={22} />
                </div>
                <div>
                  <span className="eyebrow">语音需要先组网</span>
                  <h2 id="voice-connect-title">启动虚拟网络后加入语音</h2>
                  <p>加入语音前需要先连接虚拟网络，用于确认房间在线状态；语音媒体会使用 WebRTC 自动选择 P2P 或 TURN 路径。</p>
                </div>
                <div className="modal-actions">
                  <Button type="button" onClick={() => setVoiceConnectPrompt(false)}>
                    先不加入
                  </Button>
                  <Button appearance="primary" type="button" icon={<PlugZap size={16} />} disabled={isBusy || !currentRoom} onClick={handleConnectAndJoinVoice}>
                    启动并加入
                  </Button>
                </div>
              </div>
            </div>
          )}
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
    return (
      <AccountPage
        user={state.user}
        authMode={authMode}
        loginForm={loginForm}
        authRules={authRules}
        apiBaseURL={settingsForm.apiBaseURL}
        interfaceName={state.interfaceName}
        connection={state.connection}
        currentRoom={currentRoom}
        currentMember={currentMember}
        canSubmitAuth={canSubmitAuth}
        isBusy={isBusy}
        onAuthModeChange={setAuthMode}
        onAuthSubmit={handleAuthSubmit}
        onEmailChange={handleEmailChange}
        onLoginFormChange={(patch) => setLoginForm((current) => ({ ...current, ...patch }))}
        onSendEmailCode={handleSendEmailCode}
        onChangePassword={handleChangePassword}
        onLogout={() => run("退出登录", Logout)}
      />
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
        <RoomEntryPage
          form={roomForm}
          defaultName={state.user ? defaultRoomName(state.user) : "玩家的房间"}
          roomSizePicker={renderRoomSizePicker()}
          roomSizeAllowed={roomSizeAllowed}
          capacityLimit={roomCapacityLimit(state.user)}
          disabled={isBusy}
          onCreateRoom={handleCreateRoom}
          onJoinRoom={handleJoinRoom}
          onRoomNameChange={(value) => setRoomForm((current) => ({ ...current, name: value }))}
          onJoinKeyChange={(value) => setRoomForm((current) => ({ ...current, joinKey: value }))}
        />
      );
    }

    return (
      <section className="room-workspace">
        <RoomControlPanel
          room={currentRoom}
          connection={state.connection}
          connectionRunning={connectionRunning}
          isOwner={isOwner}
          disabled={isBusy}
          onCopyKey={() => copyText(currentRoom.roomPublicKey, "房间密钥")}
          onConnect={connectCurrentRoom}
          onDeleteRoom={() => run("删除房间", () => DeleteRoom(currentRoom.id))}
          onLeaveRoom={() => run("退出房间", LeaveCurrentRoom)}
        />
        <RoomCommsPanel
          room={currentRoom}
          voiceState={state.voice}
          voiceActive={voiceActive}
          voiceMicActive={voiceMicActive}
          voiceRTCStats={voiceRTCStats}
          voiceInputLevel={voiceInputLevel}
          disabled={isBusy}
          onJoinVoice={handleJoinVoice}
          onToggleVoiceMic={handleToggleVoiceMic}
          onLeaveVoice={handleLeaveVoice}
          chatMessages={chatMessages}
          pendingChats={pendingChats}
          currentUserID={state.user?.id}
          chatListRef={chatListRef}
          chatText={chatText}
          chatSending={chatSending}
          onChatTextChange={setChatText}
          onSendChat={handleSendChat}
        />
        <RoomPlayersPanel
          members={members}
          currentUserID={state.user?.id}
          onlineCount={onlineCount}
          connectionRunning={connectionRunning}
          directPeerNames={directPeerNames}
          directPeerIPSet={directPeerIPSet}
          voiceState={state.voice}
          isOwner={isOwner}
          disabled={isBusy}
          onKick={(userID) => run("移出成员", () => KickMember({ userID }))}
          onTransfer={(userID) => run("转让房主", () => TransferOwner({ userID }))}
        />
      </section>
    );
  }

  function renderSettingsPage() {
    return (
      <SettingsPage
        form={settingsForm}
        update={state.update}
        announcement={state.announcement}
        voiceDevices={voiceDevices}
        disabled={isBusy}
        onSubmit={handleSaveSettings}
        onAPIBaseURLChange={(value) => setSettingsForm((current) => ({ ...current, apiBaseURL: value }))}
        onP2PEnabledChange={(value) => setSettingsForm((current) => ({ ...current, p2pEnabled: value }))}
        onVoiceSettingsChange={updateVoiceSettings}
        onCheckUpdates={handleCheckUpdates}
        onInstallUpdate={() => run("启动更新", InstallPreparedUpdate)}
        onCopyAnnouncement={() => copyText(state.announcement || "官方 QQ 群：715516422", "公告")}
      />
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
            <span className="status-pill warn">强制更新</span>
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
            <Button appearance="primary" icon={<Power size={16} />} disabled={isBusy} onClick={() => run("启动更新", InstallPreparedUpdate)}>
              立即重启更新
            </Button>
          </div>
        </div>
      </div>
    );
  }
}

function normalizeAppState(next: AppState): AppState {
  return {
    ...emptyState,
    ...next,
    rooms: Array.isArray(next.rooms) ? next.rooms : [],
    members: Array.isArray(next.members) ? next.members : [],
    chatMessages: Array.isArray(next.chatMessages) ? next.chatMessages : [],
    voice: {
      ...emptyState.voice,
      ...(next.voice ?? emptyState.voice),
      participants: Array.isArray(next.voice?.participants) ? next.voice.participants : [],
    },
    voiceSettings: {
      ...emptyState.voiceSettings,
      ...(next.voiceSettings ?? emptyState.voiceSettings),
    },
    connection: {
      ...emptyState.connection,
      ...(next.connection ?? emptyState.connection),
      directPeerIPs: Array.isArray(next.connection?.directPeerIPs) ? next.connection.directPeerIPs : [],
      transportDiagnostics: Array.isArray(next.connection?.transportDiagnostics) ? next.connection.transportDiagnostics : [],
    },
    update: {
      ...emptyState.update,
      ...(next.update ?? emptyState.update),
    },
    announcement: next.announcement || emptyState.announcement,
  };
}

function visibleConnectionMessage(message: string) {
  const text = message.trim();
  if (text.includes("房间事件流")) {
    return "";
  }
  return text;
}
