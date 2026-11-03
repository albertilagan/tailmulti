# Candidate source review

Reviewed the imported packet boundary and all new routing, state, DNS, supervisor, IPC, macOS activation and native-client code. Checked the runtime path against the intended first milestone; all privileged behavior remains unexecuted.

| Finding | Change and regression evidence |
| --- | --- |
| Original mapper accepted arbitrary outbound source/inbound destination | Require host source and selected profile self destination; transit tests |
| Original mapper lacked transport/fragment bounds suitable for our limited support contract | Validate fixed headers and lengths, reject unsupported fragments/options/extensions; malformed packet tests and bounded fuzzing |
| Incremental UDP address changes could alter an absent IPv4 UDP checksum | Preserve zero-checksum sentinel; independent TCP/UDP/ICMP v4/v6 checksum tests |
| Full/closed account queues could stop shared forwarding | Nonblocking outbound queues and local handling of closed account channels; congestion and unexpected-closure tests |
| Cached answers could acquire a new identity after lease reclamation | Retain all leases and inactive routes; disconnect/remove/restart tests |
| General status could expose login URLs through inherited control behavior | Replace server, authorize every endpoint, separate login response, reject browser origins and strict-body violations; Unix peer-credential tests |
| Inherited preferences could enable OS routing, services or updates | Force packet-mode settings at startup and pin coordination URL; preference regression test |
| Native menu needed to survive closing its window | Model-owned status polling and explicit main-window opening; compiled, not visually exercised |
| `/etc` is a macOS symlink | Use canonical `/private/etc/resolver` for strict ancestor checks; actual activation remains untested |
| Reconciliation could rewrite durable state every poll | Save only changed plans; unchanged-state and failed-new-lease tests |
| Repeated packaging could fail on a read-only copied Go license | Assemble a fresh staging bundle before replacing the previous generated bundle |

Known validation gaps are not waived as passes. Native rendered layout, macOS resolver/route behavior, live ACL enforcement and account enrollment, VPN coexistence, sleep/wake, crash guards and persistent helper installation require the procedures in ACTIVATION.md. The candidate does not claim a completed live connectivity milestone or distribution readiness.
