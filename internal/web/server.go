package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/account"
	"github.com/LwyV-Labs/LwyLink/internal/appsettings"
	"github.com/LwyV-Labs/LwyLink/internal/clientrelease"
	"github.com/LwyV-Labs/LwyLink/internal/config"
	"github.com/LwyV-Labs/LwyLink/internal/dbutil"
	"github.com/LwyV-Labs/LwyLink/internal/mail"
	"github.com/LwyV-Labs/LwyLink/internal/node"
	"github.com/LwyV-Labs/LwyLink/internal/room"
)

const maxAdminReleaseUploadBodyBytes = (512 << 20) + (1 << 20)
const maxVoiceRelayTrafficReportBytes int64 = 32 << 20
const maxAPIJSONBodyBytes int64 = 256 << 10

const (
	apiRateLimitWindow            = time.Minute
	rateLimitLoginIP              = 30
	rateLimitLoginAccount         = 10
	rateLimitRegisterIP           = 20
	rateLimitRegisterAccount      = 6
	rateLimitPasswordIP           = 20
	rateLimitPasswordAccount      = 6
	rateLimitPasswordUser         = 10
	rateLimitRoomCreateIP         = 40
	rateLimitRoomCreateUser       = 20
	rateLimitRoomJoinIP           = 60
	rateLimitRoomJoinUser         = 20
	rateLimitRoomActionIP         = 120
	rateLimitRoomActionUser       = 60
	rateLimitRoomChatIP           = 240
	rateLimitRoomChatUser         = 120
	rateLimitVoiceActionIP        = 180
	rateLimitVoiceActionUser      = 120
	rateLimitVoiceSignalIP        = 300
	rateLimitVoiceSignalUser      = 300
	rateLimitVoiceTrafficIP       = 180
	rateLimitVoiceTrafficUser     = 90
	rateLimitNetworkDiagIP        = 180
	rateLimitNetworkDiagUser      = 90
	rateLimitSmokeRegisterIP      = 80
	rateLimitSmokeRegisterAccount = 10
	rateLimitAdminIP              = 180
)

type Server struct {
	addr            string
	adminToken      string
	accounts        *account.Service
	settings        *appsettings.Service
	rooms           *room.Service
	nodes           *node.Service
	releases        *clientrelease.Service
	mail            *mail.Service
	voiceICEServers []config.ICEServerConfig
	voiceTURN       config.TURNCredentialConfig
	downloadLimiter *windowLimiter
	apiLimiter      *windowLimiter
	turnTrafficUsed atomic.Int64
	turnChargeFails atomic.Int64
	turnUsage       *coturnUsageCollector
	turnLedger      *turnUsageLedger
	httpServer      *http.Server
}

// NewServer exposes the JSON client API and the embedded admin console.
func NewServer(addr, adminToken string, accounts *account.Service, settings *appsettings.Service, rooms *room.Service, nodes *node.Service, releases *clientrelease.Service, mailSvc *mail.Service, voiceICEServers []config.ICEServerConfig, voiceTURN config.TURNCredentialConfig) *Server {
	s := &Server{
		addr:            addr,
		adminToken:      adminToken,
		accounts:        accounts,
		settings:        settings,
		rooms:           rooms,
		nodes:           nodes,
		releases:        releases,
		mail:            mailSvc,
		voiceICEServers: normalizeICEServers(voiceICEServers),
		voiceTURN:       voiceTURN,
		downloadLimiter: newWindowLimiter(downloadRateLimitWindow),
		apiLimiter:      newWindowLimiter(apiRateLimitWindow),
	}
	if accounts != nil {
		s.turnUsage = newCoturnUsageCollector(voiceTURN, s.chargeCoturnTURNTraffic)
	}
	return s
}

func (s *Server) SetTurnUsageLedger(ledger *turnUsageLedger) {
	s.turnLedger = ledger
	if s.turnUsage != nil {
		s.turnUsage.SetLedger(ledger)
	}
}

// ListenAndServe registers all HTTP routes and shuts down gracefully when the
// parent context is canceled by the process supervisor.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.turnUsage != nil {
		s.turnUsage.Start(ctx)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/metrics", s.handlePrometheusMetrics)
	mux.HandleFunc("/api/register", s.handleRegister)
	mux.HandleFunc("/api/smoke/register", s.handleSmokeRegister)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/email/verification/send", s.handleSendEmailVerification)
	mux.HandleFunc("/api/password/reset", s.handlePasswordReset)
	mux.HandleFunc("/api/password/change", s.handlePasswordChange)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/announcement", s.handleAnnouncement)
	mux.HandleFunc("/api/rooms", s.handleRooms)
	mux.HandleFunc("/api/rooms/join", s.handleJoinRoom)
	mux.HandleFunc("/api/rooms/delete", s.handleDeleteRoom)
	mux.HandleFunc("/api/rooms/members", s.handleRoomMembers)
	mux.HandleFunc("/api/rooms/chat", s.handleRoomChat)
	mux.HandleFunc("/api/rooms/voice", s.handleRoomVoice)
	mux.HandleFunc("/api/rooms/voice/join", s.handleJoinRoomVoice)
	mux.HandleFunc("/api/rooms/voice/leave", s.handleLeaveRoomVoice)
	mux.HandleFunc("/api/rooms/voice/mic", s.handleRoomVoiceMic)
	mux.HandleFunc("/api/rooms/voice/webrtc/ice", s.handleRoomVoiceWebRTCICE)
	mux.HandleFunc("/api/rooms/voice/webrtc/signal", s.handleRoomVoiceWebRTCSignal)
	mux.HandleFunc("/api/rooms/voice/webrtc/traffic", s.handleRoomVoiceWebRTCTraffic)
	mux.HandleFunc("/api/rooms/network-diagnostics", s.handleRoomNetworkDiagnostics)
	mux.HandleFunc("/api/rooms/events", s.handleRoomEvents)
	mux.HandleFunc("/api/rooms/leave", s.handleLeaveRoom)
	mux.HandleFunc("/api/rooms/kick", s.handleKickRoomMember)
	mux.HandleFunc("/api/rooms/transfer-owner", s.handleTransferRoomOwner)
	mux.HandleFunc("/api/client/releases/ticket", s.handleClientReleaseTicket)
	mux.HandleFunc("/api/client/releases/latest", s.handleLatestClientRelease)
	mux.HandleFunc("/downloads/client/", s.handleClientReleaseDownload)
	mux.Handle("/site/assets/", http.StripPrefix("/site/assets/", siteAssetHandler()))
	mux.Handle("/admin/assets/", http.StripPrefix("/admin/assets/", adminAssetHandler()))
	mux.HandleFunc("/", s.handleSite)
	mux.HandleFunc("/admin", s.handleAdmin)
	mux.HandleFunc("/admin/metrics", s.handleAdminMetrics)
	mux.HandleFunc("/admin/turn-usage", s.handleAdminTurnUsage)
	mux.HandleFunc("/admin/nodes", s.handleAdminNodes)
	mux.HandleFunc("/admin/users", s.handleAdminUsers)
	mux.HandleFunc("/admin/users/status", s.handleAdminUserStatus)
	mux.HandleFunc("/admin/users/membership", s.handleAdminUserMembership)
	mux.HandleFunc("/admin/users/traffic", s.handleAdminUserTraffic)
	mux.HandleFunc("/admin/users/delete", s.handleAdminUserDelete)
	mux.HandleFunc("/admin/rooms", s.handleAdminRooms)
	mux.HandleFunc("/admin/rooms/status", s.handleAdminRoomStatus)
	mux.HandleFunc("/admin/settings/announcement", s.handleAdminAnnouncement)
	mux.HandleFunc("/admin/nodes/save", s.handleAdminNodeSave)
	mux.HandleFunc("/admin/nodes/enabled", s.handleAdminNodeEnabled)
	mux.HandleFunc("/admin/nodes/check", s.handleAdminNodeCheck)
	mux.HandleFunc("/admin/releases", s.handleAdminReleases)
	mux.HandleFunc("/admin/releases/upload", s.handleAdminReleaseUpload)
	mux.HandleFunc("/admin/releases/delete", s.handleAdminReleaseDelete)

	s.httpServer = &http.Server{
		Addr:              s.addr,
		Handler:           withAccessLog(withCORS(withAPIJSONBodyLimit(mux))),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("http api listening on http://%s", s.addr)
	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"generatedAt": time.Now().UTC(),
	})
}

