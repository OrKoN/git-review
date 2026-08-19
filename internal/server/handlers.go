package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"git-review/internal/repository"
)

func (s *Server) repositoryHandler(w http.ResponseWriter, r *http.Request) {
	state, err := s.repo.State(r.Context())
	if err != nil {
		writeError(w, 500, "git_error", err.Error())
		return
	}
	state.Fingerprint, _ = s.repo.SnapshotFingerprint(r.Context())
	state.ReviewMessage = s.reviewMessage
	writeJSON(w, 200, state)
}

func (s *Server) diffHandler(w http.ResponseWriter, r *http.Request) {
	out, fingerprint, err := s.repo.DiffWithFingerprint(r.Context(), r.URL.Query().Get("path"), r.URL.Query().Get("area"))
	if err != nil {
		writeError(w, 422, "diff_error", err.Error())
		return
	}
	writeJSON(w, 200, struct {
		repository.DiffDocument
		Fingerprint string `json:"fingerprint"`
	}{DiffDocument: repository.ParseDiff(out), Fingerprint: fingerprint})
}

func (s *Server) fileHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if r.Method == http.MethodGet {
		content, err := s.repo.ReadFile(path)
		if err != nil {
			writeError(w, 422, "file_error", err.Error())
			return
		}
		writeJSON(w, 200, content)
		return
	}
	var input struct {
		Text        string `json:"text"`
		Fingerprint string `json:"fingerprint"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	content, err := s.repo.WriteFile(path, input.Fingerprint, input.Text)
	if errors.Is(err, repository.ErrStale) {
		writeError(w, 409, "stale_state", err.Error())
		return
	}
	if err != nil {
		writeError(w, 422, "file_error", err.Error())
		return
	}
	s.repositoryChanged(r.Context())
	writeJSON(w, 200, content)
}

func (s *Server) changeHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action      string `json:"action"`
		Path        string `json:"path"`
		Scope       string `json:"scope,omitempty"`
		Fingerprint string `json:"fingerprint,omitempty"`
		Hunk        int    `json:"hunk,omitempty"`
		Lines       []int  `json:"lines,omitempty"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Action == "stage_all" {
		err := s.repo.StageAll(r.Context(), input.Fingerprint)
		if errors.Is(err, repository.ErrStale) {
			writeError(w, 409, "stale_state", err.Error())
			return
		}
		if err != nil {
			writeError(w, 422, "git_error", err.Error())
			return
		}
		s.repositoryChanged(r.Context())
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var err error
	if input.Scope == "hunk" || input.Scope == "lines" {
		err = s.repo.ApplyPatch(r.Context(), repository.PatchTarget{Path: input.Path, Action: input.Action, Fingerprint: input.Fingerprint, Hunk: input.Hunk, Lines: input.Lines})
		if errors.Is(err, repository.ErrStale) {
			writeError(w, 409, "stale_state", err.Error())
			return
		}
		if err != nil {
			writeError(w, 422, "git_error", err.Error())
			return
		}
		s.repositoryChanged(r.Context())
		w.WriteHeader(http.StatusNoContent)
		return
	}
	area := "unstaged"
	if input.Action == "unstage" {
		area = "staged"
	}
	_, currentFingerprint, fingerprintErr := s.repo.DiffWithFingerprint(r.Context(), input.Path, area)
	if fingerprintErr != nil {
		writeError(w, 422, "git_error", fingerprintErr.Error())
		return
	}
	if input.Fingerprint == "" || input.Fingerprint != currentFingerprint {
		writeError(w, 409, "stale_state", repository.ErrStale.Error())
		return
	}
	switch input.Action {
	case "stage":
		err = s.repo.Stage(r.Context(), input.Path)
	case "unstage":
		err = s.repo.Unstage(r.Context(), input.Path)
	case "discard":
		err = s.repo.Discard(r.Context(), input.Path)
	default:
		writeError(w, 400, "invalid_action", "Action must be stage, unstage, discard, or stage_all")
		return
	}
	if err != nil {
		writeError(w, 422, "git_error", err.Error())
		return
	}
	s.repositoryChanged(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) commitHandler(w http.ResponseWriter, r *http.Request) {
	var input struct{ Subject, Body, Fingerprint string }
	if !decodeJSON(w, r, &input) {
		return
	}
	current, err := s.repo.SnapshotFingerprint(r.Context())
	if err != nil {
		writeError(w, 422, "git_error", err.Error())
		return
	}
	if input.Fingerprint == "" || current != input.Fingerprint {
		writeError(w, 409, "stale_state", repository.ErrStale.Error())
		return
	}
	if err := s.repo.Commit(r.Context(), input.Subject, input.Body); err != nil {
		if errors.Is(err, repository.ErrNoStagedChanges) {
			writeError(w, 409, "no_staged_changes", err.Error())
			return
		}
		writeError(w, 422, "commit_error", err.Error())
		return
	}
	s.repositoryChanged(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) commentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.mu.RLock()
		comments := make([]Comment, 0, len(s.comments))
		for _, comment := range s.comments {
			comments = append(comments, comment)
		}
		s.mu.RUnlock()
		writeJSON(w, 200, comments)
		return
	}
	var comment Comment
	if !decodeJSON(w, r, &comment) {
		return
	}
	comment.Body = strings.TrimSpace(comment.Body)
	if comment.Path == "" || comment.Body == "" || len(comment.Body) > 8<<10 {
		writeError(w, 400, "invalid_comment", "Path and a 1-8 KiB comment are required")
		return
	}
	messageComment := comment.Path == "@commit-message" && s.reviewMessage != ""
	sourceComment := comment.Area == "file" && s.repo.IsReviewable(r.Context(), comment.Path)
	if !messageComment && !sourceComment && !s.repo.IsChanged(r.Context(), comment.Path) {
		writeError(w, 400, "invalid_comment", "Comment path is not a reviewable repository file")
		return
	}
	if messageComment {
		comment.Area, comment.Side, comment.Context, comment.Line = "", "", s.reviewMessage, 0
	} else if sourceComment && comment.Line > 0 {
		if comment.Side != "new" {
			writeError(w, 400, "invalid_comment", "Source comments require the new side")
			return
		}
		content, err := s.repo.ReadFile(comment.Path)
		line, found := locateSourceLine(content.Text, comment.Context)
		if err != nil || !found || line != comment.Line {
			writeError(w, 409, "stale_state", "Comment line is no longer present")
			return
		}
	} else if comment.Line > 0 {
		if (comment.Area != "staged" && comment.Area != "unstaged") || (comment.Side != "old" && comment.Side != "new") {
			writeError(w, 400, "invalid_comment", "Line comments require a valid area and side")
			return
		}
		diff, err := s.repo.Diff(r.Context(), comment.Path, comment.Area)
		line, found := locateComment(diff, comment.Side, comment.Context)
		if err != nil || !found || line != comment.Line {
			writeError(w, 409, "stale_state", "Comment line is no longer present")
			return
		}
	} else {
		comment.Area, comment.Side, comment.Context = "", "", ""
	}
	if strings.Contains(comment.Body, "--- end git-review comment ---") {
		writeError(w, 400, "invalid_comment", "Comment contains a reserved event delimiter")
		return
	}
	if strings.ContainsAny(comment.Body, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x0b\x0c\x0e\x0f") {
		writeError(w, 400, "invalid_comment", "Comment contains control characters")
		return
	}
	comment.ID, _ = randomHex(12)
	comment.Created = time.Now()
	comment.Resolved = false
	s.mu.Lock()
	s.comments[comment.ID] = comment
	s.mu.Unlock()
	s.printComment("added", comment)
	s.publish("comments")
	writeJSON(w, 201, comment)
}

func (s *Server) commentHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	comment, ok := s.comments[id]
	if !ok {
		s.mu.Unlock()
		writeError(w, 404, "not_found", "Comment not found")
		return
	}
	if r.Method == http.MethodDelete {
		delete(s.comments, id)
		s.mu.Unlock()
		s.printComment("deleted", comment)
		s.publish("comments")
		w.WriteHeader(204)
		return
	}
	var input struct {
		Resolved bool `json:"resolved"`
	}
	if !decodeJSON(w, r, &input) {
		s.mu.Unlock()
		return
	}
	comment.Resolved = input.Resolved
	s.comments[id] = comment
	s.mu.Unlock()
	event := "reopened"
	if comment.Resolved {
		event = "resolved"
	}
	s.printComment(event, comment)
	s.publish("comments")
	writeJSON(w, 200, comment)
}
