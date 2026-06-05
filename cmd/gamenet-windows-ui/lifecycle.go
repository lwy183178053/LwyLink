package main

import (
	"context"
	"fmt"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) HideToTray() error {
	return a.hideToTray(a.currentContext(), true)
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	allowQuit := a.allowQuit
	a.mu.Unlock()
	if allowQuit {
		return false
	}
	_ = a.hideToTray(ctx, true)
	return true
}

func (a *App) hideToTray(ctx context.Context, notify bool) error {
	if ctx == nil {
		return nil
	}
	wailsRuntime.WindowHide(ctx)
	if notify {
		a.notifyTrayHiddenOnce()
	}
	return nil
}

func (a *App) showMainWindow() {
	ctx := a.currentContext()
	if ctx == nil {
		return
	}
	wailsRuntime.WindowShow(ctx)
	wailsRuntime.WindowUnminimise(ctx)

	// Windows sometimes keeps a restored frameless WebView behind the active app.
	// A short always-on-top pulse makes the second-instance and tray actions feel direct.
	wailsRuntime.WindowSetAlwaysOnTop(ctx, true)
	go func() {
		time.Sleep(160 * time.Millisecond)
		wailsRuntime.WindowSetAlwaysOnTop(ctx, false)
	}()
}

func (a *App) quitApplication() {
	a.allowApplicationQuit()
	a.stopTray()
	ctx := a.currentContext()
	if ctx != nil {
		wailsRuntime.Quit(ctx)
	}
}

func (a *App) allowApplicationQuit() {
	a.mu.Lock()
	a.allowQuit = true
	a.mu.Unlock()
}

func (a *App) currentContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}

func (a *App) initializeNotifications(ctx context.Context) {
	if ctx == nil || !wailsRuntime.IsNotificationAvailable(ctx) {
		return
	}
	if err := wailsRuntime.InitializeNotifications(ctx); err != nil {
		return
	}
	allowed, err := wailsRuntime.RequestNotificationAuthorization(ctx)
	if err != nil || !allowed {
		return
	}
	a.mu.Lock()
	a.notificationsReady = true
	a.mu.Unlock()
}

func (a *App) cleanupNotifications(ctx context.Context) {
	if ctx == nil {
		ctx = a.currentContext()
	}
	if ctx != nil {
		wailsRuntime.CleanupNotifications(ctx)
	}
}

func (a *App) notifyTrayHiddenOnce() {
	a.mu.Lock()
	if a.trayNoticeShown {
		a.mu.Unlock()
		return
	}
	a.trayNoticeShown = true
	a.mu.Unlock()
	a.sendNotification("LwyV-GameNet", "程序已在系统托盘继续运行，右键托盘图标可以显示窗口或退出程序。")
}

func (a *App) sendNotification(title, body string) {
	ctx := a.currentContext()
	if ctx == nil {
		return
	}
	a.mu.Lock()
	ready := a.notificationsReady
	a.mu.Unlock()
	if !ready {
		return
	}
	_ = wailsRuntime.SendNotification(ctx, wailsRuntime.NotificationOptions{
		ID:    fmt.Sprintf("lwyv-gamenet-%d", time.Now().UnixNano()),
		Title: title,
		Body:  body,
	})
}

func (a *App) toggleConnectionFromTray() {
	a.mu.Lock()
	status := a.connection.Status
	var roomID int64
	if a.currentRoom != nil {
		roomID = a.currentRoom.ID
	}
	a.mu.Unlock()

	var err error
	switch status {
	case statusConnected, statusConnecting:
		_, err = a.Disconnect()
	case statusDisconnecting:
		err = fmt.Errorf("虚拟网络正在断开")
	default:
		if roomID <= 0 {
			err = fmt.Errorf("当前没有加入房间")
		} else {
			_, err = a.ConnectRoom(roomID)
		}
	}
	if err != nil {
		a.sendNotification("托盘操作失败", err.Error())
	}
}

func (a *App) checkOrInstallUpdateFromTray() {
	a.mu.Lock()
	status := a.update.Status
	a.mu.Unlock()

	var err error
	if status == updateStatusReady {
		_, err = a.InstallPreparedUpdate()
	} else {
		_, err = a.CheckForUpdates()
	}
	if err != nil {
		a.sendNotification("更新操作失败", err.Error())
	}
}
