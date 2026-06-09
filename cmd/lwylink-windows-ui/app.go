package main

import (
	"context"
	"encoding/json"
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

var AppVersion = "0.2.6"

type App struct {
	ctx context.Context

	mu         sync.Mutex
	api        *client.API
	apiBaseURL string
	ifName     string
	voicePrefs VoicePreferences

	user         *account.PublicUser
	rooms        []room.Room
	currentRoom  *room.Room
	members      []room.MemberView
	chatMessages []room.ChatMessage
	voiceState   room.VoiceState
	announcement string

	connectSeq    int64
	connectCancel context.CancelFunc
	connection    ConnectionState

	roomEventsSeq    int64
	roomEventsCancel context.CancelFunc

	updateSeq          int64
	preparedUpdatePath string
	update             UpdateState
	lastManualUpdateAt time.Time

	tray               *trayController
	allowQuit          bool
	trayNoticeShown    bool
	notificationsReady bool
}

type SettingsInput struct {
	APIBaseURL    string            `json:"apiBaseURL"`
	InterfaceName string            `json:"interfaceName"`
	VoiceSettings *VoicePreferences `json:"voiceSettings,omitempty"`
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

type VoiceWebRTCSignalInput struct {
	RoomID   int64           `json:"roomID"`
	ToUserID int64           `json:"toUserID"`
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
}

type VoiceWebRTCTrafficInput struct {
	RoomID int64 `json:"roomID"`
	Bytes  int64 `json:"bytes"`
}

type VoiceICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type MemberActionInput struct {
	UserID int64 `json:"userID"`
}

type AppState struct {
	APIBaseURL    string            `json:"apiBaseURL"`
	InterfaceName string            `json:"interfaceName"`
	VoiceSettings VoicePreferences  `json:"voiceSettings"`
	User          *ViewUser         `json:"user,omitempty"`
	Rooms         []ViewRoom        `json:"rooms"`
	CurrentRoom   *ViewRoom         `json:"currentRoom,omitempty"`
	Members       []ViewMember      `json:"members"`
	ChatMessages  []ViewChatMessage `json:"chatMessages"`
	Voice         ViewVoiceState    `json:"voice"`
	Connection    ConnectionState   `json:"connection"`
	Update        UpdateState       `json:"update"`
	Announcement  string            `json:"announcement"`
}

type ViewUser struct {
	ID                         int64  `json:"id"`
	Username                   string `json:"username"`
	Email                      string `json:"email"`
	DisplayName                string `json:"displayName"`
	Role                       string `json:"role"`
	Membership                 string `json:"membership"`
	Disabled                   bool   `json:"disabled"`
	RelayTrafficUsedBytes      int64  `json:"relayTrafficUsedBytes"`
	RelayTrafficExtraBytes     int64  `json:"relayTrafficExtraBytes"`
	RelayTrafficQuotaBytes     int64  `json:"relayTrafficQuotaBytes"`
	RelayTrafficRemainingBytes int64  `json:"relayTrafficRemainingBytes"`
	RelayTrafficPeriod         string `json:"relayTrafficPeriod"`
	CreatedAt                  string `json:"createdAt"`
}

type ViewRoom struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	OwnerUserID      int64  `json:"ownerUserID"`
	NodeID           int64  `json:"nodeID"`
	RoomPublicKey    string `json:"roomPublicKey"`
	NetworkCIDR      string `json:"networkCIDR"`
	MaxMembers       int    `json:"maxMembers"`
	VoiceMaxMembers  int    `json:"voiceMaxMembers"`
	VoiceMaxSpeakers int    `json:"voiceMaxSpeakers"`
	MemberCount      int    `json:"memberCount"`
	Status           string `json:"status"`
	NodeName         string `json:"nodeName,omitempty"`
	NodeOverlayAddr  string `json:"nodeOverlayAddr,omitempty"`
	NodeUDPAddr      string `json:"nodeUDPAddr,omitempty"`
	NodePublicKey    string `json:"nodePublicKey,omitempty"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

type ViewMember struct {
	RoomID      int64  `json:"roomID"`
	UserID      int64  `json:"userID"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	Online      bool   `json:"online"`
	VirtualIP   string `json:"virtualIP,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

type ViewChatMessage struct {
	ID          string `json:"id"`
	RoomID      int64  `json:"roomID"`
	UserID      int64  `json:"userID"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Message     string `json:"message"`
	CreatedAt   string `json:"createdAt"`
}

type ViewVoiceParticipant struct {
	RoomID      int64  `json:"roomID"`
	UserID      int64  `json:"userID"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Speaking    bool   `json:"speaking"`
	Muted       bool   `json:"muted"`
	JoinedAt    string `json:"joinedAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type ViewVoiceState struct {
	RoomID           int64                  `json:"roomID"`
	MaxMembers       int                    `json:"maxMembers"`
	MaxSpeakers      int                    `json:"maxSpeakers"`
	ParticipantCount int                    `json:"participantCount"`
	SpeakerCount     int                    `json:"speakerCount"`
	Participants     []ViewVoiceParticipant `json:"participants"`
	UpdatedAt        string                 `json:"updatedAt"`
}

type ConnectionState struct {
	Status               string                `json:"status"`
	Message              string                `json:"message"`
	RoomID               int64                 `json:"roomID"`
	IP                   string                `json:"ip"`
	TransportMode        string                `json:"transportMode"`
	DirectPeerIPs        []string              `json:"directPeerIPs"`
	TransportDiagnostics []TransportDiagnostic `json:"transportDiagnostics"`
}

type TransportDiagnostic struct {
	VirtualIP          string `json:"virtualIP"`
	CandidateCount     int    `json:"candidateCount"`
	SelectedType       string `json:"selectedType"`
	LastProbeAt        string `json:"lastProbeAt"`
	FailedProbes       int    `json:"failedProbes"`
	LastObservedPeerAt string `json:"lastObservedPeerAt"`
}

type fetchedRoomState struct {
	rooms       []room.Room
	currentRoom *room.Room
	members     []room.MemberView
	voice       room.VoiceState
}

func NewApp() *App {
	prefs := loadClientPreferences()
	return &App{
		api:          client.NewAPI(defaultAPIBaseURL),
		apiBaseURL:   defaultAPIBaseURL,
		ifName:       defaultIfName,
		voicePrefs:   prefs.VoiceSettings,
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
	if voice, err := api.RoomVoiceState(ctx, current.ID); err == nil {
		state.voice = voice
	}
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
	if input.VoiceSettings != nil {
		a.voicePrefs = normalizeVoicePreferences(*input.VoiceSettings)
	}
	_ = saveClientPreferences(clientPreferences{VoiceSettings: a.voicePrefs})
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

func (a *App) JoinRoomVoice(roomID int64) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	if a.currentRoom == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("当前没有加入房间")
	}
	if roomID <= 0 {
		roomID = a.currentRoom.ID
	}
	if roomID != a.currentRoom.ID {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("只能加入当前房间语音")
	}
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	voiceState, err := api.JoinRoomVoice(ctx, roomID)
	if err != nil {
		return AppState{}, err
	}
	a.mu.Lock()
	if a.apiBaseURL != apiBaseURL || a.currentRoom == nil || a.currentRoom.ID != roomID {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("当前房间已变化，请重试")
	}
	a.voiceState = voiceState
	return a.commitStateLocked(), nil
}

func (a *App) LeaveRoomVoice(roomID int64) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	if a.currentRoom == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("当前没有加入房间")
	}
	if roomID <= 0 {
		roomID = a.currentRoom.ID
	}
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	voiceState, err := api.LeaveRoomVoice(ctx, roomID)
	if err != nil {
		return AppState{}, err
	}
	a.mu.Lock()
	if a.apiBaseURL != apiBaseURL {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("服务器状态已变化，请重试")
	}
	a.voiceState = voiceState
	return a.commitStateLocked(), nil
}

