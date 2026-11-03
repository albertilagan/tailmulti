# Source provenance and adaptations

Imported from https://github.com/maisem/tailmix/commit/98d62fed39d388b8eb47b77fcb190a165e08a075. `UPSTREAM.json` records SHA-256 hashes of each original imported file. The reference snapshot under ../upstream/tailmix is never edited.

Tailmix's BSD-3-Clause license is in LICENSE. Nested Tailscale-derived tsnet and netns retain their LICENSE, UPSTREAM records and copyright headers. Their initial base is https://github.com/tailscale/tailscale/commit/fad8b9b8a957c3d4d81d1d9f61c09090dd8d0ba3. The linked Tailscale module is pinned by go.mod/go.sum. Do not remove these notices from binary distributions.

Imported packages: effectiveip, packetmap, tunmux, hosttun, tsnet, netns, and the Tailscale-specific profile adapter and its tests. Imports now point to tailmulti.local/engine. Original tests are retained. Product-specific additions are under internal/ and cmd/. Packet validation, lifecycle notification, startup preferences and queue isolation changes are called out in source comments and covered by Tailmulti regression tests.

Excluded: upstream CLI/daemon, controlapi, installers, release automation, version/updater packages, SOCKS service, raw WireGuard management and SSH server. There is no binary download or automatic update endpoint. Runtime logs from upstream are discarded, remote log upload remains disabled, and authentication URLs are available only through an authenticated login operation. The GUI never reads identity state.

Dependency notices for distribution are generated from the resolved module graph by scripts/build-app.sh. This candidate is not a signed/notarized release.

## Changes to imported code

- `packetmap`: require the expected host source and profile-local inbound destination; validate packet length and supported transport before translation. Reject fragments, IPv4 options, IPv6 extension headers and non-echo ICMP. Preserve disabled UDPv4 checksums and normalize a computed UDP zero checksum. Added independent full-checksum and malformed-packet tests.
- `tunmux`: shared outbound forwarding uses nonblocking per-profile queues so a full or closing account cannot stall the other accounts. Added queue-congestion regression coverage.
- `profile/tsnetengine`: pin the official control-plane URL rather than accepting an ambient override, and notify on all backend state changes plus browser-login changes. Auth-key environment fallback remains disabled.
- `tsnet`: when using a supplied packet TUN, force shields-up, disable OS DNS/subnet route preferences, clear advertised routes/exit selection, disable inbound SSH/web service preferences and explicitly disable Tailscale update checks/apply. Added a startup-preference regression test. The in-memory LocalAPI remains internal to the engine; it is not exposed through Tailmulti control.
- `hosttun`: product labels use Tailmulti. The macOS adapter remains behind the new activation gate; the unused Linux adapter is retained for provenance/test continuity, but there is no Linux Tailmulti daemon target in this milestone.
- `tsnet/underlay_darwin`: imports the locally maintained netns adapter under Tailmulti's module name. No transport or cryptographic algorithms are replaced.

All host mutation in our executable passes through `internal/host` and an explicit `--activate` gate. The imported `hosttun` implementation contains the low-level route/utun operations. Existing upstream tests remain alongside Tailmulti's new tests; no inherited live account test was imported or run.
- `tunmux/chantun` and supervisor: a closed per-account channel is a local failure. The supervisor withdraws that account's mappings and DNS instead of terminating the shared mux; host device write failures still terminate the aggregate service.

The Darwin host adapter corrects the pinned Tailscale v1.104.4 route reader's IPv6 RTF_HOST /32 representation at the decision boundary, using the kernel flag to derive /128. The module cache and upstream snapshot are unchanged. Only kernel-local loopback routes for reserved addresses verified on Tailmulti's active utun are exempted from collision checks. Opt-in read-only host tests exercise a real active interface without creating it or changing routes.

## Automatic custom-hostname DNS

The 0.4 candidate adds independently written DNS forwarding and translation under `internal/names`, durable account ownership under `internal/daemon` and `internal/routing`, and a native SystemConfiguration adapter under `internal/host`. The adapter uses public Apple APIs through cgo and Objective-C. It neither instantiates Tailscale's host DNS manager nor reuses its service UUID or cleanup scripts. Platform sources and limitations are recorded in `../docs/AUTOMATIC_DNS_PLATFORM_RESEARCH.md`.

The resolver forwards ordinary queries directly to the selected physical network's existing DNS servers, binds UDP/TCP to that interface, and translates uniquely owned active peer addresses. Public responses remain unchanged unless private addresses must be removed. Translated answers discard DNSSEC proofs. Exact custom names and their account ownership persist before publication. Retained canonical peer leases and historical local-identity addresses prevent automatic ownership transfer after disconnect or removal. Private corporate resolver forwarding and subnet routing are still separate work.

## Tailscale Service destinations

Version 0.4.1 connects the imported profile adapter's existing Service discovery to Tailmulti's planner. `internal/routing/targets.go` combines device destinations with validated Service names and VIPs from each authenticated account. Service lease identities use a reserved `service/` prefix and follow the service rather than its current host. The same allocator, packet mapper, persistence ordering, canonical-address collision index and DNS tombstones cover both types. Services are not reported as physical devices.

No subnet-acceptance preference or transport code changes are needed. The pinned Tailscale route manager already accepts individual Tailscale VIP routes with `RouteAll` disabled. Private LAN routes remain disabled. No user-managed service, domain or IP list is introduced. The upstream reference and module cache remain unchanged.

The same update removes the fixed port-53 activation requirement. Tailmulti reserves UDP and TCP on an available privileged loopback port from 53 through 63, then supplies that port to SystemConfiguration and owned resolver files. Existing listeners are left in place. Restoration accepts only the exact owned format for those supported ports; unprivileged fallback ports are not used.
