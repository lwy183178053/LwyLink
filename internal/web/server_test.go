package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	if !strings.Contains(rec.Body.String(), `placeholder="例如 /S，可留空"`) {
		t.Fatalf("rendered releases html missing install args placeholder")
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

func TestSitePagesRender(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "admin-token", nil, nil, nil, nil, nil, nil)
	for _, path := range []string{"/", "/login", "/register", "/dashboard", "/download"} {
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
