package hub

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testHub(t *testing.T) *Server {
	t.Helper()
	app, err := New(":0", filepath.Join(t.TempDir(), "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestRepositoryListAndAccessNeverExposeToken(t *testing.T) {
	app := testHub(t)
	app.repos["repo-1"] = Repository{ID: "repo-1", Name: "project", Host: "agent", Token: strings.Repeat("a", 64), lastSeen: time.Now()}
	list := httptest.NewRecorder()
	app.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/repositories", nil))
	if strings.Contains(list.Body.String(), strings.Repeat("a", 64)) {
		t.Fatal("repository list exposed token")
	}
	access := httptest.NewRecorder()
	app.Handler().ServeHTTP(access, httptest.NewRequest(http.MethodGet, "/api/repositories/repo-1/access", nil))
	if access.Code != http.StatusOK || access.Body.String() != `{"baseUrl":"/api/repositories/repo-1/proxy"}`+"\n" {
		t.Fatalf("access = %d %s", access.Code, access.Body.String())
	}
}

func TestStaticAppAllowsCodeMirrorStylesButNotInlineScripts(t *testing.T) {
	app := testHub(t)
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "style-src 'self' 'unsafe-inline'") {
		t.Fatalf("styles blocked by %q", policy)
	}
	if !strings.Contains(policy, "connect-src 'self'") || strings.Contains(policy, "connect-src 'self' http:") {
		t.Fatalf("cross-origin connections permitted by %q", policy)
	}
}

func TestProxyRejectsAgentAndCrossOriginMutation(t *testing.T) {
	app := testHub(t)
	for _, path := range []string{"/api/repositories/repo/proxy/api/agent/stop", "/api/repositories/repo/proxy/not-api"} {
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("%s = %d", path, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "http://hub/api/repositories/repo/proxy/api/change", strings.NewReader("{}"))
	request.Header.Set("Origin", "http://evil.example")
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin mutation = %d", response.Code)
	}
}

func TestExpiryRemovesDisconnectedRepository(t *testing.T) {
	app := testHub(t)
	app.repos["old"] = Repository{ID: "old", lastSeen: time.Now()}
	if !app.expireAt(time.Now().Add(repositoryTTL * 2)) {
		t.Fatal("repository did not expire")
	}
}

func TestProxyPathAllowlist(t *testing.T) {
	for _, path := range []string{"api/repository", "api/comments/id", "api/events"} {
		if !allowedProxyPath(path) {
			t.Errorf("allowed path %q rejected", path)
		}
	}
	for _, path := range []string{"api/agent/stop", "api/agent/events", "api/repository-extra", "debug", "../api/repository"} {
		if allowedProxyPath(path) {
			t.Errorf("forbidden path %q allowed", path)
		}
	}
}
