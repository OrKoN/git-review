package server

import (
	"context"
	"net/http"
	"time"

	"git-review/internal/httpapi"
	"git-review/internal/repository"
)

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/repository", s.auth(http.HandlerFunc(s.repositoryHandler)))
	mux.Handle("GET /api/files", s.auth(http.HandlerFunc(s.filesHandler)))
	mux.Handle("GET /api/diff", s.auth(http.HandlerFunc(s.diffHandler)))
	mux.Handle("GET /api/file", s.auth(http.HandlerFunc(s.fileHandler)))
	mux.Handle("PUT /api/file", s.auth(http.HandlerFunc(s.fileHandler)))
	mux.Handle("POST /api/change", s.auth(http.HandlerFunc(s.changeHandler)))
	mux.Handle("POST /api/commit", s.auth(http.HandlerFunc(s.commitHandler)))
	mux.Handle("GET /api/comments", s.auth(http.HandlerFunc(s.commentsHandler)))
	mux.Handle("POST /api/comments", s.auth(http.HandlerFunc(s.commentsHandler)))
	mux.Handle("PATCH /api/comments/{id}", s.auth(http.HandlerFunc(s.commentHandler)))
	mux.Handle("DELETE /api/comments/{id}", s.auth(http.HandlerFunc(s.commentHandler)))
	mux.Handle("GET /api/events", s.auth(http.HandlerFunc(s.eventsHandler)))
	mux.Handle("GET /api/agent/events", s.auth(http.HandlerFunc(s.agentEventsHandler)))
	mux.Handle("POST /api/agent/stop", s.auth(http.HandlerFunc(s.stopHandler)))
	return limitBody(mux)
}

func (s *Server) filesHandler(w http.ResponseWriter, r *http.Request) {
	files, err := s.repo.Files(r.Context())
	if err != nil {
		writeError(w, 500, "git_error", err.Error())
		return
	}
	writeJSON(w, 200, files)
}

func (s *Server) stopHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"summary": s.Summary()})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(ctx)
	}()
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, repository.MaxEditableSize+64<<10)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !httpapi.BearerMatches(r.Header.Get("Authorization"), s.token) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		s.mu.Lock()
		s.lastClient = time.Now()
		s.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return httpapi.DecodeJSON(w, r, dst, "Request body must be one valid JSON object")
}

var writeJSON = httpapi.WriteJSON
var writeError = httpapi.WriteError
