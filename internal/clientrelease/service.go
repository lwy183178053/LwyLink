package clientrelease

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

const maxReleaseUploadBytes = 512 << 20

type releaseRepository interface {
	Create(ctx context.Context, release Release) (Release, error)
	List(ctx context.Context, filter Filter) ([]Release, error)
	Count(ctx context.Context) (int, error)
	FindByStoredFileName(ctx context.Context, storedFileName string) (Release, error)
	Delete(ctx context.Context, id int64) (Release, error)
	LogDownload(ctx context.Context, item DownloadLog) error
}

type Service struct {
	repo       releaseRepository
	storageDir string
}

func NewService(repo releaseRepository, storageDir string) *Service {
	if strings.TrimSpace(storageDir) == "" {
		storageDir = filepath.Join("data", "client-releases")
	}
	return &Service{repo: repo, storageDir: storageDir}
}

func (s *Service) Upload(ctx context.Context, input UploadInput, file multipart.File, header *multipart.FileHeader) (Release, error) {
	if header == nil {
		return Release{}, fmt.Errorf("请选择客户端安装包")
	}
	version := NormalizeVersion(input.Version)
	platform := NormalizePlatform(input.Platform)
	if err := ValidateVersion(version); err != nil {
		return Release{}, err
	}
	if err := ValidatePlatform(platform); err != nil {
		return Release{}, err
	}
	if err := os.MkdirAll(s.storageDir, 0750); err != nil {
		return Release{}, err
	}

	originalName := sanitizeOriginalFileName(header.Filename)
	storedName := s.storedFileName(platform, version, originalName)
	finalPath := filepath.Join(s.storageDir, storedName)
	tmpPath := finalPath + ".tmp"
	defer os.Remove(tmpPath)

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
	if err != nil {
		return Release{}, err
	}
	hasher := sha256.New()
	size, copyErr := copyWithHash(out, file, hasher, maxReleaseUploadBytes)
	closeErr := out.Close()
	if copyErr != nil {
		return Release{}, copyErr
	}
	if closeErr != nil {
		return Release{}, closeErr
	}
	if size <= 0 {
		return Release{}, fmt.Errorf("上传文件为空")
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return Release{}, err
	}

	release, err := s.repo.Create(ctx, Release{
		Version:        version,
		Platform:       platform,
		Notes:          strings.TrimSpace(input.Notes),
		Mandatory:      input.Mandatory,
		InstallArgs:    strings.TrimSpace(input.InstallArgs),
		FileName:       originalName,
		StoredFileName: storedName,
		ContentType:    strings.TrimSpace(header.Header.Get("Content-Type")),
		SizeBytes:      size,
		SHA256:         hex.EncodeToString(hasher.Sum(nil)),
	})
	if err != nil {
		_ = os.Remove(finalPath)
		return Release{}, err
	}
	return release, nil
}

func (s *Service) List(ctx context.Context, filter Filter) ([]Release, error) {
	return s.repo.List(ctx, filter)
}

func (s *Service) Count(ctx context.Context) (int, error) {
	return s.repo.Count(ctx)
}

func (s *Service) LogDownload(ctx context.Context, item DownloadLog) error {
	if s == nil || s.repo == nil {
		return nil
	}
	return s.repo.LogDownload(ctx, item)
}

func (s *Service) Latest(ctx context.Context, platform string) (Release, error) {
	platform = NormalizePlatform(platform)
	releases, err := s.repo.List(ctx, Filter{Platform: platform, Limit: 1000})
	if err != nil {
		return Release{}, err
	}
	if len(releases) == 0 {
		return Release{}, ErrNotFound
	}
	latest := releases[0]
	for _, item := range releases[1:] {
		if CompareVersions(item.Version, latest.Version) > 0 {
			latest = item
		}
	}
	return latest, nil
}

func (s *Service) FindByStoredFileName(ctx context.Context, storedFileName string) (Release, error) {
	storedFileName = filepath.Base(strings.TrimSpace(storedFileName))
	if storedFileName == "" || storedFileName == "." {
		return Release{}, ErrNotFound
	}
	return s.repo.FindByStoredFileName(ctx, storedFileName)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("bad release id")
	}
	release, err := s.repo.Delete(ctx, id)
	if err != nil {
		return err
	}
	if err := os.Remove(s.FilePath(release)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Service) FilePath(release Release) string {
	return filepath.Join(s.storageDir, filepath.Base(release.StoredFileName))
}

func (s *Service) storedFileName(platform, version, originalName string) string {
	ext := strings.ToLower(filepath.Ext(originalName))
	if len(ext) > 16 {
		ext = ""
	}
	return fmt.Sprintf("%s-%s-%s%s", filenameToken(platform), filenameToken(version), randomSuffix(), ext)
}

func copyWithHash(dst io.Writer, src io.Reader, hasher hash.Hash, maxBytes int64) (int64, error) {
	limited := &io.LimitedReader{R: src, N: maxBytes + 1}
	size, err := io.Copy(io.MultiWriter(dst, hasher), limited)
	if err != nil {
		return 0, err
	}
	if size > maxBytes {
		return 0, fmt.Errorf("上传文件不能超过 %d MB", maxBytes>>20)
	}
	return size, nil
}

func sanitizeOriginalFileName(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	if value == "" || value == "." {
		return "client-release.bin"
	}
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '\'' || r == '/' || r == '\\' {
			return '-'
		}
		return r
	}, value)
	if len([]rune(value)) > 180 {
		value = trimFileName(value, 180)
	}
	return value
}

func trimFileName(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	ext := filepath.Ext(value)
	extRunes := []rune(ext)
	keep := maxRunes - len(extRunes)
	if keep <= 0 {
		return string(runes[:maxRunes])
	}
	return string(runes[:keep]) + ext
}

func filenameToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), ".-_")
	if out == "" {
		return "release"
	}
	return out
}

func randomSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(b[:])
}
