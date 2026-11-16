package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html app.js style.css favicon.svg install.js install.css manifest.webmanifest icon-180.png icon-192.png icon-512.png
var assets embed.FS

func Handler() http.Handler {
	sub, _ := fs.Sub(assets, ".")
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.webmanifest" {
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		files.ServeHTTP(w, r)
	})
}
