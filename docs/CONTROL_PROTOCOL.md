# Local control protocol v1

Transport: HTTP/1.1 over a Unix-domain socket, Host `tailmulti`. Production path is `/Library/Application Support/Tailmulti/run/control.sock`. Every request authenticates the kernel peer UID against the explicitly configured desktop owner or root. The client verifies that the server UID is root, or the current user for an explicitly selected preview socket. Production socket parents are root-owned and not writable by the app. On its first approved launch the service binds the user signed in at the console and exclusively creates the root-owned policy before opening control. Later launches use the stored owner. No client can claim or change ownership through IPC.

This trusts the selected user's processes. It does not authenticate a code-signing identity, defend against root, or authorize arbitrary local users. There is no TCP endpoint, bearer token, raw LocalAPI proxy, auth-key input, arbitrary path, shell command or update API. Browser Origin headers are rejected. Request bodies are capped at 4 KiB, reject unknown fields and must contain exactly one JSON value. Responses carry Content-Length and Cache-Control: no-store.

| Method and path | Request | Response |
| --- | --- | --- |
| GET /v1/status | none | version, mode, health, accounts, devices, conflicts |
| GET /v1/diagnostics | none | version, mode, health and counts only |
| GET /v1/setup | none | version, ready, checks with id/state/title/detail; read-only |
| POST /v1/network/start | `{}` | repeats host checks, then starts the network session; idempotent while active |
| POST /v1/network/stop | `{"restoreDNS":false}` | stops networking while keeping control available; true also removes verified owned resolver files |
| POST /v1/accounts | `{"name":"Home"}` | disabled account, with generated opaque ID |
| POST /v1/accounts/{id}/connect | `{}` | `{"ok":true}` or error; starts only that identity |
| POST /v1/accounts/{id}/disconnect | `{}` | disables desired state, removes mappings, stops identity |
| POST /v1/accounts/{id}/remove | `{}` | disables and hides account; retains leases and private identity files |
| POST /v1/accounts/{id}/login | `{}` | transient HTTPS browser URL for a pending login |

Login responses are sensitive and must never be logged. General status and diagnostics never include login URLs, key material or underlying state paths. Device names and local addresses are deliberately available in the authenticated UI; redacted diagnostics contain neither names nor profile IDs. Errors are generic and omit underlying credential-bearing engine errors.

Connect in preview mode always returns HTTP 409 and leaves the account disabled. The app never reports simulated peers or successful connectivity in preview. HTTP 403 means peer authorization or request origin/Host checks failed. HTTP 400 means malformed input. HTTP 409 means the requested operation could not be completed. IDs cannot be supplied at creation and never derive from account names.

Modes are `preview`, `inactive` and `active`. The installed `--service` always starts inactive, including after a crash or reboot. Account names can be saved while inactive, but connecting and login retrieval require active networking. The preview backend has no network-control capability and returns 409 for these endpoints. A failed network start returns to inactive service when account state can be reloaded. Packet-worker failure does the same without dropping the authenticated control socket. Neither operation changes the configured owner.

Stopping preserves account keys and desired connection settings. Reject guards and resolver files remain by default. Explicit `restoreDNS:true` removes only regular, correctly owned resolver files with exact expected Tailmulti contents. Foreign entries remain untouched, modified owned files cause an error, and guard routes are never removed by this API. The owner UID controls who may manage the service; host routes affect applications across the Mac.
