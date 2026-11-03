# Activation and recovery

For the normal user flow, see [installation](INSTALLATION.md). Builds and preview
IPC tests do not enroll accounts or change the host network.

## Service ownership

The app uses SMAppService with a bundle-contained launch daemon. It does not open
an external Installer package or invoke a root shell. After native approval, the
service starts with networking off. The app waits for the real, UID-verified root
socket before claiming setup succeeded.

| Location | Purpose |
| --- | --- |
| `Tailmulti.app/Contents/Library/LaunchDaemons/dev.tailmulti.tailmultid.plist` | Bundle-relative service definition |
| `Tailmulti.app/Contents/Library/HelperTools/tailmultid` | Tailmulti's signed engine |
| `/Library/Application Support/Tailmulti/installation.json` | Root-owned owner policy |
| `/Library/Application Support/Tailmulti/state` | Private account identities and persistent mappings |
| `/Library/Application Support/Tailmulti/run/control.sock` | Authenticated control interface |

The local build embeds its desktop UID in the signed helper and launch arguments.
Only that UID and root may use the control API. The Swift client checks the socket
server's UID and validates the bundle's helper hash, arguments and Apple signing
team before registration. This authenticates the local user, not each client
process's code signature. Do not distribute this user-bound build to other Macs.

## Networking

The global switch creates one utun interface, synthetic destination host routes,
and a loopback DNS listener on an available privileged port. Synthetic pools are
`198.18.0.0/15` and `fd74:6169:6c6d::/64`. Broader reject routes keep cached synthetic
destinations from falling back to the ordinary network.

While on, a temporary SystemConfiguration resolver forwards ordinary DNS to the
physical network's existing servers. Uniquely owned tailnet addresses in answers
are translated automatically. Owned exact-name resolver entries protect retained
name ownership. No manual domain list is required for public DNS answers pointing
to known tailnet destinations. Browser-managed DNS, competing scoped resolvers,
private DNS forwarding and subnets have separate limitations.

Turning off stops the account engines, removes the active interface and temporary
default resolver, and removes Tailmulti-owned resolver files. It restores the
normal system resolver while retaining sign-ins, enabled preferences and synthetic
address leases. Cached synthetic addresses remain blocked for the current boot.
Other VPNs are not stopped or uninstalled automatically.

## Updates, failures and removal

Build candidates separately. Never overwrite or move a registered app bundle.
Turn Tailmulti off and remove its background service in Settings before replacing
it. Reopen the replacement and allow its service. Explain the interruption before
performing this on a live machine.

On refusal or cleanup failure, inspect the reported check or redacted diagnostics.
Do not delete another application's resolver files, routes or listeners. If the
helper cannot answer, the app cannot promise cleanup succeeded. Keep the previous
app and private state available for recovery.

A schema migration retains private metadata before adding DNS ownership fields.
Older binaries cannot necessarily decode newer state. Never blindly restore an
old snapshot after new addresses were issued. A downgrade must preserve account
identities, every retained lease and name tombstone. No automatic downgrade or
full credential-erasure procedure is implemented.

Approved service startup after a crash or reboot is inactive. Reject guards do
not survive reboot, and inactive startup does not reinstall them. Crash/reboot,
sleep/wake, network transitions and pre-login availability still require live
acceptance. See [validation](VALIDATION.md).
