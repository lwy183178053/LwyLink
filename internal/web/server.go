package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LwyV-Labs/LwyV-GameNet/internal/account"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/node"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/room"
)

type Server struct {
	addr       string
	adminToken string
	accounts   *account.Service
	rooms      *room.Service
	nodes      *node.Service
	httpServer *http.Server
}

// NewServer exposes the JSON client API and the embedded admin console.
func NewServer(addr, adminToken string, accounts *account.Service, rooms *room.Service, nodes *node.Service) *Server {
	return &Server{
		addr:       addr,
		adminToken: adminToken,
		accounts:   accounts,
		rooms:      rooms,
		nodes:      nodes,
	}
}

// ListenAndServe registers all HTTP routes and shuts down gracefully when the
// parent context is canceled by the process supervisor.
func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/register", s.handleRegister)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/rooms", s.handleRooms)
	mux.HandleFunc("/api/rooms/join", s.handleJoinRoom)
	mux.HandleFunc("/api/rooms/delete", s.handleDeleteRoom)
	mux.HandleFunc("/api/rooms/members", s.handleRoomMembers)
	mux.HandleFunc("/api/rooms/chat", s.handleRoomChat)
	mux.HandleFunc("/api/rooms/events", s.handleRoomEvents)
	mux.HandleFunc("/api/rooms/leave", s.handleLeaveRoom)
	mux.HandleFunc("/api/rooms/kick", s.handleKickRoomMember)
	mux.HandleFunc("/api/rooms/transfer-owner", s.handleTransferRoomOwner)
	mux.Handle("/admin/assets/", http.StripPrefix("/admin/assets/", adminAssetHandler()))
	mux.HandleFunc("/admin", s.handleAdmin)
	mux.HandleFunc("/admin/nodes", s.handleAdminNodes)
	mux.HandleFunc("/admin/users", s.handleAdminUsers)
	mux.HandleFunc("/admin/users/status", s.handleAdminUserStatus)
	mux.HandleFunc("/admin/users/membership", s.handleAdminUserMembership)
	mux.HandleFunc("/admin/users/delete", s.handleAdminUserDelete)
	mux.HandleFunc("/admin/rooms", s.handleAdminRooms)
	mux.HandleFunc("/admin/rooms/status", s.handleAdminRoomStatus)
	mux.HandleFunc("/admin/nodes/save", s.handleAdminNodeSave)
	mux.HandleFunc("/admin/nodes/enabled", s.handleAdminNodeEnabled)
	mux.HandleFunc("/admin/nodes/check", s.handleAdminNodeCheck)

	s.httpServer = &http.Server{
		Addr:              s.addr,
		Handler:           withCORS(mux),
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

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.accounts.Register(r.Context(), req.Username, req.Password, req.DisplayName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.accounts.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user.Public())
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
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
	roomInfo, err := s.rooms.JoinRoom(r.Context(), user.ID, req.RoomPublicKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roomInfo)
}

func (s *Server) handleDeleteRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
	if err := s.rooms.DeleteRoom(r.Context(), user.ID, req.RoomID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRoomMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
	chat, err := s.rooms.SendChat(r.Context(), user.ID, req.RoomID, req.Message)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, chat)
}

func (s *Server) handleRoomEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
	if err := s.rooms.LeaveRoom(r.Context(), user.ID, req.RoomID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleKickRoomMember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
	if err := s.rooms.KickMember(r.Context(), user.ID, req.RoomID, req.UserID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTransferRoomOwner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
	roomInfo, err := s.rooms.TransferOwner(r.Context(), user.ID, req.RoomID, req.UserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roomInfo)
}

func (s *Server) userFromRequest(r *http.Request) (account.User, error) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return account.User{}, fmt.Errorf("missing bearer token")
	}
	return s.accounts.AuthenticateToken(r.Context(), strings.TrimPrefix(auth, "Bearer "))
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	// Rejecting unknown fields catches client/UI drift early while the protocol
	// is still evolving.
	dec.DisallowUnknownFields()
	return dec.Decode(v)
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

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "admin token required")
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
	if s.nodes != nil {
		nodes, err := s.nodes.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(nodes) > 5 {
			nodes = nodes[:5]
		}
		s.renderAdmin(w, adminPageData{
			Active: "overview",
			Title:  "概览",
			Token:  s.adminToken,
			Stats:  stats,
			Users:  users,
			Rooms:  rooms,
			Nodes:  nodes,
		})
		return
	}
	s.renderAdmin(w, adminPageData{
		Active: "overview",
		Title:  "概览",
		Token:  s.adminToken,
		Stats:  stats,
		Users:  users,
		Rooms:  rooms,
	})
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "admin token required")
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
	if !s.adminAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "admin token required")
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
	s.renderAdmin(w, data)
}

