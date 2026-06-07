package clientrelease

import (
	"context"
	"testing"
)

func TestServiceLogDownloadPersistsRecord(t *testing.T) {
	repo := &fakeReleaseRepo{}
	svc := NewService(repo, "")
	item := DownloadLog{
		UserID:         7,
		IP:             "203.0.113.10",
		Platform:       "windows-amd64",
		Version:        "0.1.15",
		FileName:       "LwyLink.exe",
		StoredFileName: "windows-0.1.15.exe",
		Result:         "ticket",
	}
	if err := svc.LogDownload(context.Background(), item); err != nil {
		t.Fatalf("log download: %v", err)
	}
	if len(repo.logs) != 1 {
		t.Fatalf("logs=%d, want 1", len(repo.logs))
	}
	if repo.logs[0] != item {
		t.Fatalf("log=%+v, want %+v", repo.logs[0], item)
	}
}

type fakeReleaseRepo struct {
	logs []DownloadLog
}

func (r *fakeReleaseRepo) Create(context.Context, Release) (Release, error) { return Release{}, nil }
func (r *fakeReleaseRepo) List(context.Context, Filter) ([]Release, error)  { return nil, nil }
func (r *fakeReleaseRepo) Count(context.Context) (int, error)               { return 0, nil }
func (r *fakeReleaseRepo) FindByStoredFileName(context.Context, string) (Release, error) {
	return Release{}, ErrNotFound
}
func (r *fakeReleaseRepo) Delete(context.Context, int64) (Release, error) { return Release{}, nil }
func (r *fakeReleaseRepo) LogDownload(_ context.Context, item DownloadLog) error {
	r.logs = append(r.logs, item)
	return nil
}
