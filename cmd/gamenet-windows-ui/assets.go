package main

import "embed"

// Wails serves the built frontend from this embedded filesystem. The frontend
// build produces cmd/gamenet-windows-ui/frontend/dist before the final Go build.
//
//go:embed all:frontend/dist
var assets embed.FS
