package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/clientrelease"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	updateStatusIdle        = "idle"
	updateStatusChecking    = "checking"
	updateStatusDownloading = "downloading"
	updateStatusReady       = "ready"
	updateStatusInstalling  = "installing"
	updateStatusError       = "error"

	updatePlatform = "windows-amd64"

	updateApplyFlag = "--lwylink-apply-update"
)

type UpdateState struct {
	Status         string `json:"status"`
	Message        string `json:"message"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	Platform       string `json:"platform"`
	Notes          string `json:"notes"`
	Mandatory      bool   `json:"mandatory"`
	InstallArgs    string `json:"installArgs"`
	FileName       string `json:"fileName"`
	SizeBytes      int64  `json:"sizeBytes"`
	SHA256         string `json:"sha256"`
}

func (a *App) CheckForUpdates() (AppState, error) {
	a.mu.Lock()
	a.resetUpdateLocked()
	a.startUpdateCheckLocked("正在检查更新")
	return a.commitStateLocked(), nil
}

func (a *App) InstallPreparedUpdate() (AppState, error) {
	a.mu.Lock()
	if a.update.Status != updateStatusReady || a.preparedUpdatePath == "" {
		a.mu.Unlock()
		return AppState{}, fmt.Errorf("更新包尚未准备好")
	}
	updatePath := a.preparedUpdatePath
	installArgs := strings.TrimSpace(a.update.InstallArgs)
	a.update.Status = updateStatusInstalling
	a.update.Message = "正在启动更新程序"
	state := a.commitStateLocked()

	var err error
	if installArgs != "" {
		err = startInstaller(updatePath, installArgs)
	} else {
		err = startSelfUpdater(updatePath)
	}
	if err != nil {
		a.mu.Lock()
		a.update.Status = updateStatusError
		a.update.Message = friendlyConnectionError(err)
		return a.commitStateLocked(), err
	}
	if ctx := a.currentContext(); ctx != nil {
		a.allowApplicationQuit()
		wailsRuntime.Quit(ctx)
	}
	return state, nil
}

func (a *App) resetUpdateLocked() {
	a.updateSeq++
	a.preparedUpdatePath = ""
	a.update = UpdateState{
		Status:         updateStatusIdle,
		CurrentVersion: AppVersion,
		Platform:       updatePlatform,
	}
}

func (a *App) startUpdateCheckLocked(message string) {
	if a.apiBaseURL == "" {
		return
	}
	a.updateSeq++
	seq := a.updateSeq
	apiBaseURL := a.apiBaseURL
	token := a.api.Token()
	a.preparedUpdatePath = ""
	a.update = UpdateState{
		Status:         updateStatusChecking,
		Message:        message,
		CurrentVersion: AppVersion,
		Platform:       updatePlatform,
	}
	go a.runUpdateCheck(seq, apiBaseURL, token)
}

func (a *App) runUpdateCheck(seq int64, apiBaseURL, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	release, err := fetchLatestRelease(ctx, apiBaseURL)
	if err != nil {
		if errors.Is(err, clientrelease.ErrNotFound) {
			a.applyUpdateState(seq, UpdateState{
				Status:         updateStatusIdle,
				Message:        "暂无客户端更新",
				CurrentVersion: AppVersion,
				Platform:       updatePlatform,
			}, "")
			return
		}
		a.applyUpdateState(seq, UpdateState{
			Status:         updateStatusError,
			Message:        "检查更新失败: " + err.Error(),
			CurrentVersion: AppVersion,
			Platform:       updatePlatform,
		}, "")
		return
	}
	if !clientrelease.IsNewerVersion(release.Version, AppVersion) {
		a.applyUpdateState(seq, UpdateState{
			Status:         updateStatusIdle,
			Message:        "已是最新版本",
			CurrentVersion: AppVersion,
			LatestVersion:  release.Version,
			Platform:       release.Platform,
		}, "")
		return
	}
	if strings.TrimSpace(token) == "" {
		a.applyUpdateState(seq, updateStateFromRelease(release, updateStatusIdle, "发现新版本，请登录后下载"), "")
		return
	}

	downloading := updateStateFromRelease(release, updateStatusDownloading, "发现新版本，正在后台下载")
	a.applyUpdateState(seq, downloading, "")

	path, err := downloadPreparedUpdate(ctx, apiBaseURL, token, release)
	if err != nil {
		failed := updateStateFromRelease(release, updateStatusError, "下载更新失败: "+err.Error())
		a.applyUpdateState(seq, failed, "")
		return
	}

	ready := updateStateFromRelease(release, updateStatusReady, "新版本已下载，等待重启更新")
	a.applyUpdateState(seq, ready, path)
}

func (a *App) applyUpdateState(seq int64, update UpdateState, preparedPath string) {
	a.mu.Lock()
	if a.updateSeq != seq {
		a.mu.Unlock()
		return
	}
	a.update = update
	a.preparedUpdatePath = preparedPath
	a.commitStateLocked()
}

func runUpdaterFromArgs(args []string) (bool, error) {
	if len(args) < 4 || args[1] != updateApplyFlag {
		return false, nil
	}
	updatePath := args[2]
	targetPath := args[3]
	if err := replaceExecutableWithRetry(updatePath, targetPath, 60*time.Second); err != nil {
		return true, err
	}
	cmd := exec.Command(targetPath)
	cmd.Dir = filepath.Dir(targetPath)
	return true, cmd.Start()
}

func startInstaller(updatePath, installArgs string) error {
	cmd := exec.Command(updatePath, splitInstallArgs(installArgs)...)
	cmd.Dir = filepath.Dir(updatePath)
	return cmd.Start()
}

func startSelfUpdater(updatePath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	helperDir := filepath.Join(os.TempDir(), "LwyLink", "updater")
	if err := os.MkdirAll(helperDir, 0700); err != nil {
		return err
	}
	helperPath := filepath.Join(helperDir, fmt.Sprintf("updater-%d.exe", os.Getpid()))
	_ = os.Remove(helperPath)
	mode := os.FileMode(0755)
	if info, err := os.Stat(currentExe); err == nil {
		mode = info.Mode().Perm()
	}
	if err := copyFile(currentExe, helperPath, mode); err != nil {
		return err
	}
	cmd := exec.Command(helperPath, updateApplyFlag, updatePath, currentExe)
	cmd.Dir = filepath.Dir(currentExe)
	return cmd.Start()
}

func fetchLatestRelease(ctx context.Context, apiBaseURL string) (clientrelease.PublicRelease, error) {
	base := strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	endpoint := base + "/api/client/releases/latest?platform=" + url.QueryEscape(updatePlatform)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return clientrelease.PublicRelease{}, err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return clientrelease.PublicRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return clientrelease.PublicRelease{}, clientrelease.ErrNotFound
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return clientrelease.PublicRelease{}, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var release clientrelease.PublicRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return clientrelease.PublicRelease{}, err
	}
	if release.Version == "" || release.DownloadURL == "" || release.SHA256 == "" {
		return clientrelease.PublicRelease{}, fmt.Errorf("更新清单不完整")
	}
	return release, nil
}

func downloadPreparedUpdate(ctx context.Context, apiBaseURL, token string, release clientrelease.PublicRelease) (string, error) {
	downloadURL, err := resolveDownloadURL(apiBaseURL, release.DownloadURL)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(os.TempDir(), "LwyLink", "updates")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	fileName := release.FileName
	if strings.TrimSpace(fileName) == "" {
		fileName = "LwyLink-" + release.Version + ".exe"
	}
	targetPath := filepath.Join(dir, updateFileName(release, fileName))
	if verifyFileSHA256(targetPath, release.SHA256) == nil {
		return targetPath, nil
	}
	tmpPath := targetPath + ".tmp"
	_ = os.Remove(tmpPath)
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(tmpPath)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("下载 HTTP %d", resp.StatusCode)
	}
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if err := verifyFileSHA256(tmpPath, release.SHA256); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return "", err
	}
	complete = true
	return targetPath, nil
}

func resolveDownloadURL(apiBaseURL, value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if parsed.IsAbs() {
		return parsed.String(), nil
	}
	base, err := url.Parse(strings.TrimRight(apiBaseURL, "/") + "/")
	if err != nil {
		return "", err
	}
	return base.ResolveReference(parsed).String(), nil
}

func verifyFileSHA256(path, want string) error {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return fmt.Errorf("缺少更新包校验值")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return err
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != want {
		return fmt.Errorf("更新包校验失败")
	}
	return nil
}

func replaceExecutableWithRetry(updatePath, targetPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		err := replaceExecutable(updatePath, targetPath)
		if err == nil {
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return lastErr
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func replaceExecutable(updatePath, targetPath string) error {
	updatePath = filepath.Clean(updatePath)
	targetPath = filepath.Clean(targetPath)
	if updatePath == targetPath {
		return fmt.Errorf("更新包路径不能等于目标程序路径")
	}
	info, err := os.Stat(targetPath)
	mode := os.FileMode(0755)
	if err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}

	backupPath := targetPath + ".bak"
	_ = os.Remove(backupPath)
	hasBackup := false
	if err == nil {
		if err := os.Rename(targetPath, backupPath); err != nil {
			return err
		}
		hasBackup = true
	}
	if err := copyFile(updatePath, targetPath, mode); err != nil {
		if hasBackup {
			_ = os.Rename(backupPath, targetPath)
		}
		return err
	}
	if hasBackup {
		_ = os.Remove(backupPath)
	}
	return nil
}

func copyFile(srcPath, dstPath string, mode os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(dstPath)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(dstPath)
		return err
	}
	return nil
}

func updateStateFromRelease(release clientrelease.PublicRelease, status, message string) UpdateState {
	return UpdateState{
		Status:         status,
		Message:        message,
		CurrentVersion: AppVersion,
		LatestVersion:  release.Version,
		Platform:       release.Platform,
		Notes:          release.Notes,
		Mandatory:      release.Mandatory,
		InstallArgs:    release.InstallArgs,
		FileName:       release.FileName,
		SizeBytes:      release.SizeBytes,
		SHA256:         release.SHA256,
	}
}

func updateFileName(release clientrelease.PublicRelease, fallback string) string {
	name := filepath.Base(strings.TrimSpace(fallback))
	if name == "" || name == "." {
		name = "LwyLink-" + release.Version + ".exe"
	}
	hashPrefix := release.SHA256
	if len(hashPrefix) > 12 {
		hashPrefix = hashPrefix[:12]
	}
	return release.Platform + "-" + release.Version + "-" + hashPrefix + "-" + name
}

func splitInstallArgs(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var args []string
	var current strings.Builder
	inQuotes := false
	escape := false
	for _, r := range value {
		if escape {
			current.WriteRune(r)
			escape = false
			continue
		}
		if r == '\\' {
			escape = true
			continue
		}
		if r == '"' {
			inQuotes = !inQuotes
			continue
		}
		if (r == ' ' || r == '\t') && !inQuotes {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
