// Package web serves the production frontend embedded when Arveld is built.
package web

import (
	"bytes"
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// The explicit entry pattern makes a missing frontend build a compilation error.
// Run task web:build before invoking Go directly.
//
//go:embed dist/index.html dist
var files embed.FS

// NewHandler serves immutable build assets and the uncached SPA entry document.
// The caller must keep API and Agent protocol paths outside this fallback.
func NewHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		} else {
			name = strings.TrimSuffix(name, "/")
		}
		if name == "." || !fs.ValidPath(name) || strings.Contains(name, `\`) {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		data, err := files.ReadFile("dist/" + name)
		if err != nil {
			// Missing assets and directories must not become HTML or directory listings.
			if !errors.Is(err, fs.ErrNotExist) || name == "assets" || strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
			data, err = files.ReadFile("dist/" + name)
			if err != nil {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		}
		cache := "no-cache"
		if name == "index.html" {
			cache = "no-store"
		} else if strings.HasPrefix(name, "assets/") {
			cache = "public, max-age=31536000, immutable"
		}
		w.Header().Set("Cache-Control", cache)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	})
}
