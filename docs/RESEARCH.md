# Architecture research

Primary-source investigation began on 2026-11-03. This document records the
basis for the current implementation. [Validation](VALIDATION.md) records what
has actually been tested; source documentation alone does not prove behavior.

## Packet engine and reuse

The standard Tailscale client selects one active tailnet. Embedded `tsnet.Server`
instances can keep separate identity state, but the public API does not expose
the complete packet multiplexing contract used here. Tailmix's adapter accepts a
TUN and exposes the backend while retaining Tailscale's authentication, encryption,
transport and policy enforcement. Tailmulti adapts that integration and owns its
routing plan, address allocation, DNS, account lifecycle and control protocol.

Sources: [account switching](https://tailscale.com/docs/features/client/fast-user-switching),
[tsnet API](https://tailscale.com/docs/reference/tsnet-server-api),
[pinned Tailmix source](https://github.com/maisem/tailmix/tree/98d62fed39d388b8eb47b77fcb190a165e08a075),
[adaptation record](../engine/ADAPTATIONS.md).

## utun and Network Extension

The development engine uses one privileged utun interface and per-account packet
queues. This keeps the Go packet integration direct and lets networking outlive
the GUI. It also requires explicit ownership of host routes, DNS and cleanup.

Apple describes access to an isolated network as an appropriate packet-tunnel
use case. A Network Extension remains a plausible future implementation, requiring
a packet-flow bridge, extension lifecycle, entitlements and distribution testing.
This is an architectural inference, not approval from Apple.

Sources: [Apple TN3120](https://developer.apple.com/documentation/technotes/tn3120-expected-use-cases-for-network-extension-packet-tunnel-providers),
[packet tunnel provider](https://developer.apple.com/documentation/networkextension/nepackettunnelprovider).

## DNS and collisions

Full tailnet names resolve to persistent, account-specific synthetic addresses.
The same canonical address in different accounts does not imply the same local
address. Unknown or ambiguous ownership must not select an account by guesswork.

While networking is on, a temporary SystemConfiguration resolver handles ordinary
system DNS. Replies pointing to a uniquely known tailnet destination are translated
without a manually managed domain list. Ordinary DNS forwarding uses the physical
network's existing servers, not a public resolver chosen by Tailmulti. Durable name
ownership and reject guards protect retained destinations. Browser-managed DNS and
private company DNS forwarding have separate support limits.

The native resolver dictionary and ordering were checked against Apple SDK headers
and configd source. Temporary-value cleanup and resolver selection still require
live lifecycle tests; a successful write is insufficient evidence.

Sources: [automatic DNS platform research](AUTOMATIC_DNS_PLATFORM_RESEARCH.md),
[DNS implementation plan](AUTOMATIC_DNS_PLAN.md),
[Apple matchDomains](https://developer.apple.com/documentation/networkextension/nednssettings/matchdomains).

## Local control and service setup

Every request uses kernel peer credentials. The service authorizes one configured
UID and root, and the Swift client verifies the server UID. This trusts processes
running as the selected user, not a particular client's code signature. There is
no arbitrary command, supplied state path, auth-key parameter or raw LocalAPI proxy.

The app registers its bundled daemon through SMAppService after a user action.
The local build embeds its owner UID and verifies app/helper signatures from the
same Apple team. This is suitable for a local development candidate; portable
owner provisioning and notarized distribution are unfinished.

Sources: [Tailscale peercred](https://github.com/tailscale/peercred),
[SMAppService](https://developer.apple.com/documentation/servicemanagement/smappservice),
[native setup research](NATIVE_SETUP_RESEARCH.md), [control protocol](CONTROL_PROTOCOL.md).

## Installation, update and license audit

Tailmix's installer and updater target Tailmix releases and can replace its binaries.
Those implementation files are not imported into Tailmulti. The immutable reference
still contains them for inspection. Tailmulti has its own local build scripts and
source-release workflow, and no self-updater.

Original Tailmulti code uses BSD-3-Clause. Tailmix and the Tailscale adapters retain
their original notices, and bundle builds collect dependency notices. The full
reference snapshot and imported-source hashes are checked from committed manifests.
See [NOTICE](../NOTICE) and [release maintenance](RELEASING.md).
