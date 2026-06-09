package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/account"
	"github.com/LwyV-Labs/LwyLink/internal/clientrelease"
	"github.com/LwyV-Labs/LwyLink/internal/room"
)

func TestParseTrafficGrantBytes(t *testing.T) {
	got, err := parseTrafficGrantBytes("1536")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := int64(1.5 * 1024 * 1024 * 1024)
	if got != want {
		t.Fatalf("bytes=%d, want %d", got, want)
	}
}

func TestParseTrafficGrantBytesRejectsInvalidValue(t *testing.T) {
	if _, err := parseTrafficGrantBytes("0"); err == nil {
		t.Fatalf("expected zero traffic grant to fail")
	}
	if _, err := parseTrafficGrantBytes("1048577"); err == nil {
		t.Fatalf("expected oversized traffic grant to fail")
	}
}

func TestAdminPaginationBounds(t *testing.T) {
	page, perPage := adminPagination("-3", "999")
	if page != 1 || perPage != maxAdminPerPage {
		t.Fatalf("adminPagination bounds = %d/%d, want 1/%d", page, perPage, maxAdminPerPage)
	}
	page, perPage = adminPagination("2", "0")
	if page != 2 || perPage != defaultAdminPerPage {
		t.Fatalf("adminPagination defaults = %d/%d, want 2/%d", page, perPage, defaultAdminPerPage)
	}
	if got := (adminListRequest{Page: 0, PerPage: 50}).Offset(); got != 0 {
		t.Fatalf("adminListRequest.Offset() = %d, want 0", got)
	}
}

func TestNormalizedAdminStatus(t *testing.T) {
	allowed := []string{"active", "disabled"}
	if got := normalizedAdminStatus(" Active ", allowed); got != "active" {
		t.Fatalf("normalizedAdminStatus() = %q, want active", got)
	}
	if got := normalizedAdminStatus("paused", allowed); got != "" {
		t.Fatalf("normalizedAdminStatus() = %q, want empty", got)
	}
}

func TestAdminTemplateRendersOverviewAndTrafficDialog(t *testing.T) {
	data := adminPageData{
		Active:       "overview",
		Title:        "概览",
		Token:        "test-admin-token",
		Stats:        adminStats{Users: 1, Rooms: 1},
		Announcement: "hello",
		Users: []account.User{{
			ID:                    1,
			Username:              "player",
			DisplayName:           "Player",
			Membership:            account.MembershipVIP,
			RelayTrafficUsedBytes: 1024,
			RelayTrafficPeriod:    "2026-06",
		}},
		Rooms: []room.Room{{
			ID:          1,
			Name:        "room",
			MaxMembers:  8,
			MemberCount: 1,
			Status:      room.StatusOpen,
		}},
	}

	rec := httptest.NewRecorder()
	NewServer("127.0.0.1:0", data.Token, nil, nil, nil, nil, nil, nil).renderAdmin(rec, data)
	html := rec.Body.String()
	for _, want := range []string{"最近账号", "最近房间", "compact-table", "Player"} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered overview html missing %q", want)
		}
	}

	data.Active = "metrics"
	data.Title = "监控"
	data.Metrics = adminMetrics{
		GeneratedAt:           time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
		Users:                 1,
		ActiveUsers:           1,
		Rooms:                 1,
		OpenRooms:             1,
		OverlayConnections:    1,
		VoiceRooms:            1,
		VoiceParticipants:     2,
		VoiceSpeakers:         1,
		RelayTrafficUsedBytes: 2048,
		DownloadsLastHour:     3,
		Releases:              1,
		LatestWindowsRelease:  &clientrelease.Release{Version: "0.2.1", Mandatory: true},
		RecentErrors:          []string{"node: last check failed"},
	}
	rec = httptest.NewRecorder()
	NewServer("127.0.0.1:0", data.Token, nil, nil, nil, nil, nil, nil).renderAdmin(rec, data)
	html = rec.Body.String()
	for _, want := range []string{"实时状态", "Overlay 连接", "0.2.1", "node: last check failed"} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered metrics html missing %q", want)
		}
	}

	data.Active = "users"
	data.Title = "玩家账号"
	rec = httptest.NewRecorder()
	NewServer("127.0.0.1:0", data.Token, nil, nil, nil, nil, nil, nil).renderAdmin(rec, data)
	html = rec.Body.String()
	for _, want := range []string{"traffic-dialog", "data-open-traffic", `placeholder="例如 1024"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered users html missing %q", want)
		}
	}

	data.Active = "releases"
	data.Title = "更新发布"
	rec = httptest.NewRecorder()
	NewServer("127.0.0.1:0", data.Token, nil, nil, nil, nil, nil, nil).renderAdmin(rec, data)
	releasesHTML := rec.Body.String()
	for _, want := range []string{`placeholder="例如 /S，可留空"`, `name="mandatory" value="true"`, "当前阶段所有客户端发布都会强制更新"} {
		if !strings.Contains(releasesHTML, want) {
			t.Fatalf("rendered releases html missing %q", want)
		}
	}
}

func TestAdminReleaseUploadRejectsOversizedBodyBeforeMultipartParse(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "LwyLink.exe")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if _, err := file.Write(bytes.Repeat([]byte("x"), 128)); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/releases/upload?token=admin-token", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	releases := clientrelease.NewService(nil, "")
	NewServer("127.0.0.1:0", "admin-token", nil, nil, nil, nil, releases, nil).handleAdminReleaseUploadLimited(rec, req, 32)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "too large") {
		t.Fatalf("expected too large error, got %q", rec.Body.String())
	}
}

func TestReadJSONRejectsTrailingJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"name":"room"} {"name":"other"}`))
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(req, &body); err == nil {
		t.Fatalf("readJSON accepted trailing JSON")
	}
}

