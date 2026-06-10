package main

import (
	"testing"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/client"
)

func TestParseConnectFlagsDefaultsToPublicAPI(t *testing.T) {
	t.Setenv("LWYLINK_API", "")

	cfg, err := parseConnectFlags(nil)
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if cfg.apiBaseURL != defaultAPIBaseURL {
		t.Fatalf("apiBaseURL=%q, want %q", cfg.apiBaseURL, defaultAPIBaseURL)
	}
}

func TestViewTransportDiagnostics(t *testing.T) {
	probeAt := time.Date(2026, 6, 9, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	got := viewTransportDiagnostics([]client.TransportPeerDiagnostic{
		{
			VirtualIP:           "172.16.1.11",
			CandidateCount:      3,
			CandidateTypes:      []string{"lan", "srflx-stun"},
			SelectedType:        "srflx-stun",
			SelectedPath:        "selected",
			LastProbeAt:         probeAt,
			LastSuccessAt:       probeAt.Add(time.Second),
			LastFailureAt:       probeAt.Add(-time.Second),
			LastRTT:             18 * time.Millisecond,
			FailedProbes:        2,
			ConsecutiveFailures: 1,
			FailureReason:       "probe timeout",
			DisabledReason:      "udp disabled",
			LastObservedPeerAt:  probeAt.Add(2 * time.Second),
		},
	})
	if len(got) != 1 {
		t.Fatalf("diagnostics len=%d, want 1", len(got))
	}
	if got[0].VirtualIP != "172.16.1.11" || got[0].CandidateCount != 3 || got[0].SelectedType != "srflx-stun" || got[0].SelectedPath != "selected" || got[0].FailedProbes != 2 {
		t.Fatalf("bad diagnostics: %+v", got[0])
	}
	if len(got[0].CandidateTypes) != 2 || got[0].CandidateTypes[1] != "srflx-stun" || got[0].LastRTTMS != 18 || got[0].ConsecutiveFailures != 1 || got[0].FailureReason != "probe timeout" || got[0].DisabledReason != "udp disabled" {
		t.Fatalf("bad diagnostic evidence: %+v", got[0])
	}
	if got[0].LastProbeAt != "2026-06-09T04:00:00Z" || got[0].LastSuccessAt != "2026-06-09T04:00:01Z" || got[0].LastFailureAt != "2026-06-09T03:59:59Z" || got[0].LastObservedPeerAt != "2026-06-09T04:00:02Z" {
		t.Fatalf("bad diagnostic times: %+v", got[0])
	}
}
