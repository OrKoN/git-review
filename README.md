# git-review

`git-review` is a self-hosted web interface for reviewing changes in Git
worktrees on remote machines. From one browser, you can inspect diffs, edit
files, stage or discard individual changes, commit approved work, and leave
comments for the process that created the changes.

It is designed for a single user working across trusted machines. A central hub
provides the web UI, while a lightweight repository server connects each
worktree to it through an encrypted, outbound-only tunnel.

## Components

- `git-review-hub` hosts the web UI and keeps an in-memory registry of live repositories.
- `git-repo-server` owns one worktree and exposes its token-protected review API.
- `git-review` starts or reattaches to the current worktree's background server,
  opens its hub URL, and streams review comments to stdout for an agent.

Repository servers create encrypted, mutually authenticated outbound tunnels to
the hub. The browser uses only the hub origin; no agent port or repository token
is exposed to it.

See [SECURITY.md](SECURITY.md) for the trust model, deployment requirements,
credential handling, threat boundaries, and incident guidance.

## Build

```sh
./tools/install-go.sh
npm --prefix web ci
bazel build //:build
bazel test //cmd/... //internal/...
bazel build //:frontend_checks
```

The common workflows also have short commands:

```sh
./b build
./b test
./b release
```

`./b test` runs both Go and frontend checks. `./b release` builds the complete
Linux amd64/arm64 suite and the Intel Mac hub in one parallel Bazel invocation,
then publishes the binaries with stable names in `bin/`. Unsuffixed names in
`bin/` point to the current build machine's platform when it is released.

`bazel build //:build` builds all three host binaries in parallel and does not
run tests. `bazel test //cmd/... //internal/...` runs Go tests, while `bazel build
//:frontend_checks` runs the independently scheduled frontend lint, type-check,
and unit-test targets. Run `bazel run //cmd/line-count` to inspect source line
counts.

Host binaries are available below `bazel-bin/cmd/`. The complete release matrix
can also be built directly with:

```sh
bazel build //:release
```

The Intel Mac hub is a CGO-free `darwin/amd64` binary. Repository servers and
agent CLIs are released for Linux amd64 and arm64.

## Configure secure hub communication

Start the hub on the Intel Mac. Port 8080 serves the LAN web UI and port 8443
accepts encrypted agent tunnels:

```sh
./git-review-hub-darwin-amd64 --listen :8080
```

On first start the hub creates a private CA, tunnel key, and enrolled-host
registry in `~/.config/git-review-hub/identity.json`. Back up this file and keep
it private; replacing it requires every agent host to enroll again. Override the
location with `--state`. The tunnel listener defaults to `:8443` and can be
changed with `--tunnel-listen`.

Open `http://MAC_ADDRESS:8080/` in the review browser. The UI remains
unauthenticated, so anyone who can reach it can review and mutate connected
repositories. Bind port 8080 only to a trusted LAN. Agent traffic on port 8443
uses TLS with client certificates even though the LAN UI uses HTTP.

Enroll each agent host once. On the hub, generate a command using an address the
agent can reach:

```sh
git-review-hub-darwin-amd64 enroll \
  --hub-url http://MAC_ADDRESS:8080 \
  --name build-agent-1
```

If the externally reachable tunnel is not `MAC_ADDRESS:8443`, also pass
`--tunnel-address HOST:PORT`. The command prints a one-time enrollment bundle.
On the agent, start enrollment and paste the bundle within ten minutes:

```sh
git-review enroll
# Paste gr1:... when prompted.
```

The agent verifies the hub's pinned private CA, generates its private key
locally, and stores its host certificate in
`~/.config/git-review/credentials.json` with mode `0600`. Enrollment never
copies an agent private key over the network. Multiple hubs can be enrolled in
the same file; the most recently enrolled hub becomes the default.

List or revoke enrolled hosts on the hub:

```sh
git-review-hub-darwin-amd64 hosts
git-review-hub-darwin-amd64 revoke HOST_ID
```

Revocation terminates that host's active repository tunnels and prevents it
from reconnecting. Generate a new enrollment command to restore access.

## Start a review from an agent host

After enrollment, place `git-review` and `git-repo-server` beside one another
and run inside the worktree:

```sh
git-review --message $'Improve diff review\n\nExplain the intent and tradeoffs.'
```

The enrolled hub is used automatically. `--hub` or `GIT_REVIEW_HUB_URL` can
select another already-enrolled hub.

The CLI starts a detached repository server if necessary, attaches its comment
stream, and then prints/copies the plain hub URL. Choose the repository in the
hub selector; no repository ID or access token is placed in the URL. Ctrl-C detaches the
CLI but leaves the repository server running. Re-running the command reattaches.
Use `--idle-timeout` for automatic cleanup, or stop explicitly:

```sh
git-review stop
```

Runtime state and daemon logs are stored with mode `0600` below
`$XDG_RUNTIME_DIR/git-review`, falling back to a per-user directory under the
system temporary directory.

Repository servers listen only on loopback and make outbound connections to the
hub tunnel port. Agent firewalls and NAT therefore need no inbound rule. The hub
URL must be an absolute `http://` or `https://` origin without a path, query, or
fragment. To protect browser-to-hub traffic as well, place the UI behind trusted
HTTPS; the built-in tunnel encryption protects only hub-to-agent communication.

## License

`git-review` is available under the [MIT License](LICENSE).
