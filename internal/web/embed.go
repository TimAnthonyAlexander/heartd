// Package web serves the embedded React dashboard.
//
// The dashboard is built by Vite into the dist/ directory and compiled into
// the binary via go:embed. A placeholder index.html is committed so the
// package always compiles even before the frontend is built.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler that serves the embedded dashboard.
// Unknown paths fall back to index.html so client-side routing works.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve the file if it exists; otherwise fall back to index.html.
		if _, err := fs.Stat(sub, trimLeadingSlash(r.URL.Path)); err != nil {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}

func trimLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	if p == "" {
		return "."
	}
	return p
}
