# Releases

The current workflow produces **source prereleases only**. A local app embeds its
builder's UID in the helper and signed launch arguments. Uploading it would not
provide a safe, portable installer. Binary releases need a reviewed first-owner
provisioning design, Developer ID signing, notarization, and clean-Mac testing.

## Prepare a source release

1. Update `CFBundleShortVersionString` and `CFBundleVersion` in `macos/Resources/Info.plist`, and update CHANGELOG.md and validation notes.
2. Run `scripts/check.sh`. Inspect the public source, licenses and known limitations.
3. Create the archive locally with `python3 scripts/package-source.py v0.4.4`, using the matching version. It writes `dist/` and never publishes. Review its contents and checksum.
4. Once publication is authorized, commit the reviewed source and create the matching `vMAJOR.MINOR.PATCH` tag, optionally with a suffix such as `-rc.1`. Publish that tag through your normal repository workflow.
5. GitHub runs the checks and creates a **draft prerelease** containing the source archive and checksum. Inspect the draft and publish it manually when ready. The workflow never marks it as the latest stable release.

If any release or draft already uses the tag, the job fails instead of creating a
duplicate draft or overwriting assets. Delete a rejected draft before rerunning.
The workflow has no signing or notarization secrets. Only the draft-release job
gets `contents: write`; test jobs have read-only repository access. Checkout is
pinned to a full commit and does not persist credentials. Release tags enter the
shell through a quoted environment variable and are validated against Info.plist.

The source packager uses `release-manifest.json`, rejects unlisted top-level
entries and symlinks, scans for common private data, and excludes local output.
Archives have deterministic ordering, timestamps and ownership. The CI clean
checkout check also rejects accidentally committed ignored artifacts. This is a
focused check, not proof that every possible secret or private detail is absent.

## GitHub setup

Enable Actions, private vulnerability reporting and Dependabot security updates on the new repository. Protect
the default branch and release tags. Require the Checks workflow before merging.
Do not upload `.cache/`, `.local-notes/`, `build/`, `dist/`, local agent instructions
(`AGENTS.md`, `CLAUDE.md`), signing identities or account state when initializing
the repository. `.gitignore` does not remove
files already committed; inspect the initial file list before publication.

CI uses GitHub's `xcode-27` Apple silicon runner, which GitHub labels a preview
image, because `macos-latest` does not yet provide SDK 27.2. On that image
`/Applications/Xcode_27.2.app` currently points to the Xcode 27.2 Release Candidate,
not the final build; the workflow prints `xcodebuild -version` so the exact build is
recorded. Move CI to a stable `macos-*` label with final Xcode 27.2 once one exists.
The build refuses SDKs older than 27.2. Go comes from `actions/setup-go` using the
version in `engine/go.mod`. See the [runner matrix](https://github.com/actions/runner-images)
and [Xcode 27 image contents](https://github.com/actions/runner-images/blob/main/images/macos/xcode-27-arm64-Readme.md).

Draft behavior follows the [GitHub CLI release command](https://cli.github.com/manual/gh_release_create).
Workflow permissions and action pinning follow GitHub's [secure use guidance](https://docs.github.com/en/actions/reference/security/secure-use).

## Before an app release

- Replace build-time owner binding with a reviewed, authenticated first-owner flow, including multi-user and reinstallation behavior.
- Sign app and helper with the same Developer ID team, notarize and staple the distributable, and verify it on a clean Mac without development certificates.
- Complete the live acceptance matrix in VALIDATION.md, including the latest menu crash fix and account-disconnect isolation.
- Verify minimum OS support, relocation, reboot, removal, credential erasure and update recovery.
- Retain all license/notice files. Document any new dependency's redistribution requirements before bundling it.

No self-updater is implemented. Tailmix's release scripts remain immutable
reference material and must never run as Tailmulti's release process.
