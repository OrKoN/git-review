package registration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"git-review/internal/identity"
	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

type Info struct{ ID, Name, Host, Branch, Token string }

func NormalizeHubURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("hub URL must be an absolute HTTP or HTTPS URL without credentials, path, query, or fragment")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

type Client struct{ Credentials identity.Credentials }

func (c Client) Maintain(ctx context.Context, info Info, handler http.Handler, ready chan<- error) {
	first := true
	delay := time.Second
	for {
		err := c.serve(ctx, info, handler, func() {
			delay = time.Second
			if first {
				ready <- nil
				close(ready)
				first = false
			}
		})
		if first {
			ready <- err
			close(ready)
			return
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}

func (c Client) serve(ctx context.Context, info Info, handler http.Handler, connected func()) error {
	tlsConfig, err := c.Credentials.TLSConfig()
	if err != nil {
		return err
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
	tunnelURL := strings.Replace(c.Credentials.Tunnel, "https://", "wss://", 1)
	connection, _, err := websocket.Dial(ctx, tunnelURL+"/v1/tunnel", &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		return fmt.Errorf("connect tunnel: %w", err)
	}
	defer connection.CloseNow()
	payload, _ := json.Marshal(info)
	if err := connection.Write(ctx, websocket.MessageText, payload); err != nil {
		return err
	}
	if _, message, err := connection.Read(ctx); err != nil || string(message) != "ready" {
		if err != nil {
			return err
		}
		return errors.New("hub rejected tunnel registration")
	}
	stream := websocket.NetConn(ctx, connection, websocket.MessageBinary)
	config := yamux.DefaultConfig()
	config.LogOutput = io.Discard
	session, err := yamux.Client(stream, config)
	if err != nil {
		return err
	}
	defer session.Close()
	connected()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	err = server.Serve(yamuxListener{session: session})
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, yamux.ErrSessionShutdown) {
		return nil
	}
	return err
}

type yamuxListener struct{ session *yamux.Session }

func (l yamuxListener) Accept() (net.Conn, error) { return l.session.AcceptStream() }
func (l yamuxListener) Close() error              { return l.session.Close() }
func (l yamuxListener) Addr() net.Addr            { return tunnelAddr("yamux") }

type tunnelAddr string

func (a tunnelAddr) Network() string { return string(a) }
func (a tunnelAddr) String() string  { return string(a) }
