package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"git-review/internal/identity"
)

func enrollHost() {
	var encoded string
	if len(os.Args) == 3 {
		encoded = os.Args[2]
	} else if len(os.Args) == 2 {
		fmt.Fprintln(os.Stderr, "Paste the enrollment bundle, then press Enter:")
		var err error
		encoded, err = readEnrollment(os.Stdin)
		if err != nil {
			fatal(err)
		}
	} else {
		fatal(errors.New("usage: git-review enroll [gr1:…]"))
	}
	bundle, err := identity.ParseEnrollment(encoded)
	if err != nil {
		fatal(err)
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "agent"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	credentials, err := identity.Enroll(ctx, bundle, host)
	if err != nil {
		fatal(err)
	}
	path := identity.DefaultCredentialsPath()
	if err := identity.SaveCredentials(path, credentials); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stdout, "Enrolled %s with %s. Credentials saved to %s\n", credentials.HostID, credentials.HubURL, path)
}

func readEnrollment(input io.Reader) (string, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1024), 16<<10)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read enrollment bundle: %w", err)
		}
		return "", errors.New("enrollment bundle is empty")
	}
	return strings.TrimSpace(scanner.Text()), nil
}
