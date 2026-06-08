package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/account"
	"github.com/LwyV-Labs/LwyLink/internal/appsettings"
	"github.com/LwyV-Labs/LwyLink/internal/client"
	"github.com/LwyV-Labs/LwyLink/internal/overlay"
	"github.com/LwyV-Labs/LwyLink/internal/room"
	"github.com/LwyV-Labs/LwyLink/internal/tun"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultAPIBaseURL = "https://www.lwylink.xyz"
	defaultIfName     = "LwyLink"
	maxChatMessages   = 120

	statusDisconnected  = "disconnected"
	statusConnecting    = "connecting"
	statusConnected     = "connected"
	statusDisconnecting = "disconnecting"
	statusError         = "error"
)

var AppVersion = "0.1.18"

type App struct {
	ctx context.Context

	mu         sync.Mutex
	api        *client.API
	apiBaseURL string
	ifName     string
	p2pEnabled bool

	user         *account.PublicUser
	rooms        []room.Room
	currentRoom  *room.Room
	members      []room.MemberView
	chatMessages []room.ChatMessage
	announcement string

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
	P2PEnabled    *bool  `json:"p2pEnabled,omitempty"`
}

type RegisterInput struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
	EmailCode   string `json:"emailCode"`
}

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type EmailVerificationInput struct {
	Email   string `json:"email"`
	Purpose string `json:"purpose"`
}

type ResetPasswordInput struct {
	Email       string `json:"email"`
	EmailCode   string `json:"emailCode"`
	NewPassword string `json:"newPassword"`
}

type ChangePasswordInput struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
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
	P2PEnabled    bool                `json:"p2pEnabled"`
	User          *account.PublicUser `json:"user,omitempty"`
	Rooms         []room.Room         `json:"rooms"`
	CurrentRoom   *room.Room          `json:"currentRoom,omitempty"`
	Members       []room.MemberView   `json:"members"`
	ChatMessages  []room.ChatMessage  `json:"chatMessages"`
	Connection    ConnectionState     `json:"connection"`
	Update        UpdateState         `json:"update"`
	Announcement  string              `json:"announcement"`
}

type ConnectionState struct {
	Status        string   `json:"status"`
	Message       string   `json:"message"`
	RoomID        int64    `json:"roomID"`
	IP            string   `json:"ip"`
	DirectPeerIPs []string `json:"directPeerIPs"`
}

type fetchedRoomState struct {
	rooms       []room.Room
	currentRoom *room.Room
	members     []room.MemberView
}

func NewApp() *App {
	prefs := loadClientPreferences()
	return &App{
		api:          client.NewAPI(defaultAPIBaseURL),
		apiBaseURL:   defaultAPIBaseURL,
		ifName:       defaultIfName,
		p2pEnabled:   prefs.P2PEnabled,
		announcement: appsettings.DefaultAnnouncement,
		connection:   newConnectionState(statusDisconnected, "未连接"),
		update:       UpdateState{Status: updateStatusIdle, CurrentVersion: AppVersion},
	}
}

