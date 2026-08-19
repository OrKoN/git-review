package hub

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"time"

	"git-review/internal/httpapi"
	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

var validRepositoryID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var tunnelHostCheckInterval = 10 * time.Second

type tunnelRegistration struct {
	ID, Name, Host, Branch, Token string
}

type repositorySession struct {
	mux    *yamux.Session
	token  string
	hostID string
}

func (s *Server) runTunnel() error {
	_, _, caPEM, err := s.store.TLSCertificate()
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return errors.New("invalid tunnel CA")
	}
	s.tunnel.TLSConfig = &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		if err := s.store.Ensure(); err != nil {
			return nil, err
		}
		certPEM, keyPEM, _, err := s.store.TLSCertificate()
		if err != nil {
			return nil, err
		}
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		return &cert, err
	}, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: pool, MinVersion: tls.VersionTLS13}
	return s.tunnel.ListenAndServeTLS("", "")
}

func (s *Server) tunnelRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/enroll", s.enroll)
	mux.HandleFunc("GET /v1/tunnel", s.acceptTunnel)
	return http.MaxBytesHandler(mux, 64<<10)
}

func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	var input struct{ Code, CSR string }
	if !httpapi.DecodeJSON(w, r, &input, "Invalid enrollment request") {
		return
	}
	csr, err := base64.RawStdEncoding.DecodeString(input.CSR)
	if err != nil {
		writeError(w, 400, "invalid_csr", "Invalid certificate request")
		return
	}
	hostID, cert, err := s.store.SignEnrollment(input.Code, csr, time.Now())
	if err != nil {
		writeError(w, 403, "enrollment_rejected", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"HostID": hostID, "Cert": string(cert)})
}

func (s *Server) acceptTunnel(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		writeError(w, 401, "client_certificate_required", "Client certificate required")
		return
	}
	hostID := r.TLS.PeerCertificates[0].Subject.CommonName
	if !s.store.HostAllowed(hostID) {
		writeError(w, 403, "host_revoked", "Host is not enrolled")
		return
	}
	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	_, data, err := connection.Read(r.Context())
	if err != nil {
		return
	}
	var registration tunnelRegistration
	if json.Unmarshal(data, &registration) != nil || !validRepositoryID.MatchString(registration.ID) || registration.Name == "" || len(registration.Token) < 32 {
		connection.Close(websocket.StatusPolicyViolation, "invalid registration")
		return
	}
	s.mu.RLock()
	owner := s.sessions[registration.ID]
	s.mu.RUnlock()
	if owner != nil && owner.hostID != hostID {
		connection.Close(websocket.StatusPolicyViolation, "repository ID belongs to another host")
		return
	}
	if err := connection.Write(r.Context(), websocket.MessageText, []byte("ready")); err != nil {
		return
	}
	stream := websocket.NetConn(r.Context(), connection, websocket.MessageBinary)
	config := yamux.DefaultConfig()
	config.LogOutput = io.Discard
	session, err := yamux.Server(stream, config)
	if err != nil {
		return
	}
	repo := Repository{ID: registration.ID, Name: registration.Name, Host: registration.Host, Branch: registration.Branch, Token: registration.Token, lastSeen: time.Now()}
	s.mu.Lock()
	old := s.sessions[registration.ID]
	if old != nil && old.hostID != hostID {
		s.mu.Unlock()
		_ = session.Close()
		return
	}
	_, existed := s.repos[registration.ID]
	s.sessions[registration.ID] = &repositorySession{mux: session, token: registration.Token, hostID: hostID}
	s.repos[registration.ID] = repo
	s.mu.Unlock()
	if old != nil {
		_ = old.mux.Close()
	}
	if existed {
		s.publish("updated")
	} else {
		s.publish("connected")
	}
	ticker := time.NewTicker(tunnelHostCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			s.removeSession(registration.ID, session)
			return
		case <-session.CloseChan():
			s.removeSession(registration.ID, session)
			return
		case <-ticker.C:
			s.mu.Lock()
			if current := s.sessions[registration.ID]; current != nil && current.mux == session {
				item := s.repos[registration.ID]
				item.lastSeen = time.Now()
				s.repos[registration.ID] = item
			}
			s.mu.Unlock()
			if !s.store.HostAllowed(hostID) {
				_ = session.Close()
			}
		}
	}
}

func (s *Server) removeSession(id string, session *yamux.Session) {
	s.mu.Lock()
	current := s.sessions[id]
	if current != nil && current.mux == session {
		delete(s.sessions, id)
		delete(s.repos, id)
	}
	s.mu.Unlock()
	if current != nil && current.mux == session {
		s.publish("disconnected")
	}
}

func (s *Server) proxy(w http.ResponseWriter, r *http.Request) {
	if !allowedProxyPath(r.PathValue("path")) {
		writeError(w, 404, "not_found", "Repository endpoint is not available")
		return
	}
	if r.Method != http.MethodGet && crossOrigin(r) {
		writeError(w, 403, "invalid_origin", "Mutation requests must originate from this hub")
		return
	}
	s.mu.RLock()
	session := s.sessions[r.PathValue("id")]
	s.mu.RUnlock()
	if session == nil {
		writeError(w, 404, "not_found", "Repository is not connected")
		return
	}
	target := &url.URL{Scheme: "http", Host: "repository"}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) { return session.mux.OpenStream() }}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		writeError(w, 502, "tunnel_error", "Repository tunnel unavailable")
	}
	proxy.ModifyResponse = func(response *http.Response) error { response.Header.Set("Cache-Control", "no-store"); return nil }
	proxy.Director = nil
	proxy.Rewrite = func(request *httputil.ProxyRequest) {
		request.SetURL(target)
		request.Out.URL.Path = "/" + r.PathValue("path")
		request.Out.URL.RawPath = ""
		request.Out.Host = "repository"
		stripForwarding(request.Out.Header)
		request.Out.Header.Set("Authorization", "Bearer "+session.token)
	}
	proxy.FlushInterval = -1
	proxy.ServeHTTP(w, r)
}

func allowedProxyPath(path string) bool {
	if !strings.HasPrefix(path, "api/") {
		return false
	}
	path = "/" + path
	if strings.HasPrefix(path, "/api/agent/") {
		return false
	}
	for _, prefix := range []string{"/api/repository", "/api/files", "/api/diff", "/api/file", "/api/change", "/api/commit", "/api/comments", "/api/events"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
func crossOrigin(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	return err != nil || !strings.EqualFold(parsed.Host, r.Host)
}
func stripForwarding(header http.Header) {
	for _, name := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Connection", "Proxy-Connection", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(name)
	}
}

type yamuxListener struct {
	session *yamux.Session
	ctx     context.Context
}

func (l yamuxListener) Accept() (net.Conn, error) { return l.session.AcceptStream() }
func (l yamuxListener) Close() error              { return l.session.Close() }
func (l yamuxListener) Addr() net.Addr            { return tunnelAddr("yamux") }

type tunnelAddr string

func (a tunnelAddr) Network() string { return string(a) }
func (a tunnelAddr) String() string  { return string(a) }
