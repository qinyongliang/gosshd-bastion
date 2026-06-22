package server

import (
	"io/fs"
	"net/http"
	"strings"

	webassets "github.com/qinyongliang/gosshd-bastion/web"
)

var spaRoutes = map[string]bool{
	"":             true,
	"dashboard":    true,
	"orgs":         true,
	"org-admin":    true,
	"keys":         true,
	"targets":      true,
	"agents":       true,
	"policies":     true,
	"audit":        true,
	"system-admin": true,
}

func (a *App) serveWeb(w http.ResponseWriter, r *http.Request) {
	assets, err := fs.Sub(webassets.FS, "dist")
	if err != nil {
		http.Error(w, "web assets unavailable", http.StatusInternalServerError)
		return
	}
	assetPath := strings.TrimPrefix(r.URL.Path, "/")
	assetPath = strings.Trim(assetPath, "/")
	if isSPARoute(assetPath) {
		assetPath = "index.html"
	} else {
		if assetPath == "" || strings.Contains(assetPath, "..") || strings.HasPrefix(assetPath, ".") {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(assets, assetPath); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	http.ServeFileFS(w, r, assets, assetPath)
}

func isSPARoute(assetPath string) bool {
	if spaRoutes[assetPath] {
		return true
	}
	return strings.HasPrefix(assetPath, "targets/") && strings.HasSuffix(assetPath, "/connect")
}
