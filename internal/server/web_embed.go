package server

import (
	"encoding/base64"
	"io/fs"
	"net/http"
	"strings"

	webassets "github.com/qinyongliang/gosshd-bastion/web"
)

var spaRoutes = map[string]bool{
	"":               true,
	"dashboard":      true,
	"orgs":           true,
	"org-admin":      true,
	"keys":           true,
	"local-terminal": true,
	"targets":        true,
	"agents":         true,
	"policies":       true,
	"audit":          true,
	"system-admin":   true,
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

func (a *App) handleFavicon(w http.ResponseWriter, r *http.Request) {
	if err := a.ensureServices(r.Context()); err == nil {
		branding, err := a.loadBrandingSettings(r.Context())
		if err == nil && strings.TrimSpace(branding.AppIcon) != "" {
			contentType, data, err := decodeDataImage(branding.AppIcon)
			if err == nil {
				w.Header().Set("Content-Type", contentType)
				w.Header().Set("Cache-Control", "public, max-age=300")
				_, _ = w.Write(data)
				return
			}
		}
	}
	a.serveWeb(w, r)
}

func decodeDataImage(value string) (string, []byte, error) {
	prefix, encoded, ok := strings.Cut(strings.TrimSpace(value), ",")
	if !ok {
		return "", nil, fs.ErrInvalid
	}
	contentType := strings.TrimPrefix(prefix, "data:")
	contentType = strings.TrimSuffix(contentType, ";base64")
	if contentType == "image/x-icon" || contentType == "image/vnd.microsoft.icon" {
		contentType = "image/x-icon"
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", nil, err
	}
	return contentType, data, nil
}

func isSPARoute(assetPath string) bool {
	if spaRoutes[assetPath] {
		return true
	}
	return strings.HasPrefix(assetPath, "targets/") && strings.HasSuffix(assetPath, "/connect")
}