func TestSitePagesRender(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "admin-token", nil, nil, nil, nil, nil, nil)
	for _, path := range []string{"/", "/login", "/register", "/reset", "/dashboard", "/download"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.handleSite(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d, want 200", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "LwyLink") {
			t.Fatalf("%s page did not render site content", path)
		}
	}
}

func TestSitePasswordResetIsSeparatePage(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "admin-token", nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	srv.handleSite(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `<section class="panel auth-panel" data-view="login">
      <div class="password-reset">`) {
		t.Fatalf("password reset form is still rendered as login view")
	}
	if !strings.Contains(body, `href="/reset"`) {
		t.Fatalf("login page does not link to reset page")
	}

	req = httptest.NewRequest(http.MethodGet, "/reset", nil)
	rec = httptest.NewRecorder()
	srv.handleSite(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status=%d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-view="reset"`) || !strings.Contains(rec.Body.String(), "重置密码") {
		t.Fatalf("reset page did not render password reset view")
	}
	css, err := siteFiles.ReadFile("site_assets/site.css")
	if err != nil {
		t.Fatalf("read site.css: %v", err)
	}
	if !strings.Contains(string(css), `[data-page="reset"] [data-view]:not([data-view="reset"])`) {
		t.Fatalf("reset page visibility rule is missing")
	}
	script, err := siteFiles.ReadFile("site_assets/site.js")
	if err != nil {
		t.Fatalf("read site.js: %v", err)
	}
	if !strings.Contains(string(script), "data-${source}-form") {
		t.Fatalf("email verification script no longer resolves source forms")
	}
}

func TestSiteFormBusySnapshotsDataBeforeDisablingInputs(t *testing.T) {
	script, err := siteFiles.ReadFile("site_assets/site.js")
	if err != nil {
		t.Fatalf("read site.js: %v", err)
	}
	js := string(script)
	formDataIndex := strings.Index(js, "const formData = new FormData(form);")
	disableIndex := strings.Index(js, "control.disabled = true;")
	if formDataIndex < 0 || disableIndex < 0 {
		t.Fatalf("site form busy implementation markers missing")
	}
	if formDataIndex > disableIndex {
		t.Fatalf("form data is captured after controls are disabled")
	}
}

func TestSmokeRegisterRequiresAdminAndSmokeEmail(t *testing.T) {
	accounts := account.NewService(&smokeAccountRepo{users: make(map[string]account.User)}, "secret", &smokeSessionStore{sessions: make(map[int64]string)})
	srv := NewServer("127.0.0.1:0", "admin-token", accounts, nil, nil, nil, nil, nil)

	body := `{"email":"player@smoke.lwylink.test","password":"secret1","displayName":"Smoke"}`
	req := httptest.NewRequest(http.MethodPost, "/api/smoke/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleSmokeRegister(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/smoke/register?token=admin-token", strings.NewReader(`{"email":"player@example.com","password":"secret1"}`))
	rec = httptest.NewRecorder()
	srv.handleSmokeRegister(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/smoke/register?token=admin-token", strings.NewReader(body))
	rec = httptest.NewRecorder()
	srv.handleSmokeRegister(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var result account.LoginResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Token == "" || result.User.Email != "player@smoke.lwylink.test" {
		t.Fatalf("bad register result: %+v", result)
	}
}

func TestLoginDisabledAccountReturnsForbidden(t *testing.T) {
	repo := &smokeAccountRepo{users: make(map[string]account.User)}
	accounts := account.NewService(repo, "secret", &smokeSessionStore{sessions: make(map[int64]string)})
	result, err := accounts.Register(context.Background(), "player@example.com", "secret1", "Player")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	u := repo.users[result.User.Email]
	u.Disabled = true
	repo.users[result.User.Email] = u
	srv := NewServer("127.0.0.1:0", "admin-token", accounts, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"email":"player@example.com","password":"secret1"}`))
	rec := httptest.NewRecorder()
	srv.handleLogin(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", rec.Code, rec.Body.String())
	}
}

