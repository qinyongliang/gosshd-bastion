package server

import (
	"io/fs"
	"net/http"
	"strings"

	webassets "github.com/qinyongliang/gosshd-bastion/web"
)

func (a *App) serveWeb(w http.ResponseWriter, r *http.Request) {
	assets, err := fs.Sub(webassets.FS, ".")
	if err != nil {
		http.Error(w, "web assets unavailable", http.StatusInternalServerError)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if _, err := fs.Stat(assets, path); err != nil {
		path = "index.html"
	}
	http.ServeFileFS(w, r, assets, path)
}
