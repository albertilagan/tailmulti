# Changelog

## 0.6.1

- App icon: two accounts' networks as blue and teal groups in one dot grid, replacing the blank placeholder. It is drawn by `scripts/make-icon.swift` during the build.
- The update feed rebuilds from the default branch when a release is published or edited.

## 0.6.0

- **Automatic updates** with Sparkle: **Check for Updates…** in the app menu, menu bar menu and Settings, plus a daily check that can be turned off. Updates must be signed with Tailmulti's update key and notarized.
- After an update, the app restarts the background service into the new version and turns Tailmulti back on if it was on.
- The service reports its build and accepts an owner-only restart request.
- Updating to 0.6.0 itself is manual (earlier versions have no updater); restart the Mac afterwards so the service runs the new version.

## 0.5.2

- **Add Account** works while Tailmulti is off: signing in turns it on first, in one step, instead of requiring the switch beforehand.

## 0.5.1

- Setup offers **Reinstall Background Service** when macOS reports the service as allowed but it never answers, for example after an earlier copy of the app was deleted or moved.

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
