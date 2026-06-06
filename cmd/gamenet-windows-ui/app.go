package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/account"
	"github.com/LwyV-Labs/LwyLink/internal/client"
	"github.com/LwyV-Labs/LwyLink/internal/overlay"
	"github.com/LwyV-Labs/LwyLink/internal/room"
	"github.com/LwyV-Labs/LwyLink/internal/tun"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultAPIBaseURL = "http://64.83.34.53:8080"
	defaultIfName     = "LwyLink"
	maxChatMessages   = 120

	statusDisconnected  = "disconnected"
	statusConnecting    = "connecting"
	statusConnected     = "connected"
	statusDisconnecting = "disconnecting"
	statusError         = "error"
)

var AppVersion = "0.1.0"

type App struct {
	ctx context.Context

	mu         sync.Mutex
	api        *client.API
	apiBaseURL string
	ifName     string

	user         *account.PublicUser
	rooms        []room.Room
	currentRoom  *room.Room
	members      []room.MemberView
	chatMessages []room.ChatMessage

	connectSeq    int64
	connectCancel context.CancelFunc
	connection    ConnectionState

	roomEventsSeq    int64
	roomEventsCancel context.CancelFunc

	updateSeq          int64
	preparedUpdatePath string
	update             UpdateState

	tray               *trayController
	allowQuit          bool
	trayNoticeShown    bool
	notificationsReady bool
}

type SettingsInput struct {
	APIBaseURL    string `json:"apiBaseURL"`
	InterfaceName string `json:"interfaceName"`
}

type RegisterInput struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

type LoginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type CreateRoomInput struct {
	Name       string `json:"name"`
	MaxMembers int    `json:"maxMembers"`
}

type JoinRoomInput struct {
	RoomPublicKey string `json:"roomPublicKey"`
}

type SendChatInput struct {
	RoomID  int64  `json:"roomID"`
	Message string `json:"message"`
}

type MemberActionInput struct {
	UserID int64 `json:"userID"`
}

type AppState struct {
	APIBaseURL    string              `json:"apiBaseURL"`
	InterfaceName string              `json:"interfaceName"`
	User          *account.PublicUser `json:"user,omitempty"`
	Rooms         []room.Room         `json:"rooms"`
	CurrentRoom   *room.Room          `json:"currentRoom,omitempty"`
	Members       []room.MemberView   `json:"members"`
	ChatMessages  []room.ChatMessage  `json:"chatMessages"`
	Connection    ConnectionState     `json:"connection"`
	Update        UpdateState         `json:"update"`
}

type ConnectionState struct {
	Status      string `json:"status"`
	Message     string `json:"message"`
	RoomID      int64  `json:"roomID"`
	RoomName    string `json:"roomName"`
	NetworkCIDR string `json:"networkCIDR"`
	OverlayAddr string `json:"overlayAddr"`
	IP          string `json:"ip"`
	Gateway     string `json:"gateway"`
	MTU         int    `json:"mtu"`
}

func NewApp() *App {
	return &App{
		api:        client.NewAPI(defaultAPIBaseURL),
		apiBaseURL: defaultAPIBaseURL,
		ifName:     defaultIfName,
		connection: ConnectionState{Status: statusDisconnected, Message: "未连接"},
		update:     UpdateState{Status: updateStatusIdle, CurrentVersion: AppVersion},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.initializeNotifications(ctx)
	a.registerTray()
	if err := tun.EnsureWintunDLL(); err != nil {
		a.mu.Lock()
		a.connection = ConnectionState{Status: statusError, Message: friendlyConnectionError(err)}
		a.commitStateLocked()
		return
	}
	a.mu.Lock()
	a.startUpdateCheckLocked("正在检查更新")
	a.commitStateLocked()
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	a.cancelConnectionLocked()
	a.cancelRoomEventsLocked()
	a.mu.Unlock()
	a.cleanupNotifications(ctx)
	a.stopTray()
}

func (a *App) CurrentState() AppState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stateLocked()
}