func newConnectionState(status, message string) ConnectionState {
	return ConnectionState{
		Status:  status,
		Message: message,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func newRoomConnectionState(status, message string, selected room.Room) ConnectionState {
	state := newConnectionState(status, message)
	state.RoomID = selected.ID
	return state
}

func newConnectedState(selected room.Room, lease overlay.DHCPResponse) ConnectionState {
	state := newRoomConnectionState(statusConnected, "已连接", selected)
	state.IP = lease.IP
	return state
}

func fetchRoomState(ctx context.Context, api *client.API) (fetchedRoomState, error) {
	rooms, err := api.Rooms(ctx)
	if err != nil {
		return fetchedRoomState{}, err
	}
	state := fetchedRoomState{rooms: rooms}
	if len(rooms) == 0 {
		return state, nil
	}
	current := rooms[0]
	state.currentRoom = &current
	members, err := api.RoomMembers(ctx, current.ID)
	if err != nil {
		return fetchedRoomState{}, err
	}
	state.members = members
	return state, nil
}

func fetchAnnouncement(ctx context.Context, api *client.API) (string, error) {
	announcement, err := api.Announcement(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(announcement) == "" {
		announcement = appsettings.DefaultAnnouncement
	}
	return announcement, nil
}

func (a *App) applyRoomMutationResult(ctx context.Context, apiBaseURL string, api *client.API, validate func(*App) error, after func(*App)) (AppState, error) {
	roomState, err := fetchRoomState(ctx, api)
	if err != nil {
		return AppState{}, err
	}

	a.mu.Lock()
	if a.apiBaseURL != apiBaseURL || a.user == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("账号或服务器状态已变化，请重试")
	}
	if validate != nil {
		if err := validate(a); err != nil {
			a.mu.Unlock()
			return AppState{}, err
		}
	}
	a.applyRoomStateLocked(roomState)
	if after != nil {
		after(a)
	}
	return a.commitStateLocked(), nil
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.initializeNotifications(ctx)
	a.registerTray()
	if err := tun.EnsureWintunDLL(); err != nil {
		a.mu.Lock()
		a.connection = newConnectionState(statusError, friendlyConnectionError(err))
		a.commitStateLocked()
		return
	}
	saved, hasSaved, _ := loadSavedLogin()
	a.mu.Lock()
	if hasSaved {
		a.apiBaseURL = saved.APIBaseURL
		a.api = client.NewAPI(saved.APIBaseURL)
	}
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	announcementCtx, announcementCancel := context.WithTimeout(context.Background(), 5*time.Second)
	announcement, _ := fetchAnnouncement(announcementCtx, api)
	announcementCancel()

	a.mu.Lock()
	if a.apiBaseURL == apiBaseURL && announcement != "" {
		a.announcement = announcement
	}
	a.startUpdateCheckLocked("正在检查更新")
	a.commitStateLocked()
	if hasSaved {
		go a.autoLogin(saved)
	}
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	a.cancelConnectionLocked()
	a.cancelRoomEventsLocked()
	a.mu.Unlock()
	_ = tun.EnableFirewallProfiles()
	a.cleanupNotifications(ctx)
	a.stopTray()
}

func (a *App) CurrentState() AppState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stateLocked()
}

// commitStateLocked captures the current state, releases the lock, and emits.
// Callers must hold a.mu before calling this; the lock is released here.
func (a *App) commitStateLocked() AppState {
	state := a.stateLocked()
	a.mu.Unlock()
	a.emitState(state)
	return state
}

func (a *App) SetSettings(input SettingsInput) (AppState, error) {
	apiBaseURL := normalizeSavedAPIBaseURL(input.APIBaseURL)
	ifName := strings.TrimSpace(input.InterfaceName)
	if ifName == "" {
		ifName = defaultIfName
	}

	a.mu.Lock()
	var announcementAPI *client.API
	if a.apiBaseURL != apiBaseURL {
		a.cancelConnectionLocked()
		a.cancelRoomEventsLocked()
		_ = clearSavedLogin()
		a.apiBaseURL = apiBaseURL
		a.api = client.NewAPI(apiBaseURL)
		a.user = nil
		a.clearRoomStateLocked()
		a.connection = newConnectionState(statusDisconnected, "切换服务器后已退出登录")
		a.resetUpdateLocked()
		a.announcement = appsettings.DefaultAnnouncement
		announcementAPI = a.api.Clone()
	}
	a.ifName = ifName
	if input.P2PEnabled != nil && a.p2pEnabled != *input.P2PEnabled {
		a.p2pEnabled = *input.P2PEnabled
		_ = saveClientPreferences(clientPreferences{P2PEnabled: a.p2pEnabled})
		if !a.p2pEnabled {
			a.connection.DirectPeerIPs = nil
		}
	}
	a.startRoomEventsLocked()
	if announcementAPI == nil {
		return a.commitStateLocked(), nil
	}
	a.mu.Unlock()

	announcementCtx, announcementCancel := context.WithTimeout(context.Background(), 5*time.Second)
	announcement, _ := fetchAnnouncement(announcementCtx, announcementAPI)
	announcementCancel()

	a.mu.Lock()
	if a.apiBaseURL == apiBaseURL && announcement != "" {
		a.announcement = announcement
	}
	a.startUpdateCheckLocked("正在检查更新")
	return a.commitStateLocked(), nil
}

func (a *App) Register(input RegisterInput) (AppState, error) {
	input.Email = strings.TrimSpace(input.Email)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	res, err := api.Register(ctx, input.Email, input.Password, input.DisplayName, input.EmailCode)
	if err != nil {
		return AppState{}, err
	}
	roomState, roomErr := fetchRoomState(ctx, api)

	a.mu.Lock()
	if a.apiBaseURL != apiBaseURL {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("服务器设置已变化，请重试")
	}
	a.api = api
	a.applyAuthenticatedUserLocked(res.User, roomState, roomErr == nil)
	state := a.commitStateLocked()
	_ = saveSavedLogin(savedLogin{APIBaseURL: apiBaseURL, Email: input.Email, Password: input.Password})
	return state, nil
}

func (a *App) SendEmailVerification(input EmailVerificationInput) (AppState, error) {
	input.Email = strings.TrimSpace(input.Email)
	input.Purpose = strings.TrimSpace(input.Purpose)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	api := a.api.Clone()
	a.mu.Unlock()
	if err := api.SendEmailVerification(ctx, input.Email, input.Purpose); err != nil {
		return AppState{}, err
	}
	a.mu.Lock()
	return a.commitStateLocked(), nil
}

func (a *App) ResetPassword(input ResetPasswordInput) (AppState, error) {
	input.Email = strings.TrimSpace(input.Email)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	api := a.api.Clone()
	a.mu.Unlock()
	if err := api.ResetPassword(ctx, input.Email, input.EmailCode, input.NewPassword); err != nil {
		return AppState{}, err
	}
	a.mu.Lock()
	return a.commitStateLocked(), nil
}

func (a *App) ChangePassword(input ChangePasswordInput) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	api := a.api.Clone()
	a.mu.Unlock()
	if err := api.ChangePassword(ctx, input.OldPassword, input.NewPassword); err != nil {
		return AppState{}, err
	}
	a.mu.Lock()
	a.cancelConnectionLocked()
	a.cancelRoomEventsLocked()
	_ = clearSavedLogin()
	a.api.ClearToken()
	a.user = nil
	a.clearRoomStateLocked()
	a.connection = newConnectionState(statusDisconnected, "密码已修改，请重新登录")
	return a.commitStateLocked(), nil
}

