# Contributing

Read [the project brief](docs/PROJECT_BRIEF.md) and [source adaptations](engine/ADAPTATIONS.md).
Engine code lives in `engine/`; native app and menu code lives in `macos/`.
Keep `upstream/tailmix/` unchanged. Its scripts are reference material only.

Use macOS 26 or later, Xcode with SDK 27.2 or newer, Go and Python 3.11 or later.
Go downloads the pinned toolchain through `scripts/go.sh`. Run:

```sh
./scripts/check.sh
```

The check script uses an ad-hoc candidate and preview IPC. It does not enroll
accounts, register a root service, modify live routes/DNS or change another VPN.
Signed local setup is described in [installation](docs/INSTALLATION.md).

Keep behavioral tests with routing, collision, DNS, identity isolation, lifecycle
and control-protocol changes. UI tests and build checks do not replace an actual
menu interaction test. Record which live checks were performed and which remain.

Never log keys, login URLs or packet payloads. Use synthetic domains and account
IDs in fixtures. Keep screenshots, diagnostics, local signing files and session
notes out of contributions. `scripts/audit-source.py` catches some common leaks;
it is not a substitute for reviewing the diff.

Preserve existing copyright and license headers. Original contributions use the
project's BSD-3-Clause license unless a file explicitly says otherwise. Changes
to Tailscale adapters need focused regression tests and provenance notes.

Do not publish another contributor's work or change their branch.
Pull requests should state the problem, resulting behavior, and validation.
