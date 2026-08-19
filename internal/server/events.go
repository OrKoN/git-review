package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

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

func (s *Server) repositoryChanged(ctx context.Context) {
	s.reconcileComments(ctx)
	s.publish("repository")
}

func (s *Server) eventsHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "stream_error", "Streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ch := make(chan string, 4)
	s.mu.Lock()
	s.activeClients++
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.activeClients--
		s.lastClient = time.Now()
		s.mu.Unlock()
	}()
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case event := <-ch:
			fmt.Fprintf(w, "event: %s\ndata: {}\n\n", event)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) agentEventsHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "stream_error", "Streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ch := make(chan string, 8)
	s.mu.Lock()
	s.agentSubs[ch] = struct{}{}
	s.activeClients++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.agentSubs, ch)
		s.activeClients--
		s.lastClient = time.Now()
		s.mu.Unlock()
	}()
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	for {
		select {
		case block := <-ch:
			for _, line := range strings.Split(strings.TrimSuffix(block, "\n"), "\n") {
				fmt.Fprintf(w, "data: %s\n", line)
			}
			fmt.Fprint(w, "\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) printComment(event string, c Comment) {
	var block strings.Builder
	fmt.Fprintf(&block, "--- git-review comment %s ---\nid: %s\nfile: %s\n", event, c.ID, safeField(c.Path))
	if c.Line > 0 {
		fmt.Fprintf(&block, "anchor: %s:%d\narea: %s\n", c.Side, c.Line, c.Area)
	} else {
		fmt.Fprintln(&block, "anchor: file")
	}
	fmt.Fprintf(&block, "time: %s\n", time.Now().Format(time.RFC3339))
	if c.Context != "" {
		fmt.Fprintf(&block, "context: %s\n", safeField(c.Context))
	}
	fmt.Fprintf(&block, "comment:\n%s\n--- end git-review comment ---\n", c.Body)
	text := block.String()
	if s.out != nil {
		_, _ = io.WriteString(s.out, text)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.agentSubs {
		select {
		case ch <- text:
		default:
		}
	}
}

func safeField(value string) string {
	return strings.NewReplacer("\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(value)
}

func (s *Server) printSummary() {
	if s.out != nil {
		_, _ = io.WriteString(s.out, s.Summary())
	}
}

func (s *Server) Summary() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var block strings.Builder
	fmt.Fprintln(&block, "--- git-review unresolved summary ---")
	for _, c := range s.comments {
		if !c.Resolved {
			fmt.Fprintf(&block, "%s %s: %s\n", c.ID, safeField(c.Path), strings.ReplaceAll(c.Body, "\n", " "))
		}
	}
	fmt.Fprintln(&block, "--- end git-review unresolved summary ---")
	return block.String()
}
