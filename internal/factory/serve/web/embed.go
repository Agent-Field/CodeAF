// Package web is the factory dashboard: a Vite + React app under this
// directory whose built dist/ is committed and embedded, so the Go build and
// CI never need Node. `make web` rebuilds dist/; embed_test.go fails when
// dist/ was built from different sources than the ones checked in.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the dashboard. A path naming a built file gets that file;
// any other path gets index.html, because the app routes in the browser and
// a deep link such as /jobs/12 must open the app, not a 404. The caller
// keeps /v1 for the API, so this handler never sees it.
func Handler() http.Handler {
	files, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the embed pattern guarantees dist exists at build time
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if info, err := fs.Stat(files, name); name == "" || err != nil || info.IsDir() || strings.HasPrefix(name, ".") {
			name = "index.html"
		}
		// Built assets carry a content hash in their names, so they may be
		// kept forever; index.html keeps the handler's no-store so a new
		// build is picked up on the next load.
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFileFS(w, r, files, name)
	})
}
