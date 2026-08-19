package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"git-review/internal/repository"
)

type Server struct {
	repo          *repository.Repository
	listener      net.Listener
	http          *http.Server
	token         string
	idle          time.Duration
	out           io.Writer
	mu            sync.RWMutex
	comments      map[string]Comment
	subs          map[chan string]struct{}
	agentSubs     map[chan string]struct{}
	activeClients int
	lastClient    time.Time
	lastState     string
	reviewMessage string
}

type Comment struct {
	ID       string    `json:"id"`
	Path     string    `json:"path"`
	Area     string    `json:"area,omitempty"`
	Side     string    `json:"side,omitempty"`
	Line     int       `json:"line,omitempty"`
	Context  string    `json:"context,omitempty"`
	Body     string    `json:"body"`
	Resolved bool      `json:"resolved"`
	Outdated bool      `json:"outdated"`
	Created  time.Time `json:"created"`
}

func randomHex(bytes int) (string, error) {
	data := make([]byte, bytes)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func New(repo *repository.Repository, port int, idle time.Duration, out io.Writer) (*Server, error) {
	return NewWithConfig(repo, port, idle, out, "")
}

func NewWithConfig(repo *repository.Repository, port int, idle time.Duration, out io.Writer, token string) (*Server, error) {
	if port < 0 || port > 65535 {
		return nil, errors.New("port must be between 0 and 65535")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	if token == "" {
		token, err = randomHex(32)
		if err != nil {
			listener.Close()
			return nil, err
		}
	}
	s := &Server{repo: repo, listener: listener, token: token, idle: idle, out: out, comments: map[string]Comment{}, subs: map[chan string]struct{}{}, agentSubs: map[chan string]struct{}{}, lastClient: time.Now()}
	s.http = &http.Server{Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	return s, nil
}

func (s *Server) Port() int                          { return s.listener.Addr().(*net.TCPAddr).Port }
func (s *Server) Handler() http.Handler              { return s.http.Handler }
func (s *Server) Token() string                      { return s.token }
func (s *Server) SetReviewMessage(message string)    { s.reviewMessage = strings.TrimSpace(message) }
func (s *Server) ReviewMessage() string              { return s.reviewMessage }
func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

func LocalNetworkHost() string {
	addresses, err := net.InterfaceAddrs()
	if err == nil {
		var candidates []net.IP
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ipv4 := ip.To4(); ipv4 != nil && !ipv4.IsLoopback() {
				candidates = append(candidates, ipv4)
			}
		}
		if selected := chooseLocalIP(candidates); selected != nil {
			return selected.String()
		}
	}
	hostname, hostnameErr := os.Hostname()
	if hostnameErr == nil && hostname != "" {
		return hostname
	}
	return "<server-ip>"
}

func chooseLocalIP(addresses []net.IP) net.IP {
	for _, rank := range []int{3, 2, 1} {
		for _, address := range addresses {
			if privateIPRank(address) == rank {
				return address
			}
		}
	}
	if len(addresses) > 0 {
		return addresses[0]
	}
	return nil
}

func privateIPRank(address net.IP) int {
	ip := address.To4()
	if ip == nil {
		return 0
	}
	switch {
	case ip[0] == 192 && ip[1] == 168:
		return 3
	case ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31:
		return 2
	case ip[0] == 10:
		return 1
	default:
		return 0
	}
}

func (s *Server) Run(ctx context.Context) error {
	defer s.printSummary()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
	}()
	if s.idle > 0 {
		go s.watchIdle(ctx)
	}
	go s.watchRepository(ctx)
	err := s.http.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
