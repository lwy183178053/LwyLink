//go:build !windows

package main

type trayController struct{}

func (a *App) registerTray() {}

func (a *App) stopTray() {}

func (a *App) updateTray(state AppState) {}
