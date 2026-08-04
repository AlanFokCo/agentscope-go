// Package webui provides an embedded web interface for AgentScope agents.
//
// It embeds a single-page application (SPA) that communicates with the
// agent [service.Service] REST/SSE API. The UI displays:
//   - Session management (create, list, delete)
//   - Streaming chat with thinking blocks, tool calls, and tool results
//   - Human-in-the-loop confirmation for tool calls
//   - Model information
//
// Usage:
//
//	svc := service.New(cfg, cm, factory)
//	webui.Mount(svc, webui.Options{})
//	svc.ListenAndServe()
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var staticFS embed.FS

// Options configures the web UI.
type Options struct {
	// PathPrefix is the URL prefix for the web UI (default "/").
	PathPrefix string

	// Title overrides the page title (default "AgentScope Studio").
	Title string
}

// Mountable is the interface that service.Service must implement for
// the web UI to attach its routes. It is satisfied by *service.Service
// because we add a MountHandler method.
type Mountable interface {
	Handler() http.Handler
}

// Handler returns an http.Handler that serves the embedded web UI.
// It serves static files from the embedded filesystem and falls back
// to index.html for SPA routing.
func Handler(opts Options) http.Handler {
	if opts.PathPrefix == "" {
		opts.PathPrefix = "/"
	}

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("webui: embedded static fs: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if opts.PathPrefix != "/" {
			path = strings.TrimPrefix(path, strings.TrimSuffix(opts.PathPrefix, "/"))
			if path == "" {
				path = "/"
			}
		}

		// Try to serve the file directly
		if path != "/" {
			clean := strings.TrimPrefix(path, "/")
			if f, err := sub.Open(clean); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// SPA fallback: serve index.html
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

// MuxMount registers the web UI handler on an existing *http.ServeMux.
func MuxMount(mux *http.ServeMux, opts Options) {
	if opts.PathPrefix == "" {
		opts.PathPrefix = "/"
	}
	handler := Handler(opts)

	// Register both exact and prefix patterns
	prefix := strings.TrimSuffix(opts.PathPrefix, "/")
	if prefix == "" {
		mux.Handle("/", handler)
	} else {
		mux.Handle(prefix+"/", http.StripPrefix(prefix, handler))
	}
}