func (s *Server) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	body, err := s.prometheusMetrics(r.Context(), time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
		EmailCode   string `json:"emailCode"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowAccountAPIRate(r, "register", account.NormalizeEmail(req.Email), rateLimitRegisterIP, rateLimitRegisterAccount) {
		writeError(w, http.StatusTooManyRequests, "registration requests are too frequent")
		return
	}
	if err := s.requireEmailVerification(r.Context(), req.Email, mail.PurposeRegister, req.EmailCode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.accounts.Register(r.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSmokeRegister(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.adminAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "admin token required")
		return
	}
	if !s.allowIPAPIRate(r, "smoke-register", rateLimitSmokeRegisterIP) {
		writeError(w, http.StatusTooManyRequests, "smoke registration requests are too frequent")
		return
	}
	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowAPIRate(rateKey("smoke-register", "account", account.NormalizeEmail(req.Email)), rateLimitSmokeRegisterAccount) {
		writeError(w, http.StatusTooManyRequests, "smoke registration requests are too frequent")
		return
	}
	if !strings.HasSuffix(account.NormalizeEmail(req.Email), "@smoke.lwylink.test") {
		writeError(w, http.StatusBadRequest, "smoke registration only accepts smoke.lwylink.test emails")
		return
	}
	result, err := s.accounts.Register(r.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSendEmailVerification(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Email   string `json:"email"`
		Purpose string `json:"purpose"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.mail == nil {
		writeError(w, http.StatusServiceUnavailable, "mail service is disabled")
		return
	}
	err := s.mail.SendVerification(r.Context(), mail.SendInput{
		Email:   req.Email,
		Purpose: req.Purpose,
		IP:      clientIP(r),
	})
	if err != nil {
		writeError(w, statusForMailError(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePasswordReset(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Email       string `json:"email"`
		EmailCode   string `json:"emailCode"`
		NewPassword string `json:"newPassword"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowAccountAPIRate(r, "password-reset", account.NormalizeEmail(req.Email), rateLimitPasswordIP, rateLimitPasswordAccount) {
		writeError(w, http.StatusTooManyRequests, "password reset requests are too frequent")
		return
	}
	if err := s.requireEmailVerification(r.Context(), req.Email, mail.PurposePasswordReset, req.EmailCode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.accounts.ResetPassword(r.Context(), req.Email, req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "password-change", user.ID, rateLimitPasswordIP, rateLimitPasswordUser) {
		writeError(w, http.StatusTooManyRequests, "password change requests are too frequent")
		return
	}
	if err := s.accounts.ChangePassword(r.Context(), user.ID, req.OldPassword, req.NewPassword); err != nil {
		writeError(w, statusForAccountError(err, http.StatusBadRequest), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) requireEmailVerification(ctx context.Context, email, purpose, code string) error {
	if s.mail == nil || !s.mail.Enabled() {
		return nil
	}
	return s.mail.Verify(ctx, email, purpose, code)
}

func statusForMailError(err error) int {
	if errors.Is(err, mail.ErrVerificationRateLimit) {
		return http.StatusTooManyRequests
	}
	if errors.Is(err, mail.ErrMailDisabled) {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadRequest
}

func statusForAccountError(err error, fallback int) int {
	if errors.Is(err, account.ErrDisabled) {
		return http.StatusForbidden
	}
	if errors.Is(err, account.ErrUnauthorized) {
		return http.StatusUnauthorized
	}
	if errors.Is(err, account.ErrRelayTrafficExceeded) {
		return http.StatusPaymentRequired
	}
	return fallback
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowAccountAPIRate(r, "login", account.NormalizeEmail(req.Email), rateLimitLoginIP, rateLimitLoginAccount) {
		writeError(w, http.StatusTooManyRequests, "login requests are too frequent")
		return
	}
	result, err := s.accounts.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeError(w, statusForAccountError(err, http.StatusUnauthorized), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user.Public())
}

func (s *Server) handleAnnouncement(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) || !s.requireSettings(w) {
		return
	}
	announcement, err := s.settings.Announcement(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"announcement": announcement})
}

func (s *Server) handleRooms(w http.ResponseWriter, r *http.Request) {
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		rooms, err := s.rooms.ListForUser(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, rooms)
	case http.MethodPost:
		var req struct {
			Name       string `json:"name"`
			MaxMembers int    `json:"maxMembers"`
		}
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.allowUserAPIRate(r, "room-create", user.ID, rateLimitRoomCreateIP, rateLimitRoomCreateUser) {
			writeError(w, http.StatusTooManyRequests, "room creation requests are too frequent")
			return
		}
		roomInfo, err := s.rooms.CreateRoom(r.Context(), user.ID, req.Name, req.MaxMembers)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, roomInfo)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleJoinRoom(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		RoomPublicKey string `json:"roomPublicKey"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "room-join", user.ID, rateLimitRoomJoinIP, rateLimitRoomJoinUser) {
		writeError(w, http.StatusTooManyRequests, "room join requests are too frequent")
		return
	}
	roomInfo, err := s.rooms.JoinRoom(r.Context(), user.ID, req.RoomPublicKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roomInfo)
}

func (s *Server) handleDeleteRoom(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		RoomID int64 `json:"roomID"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "room-action", user.ID, rateLimitRoomActionIP, rateLimitRoomActionUser) {
		writeError(w, http.StatusTooManyRequests, "room action requests are too frequent")
		return
	}
	if err := s.rooms.DeleteRoom(r.Context(), user.ID, req.RoomID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRoomMembers(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	roomID, _ := strconv.ParseInt(r.URL.Query().Get("roomID"), 10, 64)
	if roomID <= 0 {
		writeError(w, http.StatusBadRequest, "bad roomID")
		return
	}
	members, err := s.rooms.ListMembers(r.Context(), user.ID, roomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) handleRoomChat(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		RoomID  int64  `json:"roomID"`
		Message string `json:"message"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "room-chat", user.ID, rateLimitRoomChatIP, rateLimitRoomChatUser) {
		writeError(w, http.StatusTooManyRequests, "room chat requests are too frequent")
		return
	}
	chat, err := s.rooms.SendChatWithTrafficCharge(r.Context(), user.ID, req.RoomID, req.Message, func(bytes int64) error {
		return s.accounts.ConsumeRelayTraffic(r.Context(), user.ID, bytes)
	})
	if err != nil {
		writeError(w, statusForAccountError(err, http.StatusBadRequest), err.Error())
		return
	}
	s.notifyUserUpdated(r.Context(), user.ID)
	writeJSON(w, http.StatusOK, chat)
}

func (s *Server) handleRoomVoice(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	roomID, _ := strconv.ParseInt(r.URL.Query().Get("roomID"), 10, 64)
	if roomID <= 0 {
		writeError(w, http.StatusBadRequest, "bad roomID")
		return
	}
	state, err := s.rooms.VoiceState(r.Context(), user.ID, roomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleJoinRoomVoice(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		RoomID int64 `json:"roomID"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "voice-action", user.ID, rateLimitVoiceActionIP, rateLimitVoiceActionUser) {
		writeError(w, http.StatusTooManyRequests, "voice requests are too frequent")
		return
	}
	state, err := s.rooms.JoinVoice(r.Context(), user.ID, req.RoomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleLeaveRoomVoice(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		RoomID int64 `json:"roomID"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "voice-action", user.ID, rateLimitVoiceActionIP, rateLimitVoiceActionUser) {
		writeError(w, http.StatusTooManyRequests, "voice requests are too frequent")
		return
	}
	state, err := s.rooms.LeaveVoice(r.Context(), user.ID, req.RoomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleRoomVoiceMic(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		RoomID   int64 `json:"roomID"`
		Speaking bool  `json:"speaking"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "voice-action", user.ID, rateLimitVoiceActionIP, rateLimitVoiceActionUser) {
		writeError(w, http.StatusTooManyRequests, "voice requests are too frequent")
		return
	}
	state, err := s.rooms.SetVoiceSpeaking(r.Context(), user.ID, req.RoomID, req.Speaking)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleRoomVoiceWebRTCICE(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "voice-ice", user.ID, rateLimitVoiceActionIP, rateLimitVoiceActionUser) {
		writeError(w, http.StatusTooManyRequests, "voice ICE requests are too frequent")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		ICEServers []config.ICEServerConfig `json:"iceServers"`
	}{
		ICEServers: s.voiceICEConfig(time.Now(), user.ID),
	})
}

func (s *Server) handleRoomVoiceWebRTCSignal(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		RoomID   int64           `json:"roomID"`
		ToUserID int64           `json:"toUserID"`
		Type     string          `json:"type"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "voice-signal", user.ID, rateLimitVoiceSignalIP, rateLimitVoiceSignalUser) {
		writeError(w, http.StatusTooManyRequests, "voice signaling requests are too frequent")
		return
	}
	if len(req.Payload) > 64*1024 {
		writeError(w, http.StatusBadRequest, "voice signal payload is too large")
		return
	}
	signal, err := s.rooms.SendVoiceSignal(r.Context(), user.ID, req.RoomID, req.ToUserID, req.Type, req.Payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, signal)
}

func (s *Server) handleRoomVoiceWebRTCTraffic(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		RoomID int64 `json:"roomID"`
		Bytes  int64 `json:"bytes"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "voice-traffic", user.ID, rateLimitVoiceTrafficIP, rateLimitVoiceTrafficUser) {
		writeError(w, http.StatusTooManyRequests, "voice traffic reports are too frequent")
		return
	}
	if req.RoomID <= 0 {
		writeError(w, http.StatusBadRequest, "bad roomID")
		return
	}
	if req.Bytes <= 0 || req.Bytes > maxVoiceRelayTrafficReportBytes {
		writeError(w, http.StatusBadRequest, "bad traffic bytes")
		return
	}
	state, err := s.rooms.VoiceState(r.Context(), user.ID, req.RoomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !voiceStateHasParticipant(state, user.ID) {
		writeError(w, http.StatusForbidden, "请先加入语音房")
		return
	}
	authoritative := s.turnUsageAuthoritative()
	chargedBytes := req.Bytes
	if authoritative {
		log.Printf("ignore client TURN traffic charge because coturn usage accounting is enabled user=%d room=%d bytes=%d", user.ID, req.RoomID, req.Bytes)
		chargedBytes = 0
	} else {
		event := turnUsageEvent{
			UserID:     user.ID,
			Bytes:      req.Bytes,
			Source:     "client",
			Status:     turnUsageStatusCharged,
			ReportedAt: time.Now(),
		}
		if err := s.accounts.ConsumeRelayTraffic(r.Context(), user.ID, req.Bytes); err != nil {
			event.Status = turnUsageStatusFailed
			event.ErrorMessage = err.Error()
			s.recordTurnUsageEvent(r.Context(), event)
			s.turnChargeFails.Add(1)
			writeError(w, statusForAccountError(err, http.StatusBadRequest), err.Error())
			return
		}
		s.turnTrafficUsed.Add(req.Bytes)
		s.notifyUserUpdated(r.Context(), user.ID)
		s.recordTurnUsageEvent(r.Context(), event)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"chargedBytes":  chargedBytes,
		"authoritative": authoritative,
	})
}

func (s *Server) handleRoomNetworkDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "network-diagnostics", user.ID, rateLimitNetworkDiagIP, rateLimitNetworkDiagUser) {
		writeError(w, http.StatusTooManyRequests, "network diagnostic reports are too frequent")
		return
	}
	var report room.NetworkDiagnosticReport
	if err := readJSON(r, &report); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.rooms == nil {
		writeError(w, http.StatusBadRequest, "room service is unavailable")
		return
	}
	if len(report.TransportDiagnostics) > 128 {
		writeError(w, http.StatusBadRequest, "network diagnostic report is too large")
		return
	}
	if err := s.rooms.ReportNetworkDiagnostics(r.Context(), user.ID, report); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func voiceStateHasParticipant(state room.VoiceState, userID int64) bool {
	for _, participant := range state.Participants {
		if participant.UserID == userID {
			return true
		}
	}
	return false
}

func (s *Server) handleRoomEvents(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	events, cleanup, err := s.rooms.SubscribeUserEvents(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cleanup()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	writeSSE(w, flusher, room.RoomEvent{
		Type:      room.RoomEventConnected,
		CreatedAt: time.Now(),
	})

	heartbeat := time.NewTicker(3 * time.Second)
	defer heartbeat.Stop()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if err := s.rooms.TouchForUser(r.Context(), user.ID); err != nil {
				log.Printf("room event heartbeat failed user=%d: %v", user.ID, err)
			}
		case <-keepalive.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			writeSSE(w, flusher, event)
		}
	}
}

func (s *Server) handleLeaveRoom(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		RoomID int64 `json:"roomID"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "room-action", user.ID, rateLimitRoomActionIP, rateLimitRoomActionUser) {
		writeError(w, http.StatusTooManyRequests, "room action requests are too frequent")
		return
	}
	if err := s.rooms.LeaveRoom(r.Context(), user.ID, req.RoomID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleKickRoomMember(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		RoomID int64 `json:"roomID"`
		UserID int64 `json:"userID"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "room-action", user.ID, rateLimitRoomActionIP, rateLimitRoomActionUser) {
		writeError(w, http.StatusTooManyRequests, "room action requests are too frequent")
		return
	}
	if err := s.rooms.KickMember(r.Context(), user.ID, req.RoomID, req.UserID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTransferRoomOwner(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		RoomID int64 `json:"roomID"`
		UserID int64 `json:"userID"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allowUserAPIRate(r, "room-action", user.ID, rateLimitRoomActionIP, rateLimitRoomActionUser) {
		writeError(w, http.StatusTooManyRequests, "room action requests are too frequent")
		return
	}
	roomInfo, err := s.rooms.TransferOwner(r.Context(), user.ID, req.RoomID, req.UserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roomInfo)
}

func (s *Server) handleLatestClientRelease(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) || !s.requireReleases(w, http.StatusNotFound) {
		return
	}
	platform := clientrelease.NormalizePlatform(r.URL.Query().Get("platform"))
	release, err := s.releases.Latest(r.Context(), platform)
	if err != nil {
		if errors.Is(err, clientrelease.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no client release for platform")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.publicClientRelease(r, release))
}

func (s *Server) handleClientReleaseDownload(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) || !s.requireReleases(w, http.StatusNotFound) {
		return
	}
	storedName := strings.TrimPrefix(r.URL.Path, "/downloads/client/")
	storedName, err := url.PathUnescape(storedName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}
	release, err := s.releases.FindByStoredFileName(r.Context(), storedName)
	if err != nil {
		if errors.Is(err, clientrelease.ErrNotFound) {
			writeError(w, http.StatusNotFound, "release file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	auth, err := s.authorizeClientDownload(r, release)
	if err != nil {
		s.logClientDownload(r, release, 0, "unauthorized")
		writeError(w, http.StatusUnauthorized, "login required")
		return
	}
	if !s.allowClientDownload(r, auth) {
		s.logClientDownload(r, release, auth.userID, "rate_limited")
		writeError(w, http.StatusTooManyRequests, "download requests are too frequent")
		return
	}
	s.logClientDownload(r, release, auth.userID, auth.result)
	if release.ContentType != "" {
		w.Header().Set("Content-Type", release.ContentType)
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": release.FileName}))
	http.ServeFile(w, r, s.releases.FilePath(release))
}

func (s *Server) userFromRequest(r *http.Request) (account.User, error) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return account.User{}, fmt.Errorf("missing bearer token")
	}
	return s.accounts.AuthenticateToken(r.Context(), strings.TrimPrefix(auth, "Bearer "))
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !s.allowIPAPIRate(r, "admin", rateLimitAdminIP) {
		writeError(w, http.StatusTooManyRequests, "admin requests are too frequent")
		return false
	}
	if s.adminAuthorized(r) {
		return true
	}
	writeError(w, http.StatusUnauthorized, "admin token required")
	return false
}

func (s *Server) requireAdminPost(w http.ResponseWriter, r *http.Request) bool {
	return requireMethod(w, r, http.MethodPost) && s.requireAdmin(w, r)
}

func (s *Server) requireNodes(w http.ResponseWriter) bool {
	if s.nodes != nil {
		return true
	}
	writeError(w, http.StatusBadRequest, "node service is not configured")
	return false
}

func (s *Server) requireReleases(w http.ResponseWriter, status int) bool {
	if s.releases != nil {
		return true
	}
	writeError(w, status, "client release service is not configured")
	return false
}

func (s *Server) requireSettings(w http.ResponseWriter) bool {
	if s.settings != nil {
		return true
	}
	writeError(w, http.StatusBadRequest, "app settings service is not configured")
	return false
}

func (s *Server) notifyUserUpdated(ctx context.Context, userID int64) {
	if s.rooms != nil && userID > 0 {
		s.rooms.NotifyUserUpdated(ctx, userID)
	}
}

func (s *Server) redirectAdminFromForm(w http.ResponseWriter, r *http.Request, path, statusField string) {
	http.Redirect(w, r, s.adminURLFromForm(path, r, statusField), http.StatusSeeOther)
}

func formInt64(w http.ResponseWriter, r *http.Request, field, errorMessage string) (int64, bool) {
	value, _ := strconv.ParseInt(r.FormValue(field), 10, 64)
	if value <= 0 {
		writeError(w, http.StatusBadRequest, errorMessage)
		return 0, false
	}
	return value, true
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return decodeSingleJSON(dec, v)
}

func decodeSingleJSON(dec *json.Decoder, v any) error {
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain a single JSON object")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event room.RoomEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: room\ndata: %s\n\n", data)
	flusher.Flush()
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func normalizeICEServers(servers []config.ICEServerConfig) []config.ICEServerConfig {
	out := make([]config.ICEServerConfig, 0, len(servers))
	for _, server := range servers {
		urls := normalizeICEURLs(server.URLs)
		if len(urls) == 0 {
			continue
		}
		out = append(out, config.ICEServerConfig{
			URLs:       urls,
			Username:   strings.TrimSpace(server.Username),
			Credential: strings.TrimSpace(server.Credential),
		})
	}
	if len(out) == 0 {
		return []config.ICEServerConfig{{URLs: []string{"stun:stun.lwylink.xyz:3478"}}}
	}
	return out
}

func (s *Server) voiceICEConfig(now time.Time, userID int64) []config.ICEServerConfig {
	servers := normalizeICEServers(s.voiceICEServers)
	if turn, ok := turnRESTICEServer(s.voiceTURN, now, userID); ok {
		servers = append(servers, turn)
	}
	return servers
}

func turnRESTICEServer(cfg config.TURNCredentialConfig, now time.Time, userID int64) (config.ICEServerConfig, bool) {
	urls := normalizeICEURLs(cfg.URLs)
	secret := strings.TrimSpace(cfg.StaticAuthSecret)
	if len(urls) == 0 || secret == "" {
		return config.ICEServerConfig{}, false
	}
	ttl := cfg.TTLSeconds
	if ttl <= 0 {
		ttl = 3600
	}
	expiry := now.Add(time.Duration(ttl) * time.Second).Unix()
	username := strconv.FormatInt(expiry, 10)
	if userID > 0 {
		username += fmt.Sprintf(":u%d", userID)
	}
	if prefix := strings.TrimSpace(cfg.UsernamePrefix); prefix != "" {
		username += ":" + prefix
	}
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write([]byte(username))
	credential := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return config.ICEServerConfig{
		URLs:       urls,
		Username:   username,
		Credential: credential,
	}, true
}

func normalizeICEURLs(values []string) []string {
	urls := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			urls = append(urls, value)
		}
	}
	return urls
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withAPIJSONBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/") {
			r.Body = http.MaxBytesReader(w, r.Body, maxAPIJSONBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) turnUsageAuthoritative() bool {
	if s.turnUsage == nil {
		return false
	}
	stats := s.turnUsageStats(time.Now())
	return stats.LastPollAt.IsZero() || stats.LogAvailable
}

func (s *Server) chargeCoturnTURNTraffic(ctx context.Context, event turnUsageEvent) error {
	if event.UserID <= 0 || event.Bytes <= 0 {
		return nil
	}
	if s.accounts == nil {
		s.turnTrafficUsed.Add(event.Bytes)
		return nil
	}
	if err := s.accounts.ConsumeRelayTraffic(ctx, event.UserID, event.Bytes); err != nil {
		return err
	}
	s.turnTrafficUsed.Add(event.Bytes)
	s.notifyUserUpdated(ctx, event.UserID)
	return nil
}

func (s *Server) recordTurnUsageEvent(ctx context.Context, event turnUsageEvent) {
	if s.turnLedger == nil {
		return
	}
	if err := s.turnLedger.Record(ctx, event); err != nil {
		log.Printf("turn usage ledger record failed user=%d bytes=%d source=%s: %v", event.UserID, event.Bytes, event.Source, err)
	}
}

func withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &accessLogResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		entry := map[string]any{
			"event":       "http_request",
			"method":      r.Method,
			"path":        r.URL.Path,
			"status":      recorder.status,
			"bytes":       recorder.bytes,
			"duration_ms": time.Since(start).Milliseconds(),
			"ip":          clientIP(r),
			"user_agent":  r.UserAgent(),
		}
		if requestID := strings.TrimSpace(r.Header.Get("X-Request-ID")); requestID != "" {
			entry["request_id"] = requestID
		}
		data, err := json.Marshal(entry)
		if err != nil {
			log.Printf("http_request status=%d method=%s path=%s", recorder.status, r.Method, r.URL.Path)
			return
		}
		log.Print(string(data))
	})
}

type accessLogResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *accessLogResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *accessLogResponseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

func (w *accessLogResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	stats, err := s.adminStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	users, err := s.accounts.SearchUsers(r.Context(), account.UserFilter{Limit: 5})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rooms, err := s.rooms.Search(r.Context(), room.Filter{Limit: 5})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := adminPageData{
		Active:       "overview",
		Title:        "概览",
		Token:        s.adminToken,
		Stats:        stats,
		Users:        users,
		Rooms:        rooms,
		ReadOnlyRows: true,
		Announcement: appsettings.DefaultAnnouncement,
	}
	if s.settings != nil {
		announcement, err := s.settings.Announcement(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data.Announcement = announcement
	}
	if s.nodes != nil {
		nodes, err := s.nodes.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(nodes) > 5 {
			nodes = nodes[:5]
		}
		data.Nodes = nodes
	}
	s.renderAdmin(w, data)
}

func (s *Server) handleAdminMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	metrics, err := s.adminMetrics(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") || r.URL.Query().Get("format") == "json" {
		writeJSON(w, http.StatusOK, metrics)
		return
	}
	stats, err := s.adminStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := adminPageData{
		Active:       "metrics",
		Title:        "监控",
		Token:        s.adminToken,
		Stats:        stats,
		Metrics:      metrics,
		Nodes:        metrics.Nodes,
		ReadOnlyRows: true,
	}
	s.renderAdmin(w, data)
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	listReq := adminListRequestFromQuery(r, []string{"active", "disabled"})
	users, err := s.accounts.SearchUsers(r.Context(), account.UserFilter{
		Query:  listReq.Query,
		Status: listReq.Status,
		Limit:  listReq.PerPage + 1,
		Offset: listReq.Offset(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	users, hasNext := trimAdminPage(users, listReq.PerPage)
	stats, err := s.adminStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := s.adminListPageData("users", "玩家账号", "/admin/users", listReq, hasNext)
	data.Stats = stats
	data.Users = users
	s.renderAdmin(w, data)
}

func (s *Server) handleAdminRooms(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	listReq := adminListRequestFromQuery(r, []string{room.StatusOpen, room.StatusClosed})
	rooms, err := s.rooms.Search(r.Context(), room.Filter{
		Query:  listReq.Query,
		Status: listReq.Status,
		Limit:  listReq.PerPage + 1,
		Offset: listReq.Offset(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rooms, hasNext := trimAdminPage(rooms, listReq.PerPage)
	stats, err := s.adminStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := s.adminListPageData("rooms", "房间", "/admin/rooms", listReq, hasNext)
	data.Stats = stats
	data.Rooms = rooms
	if s.rooms != nil {
		data.RoomNetworkDiagnostics = s.rooms.NetworkDiagnostics(roomIDs(rooms))
	}
	s.renderAdmin(w, data)
}

func (s *Server) handleAdminNodes(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || !s.requireNodes(w) {
		return
	}
	listReq := adminListRequestFromQuery(r, []string{node.StatusOnline, node.StatusPaused, node.StatusDisabled, node.StatusUnknown})
	nodes, err := s.nodes.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	nodes = filterAdminNodes(nodes, listReq.Query, listReq.Status)
	nodes, hasNext := paginateAdminNodes(nodes, listReq.Page, listReq.PerPage)
	stats, err := s.adminStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := s.adminListPageData("nodes", "服务器节点", "/admin/nodes", listReq, hasNext)
	data.Stats = stats
	data.Nodes = nodes
	s.renderAdmin(w, data)
}

func (s *Server) handleAdminReleases(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || !s.requireReleases(w, http.StatusBadRequest) {
		return
	}
	listReq := adminListRequestFromQuery(r, nil)
	releases, err := s.releases.List(r.Context(), clientrelease.Filter{
		Query:  listReq.Query,
		Limit:  listReq.PerPage + 1,
		Offset: listReq.Offset(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	releases, hasNext := trimAdminPage(releases, listReq.PerPage)
	stats, err := s.adminStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := s.adminListPageData("releases", "更新发布", "/admin/releases", listReq, hasNext)
	data.Stats = stats
	data.Releases = releases
	s.renderAdmin(w, data)
}

func (s *Server) handleAdminTurnUsage(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if s.turnLedger == nil {
		page, perPage := adminPagination(r.URL.Query().Get("page"), r.URL.Query().Get("perPage"))
		data := s.adminListPageData("turn-usage", "TURN 对账", "/admin/turn-usage", adminListRequest{Page: page, PerPage: perPage}, false)
		data.TurnUsage = turnUsagePage{}
		s.renderAdmin(w, data)
		return
	}
	userID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("userID")), 10, 64)
	date := strings.TrimSpace(r.URL.Query().Get("date"))
	var start, end time.Time
	if date != "" {
		parsed, err := time.ParseInLocation("2006-01-02", date, time.Local)
		if err != nil {
			writeError(w, http.StatusBadRequest, "date must use YYYY-MM-DD")
			return
		}
		start = parsed
		end = parsed.AddDate(0, 0, 1)
	}
	page, perPage := adminPagination(r.URL.Query().Get("page"), r.URL.Query().Get("perPage"))
	usage, err := s.turnLedger.List(r.Context(), turnUsageFilter{
		UserID:    userID,
		StartTime: start,
		EndTime:   end,
		Limit:     perPage,
		Offset:    dbutil.Offset((page - 1) * perPage),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stats, err := s.adminStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasNext := usage.HasMore
	data := s.adminListPageData("turn-usage", "TURN 对账", "/admin/turn-usage", adminListRequest{Page: page, PerPage: perPage}, hasNext)
	data.Stats = stats
	data.TurnUsage = usage
	data.TurnUsageUserID = userID
	data.TurnUsageDate = date
	data.CurrentURL = s.adminTurnUsageURL(userID, date, page, perPage)
	if page > 1 {
		data.PrevURL = s.adminTurnUsageURL(userID, date, page-1, perPage)
	}
	if hasNext {
		data.NextURL = s.adminTurnUsageURL(userID, date, page+1, perPage)
	}
	s.renderAdmin(w, data)
}

func (s *Server) renderAdmin(w http.ResponseWriter, data adminPageData) {
	data.Nav = []adminNavItem{
		{Key: "overview", Label: "概览", URL: s.adminURL("/admin")},
		{Key: "metrics", Label: "监控", URL: s.adminURL("/admin/metrics")},
		{Key: "turn-usage", Label: "TURN 对账", URL: s.adminURL("/admin/turn-usage")},
		{Key: "nodes", Label: "节点", URL: s.adminURL("/admin/nodes")},
		{Key: "users", Label: "玩家", URL: s.adminURL("/admin/users")},
		{Key: "rooms", Label: "房间", URL: s.adminURL("/admin/rooms")},
		{Key: "releases", Label: "更新发布", URL: s.adminURL("/admin/releases")},
	}
	if data.CurrentURL == "" {
		data.CurrentURL = data.Nav[0].URL
		for _, item := range data.Nav {
			if item.Key == data.Active {
				data.CurrentURL = item.URL
				break
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := adminModuleTemplate.Execute(w, data); err != nil {
		log.Printf("render admin failed: %v", err)
	}
}

func (s *Server) handleAdminReleaseUpload(w http.ResponseWriter, r *http.Request) {
	s.handleAdminReleaseUploadLimited(w, r, maxAdminReleaseUploadBodyBytes)
}

func (s *Server) handleAdminReleaseUploadLimited(w http.ResponseWriter, r *http.Request, maxBodyBytes int64) {
	if maxBodyBytes <= 0 {
		maxBodyBytes = maxAdminReleaseUploadBodyBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if !s.requireAdminPost(w, r) || !s.requireReleases(w, http.StatusBadRequest) {
		return
	}
	if err := r.ParseMultipartForm(maxBodyBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择更新包")
		return
	}
	defer file.Close()
	_, err = s.releases.Upload(r.Context(), clientrelease.UploadInput{
		Version:     r.FormValue("version"),
		Platform:    r.FormValue("platform"),
		Notes:       r.FormValue("notes"),
		InstallArgs: r.FormValue("installArgs"),
	}, file, header)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, s.adminURL("/admin/releases"), http.StatusSeeOther)
}

func (s *Server) handleAdminReleaseDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminPost(w, r) || !s.requireReleases(w, http.StatusBadRequest) {
		return
	}
	releaseID, ok := formInt64(w, r, "releaseID", "bad releaseID")
	if !ok {
		return
	}
	if err := s.releases.Delete(r.Context(), releaseID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.redirectAdminFromForm(w, r, "/admin/releases", "status")
}

func (s *Server) handleAdminAnnouncement(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminPost(w, r) || !s.requireSettings(w) {
		return
	}
	if err := s.settings.SetAnnouncement(r.Context(), r.FormValue("announcement")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, s.adminURL("/admin"), http.StatusSeeOther)
}

func (s *Server) handleAdminUserStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminPost(w, r) {
		return
	}
	userID, ok := formInt64(w, r, "userID", "bad userID")
	if !ok {
		return
	}
	disabled := r.FormValue("disabled") == "true"
	if err := s.accounts.SetDisabled(r.Context(), userID, disabled); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.notifyUserUpdated(r.Context(), userID)
	s.redirectAdminFromForm(w, r, "/admin/users", "status")
}

func (s *Server) handleAdminUserMembership(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminPost(w, r) {
		return
	}
	userID, ok := formInt64(w, r, "userID", "bad userID")
	if !ok {
		return
	}
	if err := s.accounts.SetMembership(r.Context(), userID, r.FormValue("membership")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.notifyUserUpdated(r.Context(), userID)
	s.redirectAdminFromForm(w, r, "/admin/users", "status")
}

func (s *Server) handleAdminUserTraffic(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminPost(w, r) {
		return
	}
	userID, ok := formInt64(w, r, "userID", "bad userID")
	if !ok {
		return
	}
	bytes, err := parseTrafficGrantBytes(r.FormValue("amountMB"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.accounts.AddRelayTraffic(r.Context(), userID, bytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.notifyUserUpdated(r.Context(), userID)
	s.redirectAdminFromForm(w, r, "/admin/users", "status")
}

func (s *Server) handleAdminUserDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminPost(w, r) {
		return
	}
	userID, ok := formInt64(w, r, "userID", "bad userID")
	if !ok {
		return
	}
	if s.rooms != nil {
		if err := s.rooms.DeleteRoomsOwnedBy(r.Context(), userID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := s.accounts.Delete(r.Context(), userID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.notifyUserUpdated(r.Context(), userID)
	s.redirectAdminFromForm(w, r, "/admin/users", "status")
}

func (s *Server) handleAdminNodeSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminPost(w, r) || !s.requireNodes(w) {
		return
	}
	id, _ := strconv.ParseInt(r.FormValue("nodeID"), 10, 64)
	maxRooms, _ := strconv.Atoi(r.FormValue("maxRooms"))
	enabled := r.FormValue("enabled") != "false"
	n := node.Node{
		ID:                id,
		Name:              r.FormValue("name"),
		PublicOverlayAddr: r.FormValue("publicOverlayAddr"),
		PublicUDPAddr:     r.FormValue("publicUDPAddr"),
		HealthAddr:        r.FormValue("healthAddr"),
		OverlayPublicKey:  r.FormValue("overlayPublicKey"),
		MaxRooms:          maxRooms,
		Enabled:           enabled,
	}
	if _, err := s.nodes.CreateOrUpdate(r.Context(), n); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.redirectAdminFromForm(w, r, "/admin/nodes", "status")
}

func (s *Server) handleAdminNodeEnabled(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminPost(w, r) || !s.requireNodes(w) {
		return
	}
	nodeID, ok := formInt64(w, r, "nodeID", "bad nodeID")
	if !ok {
		return
	}
	enabled := r.FormValue("enabled") == "true"
	if err := s.nodes.SetEnabled(r.Context(), nodeID, enabled); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.redirectAdminFromForm(w, r, "/admin/nodes", "status")
}

func (s *Server) handleAdminNodeCheck(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminPost(w, r) || !s.requireNodes(w) {
		return
	}
	nodeID, ok := formInt64(w, r, "nodeID", "bad nodeID")
	if !ok {
		return
	}
	if err := s.nodes.CheckNow(r.Context(), nodeID); err != nil {
		log.Printf("manual node check failed node=%d: %v", nodeID, err)
	}
	s.redirectAdminFromForm(w, r, "/admin/nodes", "status")
}

func (s *Server) handleAdminRoomStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminPost(w, r) {
		return
	}
	roomID, ok := formInt64(w, r, "roomID", "bad roomID")
	if !ok {
		return
	}
	var err error
	status := r.FormValue("newStatus")
	if status == room.StatusOpen {
		err = s.rooms.ReopenRoom(r.Context(), roomID)
	} else {
		err = s.rooms.CloseRoom(r.Context(), roomID)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.redirectAdminFromForm(w, r, "/admin/rooms", "filterStatus")
}

func (s *Server) adminAuthorized(r *http.Request) bool {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.FormValue("token")
	}
	return token != "" && token == s.adminToken
}

type adminPageData struct {
	Active                 string
	Title                  string
	CurrentURL             string
	Token                  string
	Query                  string
	Status                 string
	Page                   int
	PerPage                int
	HasPrev                bool
	HasNext                bool
	PrevURL                string
	NextURL                string
	Stats                  adminStats
	Metrics                adminMetrics
	Nav                    []adminNavItem
	Users                  []account.User
	Rooms                  []room.Room
	RoomNetworkDiagnostics map[int64][]room.NetworkDiagnosticReport
	TurnUsage              turnUsagePage
	TurnUsageUserID        int64
	TurnUsageDate          string
	Nodes                  []node.Node
	Releases               []clientrelease.Release
	Announcement           string
	ReadOnlyRows           bool
}

type adminListRequest struct {
	Query   string
	Status  string
	Page    int
	PerPage int
}

func (r adminListRequest) Offset() int {
	return dbutil.Offset((r.Page - 1) * r.PerPage)
}

type adminNavItem struct {
	Key   string
	Label string
	URL   string
}

type adminStats struct {
	Users         int
	ActiveUsers   int
	DisabledUsers int
	Rooms         int
	OpenRooms     int
	ClosedRooms   int
	Nodes         int
	OnlineNodes   int
	PausedNodes   int
	Releases      int
}

type adminMetrics struct {
	GeneratedAt           time.Time              `json:"generatedAt"`
	Users                 int                    `json:"users"`
	ActiveUsers           int                    `json:"activeUsers"`
	Rooms                 int                    `json:"rooms"`
	OpenRooms             int                    `json:"openRooms"`
	OnlineRooms           int                    `json:"onlineRooms"`
	OverlayConnections    int                    `json:"overlayConnections"`
	AppOnlineUsers        int                    `json:"appOnlineUsers"`
	VoiceRooms            int                    `json:"voiceRooms"`
	VoiceParticipants     int                    `json:"voiceParticipants"`
	VoiceSpeakers         int                    `json:"voiceSpeakers"`
	RelayTrafficUsedBytes int64                  `json:"relayTrafficUsedBytes"`
	TURNTrafficUsedBytes  int64                  `json:"turnTrafficUsedBytes"`
	TURNChargeFailures    int64                  `json:"turnChargeFailures"`
	CoturnLogLagSeconds   float64                `json:"coturnLogLagSeconds"`
	CoturnLastUsageAt     time.Time              `json:"coturnLastUsageAt,omitempty"`
	DownloadsLastHour     int                    `json:"downloadsLastHour"`
	DownloadsLastDay      int                    `json:"downloadsLastDay"`
	Releases              int                    `json:"releases"`
	LatestWindowsRelease  *clientrelease.Release `json:"latestWindowsRelease,omitempty"`
	Nodes                 []node.Node            `json:"nodes,omitempty"`
	RecentErrors          []string               `json:"recentErrors"`
}

func (s *Server) adminStats(ctx context.Context) (adminStats, error) {
	stats := adminStats{}
	userStats, err := s.accounts.Stats(ctx)
	if err != nil {
		return stats, err
	}
	stats.Users = userStats.Total
	stats.ActiveUsers = userStats.Active
	stats.DisabledUsers = userStats.Disabled

	roomStats, err := s.rooms.Stats(ctx)
	if err != nil {
		return stats, err
	}
	stats.Rooms = roomStats.Total
	stats.OpenRooms = roomStats.Open
	stats.ClosedRooms = roomStats.Closed

	if s.nodes != nil {
		nodeStats, err := s.nodes.Stats(ctx)
		if err != nil {
			return stats, err
		}
		stats.Nodes = nodeStats.Total
		stats.OnlineNodes = nodeStats.Online
		stats.PausedNodes = nodeStats.Paused
	}
	if s.releases != nil {
		releases, err := s.releases.Count(ctx)
		if err != nil {
			return stats, err
		}
		stats.Releases = releases
	}
	return stats, nil
}

func (s *Server) adminMetrics(ctx context.Context) (adminMetrics, error) {
	metrics := adminMetrics{GeneratedAt: time.Now()}
	userStats, err := s.accounts.Stats(ctx)
	if err != nil {
		return metrics, err
	}
	metrics.Users = userStats.Total
	metrics.ActiveUsers = userStats.Active
	metrics.RelayTrafficUsedBytes = userStats.RelayTrafficUsedBytes
	metrics.TURNTrafficUsedBytes = s.turnTrafficUsed.Load()

	roomStats, err := s.rooms.Stats(ctx)
	if err != nil {
		return metrics, err
	}
	metrics.Rooms = roomStats.Total
	metrics.OpenRooms = roomStats.Open
	voiceStats := s.rooms.VoiceStats()
	metrics.VoiceRooms = voiceStats.Rooms
	metrics.VoiceParticipants = voiceStats.Participants
	metrics.VoiceSpeakers = voiceStats.Speakers
	onlineMetrics, err := s.rooms.OnlineMetrics(ctx)
	if err != nil {
		metrics.RecentErrors = append(metrics.RecentErrors, "online metrics: "+err.Error())
	} else {
		metrics.OnlineRooms = onlineMetrics.OnlineRooms
		metrics.OverlayConnections = onlineMetrics.OverlayConnections
		metrics.AppOnlineUsers = onlineMetrics.AppOnlineUsers
	}
	turnStats := s.turnUsageStats(time.Now())
	metrics.TURNChargeFailures = turnStats.ChargeFailures + s.turnChargeFails.Load()
	metrics.CoturnLogLagSeconds = turnStats.LogLagSeconds
	metrics.CoturnLastUsageAt = turnStats.LastUsageAt
	if turnStats.LastProcessError != "" {
		metrics.RecentErrors = append(metrics.RecentErrors, "coturn usage: "+turnStats.LastProcessError)
	}

	if s.nodes != nil {
		nodes, err := s.nodes.List(ctx)
		if err != nil {
			return metrics, err
		}
		metrics.Nodes = nodes
		for _, item := range nodes {
			if strings.TrimSpace(item.LastError) != "" {
				metrics.RecentErrors = append(metrics.RecentErrors, fmt.Sprintf("%s: %s", item.Name, item.LastError))
			}
		}
	}
	if s.releases != nil {
		releaseCount, err := s.releases.Count(ctx)
		if err != nil {
			return metrics, err
		}
		metrics.Releases = releaseCount
		downloadCount, err := s.releases.DownloadCountSince(ctx, time.Now().Add(-time.Hour))
		if err != nil {
			return metrics, err
		}
		metrics.DownloadsLastHour = downloadCount
		downloadDayCount, err := s.releases.DownloadCountSince(ctx, time.Now().Add(-24*time.Hour))
		if err != nil {
			return metrics, err
		}
		metrics.DownloadsLastDay = downloadDayCount
		latest, err := s.releases.Latest(ctx, "windows-amd64")
		if err == nil {
			metrics.LatestWindowsRelease = &latest
		}
	}
	return metrics, nil
}

func (s *Server) turnUsageStats(now time.Time) coturnUsageStats {
	if s.turnUsage == nil {
		return coturnUsageStats{}
	}
	return s.turnUsage.Stats(now)
}

func (s *Server) prometheusMetrics(ctx context.Context, now time.Time) (string, error) {
	metrics, err := s.adminMetrics(ctx)
	if err != nil {
		return "", err
	}
	turnStats := s.turnUsageStats(now)
	nodeDown := 0
	for _, item := range metrics.Nodes {
		if item.Enabled && item.Status != node.StatusOnline {
			nodeDown++
		}
	}

	var b strings.Builder
	writePromMetric(&b, "lwylink_users_total", "Total registered users.", float64(metrics.Users))
	writePromMetric(&b, "lwylink_users_active", "Active user accounts.", float64(metrics.ActiveUsers))
	writePromMetric(&b, "lwylink_rooms_total", "Total rooms.", float64(metrics.Rooms))
	writePromMetric(&b, "lwylink_rooms_open", "Open rooms.", float64(metrics.OpenRooms))
	writePromMetric(&b, "lwylink_rooms_online", "Open rooms with at least one online member.", float64(metrics.OnlineRooms))
	writePromMetric(&b, "lwylink_overlay_connections", "Online overlay member connections.", float64(metrics.OverlayConnections))
	writePromMetric(&b, "lwylink_room_app_online_users", "Room members with active app presence.", float64(metrics.AppOnlineUsers))
	writePromMetric(&b, "lwylink_voice_participants", "Current WebRTC voice participants.", float64(metrics.VoiceParticipants))
	writePromMetric(&b, "lwylink_relay_traffic_used_bytes", "Account relay traffic used in the current period.", float64(metrics.RelayTrafficUsedBytes))
	writePromMetric(&b, "lwylink_turn_traffic_used_bytes", "TURN relay traffic observed by coturn/client reporting.", float64(metrics.TURNTrafficUsedBytes))
	writePromCounter(&b, "lwylink_turn_charge_failures_total", "TURN traffic charge failures.", float64(metrics.TURNChargeFailures))
	writePromMetric(&b, "lwylink_coturn_usage_collector_enabled", "Whether coturn usage log collection is enabled.", boolMetric(turnStats.Enabled))
	writePromMetric(&b, "lwylink_coturn_usage_log_available", "Whether the configured coturn usage log was readable on the last poll.", boolMetric(turnStats.LogAvailable))
	writePromCounter(&b, "lwylink_coturn_usage_processed_total", "Processed coturn usage lines.", float64(turnStats.ProcessedLines))
	writePromMetric(&b, "lwylink_coturn_log_lag_seconds", "Seconds since coturn usage log was last modified.", turnStats.LogLagSeconds)
	writePromMetric(&b, "lwylink_nodes_total", "Configured server nodes.", float64(len(metrics.Nodes)))
	writePromMetric(&b, "lwylink_nodes_down", "Enabled nodes that are not online.", float64(nodeDown))
	writePromMetric(&b, "lwylink_downloads_last_hour", "Client downloads in the last hour.", float64(metrics.DownloadsLastHour))
	writePromMetric(&b, "lwylink_downloads_last_day", "Client downloads in the last day.", float64(metrics.DownloadsLastDay))
	if !turnStats.LastUsageAt.IsZero() {
		writePromMetric(&b, "lwylink_coturn_last_usage_timestamp_seconds", "Unix timestamp of the last processed coturn usage line.", float64(turnStats.LastUsageAt.Unix()))
	}
	if !turnStats.LastPollAt.IsZero() {
		writePromMetric(&b, "lwylink_coturn_last_poll_timestamp_seconds", "Unix timestamp of the last coturn usage log poll.", float64(turnStats.LastPollAt.Unix()))
	}
	return b.String(), nil
}

func boolMetric(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func writePromMetric(b *strings.Builder, name, help string, value float64) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s gauge\n", name)
	fmt.Fprintf(b, "%s %g\n", name, value)
}

func writePromCounter(b *strings.Builder, name, help string, value float64) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s counter\n", name)
	fmt.Fprintf(b, "%s %g\n", name, value)
}

func (s *Server) adminURL(path string) string {
	values := url.Values{}
	values.Set("token", s.adminToken)
	return path + "?" + values.Encode()
}

func (s *Server) adminTurnUsageURL(userID int64, date string, page, perPage int) string {
	values := url.Values{}
	values.Set("token", s.adminToken)
	if userID > 0 {
		values.Set("userID", strconv.FormatInt(userID, 10))
	}
	if strings.TrimSpace(date) != "" {
		values.Set("date", strings.TrimSpace(date))
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if perPage > 0 && perPage != defaultAdminPerPage {
		values.Set("perPage", strconv.Itoa(perPage))
	}
	return "/admin/turn-usage?" + values.Encode()
}

func (s *Server) clientReleaseDownloadURL(r *http.Request, release clientrelease.Release) string {
	scheme, host := requestPublicSchemeHost(r)
	return scheme + "://" + host + "/downloads/client/" + url.PathEscape(release.StoredFileName)
}

func requestPublicSchemeHost(r *http.Request) (string, string) {
	scheme := firstForwardedValue(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := r.Host
	if forwardedHost := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = forwardedHost
	}
	if forwarded := parseForwardedHeader(r.Header.Get("Forwarded")); forwarded != nil {
		if forwarded["proto"] != "" {
			scheme = forwarded["proto"]
		}
		if forwarded["host"] != "" {
			host = forwarded["host"]
		}
	}
	if scheme != "https" {
		scheme = "http"
	}
	if host == "" {
		host = "localhost"
	}
	return scheme, host
}

func firstForwardedValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if i := strings.IndexByte(value, ','); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

func parseForwardedHeader(value string) map[string]string {
	value = firstForwardedValue(value)
	if value == "" {
		return nil
	}
	out := make(map[string]string)
	for _, part := range strings.Split(value, ";") {
		key, raw, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		raw = strings.Trim(strings.TrimSpace(raw), `"`)
		switch key {
		case "proto", "host", "for":
			out[key] = raw
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Server) publicClientRelease(r *http.Request, release clientrelease.Release) clientrelease.PublicRelease {
	return clientrelease.PublicRelease{
		Version:     release.Version,
		Platform:    release.Platform,
		Notes:       release.Notes,
		Mandatory:   true,
		InstallArgs: release.InstallArgs,
		DownloadURL: s.clientReleaseDownloadURL(r, release),
		FileName:    release.FileName,
		SizeBytes:   release.SizeBytes,
		SHA256:      release.SHA256,
		CreatedAt:   release.CreatedAt,
	}
}

func (s *Server) adminListPageData(active, title, path string, listReq adminListRequest, hasNext bool) adminPageData {
	prevURL, nextURL := s.adminPageURLs(path, listReq, hasNext)
	return adminPageData{
		Active:     active,
		Title:      title,
		CurrentURL: s.adminURLWithFilters(path, listReq.Query, listReq.Status, listReq.Page, listReq.PerPage),
		Token:      s.adminToken,
		Query:      listReq.Query,
		Status:     listReq.Status,
		Page:       listReq.Page,
		PerPage:    listReq.PerPage,
		HasPrev:    listReq.Page > 1,
		HasNext:    hasNext,
		PrevURL:    prevURL,
		NextURL:    nextURL,
	}
}

func (s *Server) adminURLWithFilters(path, query, status string, pageSettings ...int) string {
	values := url.Values{}
	values.Set("token", s.adminToken)
	query = strings.TrimSpace(query)
	status = strings.TrimSpace(status)
	if query != "" {
		values.Set("q", query)
	}
	if status != "" {
		values.Set("status", status)
	}
	if len(pageSettings) > 0 && pageSettings[0] > 1 {
		values.Set("page", strconv.Itoa(pageSettings[0]))
	}
	if len(pageSettings) > 1 && pageSettings[1] > 0 && pageSettings[1] != defaultAdminPerPage {
		values.Set("perPage", strconv.Itoa(pageSettings[1]))
	}
	return path + "?" + values.Encode()
}

func (s *Server) adminURLFromForm(path string, r *http.Request, statusField string) string {
	page, perPage := adminPaginationFromForm(r)
	return s.adminURLWithFilters(path, r.FormValue("q"), r.FormValue(statusField), page, perPage)
}

func roomIDs(rooms []room.Room) []int64 {
	out := make([]int64, 0, len(rooms))
	for _, item := range rooms {
		if item.ID > 0 {
			out = append(out, item.ID)
		}
	}
	return out
}

func roomDiagnostics(items map[int64][]room.NetworkDiagnosticReport, roomID int64) []room.NetworkDiagnosticReport {
	if len(items) == 0 || roomID <= 0 {
		return nil
	}
	return items[roomID]
}

func (s *Server) adminPageURLs(path string, listReq adminListRequest, hasNext bool) (string, string) {
	prevURL := ""
	if listReq.Page > 1 {
		prevURL = s.adminURLWithFilters(path, listReq.Query, listReq.Status, listReq.Page-1, listReq.PerPage)
	}
	nextURL := ""
	if hasNext {
		nextURL = s.adminURLWithFilters(path, listReq.Query, listReq.Status, listReq.Page+1, listReq.PerPage)
	}
	return prevURL, nextURL
}

const (
	defaultAdminPerPage = 50
	maxAdminPerPage     = 200
)

func adminListRequestFromQuery(r *http.Request, allowedStatuses []string) adminListRequest {
	page, perPage := adminPagination(r.URL.Query().Get("page"), r.URL.Query().Get("perPage"))
	return adminListRequest{
		Query:   strings.TrimSpace(r.URL.Query().Get("q")),
		Status:  normalizedAdminStatus(r.URL.Query().Get("status"), allowedStatuses),
		Page:    page,
		PerPage: perPage,
	}
}

func adminPaginationFromForm(r *http.Request) (int, int) {
	return adminPagination(r.FormValue("page"), r.FormValue("perPage"))
}

func adminPagination(pageValue, perPageValue string) (int, int) {
	page, _ := strconv.Atoi(pageValue)
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(perPageValue)
	perPage = dbutil.Limit(perPage, defaultAdminPerPage, maxAdminPerPage)
	return page, perPage
}

func trimAdminPage[T any](items []T, perPage int) ([]T, bool) {
	if len(items) <= perPage {
		return items, false
	}
	return items[:perPage], true
}

func paginateAdminNodes(nodes []node.Node, page, perPage int) ([]node.Node, bool) {
	start := (page - 1) * perPage
	if start >= len(nodes) {
		return nil, false
	}
	end := start + perPage + 1
	if end > len(nodes) {
		end = len(nodes)
	}
	return trimAdminPage(nodes[start:end], perPage)
}

func normalizedAdminStatus(value string, allowed []string) string {
	value = strings.TrimSpace(value)
	for _, item := range allowed {
		if strings.EqualFold(value, item) {
			return item
		}
	}
	return ""
}

func filterAdminNodes(nodes []node.Node, query, status string) []node.Node {
	query = strings.ToLower(strings.TrimSpace(query))
	status = strings.ToLower(strings.TrimSpace(status))
	if query == "" && status == "" {
		return nodes
	}
	out := make([]node.Node, 0, len(nodes))
	for _, n := range nodes {
		if status != "" && strings.ToLower(n.Status) != status {
			continue
		}
		if query != "" && !nodeMatchesQuery(n, query) {
			continue
		}
		out = append(out, n)
	}
	return out
}

func nodeMatchesQuery(n node.Node, query string) bool {
	return containsLower(strconv.FormatInt(n.ID, 10), query) ||
		containsLower(n.Name, query) ||
		containsLower(n.PublicOverlayAddr, query) ||
		containsLower(n.HealthAddr, query) ||
		containsLower(n.OverlayPublicKey, query) ||
		containsLower(n.LastError, query)
}

func containsLower(value, query string) bool {
	return strings.Contains(strings.ToLower(value), query)
}

func activeClass(active, key string) string {
	if active == key {
		return "active"
	}
	return ""
}

func statusClass(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case node.StatusOnline, room.StatusOpen, turnUsageStatusCharged:
		return "good"
	case node.StatusPaused, node.StatusUnknown:
		return "warn"
	case node.StatusDisabled, room.StatusClosed, turnUsageStatusFailed:
		return "bad"
	default:
		return "neutral"
	}
}

func userStatusText(disabled bool) string {
	if disabled {
		return "禁用"
	}
	return "正常"
}

func userStatusClass(disabled bool) string {
	if disabled {
		return "bad"
	}
	return "good"
}

func timeText(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

func timePtrText(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

func formatBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KB", "MB", "GB"}
	value := float64(size)
	for _, unit := range units {
		value = value / 1024
		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%.1f TB", value/1024)
}

func parseTrafficGrantBytes(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("请输入要增加的流量")
	}
	mb, err := strconv.ParseFloat(value, 64)
	if err != nil || mb <= 0 {
		return 0, fmt.Errorf("流量必须是大于 0 的 MB 数值")
	}
	if mb > 1024*1024 {
		return 0, fmt.Errorf("单次最多增加 1048576 MB")
	}
	bytes := int64(mb * 1024 * 1024)
	if bytes <= 0 {
		return 0, fmt.Errorf("流量数值太小")
	}
	return bytes, nil
}
