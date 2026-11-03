# Tailmulti development handoff

Read docs/PROJECT_BRIEF.md, docs/RESEARCH.md and engine/ADAPTATIONS.md
before implementation. Keep the reference snapshot immutable. Do not change
live networking as part of builds or ordinary tests.

## Current state

The source is 0.4.4, build 11. It includes an independent Go engine, automatic
DNS translation, Tailscale Services, separate account identities, native
SMAppService setup, a single global switch, and an AppKit cascading menu.

The menu is the primary interaction. It provides account-scoped device search,
copy actions, and sign-in/connect/disconnect controls. New accounts sign in first.
The latest recovery work defers menu mutations out of AppKit tracking callbacks
and caches sorted rows. Automated tests pass; live search/copy/crash recovery
confirmation is still pending. Do not call the menu fully validated.

## Public release preparation

The release workflow creates a draft prerelease after macOS checks: a Developer ID
signed, notarized app zip plus the source archive. The service binds its owner to
the console user on first start, so one build works for any Mac user. Clean-Mac,
multi-user and broader live acceptance remain outstanding.

Use SDK 27.2 or newer and minimum macOS 26. `scripts/build-local.sh` stages a
separate signed candidate using a local development certificate. `scripts/check.sh`
runs safe validation. Neither registers a service or activates networking.
See docs/RELEASING.md for the workflow, source export, and remaining release gates.

Private session records, local validation output, signing configuration and
candidates are excluded from source exports. This file intentionally contains no
machine-specific paths, account names, production domains or current process IDs.
Local operators should consult their private notes before replacing an active app.

## Evidence and remaining work

See docs/VALIDATION.md for the distinction between automated checks and live
results. The full simultaneous Safari/SSH test and per-account disconnect
isolation test remain outstanding, as do sleep/wake, network changes, crash and
reboot recovery. A passing build is not evidence of those behaviors.

No updater, remote release download, or Tailmix installation code is part of
Tailmulti. No publication is implied by preparing the workflow.
