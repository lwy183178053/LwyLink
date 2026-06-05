//go:build windows

package main

import (
	_ "embed"
	"runtime"
	"strings"
	"sync"

	"github.com/getlantern/systray"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

type trayController struct {
	app *App

	mu         sync.Mutex
	ready      bool
	show       *systray.MenuItem
	hide       *systray.MenuItem
	connection *systray.MenuItem
	updateItem *systray.MenuItem
	quit       *systray.MenuItem

	stop     chan struct{}
	stopOnce sync.Once
	quitOnce sync.Once
}

func (a *App) registerTray() {
	a.mu.Lock()
	if a.tray != nil {
		a.mu.Unlock()
		return
	}
	tray := &trayController{
		app:  a,
		stop: make(chan struct{}),
	}
	a.tray = tray
	a.mu.Unlock()

	go func() {
		runtime.LockOSThread()
		systray.Run(tray.onReady, tray.onExit)
	}()
}

func (a *App) stopTray() {
	a.mu.Lock()
	tray := a.tray
	a.mu.Unlock()
	if tray != nil {
		tray.quitOnce.Do(systray.Quit)
	}
}

func (a *App) updateTray(state AppState) {
	a.mu.Lock()
	tray := a.tray
	a.mu.Unlock()
	if tray != nil {
		tray.syncState(state)
	}
}

func (t *trayController) onReady() {
	systray.SetIcon(trayIcon)
	systray.SetTooltip("LwyV-GameNet")

	show := systray.AddMenuItem("显示主窗口", "显示 LwyV-GameNet 主窗口")
	hide := systray.AddMenuItem("隐藏到托盘", "隐藏主窗口，后台保持运行")
	systray.AddSeparator()
	connection := systray.AddMenuItem("连接虚拟网络", "连接或断开当前房间的虚拟网络")
	updateItem := systray.AddMenuItem("检查客户端更新", "检查、下载或安装客户端更新")
	systray.AddSeparator()
	quit := systray.AddMenuItem("退出程序", "断开连接并退出 LwyV-GameNet")

	t.mu.Lock()
	t.show = show
	t.hide = hide
	t.connection = connection
	t.updateItem = updateItem
	t.quit = quit
	t.ready = true
	t.mu.Unlock()

	t.syncState(t.app.CurrentState())
	go t.listen(show, hide, connection, updateItem, quit)
}

func (t *trayController) onExit() {
	t.stopOnce.Do(func() {
		close(t.stop)
	})
}

func (t *trayController) listen(show, hide, connection, updateItem, quit *systray.MenuItem) {
	for {
		select {
		case <-show.ClickedCh:
			t.app.showMainWindow()
		case <-hide.ClickedCh:
			_ = t.app.HideToTray()
		case <-connection.ClickedCh:
			t.app.toggleConnectionFromTray()
		case <-updateItem.ClickedCh:
			t.app.checkOrInstallUpdateFromTray()
		case <-quit.ClickedCh:
			t.app.quitApplication()
		case <-t.stop:
			return
		}
	}
}

func (t *trayController) syncState(state AppState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.ready {
		return
	}

	if state.CurrentRoom == nil {
		t.connection.SetTitle("连接虚拟网络")
		t.connection.Disable()
	} else {
		switch state.Connection.Status {
		case statusConnected:
			t.connection.SetTitle("断开虚拟网络")
			t.connection.Enable()
		case statusConnecting:
			t.connection.SetTitle("正在连接虚拟网络")
			t.connection.Disable()
		case statusDisconnecting:
			t.connection.SetTitle("正在断开虚拟网络")
			t.connection.Disable()
		default:
			t.connection.SetTitle("连接 " + trayRoomName(state.CurrentRoom.Name))
			t.connection.Enable()
		}
	}

	switch state.Update.Status {
	case updateStatusChecking:
		t.updateItem.SetTitle("正在检查更新")
		t.updateItem.Disable()
	case updateStatusDownloading:
		t.updateItem.SetTitle("正在后台下载更新")
		t.updateItem.Disable()
	case updateStatusReady:
		t.updateItem.SetTitle("安装更新 " + state.Update.LatestVersion)
		t.updateItem.Enable()
	case updateStatusInstalling:
		t.updateItem.SetTitle("正在启动更新")
		t.updateItem.Disable()
	default:
		t.updateItem.SetTitle("检查客户端更新")
		t.updateItem.Enable()
	}
}

func trayRoomName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "当前房间"
	}
	const max = 18
	runes := []rune(name)
	if len(runes) <= max {
		return name
	}
	return string(runes[:max]) + "..."
}
