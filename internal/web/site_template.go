package web

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
)

//go:embed site_assets/*
var siteFiles embed.FS

var siteModuleTemplate = template.Must(template.ParseFS(siteFiles, "site_assets/site.html"))

func siteAssetHandler() http.Handler {
	sub, err := fs.Sub(siteFiles, "site_assets")
	if err != nil {
		log.Printf("load site assets failed: %v", err)
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(sub))
}

func (s *Server) handleSite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	page := "home"
	switch r.URL.Path {
	case "/":
		page = "home"
	case "/login":
		page = "login"
	case "/register":
		page = "register"
	case "/dashboard":
		page = "dashboard"
	case "/download":
		page = "download"
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := siteModuleTemplate.Execute(w, map[string]string{"Page": page}); err != nil {
		log.Printf("render site failed: %v", err)
	}
}
