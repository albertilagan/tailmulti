# Tailmulti

Use separate Tailscale accounts at the same time from ordinary macOS apps.
Tailmulti has its own Go routing engine, a SwiftUI app, and a native menu bar
for account controls, device search, and copying hostnames and addresses.

Each account keeps its own identity and credentials. Tailmulti gives destinations
unique local addresses, selects the correct account, and translates packets.
Tailscale supplies authentication, encryption, transport, and access policy.
Selected packet-engine code is adapted from Tailmix. No Tailmix installation is required.

**Early development software.** Expect rough edges and read the known limits below.

## Install

Requires an Apple silicon Mac running macOS 26 or later.

1. Download `Tailmulti-<version>-macOS.zip` from the [latest release](https://github.com/albertilagan/tailmulti/releases).
2. Unzip it and move **Tailmulti.app** to Applications, then open it. The app is signed with Developer ID and notarized by Apple.

The first Mac user who approves the background service becomes its only owner.

## Build from source

Requirements: Xcode 27.2 or later, Go, Python 3.11 or later, and an Apple Development
signing certificate with its private key in your login keychain. Xcode's Accounts
settings can create the certificate. Complete Xcode's first-launch setup before building.

From an extracted source release or a local checkout:

```sh
./scripts/build-local.sh
open "$(cat build/candidate-app-path.txt)"
```

The script chooses an installed Xcode with SDK 27.2 or newer, uses a local Apple
Development certificate, and builds a separate candidate for your Mac user.
It does not install a helper or change networking. Go downloads the pinned
toolchain and dependencies on the first build.

## First run

In Tailmulti:

1. Choose **Allow Background Service** and approve it in macOS if prompted.
2. Turn **Tailmulti** on. If another VPN conflicts, follow the reported check.
3. Choose **Add Account**, give it a label, and **Sign In**. Repeat for the other account.

Keep the app at the path where you approve its service. Keep native Tailscale
installed, but disconnect it if Tailmulti reports a route conflict. The Tailmulti
switch disconnects all accounts and restores normal DNS when turned off.

For certificate selection, updates, and removal, see [installation](docs/INSTALLATION.md).
Without a signing certificate, `scripts/build-app.sh --candidate` builds an
ad-hoc bundle for development checks, with background-service registration disabled.

## What works and what remains

- Full tailnet hostnames and Tailscale Services receive account-specific local addresses.
- Ordinary system DNS answers pointing to a uniquely known tailnet destination are translated automatically. No domain list is needed for this case.
- Public internet connections keep their usual route. Tailmulti forwards ordinary DNS to the network's existing resolvers while on.
- Subnet routing, exit nodes, and account-specific private DNS forwarding are not implemented. Apps that bypass macOS DNS may bypass translation.

Live testing has reached a personal Tailscale Service and a company HTTPS service
through normal macOS DNS with both accounts connected. Global off/on restored DNS
and reconnected the saved accounts. The full Safari/SSH and per-account disconnect
isolation acceptance test is still outstanding. The latest native menu's crash
recovery has automated coverage but needs live confirmation. See [validation](docs/VALIDATION.md).

## Development

```sh
./scripts/check.sh
```

This runs source/provenance checks, Go race tests and vet, Swift tests, an ad-hoc
candidate build, and preview IPC tests. It never registers a helper, enrolls an
account, or activates networking. See [contributing](CONTRIBUTING.md) and
[release maintenance](docs/RELEASING.md).

## Source and license

Tailmulti's code is BSD-3-Clause. Adapted code retains its original notices.
See [LICENSE](LICENSE), [NOTICE](NOTICE), and [source adaptations](engine/ADAPTATIONS.md).
The pinned, unmodified Tailmix reference is under `upstream/`; its installer,
updater, and release scripts are not part of Tailmulti's product.

Tailmulti is independent of Tailscale and Tailmix.
