# Local installation

There is no portable, notarized Tailmulti download yet. Source prereleases are
for developers building on their own Mac. An app built for somebody else's macOS
user must not be reused: its helper embeds that builder's control UID.

## Build and open

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

Keep the approved app in place. Move it to your preferred location before allowing
the background service, and do not replace or move a registered bundle. Relocation
and boot-before-login behavior still need live acceptance testing.

## Update or remove

For a local update, build a new candidate first. In the old app, turn Tailmulti
off, then open Settings → General → Advanced → Recovery and remove the background
service. Quit the old app, open the new candidate, and allow its service. Both
accounts are temporarily disconnected during this change. Account data stays saved.

To remove the app, follow the same off/remove-service steps before deleting its
bundle. Account credentials remain in the root-private Tailmulti state directory.
There is no built-in credential-erasure or automatic downgrade flow yet. Do not
delete or restore lease/state files manually; cached addresses must not be reused
for a different identity. See [recovery details](ACTIVATION.md).
