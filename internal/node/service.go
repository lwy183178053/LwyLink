package node

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateOrUpdate(ctx context.Context, n Node) (Node, error) {
	n = normalize(n)
	if err := validate(n); err != nil {
		return Node{}, err
	}
	if n.ID > 0 {
		return s.repo.Update(ctx, n)
	}
	if n.Status == "" {
		n.Status = StatusUnknown
	}
	return s.repo.Create(ctx, n)
}

func (s *Service) List(ctx context.Context) ([]Node, error) {
	return s.repo.List(ctx)
}

func (s *Service) Stats(ctx context.Context) (Stats, error) {
	return s.repo.Stats(ctx)
}

func (s *Service) FindByPublicKey(ctx context.Context, publicKey string) (Node, error) {
	return s.repo.FindByPublicKey(ctx, publicKey)
}

func (s *Service) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	return s.repo.SetEnabled(ctx, id, enabled)
}

func (s *Service) SetStatus(ctx context.Context, id int64, status, lastError string) error {
	return s.repo.SetStatus(ctx, id, status, lastError)
}

// SelectForRoom chooses a node for a newly created room. The scheduler skips
// disabled/paused nodes and nodes that have reached max_rooms, then picks the
// lowest load ratio so rooms spread across available nodes.
func (s *Service) SelectForRoom(ctx context.Context) (Node, error) {
	nodes, err := s.repo.List(ctx)
	if err != nil {
		return Node{}, err
	}

	candidates := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if !n.Enabled || n.Status != StatusOnline {
			continue
		}
		if n.MaxRooms > 0 && n.CurrentRooms >= n.MaxRooms {
			continue
		}
		candidates = append(candidates, n)
	}
	if len(candidates) == 0 {
		return Node{}, fmt.Errorf("no available room node")
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := loadScore(candidates[i])
		right := loadScore(candidates[j])
		if left == right {
			return candidates[i].ID < candidates[j].ID
		}
		return left < right
	})
	return candidates[0], nil
}

func normalize(n Node) Node {
	n.Name = strings.TrimSpace(n.Name)
	n.PublicOverlayAddr = strings.TrimSpace(n.PublicOverlayAddr)
	n.PublicUDPAddr = strings.TrimSpace(n.PublicUDPAddr)
	n.HealthAddr = strings.TrimSpace(n.HealthAddr)
	n.OverlayPublicKey = strings.TrimSpace(n.OverlayPublicKey)
	if n.MaxRooms <= 0 {
		n.MaxRooms = 100
	}
	if n.Status == "" {
		n.Status = StatusUnknown
	}
	return n
}

func validate(n Node) error {
	if len(n.Name) < 2 || len(n.Name) > 80 {
		return fmt.Errorf("node name length must be 2-80")
	}
	if n.PublicOverlayAddr == "" {
		return fmt.Errorf("public overlay address is required")
	}
	if n.OverlayPublicKey == "" {
		return fmt.Errorf("overlay public key is required")
	}
	return nil
}

func loadScore(n Node) float64 {
	if n.MaxRooms <= 0 {
		return float64(n.CurrentRooms)
	}
	return float64(n.CurrentRooms) / float64(n.MaxRooms)
}
