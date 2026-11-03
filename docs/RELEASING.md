# Releases

A version tag produces a **draft prerelease** with two downloads: a Developer ID
signed, notarized and stapled `Tailmulti-<tag>-macOS.zip`, and a deterministic
source archive, each with a SHA-256 checksum. The app works for any Mac user
because the service binds its owner when it first starts.

## Prepare a release

1. Update `CFBundleShortVersionString` and `CFBundleVersion` in `macos/Resources/Info.plist`, and update CHANGELOG.md and validation notes.
2. Run `scripts/check.sh`. Inspect the public source, licenses and known limitations.
3. Optionally create the source archive locally with `python3 scripts/package-source.py vX.Y.Z`, using the matching version. It writes `dist/` and never publishes. Review its contents and checksum.
4. Once publication is authorized, commit the reviewed source and create the matching `vMAJOR.MINOR.PATCH` tag, optionally with a suffix such as `-rc.1`. Publish that tag through your normal repository workflow.
5. GitHub runs the checks, waits for approval of the `release` environment, signs and notarizes the app, and creates a **draft prerelease** with the app, source archive and checksums. Download the app from the draft, test it, and publish manually when ready. The workflow never marks it as the latest stable release.

If any release or draft already uses the tag, the job fails instead of creating a
duplicate draft or overwriting assets. Delete a rejected draft before rerunning.
Signing and notarization secrets exist only in the `release` environment, which
only the app job uses; it imports the certificate into a temporary keychain and
deletes it afterwards. Only the draft-release job gets `contents: write`; other
jobs have read-only repository access. Checkout is
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

## Signing secrets

Create a GitHub environment named `release` with yourself as a required reviewer and
deployments limited to `v*` tags. Add these environment secrets:

| Secret | Value |
| --- | --- |
| `MACOS_CERT_P12` | Base64 of a `.p12` export of the Developer ID Application certificate and private key |
| `MACOS_CERT_PASSWORD` | The `.p12` export password |
| `AC_API_KEY_P8` | Contents of an App Store Connect API key `.p8` file, used by `notarytool` |
| `AC_API_KEY_ID` | That key's ID |
| `AC_API_ISSUER_ID` | The key's issuer ID |

The workflow requires the certificate's team to be `ZR86C4659Z`; change
`TAILMULTI_TEAM_ID` in `release.yml` if the signing team changes.
`scripts/package-app.sh` runs the same build, signing and notarization locally
when these values are provided as `TAILMULTI_DEVELOPER_ID`, `TAILMULTI_TEAM_ID` and
`NOTARY_KEY_PATH`/`NOTARY_KEY_ID`/`NOTARY_ISSUER_ID`.

## Before a stable release

- Verify the downloaded app on a clean Mac without development certificates, including a second Mac user being refused and ownership reset.
- Complete the live acceptance matrix in VALIDATION.md, including the latest menu crash fix and account-disconnect isolation.
- Verify minimum OS support, relocation, reboot, removal, credential erasure and update recovery.
- Retain all license/notice files. Document any new dependency's redistribution requirements before bundling it.

No self-updater is implemented. Tailmix's release scripts remain immutable
reference material and must never run as Tailmulti's release process.
