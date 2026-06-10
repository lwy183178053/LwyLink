package web

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
)

//go:embed admin_assets/*
var adminFiles embed.FS

var adminModuleTemplate = template.Must(template.New("admin.html").Funcs(template.FuncMap{
	"activeClass":     activeClass,
	"statusClass":     statusClass,
	"userStatusText":  userStatusText,
	"userStatusClass": userStatusClass,
	"formatBytes":     formatBytes,
	"timeText":        timeText,
	"timePtrText":     timePtrText,
	"roomDiagnostics": roomDiagnostics,
}).ParseFS(adminFiles, "admin_assets/admin.html"))

func adminAssetHandler() http.Handler {
	sub, err := fs.Sub(adminFiles, "admin_assets")
	if err != nil {
		log.Printf("load admin assets failed: %v", err)
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(sub))
}