func (a *App) Login(input LoginInput) (AppState, error) {
	input.Email = strings.TrimSpace(input.Email)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	res, err := api.Login(ctx, input.Email, input.Password)
	if err != nil {
		return AppState{}, err
	}
	roomState, roomErr := fetchRoomState(ctx, api)

	a.mu.Lock()
	if a.apiBaseURL != apiBaseURL {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("服务器设置已变化，请重试")
	}
	a.api = api
	a.applyAuthenticatedUserLocked(res.User, roomState, roomErr == nil)
	state := a.commitStateLocked()
	_ = saveSavedLogin(savedLogin{APIBaseURL: apiBaseURL, Email: input.Email, Password: input.Password})
	return state, nil
}

func (a *App) Logout() (AppState, error) {
	a.mu.Lock()
	a.cancelConnectionLocked()
	a.cancelRoomEventsLocked()
	_ = clearSavedLogin()
	a.api.ClearToken()
	a.user = nil
	a.clearRoomStateLocked()
	a.connection = newConnectionState(statusDisconnected, "已退出登录")
	return a.commitStateLocked(), nil
}

func (a *App) autoLogin(saved savedLogin) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	api := client.NewAPI(saved.APIBaseURL)
	res, err := api.Login(ctx, saved.Email, saved.Password)
	var roomState fetchedRoomState
	var roomErr error
	if err == nil {
		roomState, roomErr = fetchRoomState(ctx, api)
	}

	a.mu.Lock()
	if a.user != nil || a.apiBaseURL != saved.APIBaseURL {
		a.mu.Unlock()
		return
	}
	if err != nil {
		if a.connection.Status == statusDisconnected {
			a.connection.Message = "自动登录失败，请手动登录"
		}
		a.commitStateLocked()
		return
	}
	a.api = api
	a.applyAuthenticatedUserLocked(res.User, roomState, roomErr == nil)
	a.commitStateLocked()
}

