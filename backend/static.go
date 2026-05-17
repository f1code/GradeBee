package handler

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// staticFiles holds the embedded frontend dist, copied into backend/static/
// at Docker build time by the multi-stage Dockerfile.
//
//go:embed static
var staticFiles embed.FS

// spaFS is the http.FileSystem rooted at the embedded "static" directory.
// Initialised once at startup.
var spaFS http.FileSystem

// spaIndexHTML caches the contents of index.html (read once at startup).
var spaIndexHTML []byte

func init() {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		// Should never happen — the directory is embedded at compile time.
		panic("static embed sub: " + err.Error())
	}
	spaFS = http.FS(sub)

	// Pre-read index.html so the SPA fallback can serve it without
	// triggering http.FileServer's directory canonicalisation (which
	// 301-redirects requests for /index.html to ./).
	if f, err := spaFS.Open("/index.html"); err == nil {
		defer f.Close()
		buf := &bytes.Buffer{}
		if _, err := io.Copy(buf, f); err == nil {
			spaIndexHTML = buf.Bytes()
		}
	}
}

// spaHandler returns an http.Handler that serves the embedded SPA:
//   - existing file under /assets/* → served with immutable cache
//   - existing file (non-/assets) → served with no special caching
//   - anything else → serve index.html (client-side routing fallback)
func spaHandler() http.Handler {
	fileServer := http.FileServer(spaFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only GET/HEAD make sense for static content.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Try to open the file as-is; if it exists and is a regular file, serve it.
		urlPath := r.URL.Path
		if urlPath == "" {
			urlPath = "/"
		}
		if urlPath != "/" {
			if f, err := spaFS.Open(urlPath); err == nil {
				stat, statErr := f.Stat()
				f.Close()
				if statErr == nil && !stat.IsDir() {
					if strings.HasPrefix(urlPath, "/assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					fileServer.ServeHTTP(w, r)
					return
				}
			}
		}

		// Fallback: serve index.html for SPA client-side routing.
		serveSPAIndex(w)
	})
}

// serveSPAIndex writes the cached index.html with no-cache headers.
func serveSPAIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if spaIndexHTML == nil {
		http.Error(w, "index.html not embedded", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(spaIndexHTML); err != nil {
		// Client disconnected mid-write; nothing useful to do.
		_ = err
	}
}