type smokeAccountRepo struct {
	nextID int64
	users  map[string]account.User
}

func (r *smokeAccountRepo) Create(_ context.Context, username, passwordHash, displayName string) (account.User, error) {
	if _, ok := r.users[username]; ok {
		return account.User{}, account.ErrUsernameUsed
	}
	r.nextID++
	u := account.User{ID: r.nextID, Username: username, PasswordHash: passwordHash, DisplayName: displayName, Role: "player"}
	r.users[username] = u
	return u, nil
}

func (r *smokeAccountRepo) FindByUsername(_ context.Context, username string) (account.User, error) {
	u, ok := r.users[username]
	if !ok {
		return account.User{}, account.ErrNotFound
	}
	return u, nil
}

func (r *smokeAccountRepo) FindByID(_ context.Context, id int64) (account.User, error) {
	for _, u := range r.users {
		if u.ID == id {
			return u, nil
		}
	}
	return account.User{}, account.ErrNotFound
}

func (r *smokeAccountRepo) UpdatePassword(_ context.Context, userID int64, passwordHash string) error {
	for email, u := range r.users {
		if u.ID == userID {
			u.PasswordHash = passwordHash
			r.users[email] = u
			return nil
		}
	}
	return sql.ErrNoRows
}

func (r *smokeAccountRepo) List(context.Context, account.UserFilter) ([]account.User, error) {
	return nil, nil
}
func (r *smokeAccountRepo) Stats(context.Context) (account.Stats, error) { return account.Stats{}, nil }
func (r *smokeAccountRepo) SetDisabled(context.Context, int64, bool) error {
	return nil
}
func (r *smokeAccountRepo) SetMembership(context.Context, int64, string) error {
	return nil
}
func (r *smokeAccountRepo) AddRelayTraffic(context.Context, int64, int64) error { return nil }
func (r *smokeAccountRepo) ConsumeRelayTraffic(context.Context, int64, int64) error {
	return nil
}
func (r *smokeAccountRepo) Delete(context.Context, int64) error { return nil }

type smokeSessionStore struct {
	sessions map[int64]string
}

func (s *smokeSessionStore) SetCurrentSession(_ context.Context, userID int64, sessionID string, _ time.Duration) error {
	s.sessions[userID] = sessionID
	return nil
}

func (s *smokeSessionStore) CurrentSession(_ context.Context, userID int64) (string, bool, error) {
	value, ok := s.sessions[userID]
	return value, ok, nil
}

func (s *smokeSessionStore) DeleteCurrentSession(_ context.Context, userID int64) error {
	delete(s.sessions, userID)
	return nil
}
