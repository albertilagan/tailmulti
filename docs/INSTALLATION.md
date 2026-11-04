# Installation

## Download

Use an Apple silicon Mac with macOS 26 or later. Download `Tailmulti-<version>-macOS.zip`
from the [releases page](https://github.com/albertilagan/tailmulti/releases), check it
against the matching `.sha256` file if you like, unzip it, and move Tailmulti.app to
Applications before opening it. Release builds are signed with Developer ID and
notarized by Apple.

## Ownership

The background service has a single owner: the Mac user signed in at the screen
when the service first starts, which is normally the person who just approved it.
That user and root are the only ones who can control it. Another user on the same
Mac sees "set up for another user account" and cannot read or change its accounts.
The owner is stored in a root-owned policy file and survives updates and service
removal, so the same user can reinstall without signing in to accounts again.

To hand Tailmulti to a different Mac user, the current owner turns it off and
removes the background service, then an administrator deletes all of its saved
data, including every account sign-in:

```sh
sudo rm -rf "/Library/Application Support/Tailmulti"
```

## Build from source

Use an Apple silicon Mac with macOS 26 or later, Xcode 27.2 or later, Go, Python
3.11 or later, and a valid Apple Development certificate with its private key.
Create the certificate in Xcode Settings → Accounts → Manage Certificates if
needed. Finish Xcode's first-launch setup, then run from the source directory:

```sh
./scripts/build-local.sh
open "$(cat build/candidate-app-path.txt)"
```

The first build downloads Go 1.27.3 and the pinned modules. A candidate build
leaves any registered app unchanged. The build chooses a compatible Xcode without
changing the system's developer-directory setting. To select one explicitly:

```sh
DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer ./scripts/build-local.sh
```

With multiple valid Apple Development identities, the script lists their hashes
and asks you to choose one through an environment variable:

```sh
TAILMULTI_SIGNING_IDENTITY=YOUR_CERTIFICATE_SHA1 ./scripts/build-local.sh
```

The hash is a certificate selector, not a private key. Do not export or share the
private key. The script also accepts an existing `.cache/native-signing.json`
configuration after checking that its identity is still available.

## Connect

1. Open Tailmulti and choose **Allow Background Service**. Approve the background service in System Settings if macOS requests it. It starts with networking off.
2. Turn on **Tailmulti**. Checks run before routes and DNS change. Resolve any reported VPN conflict first. You do not need to uninstall native Tailscale.
3. Choose **Add Account**, enter a label such as Personal, then **Sign In** in your browser. Repeat for Work. Each account signs in separately and still needs any required administrator approval.
4. Use full hostnames in Safari, SSH, and other apps. Browse devices and copy hostnames or local addresses from each account's menu.

Turning Tailmulti on creates a shared virtual interface and makes Tailmulti handle
system DNS. Ordinary DNS uses your network's existing servers. Internet traffic
keeps its ordinary route. These changes affect the whole Mac.

The single Tailmulti switch turns everything off and restores normal DNS.
Sign-ins and account preferences stay saved. Turning it on reconnects the accounts
that were enabled. Closing the app window is not a disconnect action.

Keep the approved app in place. Move it to your preferred location, usually
Applications, before allowing the background service, and do not replace or move a registered bundle. Relocation
and boot-before-login behavior still need live acceptance testing.

## Updates

From 0.6.0, release builds update themselves with [Sparkle](https://sparkle-project.org).
They check daily for a newer published release, and **Check for Updates…** is in the
app menu, the menu bar menu and Settings. Updates are verified with Tailmulti's update
signing key and Apple's notarization before they are installed. After an update the
app restarts the background service into the new version and turns Tailmulti back on
if it was on, which briefly disconnects every account. Settings → General can turn off
automatic checks.

Updating to 0.6.0 from an earlier version is manual: replace the app, then restart the
Mac, or remove and allow the background service in Settings → General → Advanced →
Recovery, so the service runs the new version.

Builds made from source never update themselves.

## Remove

To remove the app, turn Tailmulti off, then open Settings → General → Advanced →
Recovery and remove the background service before deleting its bundle. Account credentials remain in the root-private Tailmulti state directory.
There is no built-in credential-erasure or automatic downgrade flow yet. Do not
delete or restore lease/state files manually; cached addresses must not be reused
for a different identity. See [recovery details](ACTIVATION.md).
