package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/clientrelease"
)

const (
	downloadTicketTTL        = 10 * time.Minute
	downloadIPLimit          = 30
	downloadUserLimit        = 10
	downloadRateLimitWindow  = time.Hour
	downloadTicketIPLimit    = 60
	downloadTicketUserLimit  = 20
	downloadTicketSignPrefix = "LwyLink client download ticket v1"
)

type clientDownloadTicketResponse struct {
	DownloadURL string    `json:"downloadURL"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type downloadAuth struct {
	userID int64
	result string
}

func (s *Server) handleClientReleaseTicket(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) || !s.requireReleases(w, http.StatusNotFound) {
		return
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "login required")
		return
	}
	ip := clientIP(r)
	if !s.allowDownloadRate("ticket:ip:"+ip, downloadTicketIPLimit) || !s.allowDownloadRate(fmt.Sprintf("ticket:user:%d", user.ID), downloadTicketUserLimit) {
		writeError(w, http.StatusTooManyRequests, "download requests are too frequent")
		return
	}
	var req struct {
		Platform       string `json:"platform"`
		StoredFileName string `json:"storedFileName"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	release, err := s.releaseForTicket(r, req.Platform, req.StoredFileName)
	if err != nil {
		if errors.Is(err, clientrelease.ErrNotFound) {
			writeError(w, http.StatusNotFound, "release file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	expires := time.Now().Add(downloadTicketTTL)
	ticket := s.signDownloadTicket(release.StoredFileName, user.ID, expires)
	u := s.clientReleaseDownloadURL(r, release)
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	writeJSON(w, http.StatusOK, clientDownloadTicketResponse{
		DownloadURL: u + sep + "ticket=" + url.QueryEscape(ticket),
		ExpiresAt:   expires,
	})
}

func (s *Server) releaseForTicket(r *http.Request, platform, storedName string) (clientrelease.Release, error) {
	if strings.TrimSpace(storedName) != "" {
		return s.releases.FindByStoredFileName(r.Context(), storedName)
	}
	return s.releases.Latest(r.Context(), clientrelease.NormalizePlatform(platform))
}

func (s *Server) authorizeClientDownload(r *http.Request, release clientrelease.Release) (downloadAuth, error) {
	if s.adminAuthorized(r) {
		return downloadAuth{result: "admin"}, nil
	}
	if ticket := strings.TrimSpace(r.URL.Query().Get("ticket")); ticket != "" {
		userID, err := s.verifyDownloadTicket(ticket, release.StoredFileName)
		if err != nil {
			return downloadAuth{}, err
		}
		return downloadAuth{userID: userID, result: "ticket"}, nil
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		return downloadAuth{}, err
	}
	return downloadAuth{userID: user.ID, result: "bearer"}, nil
}

func (s *Server) allowClientDownload(r *http.Request, auth downloadAuth) bool {
	ip := clientIP(r)
	if !s.allowDownloadRate("download:ip:"+ip, downloadIPLimit) {
		return false
	}
	if auth.userID > 0 && !s.allowDownloadRate(fmt.Sprintf("download:user:%d", auth.userID), downloadUserLimit) {
		return false
	}
	return true
}

func (s *Server) allowDownloadRate(key string, limit int) bool {
	limiter := s.downloadLimiter
	if limiter == nil {
		return true
	}
	return limiter.Allow(key, limit)
}

func (s *Server) logClientDownload(r *http.Request, release clientrelease.Release, userID int64, result string) {
	if s.releases == nil {
		return
	}
	_ = s.releases.LogDownload(r.Context(), clientrelease.DownloadLog{
		UserID:         userID,
		IP:             clientIP(r),
		Platform:       release.Platform,
		Version:        release.Version,
		FileName:       release.FileName,
		StoredFileName: release.StoredFileName,
		Result:         result,
	})
}

func (s *Server) signDownloadTicket(storedName string, userID int64, expires time.Time) string {
	exp := expires.Unix()
	payload := fmt.Sprintf("%s|%d|%d", storedName, userID, exp)
	sig := signDownloadPayload(s.downloadTicketSecret(), payload)
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + sig))
}

func (s *Server) verifyDownloadTicket(ticket, storedName string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(ticket)
	if err != nil {
		return 0, fmt.Errorf("bad download ticket")
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 4 || parts[0] != storedName {
		return 0, fmt.Errorf("bad download ticket")
	}
	userID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || userID <= 0 {
		return 0, fmt.Errorf("bad download ticket")
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return 0, fmt.Errorf("download ticket expired")
	}
	payload := strings.Join(parts[:3], "|")
	want := signDownloadPayload(s.downloadTicketSecret(), payload)
	if !hmac.Equal([]byte(want), []byte(parts[3])) {
		return 0, fmt.Errorf("bad download ticket")
	}
	return userID, nil
}

func (s *Server) downloadTicketSecret() []byte {
	secret := strings.TrimSpace(s.adminToken)
	if secret == "" {
		secret = "lwylink-local-download-ticket"
	}
	return []byte(secret)
}

func signDownloadPayload(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(downloadTicketSignPrefix))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

type downloadLimiter struct {
	mu     sync.Mutex
	window time.Duration
	hits   map[string][]time.Time
}

func newDownloadLimiter(window time.Duration) *downloadLimiter {
	return &downloadLimiter{
		window: window,
		hits:   make(map[string][]time.Time),
	}
}

func (l *downloadLimiter) Allow(key string, limit int) bool {
	if l == nil || limit <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	items := l.hits[key]
	kept := items[:0]
	for _, item := range items {
		if item.After(cutoff) {
			kept = append(kept, item)
		}
	}
	if len(kept) >= limit {
		l.hits[key] = kept
		return false
	}
	kept = append(kept, now)
	l.hits[key] = kept
	return true
}

func clientIP(r *http.Request) string {
	for _, header := range []string{"CF-Connecting-IP", "X-Real-IP"} {
		if ip := strings.TrimSpace(r.Header.Get(header)); ip != "" {
			return ip
		}
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if first := strings.TrimSpace(strings.Split(forwarded, ",")[0]); first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