func (a *App) commitStateLocked() AppState {
	state := a.stateLocked()
	a.mu.Unlock()
	a.emitState(state)
	return state
}

func (a *App) SetSettings(input SettingsInput) (AppState, error) {
	apiBaseURL := strings.TrimRight(strings.TrimSpace(input.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIBaseURL
	}
	ifName := strings.TrimSpace(input.InterfaceName)
	if ifName == "" {
		ifName = defaultIfName
	}

	a.mu.Lock()
	if a.apiBaseURL != apiBaseURL {
		a.cancelConnectionLocked()
		a.cancelRoomEventsLocked()
		a.apiBaseURL = apiBaseURL
		a.api = client.NewAPI(apiBaseURL)
		a.user = nil
		a.clearRoomStateLocked()
		a.connection = ConnectionState{Status: statusDisconnected, Message: "切换服务器后已退出登录"}
		a.resetUpdateLocked()
		a.startUpdateCheckLocked("正在检查更新")
	}
	a.ifName = ifName
	a.startRoomEventsLocked()
	return a.commitStateLocked(), nil
}

func (a *App) Register(input RegisterInput) (AppState, error) {
	input.Username = strings.TrimSpace(input.Username)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	res, err := a.api.Register(ctx, input.Username, input.Password, input.DisplayName)
	if err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	a.user = &res.User
	_ = a.refreshRoomStateLocked(ctx)
	a.startRoomEventsLocked()
	return a.commitStateLocked(), nil
}

func (a *App) Login(input LoginInput) (AppState, error) {
	input.Username = strings.TrimSpace(input.Username)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	res, err := a.api.Login(ctx, input.Username, input.Password)
	if err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	a.user = &res.User
	_ = a.refreshRoomStateLocked(ctx)
	a.startRoomEventsLocked()
	return a.commitStateLocked(), nil
}

func (a *App) Logout() (AppState, error) {
	a.mu.Lock()
	a.cancelConnectionLocked()
	a.cancelRoomEventsLocked()
	a.api.ClearToken()
	a.user = nil
	a.clearRoomStateLocked()
	a.connection = ConnectionState{Status: statusDisconnected, Message: "已退出登录"}
	return a.commitStateLocked(), nil
}

func (a *App) CreateRoom(input CreateRoomInput) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	a.mu.Lock()
	if a.user == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("请先登录")
	}
	if a.currentRoom != nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("你已经在一个房间中，请先退出或删除当前房间")
	}
	if _, err := a.api.CreateRoom(ctx, strings.TrimSpace(input.Name), input.MaxMembers); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	if err := a.refreshRoomStateLocked(ctx); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	a.connection = ConnectionState{Status: statusDisconnected, Message: "房间已创建，请手动启动虚拟网卡"}
	return a.commitStateLocked(), nil
}

func (a *App) JoinRoom(input JoinRoomInput) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	a.mu.Lock()
	if a.user == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("请先登录")
	}
	if a.currentRoom != nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("你已经在一个房间中，请先退出当前房间")
	}
	if _, err := a.api.JoinRoom(ctx, strings.TrimSpace(input.RoomPublicKey)); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	if err := a.refreshRoomStateLocked(ctx); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	a.connection = ConnectionState{Status: statusDisconnected, Message: "已加入房间，请手动启动虚拟网卡"}
	return a.commitStateLocked(), nil
}

func (a *App) SendRoomChat(input SendChatInput) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	if a.user == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("请先登录")
	}
	if a.currentRoom == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("当前没有加入房间")
	}
	roomID := input.RoomID
	if roomID <= 0 {
		roomID = a.currentRoom.ID
	}
	if roomID != a.currentRoom.ID {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("只能在当前房间发送聊天")
	}
	message := strings.TrimSpace(input.Message)
	chat, err := a.api.SendRoomChat(ctx, roomID, message)
	if err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	a.appendChatMessageLocked(chat)
	return a.commitStateLocked(), nil
}

