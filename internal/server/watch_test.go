package server

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"git-review/internal/repository"
)

func testRepository(t *testing.T, content string) (*repository.Repository, string) {
	t.Helper()
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := repository.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo, path
}

func TestLocateComment(t *testing.T) {
	diff := []byte("diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -4,3 +4,3 @@\n unchanged\n-old value\n+new value\n tail\n")
	tests := []struct {
		name    string
		side    string
		context string
		line    int
		found   bool
	}{
		{name: "old line", side: "old", context: "-old value", line: 5, found: true},
		{name: "new line", side: "new", context: "+new value", line: 5, found: true},
		{name: "context on old side", side: "old", context: " unchanged", line: 4, found: true},
		{name: "context on new side", side: "new", context: " tail", line: 6, found: true},
		{name: "addition is absent from old side", side: "old", context: "+new value", found: false},
		{name: "missing line", side: "new", context: "+missing", found: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			line, found := locateComment(diff, test.side, test.context)
			if line != test.line || found != test.found {
				t.Fatalf("locateComment() = (%d, %t), want (%d, %t)", line, found, test.line, test.found)
			}
		})
	}
}

func TestReconcileCommentsRelocatesAndOutdatesFileComment(t *testing.T) {
	repo, path := testRepository(t, "first\ntarget\nlast\n")
	events := make(chan string, 2)
	s := &Server{
		repo: repo,
		comments: map[string]Comment{
			"comment": {ID: "comment", Path: "file.txt", Area: "file", Line: 2, Context: "target"},
		},
		subs: map[chan string]struct{}{events: {}},
	}
	if err := os.WriteFile(path, []byte("inserted\nfirst\ntarget\nlast\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.reconcileComments(context.Background())
	if comment := s.comments["comment"]; comment.Line != 3 || comment.Outdated {
		t.Fatalf("relocated comment = line %d, outdated %t", comment.Line, comment.Outdated)
	}
	if event := <-events; event != "comments" {
		t.Fatalf("event = %q, want comments", event)
	}

	if err := os.WriteFile(path, []byte("target\nother\ntarget\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.reconcileComments(context.Background())
	if comment := s.comments["comment"]; !comment.Outdated {
		t.Fatal("comment with ambiguous context was not marked outdated")
	}
}

func TestWatchRepositoryPublishesExternalChange(t *testing.T) {
	repo, path := testRepository(t, "initial\n")
	s, err := New(repo, 0, 0, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.listener.Close() })
	events := make(chan string, 2)
	s.subs[events] = struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.watchRepository(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.RLock()
		initialized := s.lastState != ""
		s.mu.RUnlock()
		if initialized {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("repository watcher did not initialize")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.WriteFile(path, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event != "repository" {
			t.Fatalf("event = %q, want repository", event)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("repository watcher did not publish an external change")
	}
}
