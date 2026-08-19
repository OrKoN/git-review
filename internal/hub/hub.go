package hub

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"sort"
	"sync"
	"time"

	"git-review/internal/httpapi"
	"git-review/internal/identity"
	"git-review/internal/repository"
	webassets "git-review/web"
)

const repositoryTTL = 30 * time.Second

type Repository struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Branch   string `json:"branch,omitempty"`
	Token    string `json:"-"`
	lastSeen time.Time
}

type Server struct {
	http          *http.Server
	tunnel        *http.Server
	tunnelAddress string
	store         identity.Store
	mu            sync.RWMutex
	repos         map[string]Repository
	sessions      map[string]*repositorySession
	subs          map[chan string]struct{}
}

func New(address, statePath string) (*Server, error) {
	return NewWithConfig(address, ":8443", statePath)
}

func NewWithConfig(address, tunnelAddress, statePath string) (*Server, error) {
	if statePath == "" {
		statePath = identity.DefaultStatePath()
	}
	store := identity.Store{Path: statePath}
	if err := store.Ensure(); err != nil {
		return nil, err
	}
	s := &Server{tunnelAddress: tunnelAddress, store: store, repos: map[string]Repository{}, sessions: map[string]*repositorySession{}, subs: map[chan string]struct{}{}}
	s.http = &http.Server{Addr: address, Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	s.tunnel = &http.Server{Addr: tunnelAddress, Handler: s.tunnelRoutes(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	return s, nil
}

func (s *Server) Run(ctx context.Context) error {
	go s.expire(ctx)
	tunnelErrors := make(chan error, 1)
	go func() { tunnelErrors <- s.runTunnel() }()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdown)
		_ = s.tunnel.Shutdown(shutdown)
	}()
	errCh := make(chan error, 1)
	go func() { errCh <- s.http.ListenAndServe() }()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-tunnelErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		return nil
	}
}

func (s *Server) Handler() http.Handler { return s.http.Handler }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/repositories", s.list)
	mux.HandleFunc("GET /api/repositories/{id}/access", s.access)
	mux.HandleFunc("/api/repositories/{id}/proxy/{path...}", s.proxy)
	mux.HandleFunc("GET /api/events", s.events)
	static, _ := fs.Sub(webassets.Static, "dist")
	mux.Handle("/", securityHeaders(http.FileServer(http.FS(static))))
	return http.MaxBytesHandler(mux, repository.MaxEditableSize+64<<10)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CodeMirror mounts its base and selected theme through style-mod at
		// runtime. Repository text is escaped by lit-html and scripts remain
		// restricted to same-origin assets, so only inline styles are relaxed.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) list(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	repos := make([]Repository, 0, len(s.repos))
	for _, repo := range s.repos {
		repos = append(repos, repo)
	}
	s.mu.RUnlock()
	sort.Slice(repos, func(i, j int) bool { return repos[i].ID < repos[j].ID })
	writeJSON(w, 200, repos)
}

func (s *Server) access(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	repo, ok := s.repos[r.PathValue("id")]
	s.mu.RUnlock()
	if !ok {
		writeError(w, 404, "not_found", "Repository is not connected")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]string{"baseUrl": "/api/repositories/" + repo.ID + "/proxy"})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "stream_error", "Streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ch := make(chan string, 4)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.subs, ch); s.mu.Unlock() }()
	w.Write([]byte(": connected\n\n"))
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case event := <-ch:
			w.Write([]byte("event: repositories\ndata: {\"reason\":\"" + event + "\"}\n\n"))
			flusher.Flush()
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": heartbeat\n\n"))
			flusher.Flush()
		}
	}
}

func (s *Server) publish(event string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *Server) expire(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if s.expireAt(now) {
				s.publish("disconnected")
			}
		}
	}
}

func (s *Server) expireAt(now time.Time) bool {
	removed := false
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, repo := range s.repos {
		if now.Sub(repo.lastSeen) > repositoryTTL {
			delete(s.repos, id)
			delete(s.sessions, id)
			removed = true
		}
	}
	return removed
}

var writeJSON = httpapi.WriteJSON
var writeError = httpapi.WriteError
