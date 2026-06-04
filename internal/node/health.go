package node

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

// StartHealthMonitor keeps node status fresh for the scheduler. A paused node
// is never selected for new rooms, which gives the operator time to repair it.
func (s *Service) StartHealthMonitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkAll(ctx)
			}
		}
	}()
}

func (s *Service) CheckNow(ctx context.Context, nodeID int64) error {
	n, err := s.repo.FindByID(ctx, nodeID)
	if err != nil {
		return err
	}
	return s.checkOne(ctx, n)
}

func (s *Service) checkAll(ctx context.Context) {
	nodes, err := s.repo.List(ctx)
	if err != nil {
		log.Printf("node health list failed: %v", err)
		return
	}
	for _, n := range nodes {
		if ctx.Err() != nil {
			return
		}
		if !n.Enabled || n.Status == StatusDisabled {
			continue
		}
		if err := s.checkOne(ctx, n); err != nil {
			log.Printf("node health check failed node=%s: %v", n.Name, err)
		}
	}
}

func (s *Service) checkOne(ctx context.Context, n Node) error {
	addr := n.HealthAddr
	if strings.TrimSpace(addr) == "" {
		addr = n.PublicOverlayAddr
	}
	if err := probeTCP(ctx, addr, 5*time.Second); err == nil {
		return s.repo.SetStatus(ctx, n.ID, StatusOnline, "")
	} else {
		msg := fmt.Sprintf("overlay port unreachable: %v", err)
		if setErr := s.repo.SetStatus(ctx, n.ID, StatusPaused, msg); setErr != nil {
			return setErr
		}
		return fmt.Errorf("overlay port unreachable: %w", err)
	}
}

func probeTCP(ctx context.Context, addr string, timeout time.Duration) error {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}
