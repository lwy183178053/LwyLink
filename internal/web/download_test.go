package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/account"
	"github.com/LwyV-Labs/LwyLink/internal/clientrelease"
	"golang.org/x/crypto/bcrypt"
)

func TestDownloadTicketRoundTripAndExpiry(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "admin-secret", nil, nil, nil, nil, nil, nil)
	ticket := srv.signDownloadTicket("client.exe", 42, "203.0.113.8", time.Now().Add(time.Minute))
	userID, err := srv.verifyDownloadTicket(ticket, "client.exe", "203.0.113.8")
	if err != nil {
		t.Fatalf("verify ticket: %v", err)
	}
	if userID != 42 {
		t.Fatalf("userID=%d, want 42", userID)
	}
	if _, err := srv.verifyDownloadTicket(ticket, "other.exe", "203.0.113.8"); err == nil {
		t.Fatalf("ticket verified for wrong file")
	}
	if _, err := srv.verifyDownloadTicket(ticket, "client.exe", "203.0.113.9"); err == nil {
		t.Fatalf("ticket verified for wrong ip")
	}

	expired := srv.signDownloadTicket("client.exe", 42, "203.0.113.8", time.Now().Add(-time.Second))
	if _, err := srv.verifyDownloadTicket(expired, "client.exe", "203.0.113.8"); err == nil {
		t.Fatalf("expired ticket verified")
	}
}

func TestDownloadLimiterRejectsAfterLimit(t *testing.T) {
	limiter := newDownloadLimiter(time.Hour)
	if !limiter.Allow("ip:1", 2) {
		t.Fatalf("first hit rejected")
	}
	if !limiter.Allow("ip:1", 2) {
		t.Fatalf("second hit rejected")
	}
	if limiter.Allow("ip:1", 2) {
		t.Fatalf("third hit accepted")
	}
	if !limiter.Allow("ip:2", 2) {
		t.Fatalf("different key rejected")
	}
}

