package vdhcp

import "testing"

func TestAcquireStartsAtFirstHostAndUsesLastHostGateway(t *testing.T) {
	m := NewManager()
	first, err := m.Acquire(1, 100, "172.16.88.0/24", []string{"223.5.5.5"}, 1300)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	second, err := m.Acquire(1, 101, "172.16.88.0/24", []string{"223.5.5.5"}, 1300)
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	if first.IP != "172.16.88.1" {
		t.Fatalf("first IP = %s, want 172.16.88.1", first.IP)
	}
	if second.IP != "172.16.88.2" {
		t.Fatalf("second IP = %s, want 172.16.88.2", second.IP)
	}
	if first.Gateway != "172.16.88.254" || second.Gateway != "172.16.88.254" {
		t.Fatalf("gateway = %s/%s, want 172.16.88.254", first.Gateway, second.Gateway)
	}
}
