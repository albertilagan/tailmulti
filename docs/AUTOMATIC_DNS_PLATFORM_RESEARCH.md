# Automatic DNS platform research

Research date: 2026-11-03. This is a design recommendation, not an implementation or activation result.

Read `HANDOFF.md`, the project brief, existing research, implementation plan, and adaptation notes. Inspected installed SDK headers/manuals, relevant project source, the pinned Tailscale Darwin DNS manager, and Apple primary sources. No builds, tests, registration, DNS queries, account inspection, live configuration reads, or host networking changes were performed. This document is the only file written. No credentials, peer lists, private resolver addresses, or private search domains were inspected.

## Recommendation

Use a long-lived native `SCDynamicStore` writer session in the privileged engine. Add a new, Tailmulti-owned synthetic service DNS key with `SCDynamicStoreAddTemporaryValue`. Keep ordinary network configuration untouched. Read the base resolver from the physical services' `State:` and `Setup:` entities through a separate observer session, and send DNS packets directly to numeric server addresses over validated physical interfaces.

Keep exact-hostname resolver tombstones durable before publishing translated answers. The temporary default overlay allows discovery of custom public-DNS aliases while active; the retained hostname files protect already learned aliases after that overlay disappears. These are separate lifetimes.

The proposed dictionary needs an ordering correction. Apple source strips `SearchOrder` from a synthetic service that has DNS but no IPv4/IPv6 service state. Add `SupplementalMatchOrders=[100000]` to give the empty match domain an explicit order. A successful store write still does not establish that the system resolver selected it. The installed macOS versions must pass activation and crash tests before this becomes a supported behavior. [Apple DNS entity processing](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/ip_plugin.c#L4831), [supplemental resolver expansion](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/dns-configuration.c#L261).

## Installed SDK evidence

`xcrun --sdk macosx --show-sdk-path` identified:

```text
/Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX27.0.sdk
```

Files inspected under that SDK:

| File | Relevant evidence |
| --- | --- |
| `System/Library/Frameworks/SystemConfiguration.framework/Headers/SCDynamicStore.h` | Session lifetime, temporary add, copied values, notification keys, dispatch delivery, restart notification. Temporary add is public on macOS since 10.1. |
| `.../SCDynamicStoreKey.h` | Public constructors for `State:` and `Setup:` network entity keys. |
| `.../SCSchemaDefinitions.h` | DNS field types, `PrimaryService`, and `PrimaryInterface`. |
| `.../SCNetworkConfiguration.h` | Read-only service/interface inspection and service order. |
| `.../SystemConfiguration.h` | `SCError`, permission errors, missing-key and existing-key status. |
| `usr/include/netinet/in.h:432` | `IP_BOUND_IF`, a socket interface-index option. |
| `usr/include/netinet6/in6.h:506` | `IPV6_BOUND_IF`, its IPv6 counterpart. |
| `usr/share/man/man5/resolver.5` | Longest matching DNS domain, ascending search order for equal domains, `/etc/resolver`, ports, and search lists. |

The corresponding public API documentation is [SCDynamicStore](https://developer.apple.com/documentation/systemconfiguration/scdynamicstore), [temporary add](https://developer.apple.com/documentation/systemconfiguration/scdynamicstoreaddtemporaryvalue(_:_:_:)), and [DNS entity keys](https://developer.apple.com/documentation/systemconfiguration/dns-entity-keys). The installed headers supplied the full contracts where the web documentation required JavaScript.

Apple open-source internals below explain behavior but are not a promise that every shipping macOS binary matches this revision. The inspected configd revision is https://github.com/apple-oss-distributions/configd/commit/585b7f2fca293f4642d21d15c5daf187f63c4796. Do not link Tailmulti against private `dnsinfo` or configd symbols to reproduce these internals.

## Temporary default resolver

Create the key with `SCDynamicStoreKeyCreateNetworkServiceEntity`, using the State domain, a newly generated UUID, and `kSCEntNetDNS`:

```text
State:/Network/Service/<fresh Tailmulti-owned UUID>/DNS

ServerAddresses          CFArray<CFString> = ["127.0.0.1"]
ServerPort               CFNumber         = 53
SupplementalMatchDomains CFArray<CFString> = [""]
SearchOrder              CFNumber         = 100000
SupplementalMatchOrders  CFArray<CFNumber> = [100000]
```

Use real Core Foundation arrays and numbers, not string encodings. An empty string element is meaningful; an empty array supplies no match domain. Do not set `DomainName` to a fabricated suffix, create a persistent network service, alter `Setup:`, or attach this resolver to Tailmulti's utun. The synthetic entry needs only DNS state. [Schema definitions](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/SystemConfiguration.fproj/SCSchemaDefinitions.h), [key constructor](https://developer.apple.com/documentation/systemconfiguration/scdynamicstorekeycreatenetworkserviceentity(_:_:_:_:)).

For an empty supplemental domain, `add_supplemental` removes the domain field, producing a default-domain candidate. Its per-domain order overrides `SearchOrder`. The source's normal default order is 200000; supplemental defaults are normally earlier, with offsets influenced by service ordering. `get_dns_changes` removes `SearchOrder` and `SearchDomains` for DNS-only services, explaining why the five originally proposed fields do not guarantee an effective order of exactly 100000. Retaining `SearchOrder` is harmless, but rely on the matching one-element `SupplementalMatchOrders` array and verify the resulting resolver configuration. [Expansion and ordering](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/dns-configuration.c#L273), [DNS-only stripping](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/ip_plugin.c#L4956), [default-order constant](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/dnsinfo/dnsinfo.h#L40).

This creates a preferred default candidate, not an exclusive DNS interception mechanism. More specific supplemental domains, scoped queries, competing default resolvers, managed encrypted DNS, and application-owned DNS may bypass it. The ordinary physical default remains available, including as a possible fallback. Apple documents the analogous empty-domain behavior for Network Extension DNS, but that API is corroboration, not the contract for this dynamic-store technique. [Apple matchDomains](https://developer.apple.com/documentation/networkextension/nednssettings/matchdomains), [resolver selection](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/dns-configuration.c#L1560).

### Lifetime and foreign-key safety

The SDK says temporary add only succeeds if the key is absent. It also says removal occurs when the session closes unless another session updates the key. That qualification is essential. [Public header contract](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/SystemConfiguration.fproj/SCDynamicStore.h).

- Releasing the final reference to the writer closes its session. A single `CFRelease` is insufficient if another reference remains. Dispatch notifications retain the store, so either disable and drain them before releasing it or, preferably, keep the writer free of notifications and use a separate observer. [Client deallocation](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/SystemConfiguration.fproj/SCDOpen.c#L305).
- Process death releases its Mach rights. configd watches for no remaining senders, closes the session, and removes only keys still attributed to that session. This supports crash cleanup without a signal handler. Store cleanup and propagation to DNS consumers are asynchronous, not a synchronous `CFRelease` barrier. [Session setup/cleanup](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/configd.tproj/session.c#L385), [no-senders handler](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/configd.tproj/configd_server.c#L85), [owned-key removal](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/configd.tproj/_configclose.c#L111).
- A different session's ordinary `SetValue` cancels the remove-on-close ownership, even if the value is identical. Dictionary equality or an owner marker cannot prove session ownership. Updating through a new session is therefore unsafe. [Set implementation](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/configd.tproj/_configset.c#L128).

Generate a fresh UUID per activation. Check the exact candidate DNS key and whether that UUID already identifies a configured service; distinguish a missing key from read/permission failures. The final authority is `SCDynamicStoreAddTemporaryValue`, whose server-side add refuses existing data. A pre-read followed by `SetValue` is a race and can overwrite foreign configuration. On collision, leave the existing key untouched and choose another UUID or report an error. Capture `SCError` immediately in the same C wrapper call. [Atomic add path](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/configd.tproj/_configadd.c#L39).

Keep the published dictionary fixed for the activation. Observe its exact key for disappearance or changes and periodically check presence; do not repair unexpected values with `SetValue`. Close the writer to request ownership-aware removal. Avoid a read/compare/`RemoveValue` cleanup sequence, which has no public compare-and-delete protection against a concurrent replacement. Never scan and delete other synthetic service keys or a prior process's UUID based only on a prefix or marker. Foreign replacement can leave a persistent loopback resolver; report that ownership failure rather than deleting foreign data. Identical foreign writes cannot be reliably attributed through the public value API.

## Reading ordinary base DNS without reading the overlay

Read these exact keys with `SCDynamicStoreCopyMultiple`, keeping values internal to the engine:

```text
State:/Network/Global/IPv4
State:/Network/Global/IPv6
Setup:/Network/Global/IPv4                 # configured ServiceOrder, if needed

For each selected PrimaryService:
State:/Network/Service/<id>/DNS
Setup:/Network/Service/<id>/DNS
State:/Network/Service/<id>/IPv4
State:/Network/Service/<id>/IPv6
Setup:/Network/Service/<id>/Interface
```

The global IPv4 and IPv6 dictionaries expose their elected `PrimaryService` and `PrimaryInterface`. A DNS-only synthetic service does not itself become an IP primary. Read each family's pair; do not require the IPv4 key on an IPv6-only network. Service DNS is an input to IPMonitor's DNS merge, not necessarily the final merged configuration. Avoid `State:/Network/Global/DNS`, `/etc/resolv.conf`, `net.DefaultResolver`, and `scutil --dns` as sources of forwarding addresses: they describe or use effective resolution and may include the overlay. [Global publication](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/ip_plugin.c#L6025), [IPv6 publication](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/ip_plugin.c#L6128), [service DNS merge](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/ip_plugin.c#L4831).

`PrimaryInterface` means elected primary, not necessarily physical. Verify the service-to-interface association through Setup/service-interface APIs and current interface identity/type. Exclude loopback, Tailmulti, and unsupported VPN/tunnel paths. Do not assume Wi-Fi is `en0`. If another VPN is primary, the two global dictionaries alone cannot reveal a safe physical default. A later extension could enumerate enabled physical services and evaluate their routes and configured order; the narrow first version should report that unsupported situation instead of guessing or bypassing the VPN. [Public interface API](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/SystemConfiguration.fproj/SCNetworkConfiguration.h), [Apple DTS on interface identity and binding](https://developer.apple.com/forums/thread/798519).

Read the global keys first to discover service IDs, then fetch the globals again alongside the selected service entities. Retry if the primary pairs changed. Subscribe before the initial snapshot and use a change generation to invalidate stale work. `CopyMultiple` reduces inconsistent reads, but its public contract does not promise a transaction across configd's producers, route changes, and notifications. Bound the retries; return unavailable rather than use a mixed snapshot. [Copy and notification contracts](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/SystemConfiguration.fproj/SCDynamicStore.h).

### Manual settings, DHCP, IPv6, and search domains

| Case | Source behavior and recommended handling |
| --- | --- |
| Valid manual `Setup:` server list | Normally replaces dynamically learned servers. Preserve configured order. Do not silently fall back to DHCP when a valid manual server times out. |
| Missing or unusable manual server list | IPMonitor falls back to state servers after filtering unusable addresses. Validate types, address family, and active connectivity before declaring manual settings usable. |
| State DNS | May originate from DHCP or IPv6 configuration, not just DHCPv4. Preserve literal IPv6 servers and link-local scope. No DHCP lease or packet inspection is needed. |
| Search domains | Setup arrays take precedence when present. An explicitly empty Setup array differs from a missing one. Do not append DHCP domains unconditionally. `DomainName` and scalar DNS fields are selected separately, normally Setup first. |
| Both IP primaries use one service | Deduplicate the service and keep one resolver group with its supported transport families. |
| IPv4 and IPv6 primaries differ | Preserve separate service/interface groups. Apple's DNS election compares service ranks, not simply IPv4 first. Do not merge their server arrays and race private queries across networks. |
| IPv6-only | Use the IPv6 primary and usable IPv6 DNS transport. A/AAAA question types do not determine transport family; both can use an IPv6 DNS server. Loopback `127.0.0.1` remains a separate local listener, but its effective A/AAAA handling must be tested on IPv6-only macOS. |

These merge rules are visible in `merge_dns_servers`, `merge_array_prop`, `pick_prop`, and `accumulate_dns_servers`. The source has an `AppendStateArrayToSetupArray` switch, default false, so the normal precedence is not a universal promise for all system configurations. Dynamic server ordering can also be adjusted by IPv4/IPv6 reachability preferences. Implement the documented supported subset and identify mismatches; do not claim to reproduce every mDNSResponder policy. [Merge helpers](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/ip_plugin.c#L4223), [server filtering and merging](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/ip_plugin.c#L4596), [append-state option](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/ip_plugin.c#L9291).

Configured `ServiceOrder` is useful input when the two primaries differ, but runtime rank assertions also affect Apple's election. For an initial narrow API, expose both candidates and their provenance. Support one unambiguous physical resolver group; refuse ambiguous multi-service DNS until selection is explicitly implemented and tested. Do not substitute an arbitrary public resolver. [DNS primary election](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/ip_plugin.c#L8562).

Leave existing search configuration with macOS. IPMonitor extracts the physical default search list and applies it to the resulting first default resolver; inserting `SearchDomains` into the synthetic DNS-only entry does not preserve it because that field is stripped. The forwarding layer should send the received absolute question, without adding search suffixes a second time. Preserve search metadata in snapshots to detect changes, and verify system search behavior during activation. Do not turn short names into cross-account peer lookup. [Search extraction and final default](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/dns-configuration.c#L1130), [final search list assignment](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/dns-configuration.c#L1644).

### Network changes

Observe both global IP keys, selected service State/Setup DNS and IP entities, relevant Setup/interface/service-order changes, and the exact overlay key. Replace service-specific subscriptions when primaries change, then resnapshot to close the notification gap. Notification patterns may detect topology changes without logging their values. Debounce bursts but stop using a vanished interface immediately.

Publish immutable snapshots with a generation. Retire outbound sockets and ordinary-answer caches when endpoint, interface, manual configuration, or search-domain provenance changes. Reject stale in-flight responses before publishing new translations. An empty dynamic-store callback key list signals configd restart; discard prior store assumptions, re-read the base configuration, and verify/re-establish a new temporary overlay only through the guarded activation path. Never restore a cached DNS dictionary blindly. The callback restart meaning is explicit in the [SDK contract](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/SystemConfiguration.fproj/SCDynamicStore.h).

If no valid base resolver remains, fail forwarding promptly and report unavailable automatic DNS. A controlled deactivation can remove the temporary default while keeping tombstones and synthetic guards. Never use a hardcoded vendor, system hostname lookup, stale previous-network resolver, or another tailnet as emergency fallback.

## Direct outbound DNS without recursion

Build DNS UDP/TCP exchanges against numeric addresses from the validated base snapshot. Reject loopback/self-listener endpoints, Tailmulti synthetic addresses, and resolver paths that depend on Tailmulti or another unsupported tunnel. This filtering must apply after Setup/State precedence, not only to one input dictionary. Do not use the OS resolver to resolve the upstream server or the queried hostname. Interface binding alone cannot prevent recursion if the selected destination is the local resolver.

For IPv4 use `setsockopt(IPPROTO_IP, IP_BOUND_IF, ifindex)`; for IPv6 use `setsockopt(IPPROTO_IPV6, IPV6_BOUND_IF, ifindex)`. Set the option before connect or the first send, for both UDP and TCP retry. Treat a zero/stale index or socket-option failure as failure, never retry unbound. Attach and validate the scope ID for link-local IPv6 destinations. Use explicit `udp4`/`udp6` and `tcp4`/`tcp6` sockets so mapped-address behavior cannot choose the wrong option. The SDK headers and [Apple DTS guidance](https://developer.apple.com/forums/thread/798519) establish these options; reachability still needs live validation.

Binding all upstream DNS to the selected physical interface is intentionally stricter than macOS's manual-DNS behavior. Apple binds dynamically learned DNS to its network, but deliberately allows manual DNS to route over another interface. A manually configured resolver reachable only through another physical service or a VPN therefore needs an explicit supported policy or an unavailable result. Do not silently change it to DHCP or retry without binding. [Apple's explanation in `merge_dns_servers`](https://github.com/apple-oss-distributions/configd/blob/585b7f2fca293f4642d21d15c5daf187f63c4796/Plugins/IPMonitor/ip_plugin.c#L4784).

Use bounded deadlines, response-source/question/ID validation, UDP truncation-to-TCP retry, and per-generation sockets. Binding selects an interface; it does not prove connectivity or override every managed network policy. Forwarders should return a prompt local failure while active rather than stall until macOS tries another default candidate. That still cannot guarantee interception of new, unknown names when the engine is unresponsive.

## Tombstones and translated-answer publication

Keep the current durable `/private/etc/resolver` ownership model, extending it to learned exact aliases. A file named for a full hostname is still a suffix rule in macOS, so it also matches descendants. Never install an entire public-zone suffix merely because one hostname resolved to a peer. The installed `resolver(5)` manual documents longest-domain matching; equal-domain foreign resolvers remain a conflict.

For every response eligible for translation, the recommended publication sequence is:

1. Validate a bounded DNS response and establish unambiguous peer ownership under the current routing generation. Use existing durable synthetic leases. A known alias must not become transferable to another account just because its previous owner disconnected.
2. Durably retain the original query name and any CNAME/target names whose translated addresses could be cached independently. Persist their ownership/tombstone state, then atomically publish owned resolver files and sync the files and directory. Refuse foreign/symlink/path conflicts.
3. Confirm the required routes and translation mapping are ready, and recheck ownership/generation after any asynchronous work. A disconnect, collision, or persistence failure must prevent the answer.
4. Only then release synthetic addresses to the caller. Keep learned names on negative local handling when the owning account is unavailable; do not forward those known names to ordinary DNS after disconnect.

The file's existence and fsync do not prove mDNSResponder has consumed it. There is no public consumption-acknowledgement API in the inspected dynamic-store/file interfaces. This creates a real first-answer/crash race to resolve during implementation: durable-before-answer is necessary, but a delay alone is not proof of resolver installation. Require effective resolver observation plus crash-at-boundary acceptance tests, or document the remaining window and choose a stronger lifecycle design. Do not claim an absolute no-leak guarantee from file writes alone.

Apple's responder source supports the intended protection once the exact-name rule is loaded: `SetValidDNSServers` discards less-specific matches, and retries select among the retained matching set. A closed loopback listener should then fail resolution for that name rather than use the less-specific default. Equal-domain foreign entries, scoped resolution, and alternative resolver implementations still need explicit coverage. [Matching set](https://github.com/apple-oss-distributions/mDNSResponder/blob/d4658af3f5f291311c6aee4210aa6d39bda82bbe/mDNSCore/mDNS.c#L11988), [retry selection](https://github.com/apple-oss-distributions/mDNSResponder/blob/d4658af3f5f291311c6aee4210aa6d39bda82bbe/mDNSCore/mDNS.c#L12167).

Tombstones protect future OS DNS lookups, not previously cached canonical addresses or clients using DoH/direct DNS. Synthetic-address reject guards remain necessary after engine death. Newly rewritten DNSSEC-signed RRsets cannot retain a claim of authenticated original data. CNAME chains, mixed A/AAAA sets, HTTPS/SVCB address hints, auxiliary records, and signature handling need explicit policy so an unhandled record does not provide a canonical bypass. These are implementation cautions, not functionality established by this platform research.

## Narrow Go/cgo boundary

Use `darwin && cgo`, linking only public CoreFoundation and SystemConfiguration frameworks. Keep native pointers and Core Foundation ownership inside a small C implementation. A proposed Go contract is:

```go
// Design sketch only; no implementation added.
OpenObserver() (*Observer, error)             // Read-only; never enables DNS.
(*Observer).Snapshot() (BaseDNSSnapshot, error)
(*Observer).Changes() <-chan Change          // Coalesced invalidation + restart.
(*Observer).Close() error

InstallTemporaryDefault() (*Overlay, error)  // Fixed dictionary; fresh writer.
(*Overlay).Present() (bool, error)           // Presence/value, not ownership proof.
(*Overlay).Close() error                     // Releases writer; no foreign remove.
```

`BaseDNSSnapshot` should contain generation, primary service/interface associations, ordered resolver groups with numeric addresses/ports/zones, active transport families, selected search metadata, and Setup/State provenance. These are internal data, not log or app-status fields. Expose fixed failure categories such as no base DNS, unsupported primary, key conflict, permission denied, and store restarted. Do not return raw dictionaries, query names, resolver addresses, or private configuration in errors.

The host activation coordinator should reserve both UDP/TCP loopback listeners, verify base DNS and retained guards, and finish initial tombstone preparation before installing the overlay. The 0.4.1 implementation tries privileged ports 53 through 63 and supplies the selected `ServerPort` to both the overlay and owned resolver files. Avoid socket-reuse settings that could share a port with another resolver. Exhaustion or non-conflict bind errors must leave DNS unchanged. Keep writer ownership tied to active networking, not the SwiftUI window or account object. One account disconnect must not remove the shared overlay while others remain active. This port-selection change requires effective-resolver verification on the Mac, independently of the earlier platform research.

Use an explicit, idempotent `Close`, not a Go finalizer. Keep the writer independent of observer notification retains. Drain/cancel observer callbacks before releasing context; never retain Go memory directly in a native callback. Serialize activation, deactivation, publication, and network-generation transitions. Treat successful release as a cleanup request, then separately observe the resulting configuration during activation tests.

For `darwin && !cgo`, return a clear unsupported error for automatic default DNS before making any host changes. Existing exact-name DNS can remain available only as an explicitly identified reduced mode if product policy permits it. Do not silently report automatic aliases as supported, run persistent `scutil set`, use a public vendor, or ask for manual `networksetup` DNS edits. A future native helper could replace cgo only if it owns the same session lifetime; that is a separate implementation, not an implicit fallback.

## Upstream boundary

Inspected `.cache/go/pkg/mod/tailscale.com@v1.104.4/net/dns/manager_darwin.go`. Its `setGlobalDNS` executes `scutil set` at a stable Tailscale UUID and its `removeGlobalDNS` removes that same key. This is persistent relative to the short-lived scutil session, not persistent preference storage. A crash of the caller does not get temporary-session cleanup. Its `GetBaseConfig` reads `/etc/resolv.conf`, which is unsuitable for this overlay-independent requirement. Its resolver cleanup also serves a different ownership/lifecycle policy. [Pinned upstream source](https://github.com/tailscale/tailscale/blob/v1.104.4/net/dns/manager_darwin.go).

Do not reuse the Tailscale UUID, its cleanup scripts, or its resolver-file removal policy. Do not modify the module cache or immutable Tailmix snapshot. The API and schema reasoning above should inform a small independent adapter using public APIs.

## Validation boundaries

No tests were run in this research task. The following are future checks, not claimed results.

| Check class | Possible checks and limits |
| --- | --- |
| Static/read-only, possible now | Confirm installed symbols/types and source paths, review key construction and ownership logic, inspect existing source for recursive resolver use. These were the only kind of checks performed here. |
| Read-only host tests, separately authorized | A native snapshot/observer can report only booleans, counts, provenance categories, and generation changes. Check IPv4/IPv6 primary associations, exclusion of the synthetic UUID, and manual precedence without dumping values. Subscribe/unsubscribe without adding any key. An effective-config inspector may reduce `scutil --dns` in memory to redacted assertions; never print its raw output. |
| Offline tests after implementation | Synthetic CF dictionaries and mocks for missing/malformed/empty Setup values, DHCP/manual precedence, IPv6 scopes, distinct primaries, collisions, observer-retain cleanup, configd restart, network-generation races, cgo-disabled refusal, and persistence failures. Run loopback DNS protocol fixtures on unprivileged ports without system DNS registration. |
| Privileged temporary-store tests | On an explicitly approved isolated test setup, test add collision, last-reference release, normal exit, SIGKILL, another-session replacement including identical data, permission failure, and cleanup convergence. Even a non-network temporary-store mutation is outside this research task. |
| Privileged DNS activation tests | Install the exact DNS-only dictionary and verify effective order with and without `SupplementalMatchOrders`. Confirm ordinary system lookups reach Tailmulti, both UDP/TCP work, physical forwarding avoids recursion, Setup remains unchanged, and close/crash restores ordinary defaults. Do not use `dig @127.0.0.1` as proof of OS selection. |
| Isolation and lifecycle acceptance | Use controlled test names and resolvers. Crash immediately before/after tombstone write, after fsync, and after the first translated answer; prove no subsequent known-name query reaches the external test resolver. Test disconnect, conflicting ownership, CNAME caching, same-domain foreign resolver, daemon restart, sleep/wake, network switch, IPv6-only, and changed manual DNS. Instrument only controlled names; do not capture unrelated DNS payloads. |

Configd restart, VPN coexistence, network switching, and reboot belong on an approved disposable test host where possible. Reboot coverage must include retained synthetic-route guards, whose limitations are already recorded in `HANDOFF.md`. SDK contracts and source inspection establish a plausible native design; only those activation tests can establish actual resolver selection, crash convergence, and known-name isolation on supported macOS releases.