func TestClientReleaseTicketRejectsBadJSON(t *testing.T) {
	releases := clientrelease.NewService(&ticketReleaseRepo{release: clientrelease.Release{
		Version:        "0.1.18",
		Platform:       "windows-amd64",
		FileName:       "LwyLink.exe",
		StoredFileName: "client.exe",
	}}, t.TempDir())
	accounts := account.NewService(&ticketAccountRepo{user: ticketUser(t)}, "secret", &ticketSessionStore{sessionID: "session"})
	token, err := accounts.Login(context.Background(), "player@example.com", "secret1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	srv := NewServer("127.0.0.1:0", "admin-secret", accounts, nil, nil, nil, releases, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/client/releases/ticket", strings.NewReader(`{"platform":`))
	req.Header.Set("Authorization", "Bearer "+token.Token)
	rec := httptest.NewRecorder()
	srv.handleClientReleaseTicket(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestClientReleaseTicketRejectsTrailingJSON(t *testing.T) {
	releases := clientrelease.NewService(&ticketReleaseRepo{release: clientrelease.Release{
		Version:        "0.1.18",
		Platform:       "windows-amd64",
		FileName:       "LwyLink.exe",
		StoredFileName: "client.exe",
	}}, t.TempDir())
	accounts := account.NewService(&ticketAccountRepo{user: ticketUser(t)}, "secret", &ticketSessionStore{sessionID: "session"})
	token, err := accounts.Login(context.Background(), "player@example.com", "secret1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	srv := NewServer("127.0.0.1:0", "admin-secret", accounts, nil, nil, nil, releases, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/client/releases/ticket", strings.NewReader(`{} {}`))
	req.Header.Set("Authorization", "Bearer "+token.Token)
	rec := httptest.NewRecorder()
	srv.handleClientReleaseTicket(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestAuthorizeClientDownload(t *testing.T) {
	release := clientrelease.Release{StoredFileName: "client.exe"}
	srv := NewServer("127.0.0.1:0", "admin-secret", nil, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/downloads/client/client.exe", nil)
	if _, err := srv.authorizeClientDownload(req, release); err == nil {
		t.Fatalf("download without credentials was authorized")
	}

	req = httptest.NewRequest(http.MethodGet, "/downloads/client/client.exe?token=admin-secret", nil)
	auth, err := srv.authorizeClientDownload(req, release)
	if err != nil {
		t.Fatalf("admin download rejected: %v", err)
	}
	if auth.result != "admin" {
		t.Fatalf("auth result=%q, want admin", auth.result)
	}

	ticket := srv.signDownloadTicket(release.StoredFileName, 7, "192.0.2.1", time.Now().Add(time.Minute))
	req = httptest.NewRequest(http.MethodGet, "/downloads/client/client.exe?ticket="+ticket, nil)
	req.RemoteAddr = "192.0.2.1:12345"
	auth, err = srv.authorizeClientDownload(req, release)
	if err != nil {
		t.Fatalf("ticket download rejected: %v", err)
	}
	if auth.userID != 7 || auth.result != "ticket" {
		t.Fatalf("auth=%+v, want ticket user 7", auth)
	}
}

func TestClientIPUsesForwardedHeadersFromTrustedProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("CF-Connecting-IP", "not-an-ip")
	req.Header.Set("X-Real-IP", "still-not-an-ip")
	req.Header.Set("X-Forwarded-For", "bad, 203.0.113.9, 203.0.113.10")
	if got, want := clientIP(req), "203.0.113.9"; got != want {
		t.Fatalf("clientIP=%q, want %q", got, want)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:12345"
	req.Header.Set("Forwarded", `for="[2001:db8::1]";proto=https;host=lwylink.xyz`)
	if got, want := clientIP(req), "2001:db8::1"; got != want {
		t.Fatalf("forwarded clientIP=%q, want %q", got, want)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "garbage")
	if got, want := clientIP(req), "127.0.0.1"; got != want {
		t.Fatalf("fallback clientIP=%q, want %q", got, want)
	}
}

func TestClientIPIgnoresForwardedHeadersFromPublicRemote(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.10:12345"
	req.Header.Set("CF-Connecting-IP", "203.0.113.7")
	req.Header.Set("X-Real-IP", "203.0.113.8")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("Forwarded", `for=203.0.113.10;proto=https;host=lwylink.xyz`)

	if got, want := clientIP(req), "192.0.2.10"; got != want {
		t.Fatalf("clientIP=%q, want public remote %q", got, want)
	}
}

func TestPublicReleaseDownloadURLDoesNotIncludeCompatibilityTicket(t *testing.T) {
	release := clientrelease.Release{
		Version:        "0.1.17",
		Platform:       "windows-amd64",
		Mandatory:      false,
		FileName:       "LwyLink.exe",
		StoredFileName: "windows-amd64-0.1.17.exe",
		SHA256:         "abc",
	}
	srv := NewServer("127.0.0.1:0", "admin-secret", nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/client/releases/latest?platform=windows-amd64", nil)

	public := srv.publicClientRelease(req, release)
	if public.DownloadURL != "http://example.com/downloads/client/windows-amd64-0.1.17.exe" {
		t.Fatalf("public download URL=%q", public.DownloadURL)
	}
	if !public.Mandatory {
		t.Fatalf("public release was optional; current releases must be mandatory")
	}
}

func TestClientReleaseDownloadURLHonorsForwardedHeaders(t *testing.T) {
	release := clientrelease.Release{StoredFileName: "client.exe"}
	srv := NewServer("127.0.0.1:0", "admin-secret", nil, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/client/releases/latest", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "www.lwylink.xyz")
	if got, want := srv.clientReleaseDownloadURL(req, release), "https://www.lwylink.xyz/downloads/client/client.exe"; got != want {
		t.Fatalf("download URL=%q, want %q", got, want)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/client/releases/latest", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Forwarded", `for=203.0.113.1;proto=https;host=lwylink.xyz`)
	if got, want := srv.clientReleaseDownloadURL(req, release), "https://lwylink.xyz/downloads/client/client.exe"; got != want {
		t.Fatalf("forwarded download URL=%q, want %q", got, want)
	}
}

type ticketReleaseRepo struct {
	release clientrelease.Release
}

func (r *ticketReleaseRepo) Create(context.Context, clientrelease.Release) (clientrelease.Release, error) {
	return clientrelease.Release{}, nil
}

func (r *ticketReleaseRepo) List(context.Context, clientrelease.Filter) ([]clientrelease.Release, error) {
	return []clientrelease.Release{r.release}, nil
}

func (r *ticketReleaseRepo) Count(context.Context) (int, error) { return 1, nil }
func (r *ticketReleaseRepo) DownloadCountSince(context.Context, time.Time) (int, error) {
	return 0, nil
}

func (r *ticketReleaseRepo) FindByStoredFileName(_ context.Context, storedFileName string) (clientrelease.Release, error) {
	if storedFileName == r.release.StoredFileName {
		return r.release, nil
	}
	return clientrelease.Release{}, clientrelease.ErrNotFound
}

func (r *ticketReleaseRepo) Delete(context.Context, int64) (clientrelease.Release, error) {
	return clientrelease.Release{}, nil
}

func (r *ticketReleaseRepo) LogDownload(context.Context, clientrelease.DownloadLog) error {
	return nil
}

type ticketAccountRepo struct {
	user account.User
}

func ticketUser(t *testing.T) account.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret1"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return account.User{
		ID:           7,
		Username:     "player@example.com",
		PasswordHash: string(hash),
		DisplayName:  "Player",
		Role:         "player",
	}
}

func (r *ticketAccountRepo) Create(context.Context, string, string, string) (account.User, error) {
	return account.User{}, nil
}

func (r *ticketAccountRepo) FindByUsername(_ context.Context, username string) (account.User, error) {
	if username == r.user.Username {
		return r.user, nil
	}
	return account.User{}, account.ErrNotFound
}

func (r *ticketAccountRepo) FindByID(_ context.Context, id int64) (account.User, error) {
	if id == r.user.ID {
		return r.user, nil
	}
	return account.User{}, account.ErrNotFound
}

func (r *ticketAccountRepo) UpdatePassword(context.Context, int64, string) error { return nil }
func (r *ticketAccountRepo) List(context.Context, account.UserFilter) ([]account.User, error) {
	return nil, nil
}
func (r *ticketAccountRepo) Stats(context.Context) (account.Stats, error) {
	return account.Stats{}, nil
}
func (r *ticketAccountRepo) SetDisabled(context.Context, int64, bool) error { return nil }
func (r *ticketAccountRepo) SetMembership(context.Context, int64, string) error {
	return nil
}
func (r *ticketAccountRepo) AddRelayTraffic(context.Context, int64, int64) error { return nil }
func (r *ticketAccountRepo) ConsumeRelayTraffic(context.Context, int64, int64) error {
	return nil
}
func (r *ticketAccountRepo) Delete(context.Context, int64) error { return nil }

type ticketSessionStore struct {
	sessionID string
}

func (s *ticketSessionStore) SetCurrentSession(_ context.Context, _ int64, sessionID string, _ time.Duration) error {
	s.sessionID = sessionID
	return nil
}

func (s *ticketSessionStore) CurrentSession(context.Context, int64) (string, bool, error) {
	return s.sessionID, s.sessionID != "", nil
}

func (s *ticketSessionStore) DeleteCurrentSession(context.Context, int64) error {
	s.sessionID = ""
	return nil
}
