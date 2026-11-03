# Validation

## Open-source preparation, 2026-11-04

The new `scripts/check.sh` passed locally: source audit, all 185 reference-file
hashes, seven release/signing-selection tests, the full Go race suite and vet,
71 Swift tests with two gated skips, ad-hoc candidate verification and real
preview IPC. The new `scripts/build-local.sh` also built a signed candidate, and
the gated Swift signature/bundle check passed separately. None of these steps
registered a service, enrolled an account or activated networking.

Workflow YAML and permissions were checked locally. GitHub-hosted execution has
not run because the repository has not been published. The source archive and
checksum are local artifacts; no release has been published.

## Automated evidence

Before open-source preparation, the current menu recovery candidate passed 71
Swift tests with no failures and two gated skips. The separately run bundle check
and real Swift-to-preview-daemon IPC test passed. The app was built using SDK 27.2
with minimum macOS 26. The earlier engine regression passed Go race tests and vet.

Coverage includes packet translation, account isolation, address collisions,
persistence failures, DNS ownership and translation, Tailscale Services, native
socket authentication, global on/off transitions, sign-in coordination, and
account-scoped device search and copy values. These are simulated or local tests;
they do not establish live two-tailnet acceptance.

Search filtering for 500 devices per account averaged about 0.49 ms, with a worst
sample of 1.17 ms. Those measurements exclude AppKit layout, drawing and event
latency. Menu tests cover deferred updates during close callbacks and child-menu
tracking. Live confirmation of the latest menu crash fix is still pending.

Run `scripts/check.sh` to reproduce the safe checks. It builds a candidate but
never registers or activates it. The signed bundle test is optional and requires
a locally signed candidate:

```sh
TAILMULTI_TEST_APP_BUNDLE="$(cat build/candidate-app-path.txt)" \
  scripts/swift.sh test --filter StagedAppBundleTests
```

## Live evidence

On one development Mac, two separately enrolled accounts remained Running while
normal macOS DNS and HTTPS reached a personal Tailscale Service and a company
service with a custom hostname. The personal service passed both IPv4 and IPv6.
Public HTTPS also passed. TLS verification remained enabled, with no per-process
address overrides or proxies.

The global switch was tested off and on. Off removed the active default resolver
and owned resolver files, restored public DNS/HTTPS, kept sign-ins and enabled
preferences, and retained rejection of cached synthetic addresses. On reconnected
both accounts and restored access. An occasional first connection to the company
service timed out after restart and passed on retry; its cause is unresolved.

Local evidence contains private infrastructure identifiers and is excluded from
the repository and release archives. These results describe one Mac, not a
cross-platform certification or a guarantee for every network.

## Outstanding acceptance

- Simultaneous Safari and SSH to both accounts, including checking the remote identity.
- Disconnect either account while the other keeps existing SSH connections and opens new browser connections; verify no fallback through the wrong account.
- Live overlapping-address, duplicate-name, stale-cache and account-removal cases.
- Sleep/wake, network changes, daemon crash, reboot and recovery.
- Latest native menu search, copy, close/reopen and long-session crash regression.
- A clean Mac install with portable ownership, Developer ID signing and notarization.
- Minimum supported OS acceptance, app relocation, service removal and credential erasure.

The first working milestone and a general binary release remain gated on the
relevant live checks. Source prereleases are labeled as development software.