func (a *App) applyAuthenticatedUserLocked(user account.PublicUser, roomState fetchedRoomState, hasRoomState bool) {
	a.user = &user
	if hasRoomState {
		a.applyRoomStateLocked(roomState)
	}
	a.startRoomEventsLocked()
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
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	if _, err := api.CreateRoom(ctx, strings.TrimSpace(input.Name), input.MaxMembers); err != nil {
		return AppState{}, err
	}
	return a.applyRoomMutationResult(ctx, apiBaseURL, api, nil, func(app *App) {
		app.connection = newConnectionState(statusDisconnected, "房间已创建，请手动启动虚拟网卡")
	})
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
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	if _, err := api.JoinRoom(ctx, strings.TrimSpace(input.RoomPublicKey)); err != nil {
		return AppState{}, err
	}
	return a.applyRoomMutationResult(ctx, apiBaseURL, api, nil, func(app *App) {
		app.connection = newConnectionState(statusDisconnected, "已加入房间，请手动启动虚拟网卡")
	})
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
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	chat, err := api.SendRoomChat(ctx, roomID, message)
	if err != nil {
		return AppState{}, err
	}

	a.mu.Lock()
	if a.apiBaseURL != apiBaseURL || a.currentRoom == nil || a.currentRoom.ID != roomID {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("当前房间已变化，请重试")
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
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	if err := api.DeleteRoom(ctx, roomID); err != nil {
		return AppState{}, err
	}
	return a.applyRoomMutationResult(ctx, apiBaseURL, api, nil, func(app *App) {
		if app.connection.RoomID == roomID {
			app.cancelConnectionLocked()
		}
	})
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
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	if err := api.LeaveRoom(ctx, roomID); err != nil {
		return AppState{}, err
	}
	return a.applyRoomMutationResult(ctx, apiBaseURL, api, nil, func(app *App) {
		if app.connection.RoomID == roomID {
			app.cancelConnectionLocked()
		}
	})
}

func (a *App) KickMember(input MemberActionInput) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	if a.currentRoom == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("当前没有加入房间")
	}
	roomID := a.currentRoom.ID
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	if err := api.KickMember(ctx, roomID, input.UserID); err != nil {
		return AppState{}, err
	}
	return a.applyRoomMutationResult(ctx, apiBaseURL, api, func(app *App) error {
		if app.currentRoom == nil || app.currentRoom.ID != roomID {
			return fmt.Errorf("当前房间已变化，请重试")
		}
		return nil
	}, nil)
}

func (a *App) TransferOwner(input MemberActionInput) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	if a.currentRoom == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("当前没有加入房间")
	}
	roomID := a.currentRoom.ID
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	if _, err := api.TransferOwner(ctx, roomID, input.UserID); err != nil {
		return AppState{}, err
	}
	return a.applyRoomMutationResult(ctx, apiBaseURL, api, func(app *App) error {
		if app.currentRoom == nil || app.currentRoom.ID != roomID {
			return fmt.Errorf("当前房间已变化，请重试")
		}
		return nil
	}, nil)
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
	userID := a.user.ID
	a.connectCancel = cancel
	a.connection = newRoomConnectionState(statusConnecting, message, selected)
	go a.runConnection(seq, connectCtx, selected, token, userID, ifName)
	return nil
}

func (a *App) Disconnect() (AppState, error) {
	a.mu.Lock()
	if a.connectCancel == nil {
		a.connection = newConnectionState(statusDisconnected, "未连接")
		return a.commitStateLocked(), nil
	}
	a.connection.Status = statusDisconnecting
	a.connection.Message = "正在断开"
	a.connectCancel()
	return a.commitStateLocked(), nil
}