func (a *App) DeleteRoom(roomID int64) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	if a.user == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("请先登录")
	}
	if roomID <= 0 {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("请选择房间")
	}
	if err := a.api.DeleteRoom(ctx, roomID); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	if a.connection.RoomID == roomID {
		a.cancelConnectionLocked()
	}
	if err := a.refreshRoomStateLocked(ctx); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	return a.commitStateLocked(), nil
}

func (a *App) LeaveCurrentRoom() (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	if a.currentRoom == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("当前没有加入房间")
	}
	roomID := a.currentRoom.ID
	if a.connection.RoomID == roomID {
		a.cancelConnectionLocked()
	}
	if err := a.api.LeaveRoom(ctx, roomID); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	if err := a.refreshRoomStateLocked(ctx); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	return a.commitStateLocked(), nil
}

func (a *App) KickMember(input MemberActionInput) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	if a.currentRoom == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("当前没有加入房间")
	}
	if err := a.api.KickMember(ctx, a.currentRoom.ID, input.UserID); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	if err := a.refreshRoomStateLocked(ctx); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	return a.commitStateLocked(), nil
}

func (a *App) TransferOwner(input MemberActionInput) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	if a.currentRoom == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("当前没有加入房间")
	}
	if _, err := a.api.TransferOwner(ctx, a.currentRoom.ID, input.UserID); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	if err := a.refreshRoomStateLocked(ctx); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	return a.commitStateLocked(), nil
}

func (a *App) ConnectRoom(roomID int64) (AppState, error) {
	a.mu.Lock()
	if a.user == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("请先登录")
	}
	selected, ok := findRoom(a.rooms, roomID)
	if !ok {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("房间不存在于你的列表中")
	}
	if err := a.startRoomConnectionLocked(selected, "正在连接虚拟网络"); err != nil {
		a.mu.Unlock()
		return AppState{}, err
	}
	return a.commitStateLocked(), nil
}

func (a *App) startRoomConnectionLocked(selected room.Room, message string) error {
	if a.connectCancel != nil {
		return fmt.Errorf("虚拟网络已经在连接中")
	}
	if selected.NodeOverlayAddr == "" || selected.NodePublicKey == "" {
		return fmt.Errorf("房间缺少节点连接信息")
	}
	connectCtx, cancel := context.WithCancel(context.Background())
	a.connectSeq++
	seq := a.connectSeq
	token := a.api.Token()
	ifName := a.ifName
	a.connectCancel = cancel
	a.connection = ConnectionState{
		Status:      statusConnecting,
		Message:     message,
		RoomID:      selected.ID,
		RoomName:    selected.Name,
		NetworkCIDR: selected.NetworkCIDR,
		OverlayAddr: selected.NodeOverlayAddr,
	}
	go a.runConnection(seq, connectCtx, selected, token, ifName)
	return nil
}

func (a *App) Disconnect() (AppState, error) {
	a.mu.Lock()
	if a.connectCancel == nil {
		a.connection = ConnectionState{Status: statusDisconnected, Message: "未连接"}
		return a.commitStateLocked(), nil
	}
	a.connection.Status = statusDisconnecting
	a.connection.Message = "正在断开"
	a.connectCancel()
	return a.commitStateLocked(), nil
}

func (a *App) runConnection(seq int64, ctx context.Context, selected room.Room, token, ifName string) {
	err := client.ConnectRoom(ctx, client.ConnectOptions{
		OverlayAddr:     selected.NodeOverlayAddr,
		ServerPublicKey: selected.NodePublicKey,
		IfName:          ifName,
		Token:           token,
		Room:            selected,
		OnConnected: func(lease overlay.DHCPResponse) {
			a.mu.Lock()
			if a.connectSeq == seq {
				a.connection = ConnectionState{
					Status:      statusConnected,
					Message:     "已连接",
					RoomID:      selected.ID,
					RoomName:    selected.Name,
					NetworkCIDR: selected.NetworkCIDR,
					OverlayAddr: selected.NodeOverlayAddr,
					IP:          lease.IP,
					Gateway:     lease.Gateway,
					MTU:         lease.MTU,
				}
				a.commitStateLocked()
				return
			}
			a.mu.Unlock()
		},
	})

	a.mu.Lock()
	if a.connectSeq != seq {
		a.mu.Unlock()
		return
	}
	a.connectCancel = nil
	if err != nil && !errors.Is(err, context.Canceled) {
		a.connection.Status = statusError
		a.connection.Message = friendlyConnectionError(err)
	} else {
		a.connection = ConnectionState{Status: statusDisconnected, Message: "已断开"}
	}
	a.commitStateLocked()
}

