# Security

Tailmulti is development software. It runs a privileged network service and has
not completed an independent security audit or the full live acceptance matrix.
Only the latest development source is maintained; there is no supported stable
binary release yet.

## Reporting

Use the repository's Security → Report a vulnerability entry when private
vulnerability reporting is enabled. If it is unavailable, open a public issue
asking for a private contact without including exploit details or private data.
Repository owners must enable private vulnerability reporting before publication.

Do not attach account state, keys, login URLs, packet captures with payloads,
signing material, or unredacted peer information. Describe the version, trigger,
expected boundary and observed behavior. Redacted diagnostics report counts and
health without peer names.

## Boundaries

Accounts keep separate identities and state. Packet selection and DNS ownership
must not route a destination through a different account when its owner becomes
unavailable. Ambiguous destinations fail closed.

The production control API checks kernel peer credentials for every request and
allows one configured desktop UID plus root. It trusts processes running as that
user. It does not authenticate each client's code signature or defend against root.
The app cannot supply shell commands, arbitrary paths, auth keys or a raw LocalAPI
request through this protocol.

Builds, tests and preview mode do not activate networking. Native approval and
the app's switch control live setup. See [activation and recovery](docs/ACTIVATION.md)
and [known validation gaps](docs/VALIDATION.md).