func (s *Server) handleAdminNodes(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "admin token required")
		return
	}
	if s.nodes == nil {
		writeError(w, http.StatusBadRequest, "node service is not configured")
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

func (s *Server) renderAdmin(w http.ResponseWriter, data adminPageData) {
	data.Nav = []adminNavItem{
		{Key: "overview", Label: "概览", URL: s.adminURL("/admin")},
		{Key: "nodes", Label: "节点", URL: s.adminURL("/admin/nodes")},
		{Key: "users", Label: "玩家", URL: s.adminURL("/admin/users")},
		{Key: "rooms", Label: "房间", URL: s.adminURL("/admin/rooms")},
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := adminModuleTemplate.Execute(w, data); err != nil {
		log.Printf("render admin failed: %v", err)
	}
}

func (s *Server) handleAdminUserStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.adminAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "admin token required")
		return
	}
	userID, _ := strconv.ParseInt(r.FormValue("userID"), 10, 64)
	disabled := r.FormValue("disabled") == "true"
	if userID <= 0 {
		writeError(w, http.StatusBadRequest, "bad userID")
		return
	}
	if err := s.accounts.SetDisabled(r.Context(), userID, disabled); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, s.adminURLFromForm("/admin/users", r, "status"), http.StatusSeeOther)
}

func (s *Server) handleAdminUserMembership(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.adminAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "admin token required")
		return
	}
	userID, _ := strconv.ParseInt(r.FormValue("userID"), 10, 64)
	if userID <= 0 {
		writeError(w, http.StatusBadRequest, "bad userID")
		return
	}
	if err := s.accounts.SetMembership(r.Context(), userID, r.FormValue("membership")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, s.adminURLFromForm("/admin/users", r, "status"), http.StatusSeeOther)
}

func (s *Server) handleAdminUserDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.adminAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "admin token required")
		return
	}
	userID, _ := strconv.ParseInt(r.FormValue("userID"), 10, 64)
	if userID <= 0 {
		writeError(w, http.StatusBadRequest, "bad userID")
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
	http.Redirect(w, r, s.adminURLFromForm("/admin/users", r, "status"), http.StatusSeeOther)
}

func (s *Server) handleAdminNodeSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.adminAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "admin token required")
		return
	}
	if s.nodes == nil {
		writeError(w, http.StatusBadRequest, "node service is not configured")
		return
	}
	id, _ := strconv.ParseInt(r.FormValue("nodeID"), 10, 64)
	maxRooms, _ := strconv.Atoi(r.FormValue("maxRooms"))
	enabled := r.FormValue("enabled") != "false"
	n := node.Node{
		ID:                id,
		Name:              r.FormValue("name"),
		PublicOverlayAddr: r.FormValue("publicOverlayAddr"),
		HealthAddr:        r.FormValue("healthAddr"),
		OverlayPublicKey:  r.FormValue("overlayPublicKey"),
		MaxRooms:          maxRooms,
		Enabled:           enabled,
	}
	if _, err := s.nodes.CreateOrUpdate(r.Context(), n); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, s.adminURLFromForm("/admin/nodes", r, "status"), http.StatusSeeOther)
}

func (s *Server) handleAdminNodeEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.adminAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "admin token required")
		return
	}
	if s.nodes == nil {
		writeError(w, http.StatusBadRequest, "node service is not configured")
		return
	}
	nodeID, _ := strconv.ParseInt(r.FormValue("nodeID"), 10, 64)
	enabled := r.FormValue("enabled") == "true"
	if nodeID <= 0 {
		writeError(w, http.StatusBadRequest, "bad nodeID")
		return
	}
	if err := s.nodes.SetEnabled(r.Context(), nodeID, enabled); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, s.adminURLFromForm("/admin/nodes", r, "status"), http.StatusSeeOther)
}

