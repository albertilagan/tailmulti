# Changelog

## 0.5.0

- Downloadable app: releases include a Developer ID signed, notarized and stapled `Tailmulti-<version>-macOS.zip`.
- The background service binds its owner to the user signed in at the Mac's screen when it first starts, instead of a user ID embedded at build time. Other users get a clear "set up for another user account" message.

## 0.4.4 source prerelease

- Prepare source prereleases with CI, complete snapshot verification, source-only archives and checksums.
- Add local signing-certificate selection and short build/setup instructions.
- Preserve project, upstream and dependency license notices in app bundles.
- Replace private session and infrastructure details with public documentation; keep local agent instructions out of source exports.
- Install Go explicitly in CI, refuse duplicate release drafts, and leave Tailscale updates to manual provenance review.
- Native cascading menu with per-account device search and hostname/address copy actions.
- Keep the app running after closing the last window.
- Defer menu updates during AppKit tracking and reuse device rows while searching. Live crash-fix acceptance is pending.

## Earlier local development

- 0.4.3: sign-in-first account creation, SDK 27.2 and minimum macOS 26.
- 0.4.2: one global switch with DNS restoration and saved-account reconnection.
- 0.4.1: Tailscale Services and automatic DNS-port conflict handling.
- 0.4.0: automatic translation of ordinary DNS answers for known tailnet destinations.

These versions describe local development history, not previously published releases.
