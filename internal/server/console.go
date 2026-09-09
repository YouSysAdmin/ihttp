package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// consoleHandler serves the embedded single-page console.
//
// Hashed assets are immutable and cached for a year, the shell is never
// cached, and a path that is not a file gets the shell so the router in
// the page can take it - EXCEPT under /assets, where a miss is a real
// 404: a stale tab asking for a chunk that no longer exists must get an
// error the page can react to, not the shell at 200.
func consoleHandler(console fs.FS) http.Handler {
	if console == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("The console was not built into this binary. Run `task web` and rebuild, or use the API at /api.\n"))
		})
	}

	files := http.FileServerFS(console)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			files.ServeHTTP(w, r)

			return
		}

		w.Header().Set("Cache-Control", "no-cache")

		if name != "" && exists(console, name) {
			files.ServeHTTP(w, r)

			return
		}

		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	})
}

func exists(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)

	return err == nil && !info.IsDir()
}
