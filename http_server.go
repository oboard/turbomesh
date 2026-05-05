package main

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed fallback/*
var fallbackFS embed.FS

func NewHTTPHandler(domain, staticDir string, hub *SignalHub, forceHomepageHTTPS bool) http.Handler {
	mux := http.NewServeMux()
	spa := newSPAHandler(staticDir)

	mux.HandleFunc("/api/client", hub.ServeClient)
	mux.HandleFunc("/api/browser", hub.ServeBrowser)
	mux.HandleFunc("/api/tls-ask", serveTLSAsk(domain))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(hostWithoutPort(r.Host))
		isHomepage := host == strings.TrimSuffix(domain, ".")
		if isHomepage && forceHomepageHTTPS && shouldRedirectHomepageToHTTPS(r) {
			target := "https://" + r.Host + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			return
		}
		spa.ServeHTTP(w, r)
	})

	return mux
}

func serveTLSAsk(domain string) http.HandlerFunc {
	base := strings.TrimSuffix(strings.ToLower(domain), ".")
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.ToLower(r.URL.Query().Get("domain")), ".")
		if name == base || isAllowedSessionHost(name, base) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "domain is not allowed for TurboMesh TLS", http.StatusForbidden)
	}
}

func isAllowedSessionHost(name, base string) bool {
	suffix := "." + base
	if !strings.HasSuffix(name, suffix) {
		return false
	}
	label := strings.TrimSuffix(name, suffix)
	if strings.Contains(label, ".") {
		return false
	}
	return validateSlug(label) == nil
}

func shouldRedirectHomepageToHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return false
	}
	proto := strings.ToLower(r.Header.Get("X-Forwarded-Proto"))
	if proto == "https" {
		return false
	}
	return proto == "http"
}

func newSPAHandler(staticDir string) http.Handler {
	if st, err := os.Stat(staticDir); err == nil && st.IsDir() {
		return spaFileServer(os.DirFS(staticDir))
	}
	sub, _ := fs.Sub(fallbackFS, "fallback")
	return spaFileServer(sub)
}

func spaFileServer(files fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
		if path == "." || path == "" {
			path = "index.html"
		}
		if path == "index.html" {
			serveIndex(w, r, files)
			return
		}
		if f, err := files.Open(path); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w, r, files)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, files fs.FS) {
	content, err := fs.ReadFile(files, "index.html")
	if err != nil {
		http.Error(w, "frontend index.html not found", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(content))
}
