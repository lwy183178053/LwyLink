package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/clientrelease"
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

func TestPublicReleaseDownloadURLDoesNotIncludeCompatibilityTicket(t *testing.T) {
	release := clientrelease.Release{
		Version:        "0.1.17",
		Platform:       "windows-amd64",
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
