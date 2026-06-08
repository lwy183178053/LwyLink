package clientrelease

import (
	"bytes"
	"context"
	"mime/multipart"
	"testing"
	"time"
)

func TestServiceUploadAlwaysCreatesMandatoryRelease(t *testing.T) {
	repo := &fakeReleaseRepo{}
	svc := NewService(repo, t.TempDir())
	header := &multipart.FileHeader{Filename: "LwyLink.exe"}

	_, err := svc.Upload(context.Background(), UploadInput{
		Version:  "0.1.19",
		Platform: DefaultPlatform,
	}, multipartFile{bytes.NewReader([]byte("release-binary"))}, header)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created=%d, want 1", len(repo.created))
	}
	if !repo.created[0].Mandatory {
		t.Fatalf("release uploaded as optional; current releases must be mandatory")
	}
}

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
	created []Release
	logs    []DownloadLog
}

func (r *fakeReleaseRepo) Create(_ context.Context, release Release) (Release, error) {
	r.created = append(r.created, release)
	release.ID = int64(len(r.created))
	return release, nil
}
func (r *fakeReleaseRepo) List(context.Context, Filter) ([]Release, error) { return nil, nil }
func (r *fakeReleaseRepo) Count(context.Context) (int, error)              { return 0, nil }
func (r *fakeReleaseRepo) DownloadCountSince(context.Context, time.Time) (int, error) {
	return 0, nil
}
func (r *fakeReleaseRepo) FindByStoredFileName(context.Context, string) (Release, error) {
	return Release{}, ErrNotFound
}
func (r *fakeReleaseRepo) Delete(context.Context, int64) (Release, error) { return Release{}, nil }
func (r *fakeReleaseRepo) LogDownload(_ context.Context, item DownloadLog) error {
	r.logs = append(r.logs, item)
	return nil
}

type multipartFile struct {
	*bytes.Reader
}

func (m multipartFile) Close() error { return nil }