func (a *App) SetRoomVoiceMic(roomID int64, speaking bool) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	a.mu.Lock()
	if a.currentRoom == nil {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("当前没有加入房间")
	}
	if roomID <= 0 {
		roomID = a.currentRoom.ID
	}
	if roomID != a.currentRoom.ID {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("只能操作当前房间语音")
	}
	apiBaseURL := a.apiBaseURL
	api := a.api.Clone()
	a.mu.Unlock()

	voiceState, err := api.SetRoomVoiceMic(ctx, roomID, speaking)
	if err != nil {
		return AppState{}, err
	}
	a.mu.Lock()
	if a.apiBaseURL != apiBaseURL || a.currentRoom == nil || a.currentRoom.ID != roomID {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("当前房间已变化，请重试")
	}
	a.voiceState = voiceState
	return a.commitStateLocked(), nil
}

func (a *App) VoiceWebRTCICE() ([]VoiceICEServer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.mu.Lock()
	api := a.api.Clone()
	a.mu.Unlock()
	servers, err := api.RoomVoiceWebRTCICE(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]VoiceICEServer, 0, len(servers))
	for _, server := range servers {
		out = append(out, VoiceICEServer{
			URLs:       append([]string(nil), server.URLs...),
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	return out, nil
}

func (a *App) SendVoiceWebRTCSignal(input VoiceWebRTCSignalInput) (AppState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.mu.Lock()
	api := a.api.Clone()
	apiBaseURL := a.apiBaseURL
	a.mu.Unlock()
	if _, err := api.SendRoomVoiceWebRTCSignal(ctx, input.RoomID, input.ToUserID, input.Type, input.Payload); err != nil {
		return a.CurrentState(), err
	}
	a.mu.Lock()
	if a.apiBaseURL != apiBaseURL {
		a.mu.Unlock()
		return a.CurrentState(), nil
	}
	return a.commitStateLocked(), nil
}

func (a *App) ReportVoiceWebRTCTraffic(input VoiceWebRTCTrafficInput) (AppState, error) {
	if input.Bytes <= 0 {
		return a.CurrentState(), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a.mu.Lock()
	api := a.api.Clone()
	apiBaseURL := a.apiBaseURL
	userID := int64(0)
	if a.user != nil {
		userID = a.user.ID
	}
	a.mu.Unlock()

	if err := api.ReportRoomVoiceWebRTCTraffic(ctx, input.RoomID, input.Bytes); err != nil {
		var httpErr client.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 402 {
			return a.CurrentState(), fmt.Errorf("语音中继流量额度已用尽")
		}
		return a.CurrentState(), err
	}
	user, err := api.Me(ctx)
	if err != nil {
		return a.CurrentState(), nil
	}

	a.mu.Lock()
	if a.apiBaseURL != apiBaseURL || a.user == nil || a.user.ID != userID {
		a.mu.Unlock()
		return a.CurrentState(), nil
	}
	a.user = &user
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
		a.resetVoiceLocked()
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
	a.resetVoiceLocked()
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
	state.TransportMode = status.Mode
	state.DirectPeerIPs = append([]string(nil), status.DirectIPs...)
	state.TransportDiagnostics = viewTransportDiagnostics(status.Diagnostics)
}

func viewTransportDiagnostics(items []client.TransportPeerDiagnostic) []TransportDiagnostic {
	if len(items) == 0 {
		return nil
	}
	out := make([]TransportDiagnostic, 0, len(items))
	for _, item := range items {
		out = append(out, TransportDiagnostic{
			VirtualIP:          item.VirtualIP,
			CandidateCount:     item.CandidateCount,
			SelectedType:       item.SelectedType,
			LastProbeAt:        formatOptionalTime(item.LastProbeAt),
			FailedProbes:       item.FailedProbes,
			LastObservedPeerAt: formatOptionalTime(item.LastObservedPeerAt),
		})
	}
	return out
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func (a *App) cancelConnectionLocked() {
	if a.connectCancel != nil {
		a.connectCancel()
		a.connectCancel = nil
	}
	a.resetVoiceLocked()
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
	if event.Type == room.RoomEventVoiceSignal && event.VoiceSignal != nil {
		a.mu.Lock()
		ready := a.roomEventsSeq == seq && a.user != nil
		ctx := a.ctx
		a.mu.Unlock()
		if ready && ctx != nil {
			wailsRuntime.EventsEmit(ctx, "voice-webrtc-signal", event.VoiceSignal)
		}
		return
	}
	if event.Type == room.RoomEventVoiceState && event.Voice != nil {
		a.mu.Lock()
		if a.roomEventsSeq != seq || a.user == nil {
			a.mu.Unlock()
			return
		}
		a.voiceState = *event.Voice
		a.commitStateLocked()
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
	a.resetVoiceLocked()
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
		a.resetVoiceLocked()
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
		a.resetVoiceLocked()
	}
	if activeRoomID > 0 && activeRoomID != current.ID {
		a.cancelConnectionLocked()
		a.connection = newConnectionState(statusDisconnected, "当前房间已变化，虚拟网卡已断开")
	}
	a.members = next.members
	a.voiceState = next.voice
}

func (a *App) resetVoiceLocked() {
	a.voiceState = room.VoiceState{}
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
	rooms := make([]ViewRoom, 0, len(a.rooms))
	for _, item := range a.rooms {
		rooms = append(rooms, viewRoom(item))
	}
	members := make([]ViewMember, 0, len(a.members))
	for _, item := range a.members {
		members = append(members, viewMember(item))
	}
	chatMessages := make([]ViewChatMessage, 0, len(a.chatMessages))
	for _, item := range a.chatMessages {
		chatMessages = append(chatMessages, viewChatMessage(item))
	}
	var currentRoom *ViewRoom
	if a.currentRoom != nil {
		cp := viewRoom(*a.currentRoom)
		currentRoom = &cp
	}
	var user *ViewUser
	if a.user != nil {
		cp := viewUser(*a.user)
		user = &cp
	}
	return AppState{
		APIBaseURL:    a.apiBaseURL,
		InterfaceName: a.ifName,
		VoiceSettings: a.voicePrefs,
		User:          user,
		Rooms:         rooms,
		CurrentRoom:   currentRoom,
		Members:       members,
		ChatMessages:  chatMessages,
		Voice:         viewVoiceState(a.voiceState),
		Connection:    a.connection,
		Update:        a.update,
		Announcement:  a.announcement,
	}
}

func (a *App) emitState(state AppState) {
	a.updateTray(state)
	if ctx := a.currentContext(); ctx != nil {
		wailsRuntime.EventsEmit(ctx, "state-changed", state)
	}
}

func viewUser(user account.PublicUser) ViewUser {
	return ViewUser{
		ID:                         user.ID,
		Username:                   user.Username,
		Email:                      user.Email,
		DisplayName:                user.DisplayName,
		Role:                       user.Role,
		Membership:                 user.Membership,
		Disabled:                   user.Disabled,
		RelayTrafficUsedBytes:      user.RelayTrafficUsedBytes,
		RelayTrafficExtraBytes:     user.RelayTrafficExtraBytes,
		RelayTrafficQuotaBytes:     user.RelayTrafficQuotaBytes,
		RelayTrafficRemainingBytes: user.RelayTrafficRemainingBytes,
		RelayTrafficPeriod:         user.RelayTrafficPeriod,
		CreatedAt:                  formatViewTime(user.CreatedAt),
	}
}

func viewRoom(item room.Room) ViewRoom {
	return ViewRoom{
		ID:               item.ID,
		Name:             item.Name,
		OwnerUserID:      item.OwnerUserID,
		NodeID:           item.NodeID,
		RoomPublicKey:    item.RoomPublicKey,
		NetworkCIDR:      item.NetworkCIDR,
		MaxMembers:       item.MaxMembers,
		VoiceMaxMembers:  item.VoiceMaxMembers,
		VoiceMaxSpeakers: item.VoiceMaxSpeakers,
		MemberCount:      item.MemberCount,
		Status:           item.Status,
		NodeName:         item.NodeName,
		NodeOverlayAddr:  item.NodeOverlayAddr,
		NodeUDPAddr:      item.NodeUDPAddr,
		NodePublicKey:    item.NodePublicKey,
		CreatedAt:        formatViewTime(item.CreatedAt),
		UpdatedAt:        formatViewTime(item.UpdatedAt),
	}
}

func viewMember(item room.MemberView) ViewMember {
	return ViewMember{
		RoomID:      item.RoomID,
		UserID:      item.UserID,
		Username:    item.Username,
		DisplayName: item.DisplayName,
		Role:        item.Role,
		Online:      item.Online,
		VirtualIP:   item.VirtualIP,
		CreatedAt:   formatViewTime(item.CreatedAt),
	}
}

func viewChatMessage(item room.ChatMessage) ViewChatMessage {
	return ViewChatMessage{
		ID:          item.ID,
		RoomID:      item.RoomID,
		UserID:      item.UserID,
		Username:    item.Username,
		DisplayName: item.DisplayName,
		Message:     item.Message,
		CreatedAt:   formatViewTime(item.CreatedAt),
	}
}

func viewVoiceState(state room.VoiceState) ViewVoiceState {
	participants := make([]ViewVoiceParticipant, 0, len(state.Participants))
	for _, item := range state.Participants {
		participants = append(participants, ViewVoiceParticipant{
			RoomID:      item.RoomID,
			UserID:      item.UserID,
			Username:    item.Username,
			DisplayName: item.DisplayName,
			Speaking:    item.Speaking,
			Muted:       item.Muted,
			JoinedAt:    formatViewTime(item.JoinedAt),
			UpdatedAt:   formatViewTime(item.UpdatedAt),
		})
	}
	return ViewVoiceState{
		RoomID:           state.RoomID,
		MaxMembers:       state.MaxMembers,
		MaxSpeakers:      state.MaxSpeakers,
		ParticipantCount: state.ParticipantCount,
		SpeakerCount:     state.SpeakerCount,
		Participants:     participants,
		UpdatedAt:        formatViewTime(state.UpdatedAt),
	}
}

func formatViewTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
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
