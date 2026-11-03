# Implementation plan

## Implemented architecture

- Adapt focused Tailmix packet and Tailscale integration packages into `engine/`, preserving licenses and exact provenance. Keep the complete upstream snapshot unmodified.
- Own account lifecycle, immutable routing plans, persistent synthetic addresses, DNS coordination, host integration and authenticated local control.
- Keep one Tailscale engine and identity directory per account. Do not bridge accounts. Ambiguous or unavailable mappings fail closed.
- Use one macOS utun interface, account-specific packet translation, owned DNS state and synthetic-address reject guards.
- Resolve full tailnet names and Services. Translate ordinary DNS answers pointing to uniquely owned known tailnet destinations automatically.
- Provide native background-service approval, one global switch, sign-in-first account creation and a native cascading menu with account-scoped search/copy.

The app requires macOS 26 or later and is built with SDK 27.2 or later. The engine
pins Go 1.27.3 and its Tailscale dependency. No Tailmix CLI, installer, updater or
release downloader is used by the product.

## Open-source preparation

1. Separate local session/cluster details from public documentation and exclude generated output and signing material.
2. Add a project license and notice while preserving upstream and dependency licenses.
3. Verify the complete reference snapshot without relying on a private local cache.
4. Make local certificate selection and candidate builds reproducible with a short command.
5. Add CI for source checks, Go race tests/vet, Swift tests, ad-hoc bundle verification and preview IPC.
6. Add a tag-triggered workflow that creates a draft source prerelease with a deterministic source archive and SHA-256 checksum. Do not upload user-bound local app bundles.
7. Test the export and validation locally. GitHub execution requires a repository and is not claimed until a run exists.

## Before portable app distribution

Design and test first-owner provisioning without trusting caller-supplied UIDs or
weakening the root control boundary. Then add Developer ID signing, notarization,
stapling, clean-machine installation and removal tests, and an explicit update
policy. Existing embedded-owner builds are local development artifacts.

Complete live acceptance before claiming the first milestone: ordinary Safari and
SSH to both accounts simultaneously, correct remote identities, and one account's
disconnect leaving the other working without traffic fallback. Exercise collisions,
caches, sleep/wake, network changes, crashes, reboot and recovery. Confirm the
latest menu crash/search fix with real AppKit interaction. See VALIDATION.md.

Subnet routes, exit nodes and account-specific private DNS forwarding remain
separate features. See SUBNET_ROUTING_RESEARCH.md for their constraints.
