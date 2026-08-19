package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-review/internal/repository"
)

func liveServer(t *testing.T) (*Server, string, *http.Client, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "-C", root, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := repository.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	app, err := New(repo, 0, 0, &output)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = app.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server did not stop during cleanup")
		}
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", app.Port())
	client := &http.Client{}
	return app, base, client, &output
}

func requestJSON(t *testing.T, client *http.Client, method, url string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func authenticateClient(t *testing.T, app *Server, base string, client *http.Client) {
	t.Helper()
	client.Transport = roundTripperWithBearer{token: app.Token()}
}

type roundTripperWithBearer struct{ token string }

func (r roundTripperWithBearer) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.Header.Set("Authorization", "Bearer "+r.token)
	return http.DefaultTransport.RoundTrip(copy)
}

func TestBearerAuthenticationIsRequired(t *testing.T) {
	app, base, client, _ := liveServer(t)
	response, err := client.Get(base + "/api/repository")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.StatusCode)
	}
	authenticateClient(t, app, base, client)
	response, err = client.Get(base + "/api/repository")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated status = %d", response.StatusCode)
	}
}

func TestCommentLifecycleWritesCLIEvents(t *testing.T) {
	app, base, client, output := liveServer(t)
	authenticateClient(t, app, base, client)
	created := requestJSON(t, client, "POST", base+"/api/comments", map[string]any{"path": "file.txt", "body": "Review this"})
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", created.StatusCode)
	}
	var comment Comment
	if err := json.NewDecoder(created.Body).Decode(&comment); err != nil {
		t.Fatal(err)
	}
	resolved := requestJSON(t, client, "PATCH", base+"/api/comments/"+comment.ID, map[string]bool{"resolved": true})
	resolved.Body.Close()
	if resolved.StatusCode != http.StatusOK {
		t.Fatalf("resolve status = %d", resolved.StatusCode)
	}
	deleted := requestJSON(t, client, "DELETE", base+"/api/comments/"+comment.ID, nil)
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleted.StatusCode)
	}
	got := output.String()
	for _, event := range []string{"comment added", "comment resolved", "comment deleted", "Review this"} {
		if !strings.Contains(got, event) {
			t.Errorf("output missing %q:\n%s", event, got)
		}
	}
}

func TestAgentEventStreamReceivesCommentBlocks(t *testing.T) {
	app, base, client, _ := liveServer(t)
	authenticateClient(t, app, base, client)
	response, err := client.Get(base + "/api/agent/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	received := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(response.Body)
		var event strings.Builder
		for scanner.Scan() {
			event.WriteString(scanner.Text())
			event.WriteByte('\n')
			if strings.Contains(scanner.Text(), "end git-review comment") {
				break
			}
		}
		received <- event.String()
	}()
	created := requestJSON(t, client, "POST", base+"/api/comments", map[string]any{"path": "file.txt", "body": "Stream this"})
	created.Body.Close()
	select {
	case event := <-received:
		if !strings.Contains(event, "comment added") || !strings.Contains(event, "Stream this") {
			t.Fatalf("event = %q", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent event did not arrive")
	}
}

func TestReviewMessageIsExposedAndCommentable(t *testing.T) {
	app, base, client, output := liveServer(t)
	app.SetReviewMessage("Explain the change\n\nWith useful context.")
	authenticateClient(t, app, base, client)
	response, err := client.Get(base + "/api/repository")
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		ReviewMessage string `json:"reviewMessage"`
	}
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if state.ReviewMessage != "Explain the change\n\nWith useful context." {
		t.Fatalf("review message = %q", state.ReviewMessage)
	}
	created := requestJSON(t, client, "POST", base+"/api/comments", map[string]any{"path": "@commit-message", "body": "Clarify the motivation"})
	created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("comment status = %d", created.StatusCode)
	}
	if !strings.Contains(output.String(), "@commit-message") || !strings.Contains(output.String(), "Clarify the motivation") {
		t.Fatalf("CLI output missing message review:\n%s", output.String())
	}
}

func TestIdleServerStops(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "-C", root, "init", "-q")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	repo, _ := repository.Open(root)
	app, err := New(repo, 0, 10*time.Millisecond, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("idle server did not stop")
	}
}
