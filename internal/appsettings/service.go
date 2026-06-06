package appsettings

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

const (
	AnnouncementKey     = "announcement"
	DefaultAnnouncement = "官方 QQ 群：715516422"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Announcement(ctx context.Context) (string, error) {
	value, err := s.repo.Get(ctx, AnnouncementKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DefaultAnnouncement, nil
		}
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return DefaultAnnouncement, nil
	}
	return value, nil
}

func (s *Service) SetAnnouncement(ctx context.Context, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		value = DefaultAnnouncement
	}
	return s.repo.Set(ctx, AnnouncementKey, value)
}
