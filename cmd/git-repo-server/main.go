package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"git-review/internal/daemon"
	"git-review/internal/identity"
	"git-review/internal/licenses"
	"git-review/internal/registration"
	"git-review/internal/repository"
	"git-review/internal/server"
)

func randomID(bytes int) string {
	data := make([]byte, bytes)
	if _, err := rand.Read(data); err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(data)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "licenses" {
		_, _ = io.WriteString(os.Stdout, licenses.ThirdParty)
		return
	}
	root := flag.String("repo", ".", "repository worktree")
	hubURL := flag.String("hub", "", "git-review hub URL")
	stateFile := flag.String("state-file", "", "daemon state file")
	port := flag.Int("port", 0, "listen port")
	idle := flag.Duration("idle-timeout", 0, "stop after inactivity")
	message := flag.String("message", "", "proposed commit message")
	credentialsPath := flag.String("credentials", identity.DefaultCredentialsPath(), "enrolled hub credentials")
	flag.Parse()
	if *hubURL == "" || *stateFile == "" {
		log.Fatal("hub URL and state file are required")
	}
	normalizedHubURL, err := registration.NormalizeHubURL(*hubURL)
	if err != nil {
		log.Fatal(err)
	}
	credentials, err := identity.LoadCredentialsForHub(*credentialsPath, normalizedHubURL)
	if err != nil {
		log.Fatalf("load enrolled hub credentials: %v", err)
	}
	if credentials.HubURL != normalizedHubURL {
		log.Fatal("credentials belong to a different hub")
	}
	repo, err := repository.Open(*root)
	if err != nil {
		log.Fatalf("open repository: %v", err)
	}
	token, id := randomID(32), randomID(16)
	app, err := server.NewWithConfig(repo, *port, *idle, io.Discard, token)
	if err != nil {
		log.Fatalf("initialize repository server: %v", err)
	}
	app.SetReviewMessage(*message)
	host, _ := os.Hostname()
	state, _ := repo.State(context.Background())
	info := registration.Info{ID: id, Name: filepath.Base(repo.Root()), Host: host, Branch: state.Branch, Token: token}
	registrar := registration.Client{Credentials: credentials}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	ready := make(chan error, 1)
	go registrar.Maintain(ctx, info, app.Handler(), ready)
	startup := time.NewTimer(10 * time.Second)
	defer startup.Stop()
	select {
	case err := <-ready:
		if err != nil {
			log.Fatalf("connect to hub: %v", err)
		}
	case <-startup.C:
		log.Fatal("connect to hub: timed out")
	}
	daemonState := daemon.State{PID: os.Getpid(), ID: id, BaseURL: "http://127.0.0.1:" + strconv.Itoa(app.Port()), Token: token, HubURL: normalizedHubURL, Message: strings.TrimSpace(*message), Directory: repo.Directory()}
	if err := daemon.Write(*stateFile, daemonState); err != nil {
		log.Fatalf("write daemon state: %v", err)
	}
	defer func() { _ = daemon.Remove(*stateFile, id) }()
	if err := app.Run(ctx); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
