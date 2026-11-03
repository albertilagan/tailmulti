# Tailmulti project brief

## Goal

Many people belong to more than one tailnet, for example a personal account and a work account. Tailmulti lets one Mac reach both tailnets at the same time while keeping the accounts independent.

Account switching, machine sharing across accounts and per-application SOCKS configuration are not acceptable as the primary solution. Tailmulti is a new routing engine, partly based on Tailmix, with a native macOS GUI and menu bar. It is independently built and maintained and does not require an installed Tailmix CLI or Docker.

Working name: **Tailmulti**. Source workspaces: `engine/` and `macos/`.

## Expected behavior

- Add multiple accounts through their normal Tailscale authentication flows.
- Keep each account connected with separate keys, persisted state, peers, and access rules.
- Use full MagicDNS names from ordinary applications without proxy settings or per-server SSH entries.
- Select the right account for every destination, including when tailnets reuse canonical Tailscale addresses.
- Keep existing remote ACLs and device approval effective. Do not claim that custom client enrollment bypasses company client/device policy.
- Support a native menu bar for connection state and quick actions, plus a settings window for accounts, devices, route policy, and diagnostics.
- Keep public internet traffic on the normal connection unless the user explicitly selects an exit node.
- Fail closed for ambiguous or unavailable tailnet routes. Never silently send a personal destination through the company profile or vice versa.

## Architecture and validation scope

1. A Go daemon owns account engines, route policy, local virtual addresses, DNS, and network lifecycle.
2. A single host virtual network interface receives selected destination traffic.
3. A mapping layer converts unique local peer addresses into profile-specific canonical addresses before handing packets to the selected Tailscale engine; return traffic is translated back.
4. DNS resolves profile-specific names into the local address space. Subnet IP routing and suffix-based DNS forwarding have explicit conflict policies.
5. A narrow, authenticated local control interface connects the unprivileged SwiftUI app to the daemon. The app never reads account key material or receives arbitrary root execution capability.
6. Native installation and lifecycle management start and stop the networking service safely and restore host network configuration on failure or uninstall.

The development implementation uses a privileged `utun` daemon. Network Extension remains a possible future packaging approach. Public `tsnet.Server` supports multiple instances but does not itself provide the complete packet multiplexing interface used by Tailmix. See VALIDATION.md for tested behavior and the live acceptance gaps.

## Implementation sequence

1. Inspect the pinned Tailmix implementation and current primary documentation. Decide what to adapt and record licensing, maintenance cost, platform requirements, and known limits.
2. Establish the engine and app workspaces, source provenance, a supported pinned toolchain, and reproducible local builds. Remove or replace upstream-specific update and install behavior in adapted code.
3. Implement the independently owned engine: profiles, address allocation, routing, packet translation, DNS, and lifecycle. Preserve useful upstream tests and add tests for changes.
4. Implement the authenticated control interface and native app: account enrollment, per-account connect/disconnect, status, devices, conflicts, and menu-bar controls. Use real engine state rather than mock success states.
5. Package the app and helper. Document signing, notarization, installation, permissions, uninstall, and recovery.
6. Validate locally, then perform live two-tailnet tests when credentials and host-network activation are available. Fix failures before claiming the simultaneous-access milestone.

## First working milestone

Two real accounts remain connected and normal Safari and SSH reach a peer in each tailnet by full hostname without per-app proxy settings. Each peer observes the correct account identity. Disconnecting one profile does not break the other, and traffic for the disconnected profile does not escape through another profile or the ordinary network.

Additional required validation: duplicate short names, overlapping canonical IPv4/IPv6 addresses, conflicting subnet routes, DNS cache behavior, reconnect/re-authentication, sleep/wake, network changes, daemon restart/crash, application restart, and cleanup after uninstall. Confirm support limits for TCP, UDP, ICMP, inbound connections, exit nodes, and subnet routes explicitly.
