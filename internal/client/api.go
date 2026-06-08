package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/account"
	"github.com/LwyV-Labs/LwyLink/internal/room"
)

type API struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewAPI(baseURL string) *API {
	return &API{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *API) Token() string {
	return a.token
}

func (a *API) SetToken(token string) {
	a.token = strings.TrimSpace(token)
}

func (a *API) Clone() *API {
	cp := NewAPI(a.baseURL)
	cp.token = a.token
	return cp
}

func (a *API) ClearToken() {
	a.token = ""
}

func (a *API) Register(ctx context.Context, email, password, displayName, emailCode string) (account.LoginResult, error) {
	var out account.LoginResult
	err := a.request(ctx, http.MethodPost, "/api/register", map[string]string{
		"email":       email,
		"password":    password,
		"displayName": displayName,
		"emailCode":   emailCode,
	}, &out)
	if err == nil {
		a.token = out.Token
	}
	return out, err
}

func (a *API) SendEmailVerification(ctx context.Context, email, purpose string) error {
	return a.request(ctx, http.MethodPost, "/api/email/verification/send", map[string]string{
		"email":   email,
		"purpose": purpose,
	}, nil)
}

func (a *API) ResetPassword(ctx context.Context, email, emailCode, newPassword string) error {
	return a.request(ctx, http.MethodPost, "/api/password/reset", map[string]string{
		"email":       email,
		"emailCode":   emailCode,
		"newPassword": newPassword,
	}, nil)
}

func (a *API) ChangePassword(ctx context.Context, oldPassword, newPassword string) error {
	return a.request(ctx, http.MethodPost, "/api/password/change", map[string]string{
		"oldPassword": oldPassword,
		"newPassword": newPassword,
	}, nil)
}

func (a *API) Login(ctx context.Context, email, password string) (account.LoginResult, error) {
	var out account.LoginResult
	err := a.request(ctx, http.MethodPost, "/api/login", map[string]string{
		"email":    email,
		"password": password,
	}, &out)
	if err == nil {
		a.token = out.Token
	}
	return out, err
}

func (a *API) Me(ctx context.Context) (account.PublicUser, error) {
	var out account.PublicUser
	err := a.request(ctx, http.MethodGet, "/api/me", nil, &out)
	return out, err
}

func (a *API) Announcement(ctx context.Context) (string, error) {
	var out struct {
		Announcement string `json:"announcement"`
	}
	err := a.request(ctx, http.MethodGet, "/api/announcement", nil, &out)
	return out.Announcement, err
}

func (a *API) CreateRoom(ctx context.Context, name string, maxMembers int) (room.Room, error) {
	var out room.Room
	err := a.request(ctx, http.MethodPost, "/api/rooms", map[string]any{
		"name":       name,
		"maxMembers": maxMembers,
	}, &out)
	return out, err
}

func (a *API) JoinRoom(ctx context.Context, roomPublicKey string) (room.Room, error) {
	var out room.Room
	err := a.request(ctx, http.MethodPost, "/api/rooms/join", map[string]string{"roomPublicKey": roomPublicKey}, &out)
	return out, err
}

func (a *API) Rooms(ctx context.Context) ([]room.Room, error) {
	var out []room.Room
	err := a.request(ctx, http.MethodGet, "/api/rooms", nil, &out)
	return out, err
}

func (a *API) RoomMembers(ctx context.Context, roomID int64) ([]room.MemberView, error) {
	var out []room.MemberView
	path := "/api/rooms/members?roomID=" + url.QueryEscape(fmt.Sprintf("%d", roomID))
	err := a.request(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

func (a *API) SendRoomChat(ctx context.Context, roomID int64, message string) (room.ChatMessage, error) {
	var out room.ChatMessage
	err := a.request(ctx, http.MethodPost, "/api/rooms/chat", map[string]any{
		"roomID":  roomID,
		"message": message,
	}, &out)
	return out, err
}

func (a *API) RoomVoiceState(ctx context.Context, roomID int64) (room.VoiceState, error) {
	var out room.VoiceState
	path := "/api/rooms/voice?roomID=" + url.QueryEscape(fmt.Sprintf("%d", roomID))
	err := a.request(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

func (a *API) JoinRoomVoice(ctx context.Context, roomID int64) (room.VoiceState, error) {
	var out room.VoiceState
	err := a.request(ctx, http.MethodPost, "/api/rooms/voice/join", map[string]int64{"roomID": roomID}, &out)
	return out, err
}

func (a *API) LeaveRoomVoice(ctx context.Context, roomID int64) (room.VoiceState, error) {
	var out room.VoiceState
	err := a.request(ctx, http.MethodPost, "/api/rooms/voice/leave", map[string]int64{"roomID": roomID}, &out)
	return out, err
}

func (a *API) SetRoomVoiceMic(ctx context.Context, roomID int64, speaking bool) (room.VoiceState, error) {
	var out room.VoiceState
	err := a.request(ctx, http.MethodPost, "/api/rooms/voice/mic", map[string]any{"roomID": roomID, "speaking": speaking}, &out)
	return out, err
}

func (a *API) DeleteRoom(ctx context.Context, roomID int64) error {
	return a.request(ctx, http.MethodPost, "/api/rooms/delete", map[string]int64{"roomID": roomID}, nil)
}

func (a *API) LeaveRoom(ctx context.Context, roomID int64) error {
	return a.request(ctx, http.MethodPost, "/api/rooms/leave", map[string]int64{"roomID": roomID}, nil)
}

func (a *API) KickMember(ctx context.Context, roomID, userID int64) error {
	return a.request(ctx, http.MethodPost, "/api/rooms/kick", map[string]int64{"roomID": roomID, "userID": userID}, nil)
}

func (a *API) TransferOwner(ctx context.Context, roomID, userID int64) (room.Room, error) {
	var out room.Room
	err := a.request(ctx, http.MethodPost, "/api/rooms/transfer-owner", map[string]int64{"roomID": roomID, "userID": userID}, &out)
	return out, err
}

func (a *API) SubscribeRoomEvents(ctx context.Context, onEvent func(room.RoomEvent)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/api/rooms/events", nil)
	if err != nil {
		return err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	resp, err := (&http.Client{Timeout: 0}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("event stream http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	var data string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if data != "" {
				var event room.RoomEvent
				if json.Unmarshal([]byte(data), &event) == nil && onEvent != nil {
					onEvent(event)
				}
				data = ""
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			part := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				data = part
			} else {
				data += "\n" + part
			}
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return ctx.Err()
}

func (a *API) request(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e map[string]string
		if json.Unmarshal(data, &e) == nil && e["error"] != "" {
			return fmt.Errorf("%s", e["error"])
		}
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}
