# Security review

Read-only audit of the whole repository, including the immutable `upstream/tailmix`
snapshot it was adapted from.

Summary: nothing critical, two medium gaps (unbounded state growth with a hard failure
cliff; a half-configured host after a failed activation), the rest low or nits. The only
dependency work available is bumping `golang.org/x/net` (details at the end).

Scope of this pass: privileged engine (`engine/`, root daemon, `hosttun_darwin`,
`packetmap`, `tunmux`, DNS, control socket, service/state management, activation and
helper installation), the native app (`macos/`, control client and credential checks),
and the inherited upstream surface that becomes reachable once the engine runs as root.

Method: manual review of every new file and of the inherited packet, preference, IPC,
launchd and update paths; `govulncheck` (no vulnerabilities in called code);
`staticcheck`, `go vet`, `gofmt`, full `go test ./...` (all passing); targeted probe tests
against the packet mapper. Nothing was executed against a live tailnet, so all statements
about runtime behaviour are design-level, not observed.

Nothing critical was found. The design decisions that matter are already the right ones:
no root TCP listener or bearer token, kernel peer-UID checks on the socket, `Host`/`Origin`
rejection, explicit preference pinning (`ShieldsUp`, `RouteAll`, `CorpDNS`), fail-closed
routing, immutable plans, and owner-only state directories. The findings below are the
remaining gaps, ordered by how much they could cost.

## Medium

### 1. Plans, leases and DNS claims grow without bound and never expire

`engine/internal/routing/plan.go:139,162,224,238`

Leases are keyed by `(ProfileID, NodeID, CanonicalIP)` and every lease ever created stays in
state forever: `Build` re-emits a host route for each one, `domains` are re-seeded from the
previous state (`domainSet` at line 121-123) so every hostname ever observed stays a
permanent DNS claim, and nothing prunes either set when a peer's IP changes, a peer leaves,
or an account is removed.

Consequences:

- A peer whose canonical address changes gets a second lease. Removed accounts keep their
  leases and their routes on the shared utun for the life of the state file.
- Each claimed domain is one file in `/private/etc/resolver` that macOS loads into every
  resolver instance. That directory only ever grows.
- `routetable.Get` parses the full RIB on every reconciliation (3s poll), so cost tracks the
  leak.
- Line 238 is a hard cliff: once `len(s.Leases) > 100000` the build fails, so the plan stops
  publishing and every account is blocked fail-closed. Nothing in the UI recovers from that;
  only deleting the state file does.
- Deleted tailnet devices stay `NXDOMAIN` system-wide even after the profile that owned them
  is removed, so a stale name shadows real public DNS indefinitely.

Fix: age out leases and domain claims that no longer appear in any enabled profile's netmap
(drop a lease after N consecutive builds without a sighting, drop domains after a grace
window or immediately for removed profiles), and enforce a documented recovery path (a
prune at startup, or surfacing "state is at capacity" with a reset action) instead of a
silent permanent fail-closed.

### 2. A failed activation leaves the host in a half-configured state

`engine/internal/host/host_darwin.go:121-158`, `engine/internal/daemon/manager.go:222-239`

`Open` installs both synthetic-pool reject guards *before* `hosttun.Open`, and the caller only
registers the `h.Close` cleanup once `Open` returns successfully. On failure the guards stay
installed (deliberately, and logged as "reject guards remain"), so all traffic to
`198.18.0.0/15` and `fd74:…::/64` is blackholed — including `198.18.x` addresses used by other
tools — until some future activation succeeds or the host is rebooted.

Worse, the failure mode is silent for DNS. `Apply` writes resolver files before installing
routes and rolls the files back on error; `failClosed` then publishes a `NXDOMAIN` plan, but
the resolvers are already gone, so tailnet names quietly fall through to public DNS instead
of failing closed. The app shows "networking is off" with no hint that packets are being
rejected.