func (a *App) cancelConnectionLocked() {
	if a.connectCancel != nil {
		a.connectCancel()
		a.connectCancel = nil
	}
}

func (a *App) startRoomEventsLocked() {
	if a.user == nil || a.roomEventsCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.roomEventsSeq++
	seq := a.roomEventsSeq
	api := a.api.Clone()
	a.roomEventsCancel = cancel
	go a.runRoomEvents(seq, ctx, api)
}

func (a *App) cancelRoomEventsLocked() {
	if a.roomEventsCancel != nil {
		a.roomEventsCancel()
		a.roomEventsCancel = nil
	}
	a.roomEventsSeq++
}

func (a *App) runRoomEvents(seq int64, ctx context.Context, api *client.API) {
	for ctx.Err() == nil {
		err := api.SubscribeRoomEvents(ctx, func(event room.RoomEvent) {
			a.refreshAfterRoomEvent(seq, event)
		})
		if ctx.Err() != nil {
			return
		}
		a.mu.Lock()
		current := a.roomEventsSeq == seq && a.roomEventsCancel != nil
		if current && err != nil {
			a.connection.Message = "房间事件流已断开，正在重连"
			a.commitStateLocked()
		} else {
			a.mu.Unlock()
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

func (a *App) refreshAfterRoomEvent(seq int64, event room.RoomEvent) {
	if event.Type == "" {
		return
	}
	if event.Type == room.RoomEventChatMessage && event.Chat != nil {
		a.mu.Lock()
		if a.roomEventsSeq != seq || a.user == nil {
			a.mu.Unlock()
			return
		}
		a.appendChatMessageLocked(*event.Chat)
		a.commitStateLocked()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a.mu.Lock()
	if a.roomEventsSeq != seq || a.user == nil {
		a.mu.Unlock()
		return
	}
	if event.Type == room.RoomEventAccountUpdated {
		if _, err := a.refreshUserStateLocked(ctx); err != nil {
			a.mu.Unlock()
			return
		}
		a.commitStateLocked()
		return
	}
	if err := a.refreshRoomStateLocked(ctx); err != nil {
		a.mu.Unlock()
		return
	}
	a.commitStateLocked()
}

func (a *App) clearRoomStateLocked() {
	a.rooms = nil
	a.currentRoom = nil
	a.members = nil
	a.chatMessages = nil
}

func (a *App) refreshRoomStateLocked(ctx context.Context) error {
	activeRoomID := a.connection.RoomID
	previousRoomID := int64(0)
	if a.currentRoom != nil {
		previousRoomID = a.currentRoom.ID
	}
	rooms, err := a.api.Rooms(ctx)
	if err != nil {
		return err
	}
	a.rooms = rooms
	a.currentRoom = nil
	a.members = nil
	if len(rooms) == 0 {
		a.chatMessages = nil
		if activeRoomID > 0 {
			a.cancelConnectionLocked()
			a.connection = ConnectionState{Status: statusDisconnected, Message: "已离开房间，虚拟网卡已断开"}
		}
		return nil
	}
	current := rooms[0]
	a.currentRoom = &current
	if previousRoomID > 0 && previousRoomID != current.ID {
		a.chatMessages = nil
	}
	if activeRoomID > 0 && activeRoomID != current.ID {
		a.cancelConnectionLocked()
		a.connection = ConnectionState{Status: statusDisconnected, Message: "当前房间已变化，虚拟网卡已断开"}
	}
	members, err := a.api.RoomMembers(ctx, current.ID)
	if err != nil {
		return err
	}
	a.members = members
	return nil
}

func (a *App) refreshUserStateLocked(ctx context.Context) (bool, error) {
	if a.user == nil {
		return false, nil
	}
	user, err := a.api.Me(ctx)
	if err != nil {
		if !isAccountStateError(err) {
			return false, err
		}
		a.cancelConnectionLocked()
		a.cancelRoomEventsLocked()
		a.api.ClearToken()
		a.user = nil
		a.clearRoomStateLocked()
		a.connection = ConnectionState{Status: statusDisconnected, Message: "账号状态已变化，请重新登录"}
		return false, nil
	}
	a.user = &user
	return true, nil
}

func (a *App) appendChatMessageLocked(message room.ChatMessage) {
	if message.ID == "" || message.RoomID <= 0 {
		return
	}
	if a.currentRoom == nil || a.currentRoom.ID != message.RoomID {
		return
	}
	for _, item := range a.chatMessages {
		if item.ID == message.ID {
			return
		}
	}
	a.chatMessages = append(a.chatMessages, message)
	if len(a.chatMessages) > maxChatMessages {
		a.chatMessages = append([]room.ChatMessage(nil), a.chatMessages[len(a.chatMessages)-maxChatMessages:]...)
	}
}

func (a *App) stateLocked() AppState {
	rooms := make([]room.Room, len(a.rooms))
	copy(rooms, a.rooms)
	members := make([]room.MemberView, len(a.members))
	copy(members, a.members)
	chatMessages := make([]room.ChatMessage, len(a.chatMessages))
	copy(chatMessages, a.chatMessages)
	var currentRoom *room.Room
	if a.currentRoom != nil {
		cp := *a.currentRoom
		currentRoom = &cp
	}
	var user *account.PublicUser
	if a.user != nil {
		cp := *a.user
		user = &cp
	}
	return AppState{
		APIBaseURL:    a.apiBaseURL,
		InterfaceName: a.ifName,
		User:          user,
		Rooms:         rooms,
		CurrentRoom:   currentRoom,
		Members:       members,
		ChatMessages:  chatMessages,
		Connection:    a.connection,
		Update:        a.update,
	}
}

func (a *App) emitState(state AppState) {
	a.updateTray(state)
	if ctx := a.currentContext(); ctx != nil {
		wailsRuntime.EventsEmit(ctx, "state-changed", state)
	}
}

func findRoom(rooms []room.Room, roomID int64) (room.Room, bool) {
	for _, item := range rooms {
		if item.ID == roomID {
			return item, true
		}
	}
	return room.Room{}, false
}

func isAccountStateError(err error) bool {
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(lower, "account is disabled") ||
		strings.Contains(lower, "account has logged in elsewhere") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "user not found") ||
		strings.Contains(lower, "sql: no rows")
}

func friendlyConnectionError(err error) string {
	text := strings.TrimSpace(err.Error())
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "wintun client is only implemented"):
		return "Wintun 客户端只能在 Windows 上运行"
	case strings.Contains(lower, "release wintun.dll"):
		return "释放 Wintun 组件失败，请确认客户端所在目录可写，并用管理员身份运行"
	case strings.Contains(lower, "access is denied") || strings.Contains(lower, "拒绝访问"):
		return "启动虚拟网卡需要管理员权限，请用管理员身份运行客户端"
	case strings.Contains(lower, "configure tun address") || strings.Contains(lower, "new-netroute") || strings.Contains(lower, "set-netipinterface"):
		return "虚拟网卡地址或路由配置失败，请确认已用管理员身份运行客户端"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "actively refused") || strings.Contains(lower, "connectex"):
		return "无法连接房间节点，请检查服务器节点地址和端口是否开放"
	case strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "timed out"):
		return "连接房间节点超时，请检查服务器 IP、节点端口和网络连通性"
	}
	return text
}
