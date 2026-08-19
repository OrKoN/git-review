package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"git-review/internal/hub"
	"git-review/internal/identity"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "enroll":
			enrollmentCommand()
			return
		case "hosts":
			hostsCommand()
			return
		case "revoke":
			revokeCommand()
			return
		}
	}
	listen := flag.String("listen", ":8080", "HTTP UI listen address")
	tunnelListen := flag.String("tunnel-listen", ":8443", "mutually authenticated tunnel listen address")
	statePath := flag.String("state", identity.DefaultStatePath(), "persistent hub identity state")
	flag.Parse()
	app, err := hub.NewWithConfig(*listen, *tunnelListen, *statePath)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("git-review-hub UI listening on %s; secure tunnels on %s", *listen, *tunnelListen)
	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func enrollmentCommand() {
	flags := flag.NewFlagSet("enroll", flag.ExitOnError)
	hubURL := flags.String("hub-url", "", "browser-reachable hub URL")
	tunnelAddress := flags.String("tunnel-address", "", "agent-reachable tunnel host:port")
	name := flags.String("name", "agent host", "enrollment label")
	statePath := flags.String("state", identity.DefaultStatePath(), "persistent hub identity state")
	_ = flags.Parse(os.Args[2:])
	parsed, err := url.Parse(*hubURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		log.Fatal("--hub-url must be an absolute HTTP or HTTPS URL")
	}
	address := *tunnelAddress
	if address == "" {
		address = net.JoinHostPort(parsed.Hostname(), "8443")
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		log.Fatal("--tunnel-address must be HOST:PORT")
	}
	bundle, err := (&identity.Store{Path: *statePath}).CreateEnrollment(*name, strings.TrimRight(*hubURL, "/"), "https://"+address, time.Now())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Run `git-review enroll` on the agent host, then paste this one-time bundle (expires in 10 minutes):\n\n%s\n", bundle)
}

func hostsCommand() {
	flags := flag.NewFlagSet("hosts", flag.ExitOnError)
	statePath := flags.String("state", identity.DefaultStatePath(), "persistent hub identity state")
	_ = flags.Parse(os.Args[2:])
	hosts, err := (&identity.Store{Path: *statePath}).Hosts()
	if err != nil {
		log.Fatal(err)
	}
	ids := make([]string, 0, len(hosts))
	for id := range hosts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Printf("%s\t%s\n", id, hosts[id])
	}
}
func revokeCommand() {
	flags := flag.NewFlagSet("revoke", flag.ExitOnError)
	statePath := flags.String("state", identity.DefaultStatePath(), "persistent hub identity state")
	_ = flags.Parse(os.Args[2:])
	if flags.NArg() != 1 {
		log.Fatal("usage: git-review-hub revoke HOST_ID")
	}
	if err := (&identity.Store{Path: *statePath}).Revoke(flags.Arg(0)); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Revoked", flags.Arg(0))
}
