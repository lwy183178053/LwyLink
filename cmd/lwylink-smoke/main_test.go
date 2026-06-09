package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LwyV-Labs/LwyLink/internal/client"
	"github.com/LwyV-Labs/LwyLink/internal/overlay"
)

func TestSmokeRegisterUsesAdminEndpointWhenTokenConfigured(t *testing.T) {
	var gotPath string
	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.URL.Query().Get("token")
		if gotPath != "/api/smoke/register" {
			t.Fatalf("path=%q, want smoke register endpoint", gotPath)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["email"] != "owner@smoke.lwylink.test" {
			t.Fatalf("email=%q", req["email"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "session-token",
			"user": map[string]any{
				"id":       7,
				"username": "owner@smoke.lwylink.test",
				"email":    "owner@smoke.lwylink.test",
			},
		})
	}))
	defer server.Close()

	cleaner := newSmokeCleaner(server.URL, "admin-token")
	api := client.NewAPI(server.URL)
	result, err := registerSmokeUser(context.Background(), cleaner, api, "owner", "smoke-password", "Smoke Owner")
	if err != nil {
		t.Fatalf("register smoke user: %v", err)
	}
	if gotPath != "/api/smoke/register" || gotToken != "admin-token" {
		t.Fatalf("endpoint path=%q token=%q", gotPath, gotToken)
	}
	if result.Token != "session-token" || api.Token() != "session-token" {
		t.Fatalf("token result=%q api=%q", result.Token, api.Token())
	}
	if len(cleaner.userIDs) != 1 || cleaner.userIDs[0] != 7 {
		t.Fatalf("tracked users=%v", cleaner.userIDs)
	}
}

func TestUDPCandidateSummaryIncludesCandidateCounts(t *testing.T) {
	got := udpCandidateSummary(overlay.UDPPeerCandidate{
		VirtualIP:          "172.16.1.11",
		Endpoint:           "203.0.113.10:30001",
		LANEndpoints:       []string{"192.168.1.10:30001"},
		ReflexiveEndpoints: []string{"198.51.100.10:30001", "198.51.100.11:30001"},
	})
	want := "ip=172.16.1.11 observed=1 lan=1 srflx=2"
	if got != want {
		t.Fatalf("summary=%q, want %q", got, want)
	}
}
