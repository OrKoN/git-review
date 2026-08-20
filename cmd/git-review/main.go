package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"git-review/internal/daemon"
	"git-review/internal/identity"
	"git-review/internal/licenses"
	"git-review/internal/registration"
	"git-review/internal/repository"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "licenses" {
		_, _ = io.WriteString(os.Stdout, licenses.ThirdParty)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "enroll" {
		enrollHost()
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "stop" {
		stopDaemon()
		return
	}
	hubURL := flag.String("hub", defaultHubURL(), "git-review hub URL")
	port := flag.Int("port", 0, "repository server port")
	idle := flag.Duration("idle-timeout", 0, "daemon inactivity timeout (0 disables)")
	copyURL := flag.Bool("copy-url", true, "copy hub review URL using OSC 52")
	message := flag.String("message", "", "proposed commit message")
	flag.Parse()
	if *hubURL == "" {
		fatal(errors.New("no hub is enrolled; run git-review enroll first"))
	}
	normalizedHubURL, err := registration.NormalizeHubURL(*hubURL)
	if err != nil {
		fatal(err)
	}
	if _, err := identity.LoadCredentialsForHub(identity.DefaultCredentialsPath(), normalizedHubURL); err != nil {
		fatal(fmt.Errorf("hub is not enrolled: %w", err))
	}
	repo, err := repository.Open(".")
	if err != nil {
		fatal(fmt.Errorf("open repository: %w", err))
	}
	statePath, err := daemon.StatePath(repo.Root())
	if err != nil {
		fatal(err)
	}
	lock, err := daemon.Acquire(statePath)
	if err != nil {
		fatal(fmt.Errorf("lock repository daemon: %w", err))
	}
	state, err := existingState(statePath)
	if errors.Is(err, os.ErrNotExist) {
		state, err = startDaemon(repo.Directory(), statePath, normalizedHubURL, *port, *idle, *message)
		if err != nil {
			_ = lock.Close()
			fatal(err)
		}
	} else if err != nil {
		_ = lock.Close()
		fatal(err)
	} else {
		stateDirectory := state.Directory
		if stateDirectory == "" {
			stateDirectory = repo.Root()
		}
		if stateDirectory != repo.Directory() {
			_ = lock.Close()
			fatal(errors.New("repository daemon is serving a different directory; stop it first"))
		}
		if state.HubURL != normalizedHubURL {
			_ = lock.Close()
			fatal(errors.New("repository daemon is connected to a different hub; stop it first"))
		}
		requested := strings.TrimSpace(*message)
		if requested != "" && requested != state.Message {
			_ = lock.Close()
			fatal(errors.New("repository daemon already has a different proposed message; stop it first"))
		}
	}
	if err := lock.Close(); err != nil {
		fatal(fmt.Errorf("unlock repository daemon: %w", err))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ready := make(chan struct{})
	streamDone := make(chan error, 1)
	go func() { streamDone <- follow(ctx, state, ready) }()
	select {
	case <-ready:
	case err := <-streamDone:
		fatal(fmt.Errorf("attach agent event stream: %w", err))
	}

	reviewURL := state.HubURL + "/"
	if terminal, statErr := os.Stderr.Stat(); *copyURL && statErr == nil && terminal.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintf(os.Stderr, "\033]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(reviewURL)))
	}
	fmt.Fprintln(os.Stderr, reviewURL)
	if err := <-streamDone; err != nil && !errors.Is(err, context.Canceled) {
		fatal(err)
	}
}

func existingState(path string) (daemon.State, error) {
	state, err := daemon.Read(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return daemon.State{}, removeErr
			}
			return daemon.State{}, os.ErrNotExist
		}
		return daemon.State{}, err
	}
	if err := probeState(state); err == nil {
		return state, nil
	}
	if daemon.ProcessAlive(state.PID) {
		return daemon.State{}, fmt.Errorf("repository daemon process %d is running but its API is unavailable", state.PID)
	}
	if err := daemon.Remove(path, state.ID); err != nil {
		return daemon.State{}, fmt.Errorf("remove stale daemon state: %w", err)
	}
	return daemon.State{}, os.ErrNotExist
}

func probeState(state daemon.State) error {
	req, _ := http.NewRequest(http.MethodGet, state.BaseURL+"/api/repository", nil)
	req.Header.Set("Authorization", "Bearer "+state.Token)
	client := http.Client{Timeout: time.Second}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("repository API returned %s", response.Status)
	}
	return nil
}

func startDaemon(root, statePath, hub string, port int, idle time.Duration, message string) (daemon.State, error) {
	executable, err := os.Executable()
	if err != nil {
		return daemon.State{}, err
	}
	serverBinary := os.Getenv("GIT_REVIEW_REPO_SERVER")
	if serverBinary == "" {
		serverBinary = filepath.Join(filepath.Dir(executable), "git-repo-server")
	}
	logFile, err := os.OpenFile(statePath+".log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return daemon.State{}, err
	}
	defer logFile.Close()
	if err := logFile.Chmod(0o600); err != nil {
		return daemon.State{}, fmt.Errorf("secure daemon log: %w", err)
	}
	args := []string{"--repo", root, "--hub", hub, "--state-file", statePath, "--port", fmt.Sprint(port), "--idle-timeout", idle.String(), "--message", message}
	command := exec.Command(serverBinary, args...)
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return daemon.State{}, fmt.Errorf("start git-repo-server: %w", err)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if state, err := daemon.Read(statePath); err == nil {
			if err := probeState(state); err == nil {
				return state, nil
			}
		}
		if process, findErr := os.FindProcess(command.Process.Pid); findErr == nil {
			if signalErr := process.Signal(syscall.Signal(0)); signalErr != nil {
				break
			}
		}
	}
	return daemon.State{}, fmt.Errorf("repository server did not become ready; see %s", statePath+".log")
}

func follow(ctx context.Context, state daemon.State, ready chan<- struct{}) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, state.BaseURL+"/api/agent/events", nil)
	req.Header.Set("Authorization", "Bearer "+state.Token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("agent event stream returned %s", response.Status)
	}
	close(ready)
	scanner := bufio.NewScanner(response.Body)
	var block strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			block.WriteString(strings.TrimPrefix(line, "data: "))
			block.WriteByte('\n')
		} else if line == "" && block.Len() > 0 {
			fmt.Print(block.String())
			block.Reset()
		}
	}
	if errors.Is(scanner.Err(), io.ErrUnexpectedEOF) {
		return nil
	}
	return scanner.Err()
}

func stopDaemon() {
	repo, err := repository.Open(".")
	if err != nil {
		fatal(err)
	}
	path, err := daemon.StatePath(repo.Root())
	if err != nil {
		fatal(err)
	}
	lock, err := daemon.Acquire(path)
	if err != nil {
		fatal(err)
	}
	defer lock.Close()
	state, err := existingState(path)
	if err != nil {
		fatal(errors.New("no running repository daemon"))
	}
	req, _ := http.NewRequest(http.MethodPost, state.BaseURL+"/api/agent/stop", bytes.NewReader(nil))
	req.Header.Set("Authorization", "Bearer "+state.Token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal(err)
	}
	defer response.Body.Close()
	var result struct {
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		fatal(err)
	}
	_, _ = io.WriteString(os.Stdout, result.Summary)
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "git-review:", err); os.Exit(1) }

func defaultHubURL() string {
	if value := os.Getenv("GIT_REVIEW_HUB_URL"); value != "" {
		return value
	}
	credentials, err := identity.LoadCredentials(identity.DefaultCredentialsPath())
	if err == nil {
		return credentials.HubURL
	}
	return ""
}