func (s *Server) handleAdminNodeCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.adminAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "admin token required")
		return
	}
	if s.nodes == nil {
		writeError(w, http.StatusBadRequest, "node service is not configured")
		return
	}
	nodeID, _ := strconv.ParseInt(r.FormValue("nodeID"), 10, 64)
	if nodeID <= 0 {
		writeError(w, http.StatusBadRequest, "bad nodeID")
		return
	}
	if err := s.nodes.CheckNow(r.Context(), nodeID); err != nil {
		log.Printf("manual node check failed node=%d: %v", nodeID, err)
	}
	http.Redirect(w, r, s.adminURLFromForm("/admin/nodes", r, "status"), http.StatusSeeOther)
}

func (s *Server) handleAdminRoomStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.adminAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "admin token required")
		return
	}
	roomID, _ := strconv.ParseInt(r.FormValue("roomID"), 10, 64)
	if roomID <= 0 {
		writeError(w, http.StatusBadRequest, "bad roomID")
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
	http.Redirect(w, r, s.adminURLFromForm("/admin/rooms", r, "filterStatus"), http.StatusSeeOther)
}

func (s *Server) adminAuthorized(r *http.Request) bool {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.FormValue("token")
	}
	return token != "" && token == s.adminToken
}

type adminPageData struct {
	Active  string
	Title   string
	Token   string
	Query   string
	Status  string
	Page    int
	PerPage int
	HasPrev bool
	HasNext bool
	PrevURL string
	NextURL string
	Stats   adminStats
	Nav     []adminNavItem
	Users   []account.User
	Rooms   []room.Room
	Nodes   []node.Node
}

type adminListRequest struct {
	Query   string
	Status  string
	Page    int
	PerPage int
}

func (r adminListRequest) Offset() int {
	return (r.Page - 1) * r.PerPage
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
}

func (s *Server) adminStats(ctx context.Context) (adminStats, error) {
	stats := adminStats{}
	users, err := s.accounts.SearchUsers(ctx, account.UserFilter{Limit: 1000})
	if err != nil {
		return stats, err
	}
	for _, u := range users {
		stats.Users++
		if u.Disabled {
			stats.DisabledUsers++
		} else {
			stats.ActiveUsers++
		}
	}
	rooms, err := s.rooms.Search(ctx, room.Filter{Limit: 1000})
	if err != nil {
		return stats, err
	}
	for _, r := range rooms {
		stats.Rooms++
		switch r.Status {
		case room.StatusOpen:
			stats.OpenRooms++
		case room.StatusClosed:
			stats.ClosedRooms++
		}
	}
	if s.nodes != nil {
		nodes, err := s.nodes.List(ctx)
		if err != nil {
			return stats, err
		}
		for _, n := range nodes {
			stats.Nodes++
			switch n.Status {
			case node.StatusOnline:
				stats.OnlineNodes++
			case node.StatusPaused:
				stats.PausedNodes++
			}
		}
	}
	return stats, nil
}

func (s *Server) adminURL(path string) string {
	values := url.Values{}
	values.Set("token", s.adminToken)
	return path + "?" + values.Encode()
}

func (s *Server) adminListPageData(active, title, path string, listReq adminListRequest, hasNext bool) adminPageData {
	prevURL, nextURL := s.adminPageURLs(path, listReq, hasNext)
	return adminPageData{
		Active:  active,
		Title:   title,
		Token:   s.adminToken,
		Query:   listReq.Query,
		Status:  listReq.Status,
		Page:    listReq.Page,
		PerPage: listReq.PerPage,
		HasPrev: listReq.Page > 1,
		HasNext: hasNext,
		PrevURL: prevURL,
		NextURL: nextURL,
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
	if perPage <= 0 {
		perPage = defaultAdminPerPage
	}
	if perPage > maxAdminPerPage {
		perPage = maxAdminPerPage
	}
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
	value = strings.TrimSpace(strings.ToLower(value))
	for _, item := range allowed {
		if value == item {
			return value
		}
	}
	return ""
}

func filterAdminNodes(nodes []node.Node, query, status string) []node.Node {
	query = strings.ToLower(strings.TrimSpace(query))
	status = strings.TrimSpace(strings.ToLower(status))
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
	fields := []string{
		strconv.FormatInt(n.ID, 10),
		n.Name,
		n.PublicOverlayAddr,
		n.HealthAddr,
		n.OverlayPublicKey,
		n.LastError,
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func activeClass(active, key string) string {
	if active == key {
		return "active"
	}
	return ""
}

func statusClass(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case node.StatusOnline, room.StatusOpen:
		return "good"
	case node.StatusPaused, node.StatusUnknown:
		return "warn"
	case node.StatusDisabled, room.StatusClosed:
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
