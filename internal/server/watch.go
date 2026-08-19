package server

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"git-review/internal/repository"
	"github.com/fsnotify/fsnotify"
)

func (s *Server) watchRepository(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	watcher, watchErr := fsnotify.NewWatcher()
	if watchErr == nil {
		defer watcher.Close()
	}
	watched := map[string]bool{}
	addWatches := func() {
		if watcher == nil {
			return
		}
		for _, directory := range s.repo.WatchDirectories(ctx) {
			if !watched[directory] {
				if err := watcher.Add(directory); err == nil {
					watched[directory] = true
				}
			}
		}
	}
	addWatches()
	var events <-chan fsnotify.Event
	var watchErrors <-chan error
	if watcher != nil {
		events, watchErrors = watcher.Events, watcher.Errors
	}
	debounce := time.NewTimer(time.Hour)
	if !debounce.Stop() {
		<-debounce.C
	}
	reconcile := func() {
		current, err := s.repo.SnapshotFingerprint(ctx)
		if err != nil {
			return
		}
		s.mu.Lock()
		changed := s.lastState != "" && s.lastState != current
		s.lastState = current
		s.mu.Unlock()
		addWatches()
		if changed {
			s.reconcileComments(ctx)
			s.publish("repository")
		}
	}
	reconcile()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		case <-events:
			if !debounce.Stop() {
				select {
				case <-debounce.C:
				default:
				}
			}
			debounce.Reset(150 * time.Millisecond)
		case <-debounce.C:
			reconcile()
		case <-watchErrors:
			// Periodic reconciliation remains active if filesystem watching fails.
		}
	}
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)`)

func locateComment(diff []byte, side, contextLine string) (int, bool) {
	oldLine, newLine := 0, 0
	foundLine, matches := 0, 0
	for _, line := range strings.Split(string(diff), "\n") {
		if match := hunkHeader.FindStringSubmatch(line); match != nil {
			oldLine, _ = strconv.Atoi(match[1])
			newLine, _ = strconv.Atoi(match[2])
			continue
		}
		if oldLine == 0 && newLine == 0 {
			continue
		}
		oldCandidate, newCandidate := 0, 0
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			newCandidate = newLine
			newLine++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			oldCandidate = oldLine
			oldLine++
		} else if strings.HasPrefix(line, " ") {
			oldCandidate, newCandidate = oldLine, newLine
			oldLine++
			newLine++
		}
		candidate := oldCandidate
		if side == "new" {
			candidate = newCandidate
		}
		if line == contextLine && candidate > 0 {
			foundLine = candidate
			matches++
		}
	}
	return foundLine, matches == 1
}

func locateSourceLine(text, contextLine string) (int, bool) {
	found, matches := 0, 0
	for index, line := range strings.Split(text, "\n") {
		if line == contextLine {
			found = index + 1
			matches++
		}
	}
	return found, matches == 1
}

func (s *Server) reconcileComments(ctx context.Context) {
	s.mu.RLock()
	comments := make([]Comment, 0, len(s.comments))
	for _, comment := range s.comments {
		if comment.Line > 0 {
			comments = append(comments, comment)
		}
	}
	s.mu.RUnlock()
	changed := false
	for _, comment := range comments {
		line, current := 0, false
		var err error
		if comment.Area == "file" {
			var content repository.Content
			content, err = s.repo.ReadFile(comment.Path)
			if err == nil {
				line, current = locateSourceLine(content.Text, comment.Context)
			}
		} else {
			var diff []byte
			diff, err = s.repo.Diff(ctx, comment.Path, comment.Area)
			if err == nil {
				line, current = locateComment(diff, comment.Side, comment.Context)
			}
		}
		outdated := err != nil || !current
		s.mu.Lock()
		stored, exists := s.comments[comment.ID]
		if exists && (stored.Outdated != outdated || (!outdated && stored.Line != line)) {
			stored.Outdated = outdated
			if !outdated {
				stored.Line = line
			}
			s.comments[comment.ID] = stored
			changed = true
		}
		s.mu.Unlock()
	}
	if changed {
		s.publish("comments")
	}
}

func (s *Server) watchIdle(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.RLock()
			idle := time.Since(s.lastClient)
			connected := s.activeClients > 0
			s.mu.RUnlock()
			if !connected && idle >= s.idle {
				_ = s.http.Shutdown(context.Background())
				return
			}
		}
	}
}
