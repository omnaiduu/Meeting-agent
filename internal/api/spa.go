package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spa serves the built dashboard. Unknown paths get index.html so client-side
// routes like /meetings/123 work on reload. Hashed assets are cached forever.
func spa(files fs.FS) http.Handler {
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(files, p); err != nil {
			if _, err := fs.Stat(files, "index.html"); err != nil {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.Write([]byte("Meeting Agent API is running. The dashboard isn't built yet: run `make build-web`.\n"))
				return
			}
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}
