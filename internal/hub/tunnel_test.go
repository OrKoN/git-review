package hub

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-review/internal/identity"
	"git-review/internal/registration"
)

func TestEnrolledTunnelProxiesAndRevokes(t *testing.T) {
	uiAddress, tunnelAddress := freeAddress(t), freeAddress(t)
	statePath := filepath.Join(t.TempDir(), "identity.json")
	app, err := NewWithConfig(uiAddress, tunnelAddress, statePath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	store := identity.Store{Path: statePath}
	bundleText, err := store.CreateEnrollment("test agent", "http://"+uiAddress, "https://"+tunnelAddress, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := identity.ParseEnrollment(bundleText)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(bundle.CAPEM))
	unauthenticated := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: identity.ServerName, MinVersion: tls.VersionTLS13}}}
	var response *http.Response
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		response, err = unauthenticated.Get("https://" + tunnelAddress + "/v1/tunnel")
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tunnel without client certificate = %d", response.StatusCode)
	}
	rogue := identity.Store{Path: filepath.Join(t.TempDir(), "rogue.json")}
	if err := rogue.Ensure(); err != nil {
		t.Fatal(err)
	}
	_, _, rogueCA, err := rogue.TLSCertificate()
	if err != nil {
		t.Fatal(err)
	}
	forged := bundle
	forged.CAPEM = string(rogueCA)
	pinContext, pinCancel := context.WithTimeout(ctx, time.Second)
	_, pinErr := identity.Enroll(pinContext, forged, "attacker")
	pinCancel()
	if pinErr == nil {
		t.Fatal("enrollment accepted the wrong hub CA")
	}
	var credentials identity.Credentials
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		request, requestCancel := context.WithTimeout(ctx, time.Second)
		credentials, err = identity.Enroll(request, bundle, "test agent")
		requestCancel()
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("z", 64)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path == "/api/repository" {
			if r.Header.Get("Forwarded") != "" || r.Header.Get("X-Forwarded-For") != "" {
				http.Error(w, "forwarding header leaked", 500)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"branch": "main"})
			return
		}
		if r.URL.Path == "/api/events" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte("event: repository\ndata: {}\n\n"))
			return
		}
		if r.URL.Path == "/api/file" {
			copied, _ := io.Copy(io.Discard, r.Body)
			json.NewEncoder(w).Encode(map[string]int64{"bytes": copied})
			return
		}
		http.NotFound(w, r)
	})
	previous := tunnelHostCheckInterval
	tunnelHostCheckInterval = 20 * time.Millisecond
	defer func() { tunnelHostCheckInterval = previous }()
	tunnelCtx, stopTunnel := context.WithCancel(ctx)
	ready := make(chan error, 1)
	go registration.Client{Credentials: credentials}.Maintain(tunnelCtx, registration.Info{ID: "repo", Name: "project", Host: "agent", Branch: "main", Token: token}, handler, ready)
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	var list *httptest.ResponseRecorder
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		list = httptest.NewRecorder()
		app.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/repositories", nil))
		if strings.Contains(list.Body.String(), "project") {
			break
		}
	}
	if list.Code != 200 || strings.Contains(list.Body.String(), token) || !strings.Contains(list.Body.String(), "project") {
		t.Fatalf("list = %d %s", list.Code, list.Body.String())
	}
	proxy := httptest.NewRecorder()
	proxyRequest := httptest.NewRequest(http.MethodGet, "/api/repositories/repo/proxy/api/repository", nil)
	proxyRequest.Header.Set("Forwarded", "for=attacker")
	proxyRequest.Header.Set("X-Forwarded-For", "attacker")
	app.Handler().ServeHTTP(proxy, proxyRequest)
	if proxy.Code != 200 || !strings.Contains(proxy.Body.String(), "main") {
		t.Fatalf("proxy = %d %s", proxy.Code, proxy.Body.String())
	}
	if proxy.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("proxy cache policy = %q", proxy.Header().Get("Cache-Control"))
	}
	streamed := httptest.NewRecorder()
	app.Handler().ServeHTTP(streamed, httptest.NewRequest(http.MethodGet, "/api/repositories/repo/proxy/api/events", nil))
	if streamed.Code != 200 || !strings.Contains(streamed.Body.String(), "event: repository") {
		t.Fatalf("stream = %d %s", streamed.Code, streamed.Body.String())
	}
	large := httptest.NewRecorder()
	largeRequest := httptest.NewRequest(http.MethodPut, "http://hub/api/repositories/repo/proxy/api/file", bytes.NewReader(make([]byte, 128<<10)))
	largeRequest.Header.Set("Origin", "http://hub")
	app.Handler().ServeHTTP(large, largeRequest)
	if large.Code != 200 || !strings.Contains(large.Body.String(), "131072") {
		t.Fatalf("large proxy body = %d %s", large.Code, large.Body.String())
	}
	blocked := httptest.NewRecorder()
	app.Handler().ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/api/repositories/repo/proxy/api/agent/stop", nil))
	if blocked.Code != http.StatusNotFound {
		t.Fatalf("agent route = %d", blocked.Code)
	}
	stopTunnel()
	waitForRepository(t, app, "project", false)
	tunnelCtx, stopTunnel = context.WithCancel(ctx)
	ready = make(chan error, 1)
	go registration.Client{Credentials: credentials}.Maintain(tunnelCtx, registration.Info{ID: "repo", Name: "project", Host: "agent", Branch: "main", Token: token}, handler, ready)
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	waitForRepository(t, app, "project", true)
	if err := store.Revoke(credentials.HostID); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		list := httptest.NewRecorder()
		app.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/repositories", nil))
		if !strings.Contains(list.Body.String(), "project") {
			break
		}
	}
	list = httptest.NewRecorder()
	app.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/repositories", nil))
	if strings.Contains(list.Body.String(), "project") {
		t.Fatal("revoked host remained connected")
	}
	stopTunnel()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hub did not stop")
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	return address
}

func waitForRepository(t *testing.T, app *Server, name string, present bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/repositories", nil))
		if strings.Contains(response.Body.String(), name) == present {
			return
		}
	}
	t.Fatalf("repository %q presence did not become %v", name, present)
}