Fix: state the retained guards in the failure path shown to the user, and either keep the
resolver files on failure (publishing `NXDOMAIN`) or document that DNS falls open while
routing fails closed.

## Low

### 3. `preserveUDPChecksum` is only safe on an already-validated packet

`engine/packetmap/validate.go:61-77`

It reads `p[0]`, `p[9]`/`p[6]` and `p[off+6:off+8]` with no length checks; a 20-byte
IPv4/UDP packet panics with `index out of range [26] with length 20`. Both call sites happen
to call `validatePacket` first and to register the `defer` after it, so nothing reaches it
with a truncated packet today. A future caller (or a reordering) turns it into a daemon
crash, i.e. a full routing outage. Fix: bound-check and fall back to a no-op.

### 4. Any root process can drive and impersonate the engine

`engine/internal/control/server.go:38`, `macos/Sources/TailmultiKit/ControlClient.swift:107-134`

Authorization accepts uid `0` as well as the owner, and the client only proves the answering
peer's *UID*. Both are defensible (a root process can do anything anyway), but it means:

- The owner's `sudo` shell can start/stop networking and delete accounts; there is no second
  factor for destructive actions.
- Nothing verifies the *code signature* of the process at the other end, so any root process
  can present itself as the service and feed the app a forged status, a forged setup report
  (`ready: true`), or a login URL. The URL is validated to `https://login.tailscale.com`
  on both sides (server.go:105-113, ControlClient.swift:63-68), which limits the damage.

Fix if the threat model includes a compromised root-adjacent process: additionally compare
`LOCAL_PEERPID`'s code-signing team against the app's own team in both directions.

### 5. Service status polling is serialized behind long operations

`engine/internal/service/manager.go:84-100,140`, `engine/internal/control/server.go:183`

`StartNetwork`/`StopNetwork` hold the manager mutex across a host preflight (several
`/sbin/route` execs), `host.Open`, and every route/DNS mutation. The app polls `/v1/status`
every 3s with a 15s socket timeout, so a status read can block for the duration of an
activation and surface as a spurious connection error. `WriteTimeout` is 30s, which is also
the ceiling for the activation request itself.

Fix: keep the mutex for state, run the privileged work outside it (generation-counter guarded
as `watch` already does), or have the start/stop endpoints return immediately and report
progress through `health`.

### 6. Batch write uses the last packet's headroom offset for the whole batch

`engine/tunmux/mux.go:344-356`

`offset` is overwritten per packet and only the final value is passed to `host.Write`.
Correct today because every batch is one family of packet and all of them come from the same
`packetPool` with fixed headroom; it breaks silently (frame shift on the utun) the moment a
packet source with different headroom is added. Fix: assert equal offsets or write per packet.

### 7. State validates IDs but not account names or claim counts

`engine/internal/routing/plan.go:74-95`

`Validate` bounds profile IDs to 32 hex characters and rejects duplicate leases and duplicate
domains, but account names are only compared for equality. A hand-edited `state.json` (root-only,
so this is not remote) can carry absurd names or a huge domain list, each of which becomes a
resolver file. Cheap to bound.

### 8. Removed identities are never deleted from disk

`engine/internal/daemon/manager.go:158-170`

`stop` closes the engine and the account's channel but keeps
`<state-dir>/profiles/<id>` with its node key and state. Re-adding an account creates a new
identity, so old directories accumulate with no way to remove them. Contents are root-only
`0700`, so this is hygiene rather than exposure. Note also that the daemon binary itself is
launched from a path in the user's own directory (`build/Tailmulti.app`), so a user-level
compromise escalates to root on the next service start; the app's hash and signature checks
happen at registration time only.

## Nits

- `internal/control/server_test.go:106`: dead assignment, `staticcheck` SA4006.
- No `EDNS0` handling in the resolver: a client that sends an OPT record gets a reply without
  one. Legal, but some stub resolvers are happier with the option echoed.