func (a *App) runConnection(seq int64, ctx context.Context, selected room.Room, token string, userID int64, ifName string) {
	err := client.ConnectRoom(ctx, client.ConnectOptions{
		OverlayAddr:     selected.NodeOverlayAddr,
		UDPOverlayAddr:  selected.NodeUDPAddr,
		ServerPublicKey: selected.NodePublicKey,
		IfName:          ifName,
		Token:           token,
		UserID:          userID,
		Room:            selected,
		P2PEnabled:      a.isP2PEnabled,
		OnConnected: func(lease overlay.DHCPResponse) {
			a.updateConnection(seq, func(state *ConnectionState) {
				*state = newConnectedState(selected, lease)
			})
		},
		OnTransportState: func(status client.TransportStatus) {
			a.updateConnection(seq, func(state *ConnectionState) {
				applyTransportStatus(state, status)
			})
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
		a.connection = newConnectionState(statusDisconnected, "已断开")
	}
	a.commitStateLocked()
}

func (a *App) updateConnection(seq int64, apply func(*ConnectionState)) {
	a.mu.Lock()
	if a.connectSeq != seq {
		a.mu.Unlock()
		return
	}
	apply(&a.connection)
	a.commitStateLocked()
}

func applyTransportStatus(state *ConnectionState, status client.TransportStatus) {
	if strings.TrimSpace(status.Message) != "" {
		state.Message = status.Message
	}
	state.DirectPeerIPs = append([]string(nil), status.DirectIPs...)
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
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	if event.Type == room.RoomEventAccountUpdated {
		user, err := api.Me(ctx)
		a.mu.Lock()
		if a.roomEventsSeq != seq || a.apiBaseURL != apiBaseURL || a.user == nil {
			a.mu.Unlock()
			return
		}
		if err != nil {
			if !isAccountStateError(err) {
				a.mu.Unlock()
				return
			}
			a.cancelConnectionLocked()
			a.cancelRoomEventsLocked()
			a.api.ClearToken()
			a.user = nil
			a.clearRoomStateLocked()
			a.connection = newConnectionState(statusDisconnected, "账号状态已变化，请重新登录")
			a.commitStateLocked()
			return
		}
		a.user = &user
		a.commitStateLocked()
		return
	}

	roomState, err := fetchRoomState(ctx, api)
	if err != nil {
		return
	}

	a.mu.Lock()
	if a.roomEventsSeq != seq || a.apiBaseURL != apiBaseURL || a.user == nil {
		a.mu.Unlock()
		return
	}
	a.applyRoomStateLocked(roomState)
	a.commitStateLocked()
}

func (a *App) clearRoomStateLocked() {
	a.rooms = nil
	a.currentRoom = nil
	a.members = nil
	a.chatMessages = nil
}

func (a *App) applyRoomStateLocked(next fetchedRoomState) {
	activeRoomID := a.connection.RoomID
	previousRoomID := int64(0)
	if a.currentRoom != nil {
		previousRoomID = a.currentRoom.ID
	}

	a.rooms = next.rooms
	a.currentRoom = nil
	a.members = nil
	if next.currentRoom == nil {
		a.chatMessages = nil
		if activeRoomID > 0 {
			a.cancelConnectionLocked()
			a.connection = newConnectionState(statusDisconnected, "已离开房间，虚拟网卡已断开")
		}
		return
	}
	current := *next.currentRoom
	a.currentRoom = &current
	if previousRoomID > 0 && previousRoomID != current.ID {
		a.chatMessages = nil
	}
	if activeRoomID > 0 && activeRoomID != current.ID {
		a.cancelConnectionLocked()
		a.connection = newConnectionState(statusDisconnected, "当前房间已变化，虚拟网卡已断开")
	}
	a.members = next.members
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
		P2PEnabled:    a.p2pEnabled,
		User:          user,
		Rooms:         rooms,
		CurrentRoom:   currentRoom,
		Members:       members,
		ChatMessages:  chatMessages,
		Connection:    a.connection,
		Update:        a.update,
		Announcement:  a.announcement,
	}
}

func (a *App) isP2PEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.p2pEnabled
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
