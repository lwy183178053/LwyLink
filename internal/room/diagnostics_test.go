package room

import (
	"testing"
	"time"
)

func TestNetworkDiagnosticsPrunesExpiredReportsAndReturnsCopies(t *testing.T) {
	s := NewService(nil, nil, nil, "", 0)
	now := time.Now()
	s.networkReports = map[int64]map[int64]NetworkDiagnosticReport{
		1: {
			7: {
				RoomID:        1,
				UserID:        7,
				TransportMode: "udp-direct",
				DirectPeerIPs: []string{"172.16.1.11"},
				TransportDiagnostics: []TransportDiagnostic{{
					VirtualIP:      "172.16.1.11",
					CandidateTypes: []string{"lan"},
					SelectedPath:   "selected",
				}},
				UpdatedAt: now,
			},
			8: {
				RoomID:    1,
				UserID:    8,
				UpdatedAt: now.Add(-networkDiagnosticTTL - time.Second),
			},
		},
	}

	got := s.NetworkDiagnostics([]int64{1})
	reports := got[1]
	if len(reports) != 1 || reports[0].UserID != 7 {
		t.Fatalf("reports=%+v, want only fresh user 7", reports)
	}
	reports[0].DirectPeerIPs[0] = "mutated"
	reports[0].TransportDiagnostics[0].CandidateTypes[0] = "mutated"

	got = s.NetworkDiagnostics([]int64{1})
	if got[1][0].DirectPeerIPs[0] != "172.16.1.11" {
		t.Fatalf("direct peer IP was not cloned: %+v", got[1][0])
	}
	if got[1][0].TransportDiagnostics[0].CandidateTypes[0] != "lan" {
		t.Fatalf("candidate types were not cloned: %+v", got[1][0].TransportDiagnostics[0])
	}
}
