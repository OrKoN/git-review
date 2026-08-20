# Installation and setup

All three tools are standalone Go binaries published on
[GitHub Releases](https://github.com/OrKoN/git-review/releases); no Go, Node.js,
or Bazel installation is required to run them. The included installer selects
the right release for the current platform and places it in `~/.local/bin`.
Platform suffixes are removed during installation: commands are always named
`git-review`, `git-review-hub`, and `git-repo-server`.

## Hub host

The hub host needs only `git-review-hub`. Download the installer from GitHub and
run it with the `hub` role:

```sh
curl -fsSL https://raw.githubusercontent.com/OrKoN/git-review/main/install.sh | sh -s -- hub
```

The hub installer supports Intel macOS and Linux on amd64 or arm64.

## Remote VM

Each Linux VM needs both `git-review` and `git-repo-server`. Download the
installer from GitHub on the VM and install them together with:

```sh
curl -fsSL https://raw.githubusercontent.com/OrKoN/git-review/main/install.sh | sh -s -- vm
```

The VM installer supports Linux on amd64 or arm64. If `~/.local/bin` is not on
`PATH`, add it before running the tools. Set `GIT_REVIEW_INSTALL_DIR` to install
into another local bin directory, or `GIT_REVIEW_VERSION` to install a specific
release tag instead of the latest release.

## Configure secure hub communication

Start the hub. Port 8080 serves the LAN web UI and port 8443 accepts encrypted
agent tunnels:

```sh
git-review-hub --listen :8080
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
git-review-hub enroll \
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
git-review-hub hosts
git-review-hub revoke HOST_ID
```

Revocation terminates that host's active repository tunnels and prevents it
from reconnecting. Generate a new enrollment command to restore access.

## Start a review from an agent host

Run this inside the worktree after enrollment:

```sh
git-review --message $'Improve diff review\n\nExplain the intent and tradeoffs.'
```

The enrolled hub is used automatically. `--hub` or `GIT_REVIEW_HUB_URL` can
select another already-enrolled hub.

The CLI starts a detached repository server if necessary, attaches its comment
stream, and then prints and copies the plain hub URL. Choose the repository in
the hub selector; no repository ID or access token is placed in the URL. Ctrl-C
detaches the CLI but leaves the repository server running. Re-running the
command reattaches. Use `--idle-timeout` for automatic cleanup, or stop it
explicitly:

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
