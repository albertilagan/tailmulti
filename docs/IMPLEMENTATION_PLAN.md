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
6. Add a tag-triggered workflow that creates a draft prerelease with a deterministic source archive, a notarized app and SHA-256 checksums.
7. Test the export and validation locally. GitHub execution requires a repository and is not claimed until a run exists.

## Portable app distribution, 0.5.0

The service binds its owner to the console user on first start instead of a UID
embedded at build time; it never trusts a caller-supplied UID. Releases are signed
with Developer ID, notarized and stapled in a protected GitHub environment.
Clean-machine installation, multi-user and removal tests remain to be done live,
as does an explicit update policy.

Complete live acceptance before claiming the first milestone: ordinary Safari and
SSH to both accounts simultaneously, correct remote identities, and one account's
disconnect leaving the other working without traffic fallback. Exercise collisions,
caches, sleep/wake, network changes, crashes, reboot and recovery. Confirm the
latest menu crash/search fix with real AppKit interaction. See VALIDATION.md.

Subnet routes, exit nodes and account-specific private DNS forwarding remain
separate features. See SUBNET_ROUTING_RESEARCH.md for their constraints.
