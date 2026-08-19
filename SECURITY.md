# Security

## Security model

`git-review` is a single-user review tool for trusted networks. It separates
two trust boundaries:

- Repository servers authenticate to the hub through outbound-only mutual-TLS
  tunnels. Agent hosts have distinct, revocable credentials.
- The browser UI is intentionally anonymous. Anyone who can reach the UI can
  inspect repositories and perform every browser review operation, including
  editing, staging, discarding, and committing changes.

The built-in TLS tunnel protects hub-to-agent traffic. It does not encrypt the
browser's connection to an HTTP hub. Use a trusted HTTPS reverse proxy and bind
the plain HTTP listener to loopback when browser traffic crosses an untrusted
network.

## Protected assets

The following data is security-sensitive:

- The hub identity file contains the private CA key, tunnel server key, enrolled
  host registry, and unexpired enrollment-code hashes. Its default location is
  `~/.config/git-review-hub/identity.json` with mode `0600` in a mode-`0700`
  directory.
- Agent credentials contain a host private key and certificate. They default to
  `~/.config/git-review/credentials.json` with the same permissions.
- Repository daemon state contains the loopback API bearer token. Runtime state
  and logs use mode `0600` below `$XDG_RUNTIME_DIR/git-review`, or the documented
  per-user temporary fallback.
- Repository contents, diffs, comments, proposed commit messages, and Git output
  may be confidential even though the UI itself is anonymous.

Back up the hub identity file securely. Losing it requires re-enrolling every
agent. Disclosure of it allows an attacker to issue trusted host certificates;
rotate the hub identity and re-enroll all hosts after suspected compromise.

## Enrollment and certificates

- Enrollment uses a random 256-bit one-time secret, expires after ten minutes,
  and is consumed atomically. Only a hash is stored by the hub.
- The enrollment bundle pins the hub's private CA. The agent validates the TLS
  server against that CA before sending its enrollment secret.
- The agent generates its private key locally and sends only a signed
  certificate request. Private keys never cross the network.
- Tunnel connections require TLS 1.3 and a client certificate signed by the hub
  CA. The certificate identity must also exist in the live host registry and
  must not be revoked.
- Revocation closes active tunnels during the next liveness check and rejects
  subsequent connections. Repository sessions disappear with the tunnel.
- Tunnel server certificates renew under the existing CA before expiry. A CA
  replacement is a trust reset and requires host re-enrollment.

Enrollment commands are bearer capabilities until they expire. Transfer them
only to the intended agent, do not place them in shell history, tickets, chat
logs, process arguments visible to other users, or source control, and revoke
unexpected enrolled hosts immediately.

## Tunnel and proxy boundary

- Repository servers bind their local HTTP API to IPv4 loopback. They require
  no inbound firewall rule and are not directly reachable by browsers.
- Registration metadata and the per-repository API token travel only inside the
  authenticated tunnel. The hub retains tokens in memory and injects them into
  proxied requests; tokens are not returned to browser JavaScript, URLs,
  cookies, local storage, logs, or API errors.
- The hub proxies an explicit allowlist of browser repository endpoints.
  Agent-event and daemon-stop routes are never proxyable.
- The proxy removes forwarding and hop-by-hop headers, applies existing request
  body limits, marks repository responses `no-store`, preserves cancellation
  and streaming, and returns a generic tunnel error instead of internal
  credentials.
- Browser mutation requests with a cross-site Fetch Metadata value or foreign
  `Origin` are rejected. Responses do not enable cross-origin reads. These
  protections reduce drive-by requests but do not authenticate a reviewer.

The hub is a privileged relay. A compromised hub process can read or modify any
connected repository through its active session. A compromised enrolled agent
credential can authenticate that host, but repository access still requires a
repository process and its ephemeral registration token. A compromised agent
host should be revoked and its credential file removed before re-enrollment.

## Repository safety controls

Repository paths, contents, Git output, comments, and filenames are untrusted
input. The implementation must preserve these controls:

- Invoke Git directly with explicit argument arrays, controlled environment,
  bounded output, and literal `--` path separation. Never construct shell
  commands from repository data.
- Normalize and revalidate paths at the filesystem boundary. Reject traversal,
  symlinks, special files, ignored files, binary data, and oversized content.
- Require current fingerprints for mutations and serialize Git writes to reject
  stale browser state.
- Escape repository and comment content in the browser. Keep the Content
  Security Policy restrictive and do not introduce runtime CDN dependencies.
- Treat discard, edit, stage, and commit access as equivalent to local write
  access to the reviewed worktree.

## Deployment guidance

- Restrict the UI listener to a trusted LAN or an authenticated HTTPS reverse
  proxy. The application does not provide hub login or reviewer authorization.
- Expose the tunnel port only where enrolled agent hosts can reach it. Mutual
  TLS authenticates clients, but ordinary network filtering still reduces
  denial-of-service exposure.
- Do not expose the hub identity file, agent credentials, runtime directory, or
  daemon logs through backups with weaker permissions.
- Run hub and agent processes as unprivileged users. Do not share their config
  directories across operating-system accounts.
- Review `git-review-hub hosts` regularly and revoke retired, lost, or
  unexpected agents.
- Keep clocks reasonably synchronized because certificate validity and
  enrollment expiry depend on wall-clock time.

## Out of scope

The built-in model does not provide reviewer identity, per-user authorization,
approval policy, audit-log persistence, browser-facing TLS termination, secret
storage integration, protection from a compromised hub or agent account, or
availability against denial-of-service attacks. It does not make an anonymous
hub safe to publish on the public Internet.

## Reporting vulnerabilities

Do not disclose an unpatched vulnerability in a public issue. Report it
privately to the repository owner with affected versions, impact, reproduction
steps, and any suggested mitigation. Avoid including real repository contents,
enrollment bundles, certificates, private keys, bearer tokens, or daemon state.