- A profile's own node name is never in `Records` (`plan.go` builds records from `st.Peers`
  only), so a host cannot resolve its own MagicDNS name through Tailmulti.
- Two self addresses of the same family overwrite each other in `table.Sources`
  (`plan.go:157-163`); only one v4 and one v6 self address can route outbound.
- `owners` keys a peer without a node ID as `profile/` (`plan.go:143`), so several such peers
  in one profile collapse into one owner and are treated as unambiguous while producing no
  records at all.

## Verified sound (checked, no finding)

- Packet boundary: fixed-header and length validation before any parse, IPv4 options and
  IPv6 extension headers refused, fragments refused, outbound source pinned to the host
  address and inbound destination pinned to the profile's own self address, unsupported
  traffic dropped rather than passed. Unmapped inbound from another profile's peer is
  dropped, so no cross-identity forwarding.
- Checksum: incremental updates for TCP/UDP/ICMP v4 and v6, absent IPv4 UDP checksum
  preserved as a sentinel, computed zero encoded as all ones.
- Publication order: packets before host routes before DNS; removals reverse; immutable plans
  swapped atomically; a build, save or apply failure drops to an empty mapper rather than
  keeping a stale mapping.
- Socket: `O_NOFOLLOW` on lock and state files, `0600` state, temp-file-plus-rename writes,
  parent directory ownership and permission checks, socket `0666` under a root `0755`
  directory, symlink and non-socket collision refused, missing peer credentials treated as
  denial, request bodies capped at 4 KiB with unknown fields rejected and exactly one JSON
  value required.
- DNS: loopback-only bind, authoritative-only (no recursion, no open resolver), answers
  limited to the synthetic pool, ambiguous names refused instead of load-balanced, unknown
  names under a claimed domain `NXDOMAIN` (no public fallback), 512-byte truncation answered
  over TCP with a 32-connection cap and 2s deadlines.
- Preference pinning: `ShieldsUp`, `RouteAll=false`, `CorpDNS=false`, control URL pinned to
  `controlplane.tailscale.com`, `DisableAuthKeyEnv`, auth keys cleared after consumption,
  remote log upload off by default, inherited `--state`/`--socket`/`--port` flags rejected.
- Credential hygiene: no key material on the control wire, no auth URL in general status, no
  peer identifiers in diagnostics, login URL restricted to `https://login.tailscale.com`
  without userinfo or port.
- Command construction: every privileged exec uses a fixed binary and argument vector
  (`/sbin/route`, `/usr/sbin/netstat`) — no shell, no interpolated user input. Interfaces are
  matched by name, prefixes are parsed into `netip.Prefix` and re-stringified.
- Resolver file ownership: a marker comment, root ownership and non-group/world-writable
  permissions are required before a file is treated as ours; foreign files are never deleted
  and a file we cannot read blocks rather than being overwritten.
- App side: `SetupManifest` version/label/registration/owner checks, helper SHA-256 pinned,
  signature required for registration, ad-hoc builds refused, fixed `PATH` and argument list
  for the host checker with a timeout, `SIGTERM` then `SIGKILL`, bounded output, socket peer
  UID verified before the first byte is sent, `Content-Length` matched against the body,
  response size capped.

## Dependency advisories

`govulncheck ./...` reports no vulnerability in called code and three in required modules,
none of them called:

| ID | Module | Note |
| --- | --- | --- |
| GO-2026-6866 | `golang.org/x/net` v0.60.0 | raw-text rendering in HTML; fixed in v0.62.0 |
| GO-2026-6860 | `golang.org/x/net` v0.60.0 | quadratic text coalescing; fixed in v0.62.0 |
| GO-2026-5932 | `golang.org/x/crypto` v0.58.0 | unmaintained `openpgp` subpackage, unused |

Nothing in the engine parses HTML or uses OpenPGP, so neither is reachable. The `x/net` bump
is still worth taking as routine upkeep.
