# Native macOS setup research

Historical design research. The Installer-package proposal below was superseded
by bundle-contained SMAppService registration. Use [installation](INSTALLATION.md)
and [activation](ACTIVATION.md) for the implemented flow. The research originally
targeted macOS 14; the current app requires macOS 26 and SDK 27.2 or newer.

Research date: 2026-11-03. Scope: Tailmulti's own Go daemon and unsandboxed macOS 14+ app. This is a docs-only investigation. Nothing was built, signed, installed, registered, uploaded, enrolled, or activated. No host networking was inspected or changed. Apple sample archives were read in memory, not executed. No signing identity names, keys, authentication URLs, credentials, or peer identifiers are recorded here.

## Follow-up: user rejected the package wizard

The Installer recommendation below records the initial engineering tradeoff. The user subsequently rejected that UI. Tailmulti 0.3 replaces it with SMAppService and a local Apple Development-signed candidate. App and helper signature verification succeeds locally; registration/approval remains a separate live experiment. The SDK notarization requirement is unchanged. There is no claim that local signing proves an exemption, no notarization upload and no fallback to the rejected Installer flow. Current behavior and test evidence are in `HANDOFF.md` and `VALIDATION.md`.

## Recommendation for local development

Use a locally staged Installer package for the first native setup implementation. The app opens that package in macOS Installer; the user approves installation there. The package installs our own daemon, a fixed system launchd plist, and an installation policy bound to the intended desktop owner. The service starts in authenticated, control-only mode. A separate **Activate networking** action explains and authorizes utun, routes, and resolver changes before the daemon performs them. No Terminal commands should be required in that user flow.

This is an implementable design using Apple's documented package and launchd mechanisms, not a claim that Tailmulti has already passed Installer or networking tests. Apple DTS identifies installer packages as a supported way to install a persistent privileged daemon. Package signing is optional in the local `productbuild` documentation. Neither fact guarantees that a particular unsigned package will pass the target machine's Gatekeeper or management policy. [Apple privilege guidance][privileges], [local productbuild manual][productbuild], [local launchd manual][launchplist]

Do **not** describe the current ad-hoc app as an established offline SMAppService solution. The installed SDK explicitly requires notarization for apps containing LaunchDaemons. Apple DTS also recommends the same Apple-issued signing identity for app and helper, and warns about ad-hoc identity problems. Apple's development walkthrough demonstrates team-signed local testing without an explicit notarization step, but that is insufficient evidence for a notarization exemption across macOS 14 through the current host. Keep development-signed SMAppService as a separately approved experiment, not a dependency of the first local setup route. [local SMAppService header][smheader], [Apple DTS signing advice][signing-advice], [Apple DTS walkthrough][getting-started]

The user reports a read-only identity check found Developer ID Application and Apple Development identities. This research did not inspect or use them. Those certificate classes are distinct from Developer ID Installer, which signs installer packages for distribution. A suitable Installer identity has not been established. Stage an unsigned local package if needed and label it accurately; do not invent an identity, call the package Developer ID signed because its payload is signed, or upload it for notarization. [Apple certificate types][certificates]

## Evidence and project gaps at the time of research

Read before this recommendation: `HANDOFF.md`, `docs/PROJECT_BRIEF.md`, `docs/RESEARCH.md`, `docs/IMPLEMENTATION_PLAN.md`, `engine/ADAPTATIONS.md`, and `docs/ACTIVATION.md`. The following are observations of our source, not platform promises.

| Current implementation | Consequence for setup |
| --- | --- |
| [`scripts/build-app.sh`](../scripts/build-app.sh) places our daemon in `Contents/Library/HelperTools/tailmultid` and ad-hoc signs helper and app. | There is no installer, LaunchDaemon plist, or stable developer identity established by that script. |
| [`engine/cmd/tailmultid/main.go`](../engine/cmd/tailmultid/main.go) accepts privileged `--activate --owner-uid`; preview rejects root. | There is currently no root control-only mode. Merely putting the current activation command in launchd would activate networking at installation/startup. |
| Activated startup opens DNS and the host runtime before `control.Listen`. | Setup failures can prevent the control socket from appearing. Move control readiness before network activation. |
| [`engine/internal/control/server.go`](../engine/internal/control/server.go) authorizes the configured owner UID or root using kernel peer credentials. Its socket is mode `0666`. | The socket's mode is not the authorization boundary. Preserve authorization before every operation, including status, login and future activation. No existing endpoint provisions an owner or activates the host runtime. |
| [`ControlClient.swift`](../macos/Sources/TailmultiKit/ControlClient.swift) defaults to the fixed system socket and checks server UID 0. | The app can discover a newly installed real daemon without being relaunched. |
| [`TailmultiApp.swift`](../macos/Sources/Tailmulti/TailmultiApp.swift) polls status every three seconds, even with the window closed. Its client is selected once at initialization. | Production polling already supports reconnection. An app launched with the preview socket needs an explicit client switch and state reset, not a false setup-success screen. |

The table above records the pre-implementation source. The subsequent Installer, control-only service and native setup changes are tracked in `HANDOFF.md`; they have not been installed or activated.

The existing daemon is independently built from maintained engine code. Neither proposed route invokes an installed Tailmix executable. No change to the cryptography, transport, or account separation is required for packaging. [engine adaptations](../engine/ADAPTATIONS.md)

## What SMAppService actually provides

The local `SMAppService.h` is the strongest API evidence inspected. It is available from macOS 13, including the project's macOS 14 minimum. Its documented contract includes the following. [local SMAppService header][smheader]

| Topic | Documented behavior and Tailmulti implication |
| --- | --- |
| Signing | Apps using these APIs must be code signed; the header says apps containing LaunchDaemons must be notarized. Successful ad-hoc `codesign --verify` alone does not establish eligibility. |
| Plist location | `SMAppService.daemon(plistName:)` reads a plist under the calling app's `Contents/Library/LaunchDaemons`. Pass the filename including `.plist`. |
| Executable location | `BundleProgram` is relative to the app bundle, for example `Contents/Library/HelperTools/tailmultid`. It is not relative to the plist directory or the working directory. |
| App location | Apple recommends `/Applications` so the bundle is available before login. This is a recommendation, not a documented universal registration requirement. A user home, removable volume, build output, or translocated download is a poor persistent boot location. |
| Approval | A daemon is not bootstrapped until an administrator approves it. Approved daemons are bootstrapped on subsequent boots. Approval belongs to macOS; the app must not click through it or imitate it. |
| Updates | Re-register when the executable or plist changes. The header recommends unregistering first if the executable changes. The asynchronous unregister completion is the documented point after which re-registration is safe. |
| Removal | Unregister prevents future launches and kills a running service. The synchronous call does not wait for process reaping. It is not a graceful network cleanup protocol. |

Apple's migration article associates bundled jobs with the app in System Settings and provides `openSystemSettingsLoginItems()` to direct the user to approval. It does not provide an owner-UID handoff or an API for inserting runtime arguments into a registered plist. [Apple migration article][migration]

### Signing, local development, and notarization

There are three separate questions: whether code has a structurally valid signature, whether Service Management accepts its identity and approval, and whether downloaded software passes distribution policy. Apple's code-signing documentation explicitly describes subsystem-specific trust policies. Do not collapse these into one `codesign` check. [Apple TN2206][codesigning]

Apple DTS's September 2025 walkthrough used Xcode 26.0 on macOS 15.6.1, selected the same development team for app and daemon, and demonstrated registration and authenticated approval. Its minimal job required a manual start because it deliberately omitted an automatic launch trigger. We must add an appropriate trigger for a no-CLI product flow. This walkthrough is useful development evidence, not a test of our SwiftPM/Go bundle or macOS 14 and 27. [Apple DTS walkthrough][getting-started]

The SDK's notarization statement was found in both the Xcode macOS 27.0 SDK and Command Line Tools macOS 26.5 SDK. It is not safe to dismiss it as a new beta-only comment. The development walkthrough and the SDK leave an unresolved distinction between local development enforcement and the general documented daemon requirement. Do not silently reinterpret the header as applying only to distribution. [local SMAppService header][smheader], [26.5 header][smheader265]

An Apple Development identity is therefore a sensible candidate for a future local SMAppService experiment. Developer ID Application plus notarization is the documented distribution direction. Neither available identity establishes a tested, supported **offline** SMAppService daemon path on this host. No signing, registration, notarization request, or approval trial was performed here. [Apple certificate types][certificates], [Apple Developer ID guidance][developer-id]

A Developer ID Application identity cannot substitute for Developer ID Installer. A signed package also does not notarize its embedded app or bypass SMAppService's checks if the app later uses that API. Apple's notarization guidance distinguishes executable signing from package signing. [Apple notarization session][notarization]

### Bundle integrity and arguments

For SMAppService, finish the executable, plist, and other sealed resources before signing. Do not rewrite the signed bundle at first launch to insert the current UID, a token, or an activation preference. Apple describes the contained plist as protected by the bundle's code signature; modifying sealed contents invalidates that protection. [Apple package sample][sample], [Apple TN2206][codesigning]

Tailmulti policy should reject a mutable development checkout as the permanent root executable source. Use a stable installed bundle, validate paths and signatures, and replace complete bundles through an approved update process. A signature is not a filesystem write lock, and `/Applications` alone does not prove safe ownership or permissions. Root-owned, non-writable installed code is an additional project policy, not an SMAppService API precondition established by these sources. [Apple TN2206][codesigning], [local SMAppService header][smheader]

`ProgramArguments` is an argv array. Supply argv[0] and each argument separately; there is no shell expansion of `$(id -u)`, `$USER`, `~`, or quoted command strings. `UserName` sets the daemon's execution identity; it does not set Tailmulti's desktop owner. Omitting it for a system daemon preserves root execution. `BundleProgram` works only for SMAppService-installed plists; a package-installed legacy daemon needs an absolute `Program` or executable path in `ProgramArguments`. [local launchd.plist manual][launchplist], [Apple DTS correction][bundleprogram]

This is a proposed SMAppService job, **not compatible with the current daemon until `--service` is implemented**:

```xml
<dict>
  <key>Label</key><string>dev.tailmulti.tailmultid</string>
  <key>BundleProgram</key>
  <string>Contents/Library/HelperTools/tailmultid</string>
  <key>ProgramArguments</key>
  <array><string>tailmultid</string><string>--service</string></array>
  <key>KeepAlive</key><true/>
</dict>
```

`KeepAlive=true` requests a continuously running service and implies an initial run; launchd throttles rapid failures. This is a deliberate local design choice for the existing self-created Unix listener. Adding `MachServices` or `Sockets` alone would not adapt our listener to launchd activation. Future demand-based startup would require corresponding IPC/file-descriptor handling. The service must remain inactive and responsive on configuration or network conflicts instead of repeatedly exiting. [local launchd.plist manual][launchplist], [Apple launchd guide][launchd-guide]

### Approval states are not networking states

The first column below is the SDK contract; the second is proposed UI behavior. [local SMAppService header][smheader]

| `SMAppService.Status` | Tailmulti behavior |
| --- | --- |
| `.notRegistered` | Offer setup after explaining persistent root execution. Register only on an explicit user action. |
| `.requiresApproval` | Explain pending or revoked background permission and offer to open System Settings. Do not automatically retry registration in a loop. |
| `.enabled` | The service is eligible to run. Probe authenticated IPC and protocol/version/installation identity; do not claim it is alive or networking is active. |
| `.notFound` | Report missing or invalid packaged service and provide repair guidance. Do not treat it as an invitation to execute a privileged shell. |
| Unknown future value | Show an unsupported state and preserve diagnostics without guessing success. |

`register()` may throw for denied launch, an invalid signature, or already-registered state. Re-read `status` after the call and after errors. Apple's walkthrough specifically shows a registration error followed by a system approval notification. A thrown error alone is not proof that no pending registration exists. [local SMAppService header][smheader], [Apple DTS walkthrough][getting-started]

## Recommended local Installer package

Everything in this section is a proposed Tailmulti design. It uses documented package, launchd, and IPC mechanisms but still requires implementation and approved live tests.

### Payload and installation boundary

Package the independently built daemon from the same local build as the app. Use a nonrelocatable, system-volume-only package with a stable package identifier. The Installer distribution can disable home-directory and alternate-volume installation. `pkgbuild` can encode root ownership without changing the staging files' ownership. These are packaging operations, not installation. [local pkgbuild manual][pkgbuild], [Apple Distribution XML reference][distribution]

Proposed fixed payload:

| Installed path | Policy |
| --- | --- |
| `/Library/PrivilegedHelperTools/dev.tailmulti.tailmultid` | Regular executable owned by root, mode `0755`, no group/other write permission, never a symlink into `build/` or the app. |
| `/Library/LaunchDaemons/dev.tailmulti.tailmultid.plist` | Root-owned `0644`, fixed absolute `Program`, argv containing only service mode, no shell or user-controlled executable path. |
| `/Library/Application Support/Tailmulti/installation.plist` | Root-owned `0600`, intended owner UID, schema version, installation identity, and immutable policy. Separate from credentials and mutable runtime state. |
| `/Library/Application Support/Tailmulti/state` | Existing root-private runtime state location. Preserve identities, leases, and suffix ownership across repair and upgrade. |
| `/private/var/run/tailmulti/control.sock` | Existing control endpoint under validated root-owned parents. Recreated by the daemon. |

The app may contain the installer as a resource and open it through AppKit's document-opening API. Prefer the variant that explicitly opens the package with the system Installer application. It must not launch the command-line `installer` with guessed privileges. Opening a package means only that Installer was launched; it is not evidence that installation succeeded. Confirm success through the installed daemon's authenticated handshake. Apple's sample uses the Installer GUI for package approval. [local NSWorkspace header][workspace-header], [Apple package sample][sample], [local installer manual][installer]

The package's root daemon runs from its installed root-owned copy. This removes the SMAppService dependency on the app bundle's boot-time location. Installing the GUI in `/Applications` remains a sensible product convention, but is not how this legacy job finds its executable. Do not replace the running GUI as part of a helper-only setup package. [local launchd.plist manual][launchplist]

The corresponding proposed package-installed job uses the following keys. As above, `--service` requires implementation before this is usable. No owner value is accepted from app-controlled launch arguments; it comes from installed policy.

```xml
<dict>
  <key>Label</key><string>dev.tailmulti.tailmultid</string>
  <key>Program</key>
  <string>/Library/PrivilegedHelperTools/dev.tailmulti.tailmultid</string>
  <key>ProgramArguments</key>
  <array><string>tailmultid</string><string>--service</string></array>
  <key>KeepAlive</key><true/>
</dict>
```

Package hooks are a supported Installer mechanism. If used, keep them fixed and auditable: validate the destination, install/validate policy, and bootstrap only the exact system job. Use absolute tool paths and fixed arguments. Do not download code, source files from the calling user's directory, construct shell commands from account labels, register a second SMAppService job, or activate networking. The elevated execution comes from the user-approved Installer transaction, not AppleScript or `AuthorizationExecuteWithPrivileges`. [local pkgbuild manual][pkgbuild], [Apple privilege guidance][privileges]

For an install that becomes usable immediately, the package needs an approved bootstrap step; copying the plist alone principally establishes boot-time discovery. Subsequent startup uses launchd. The first implementation should refuse upgrades while networking is active and require deactivation first, rather than attempt an untested live replacement. Do not claim Installer scripts provide automatic transaction rollback. [Apple launchd guide][launchd-guide], [local pkgbuild manual][pkgbuild]

### Safe owner provisioning for this local build

Bake the **intended desktop owner's numeric UID** into the local package at build time. Obtain it from the explicitly selected local build context, validate that it is a real non-root user, and display the intended owner in the app and Installer before approval. Do not derive it at installation from Installer's UID, `SUDO_USER`, the foreground console user, or an unauthenticated IPC request. Fast user switching and an administrator approving for a standard user make those different identities.

The privileged installation, after user approval, is the trust boundary that establishes the owner policy. Kernel peer credentials then establish which process UID is making each request. These are different decisions. `getpeereid` identifies the peer; it does not establish who approved installation or who should own Tailmulti. [local getpeereid manual][getpeereid], [current control implementation](../engine/internal/control/server.go)

Required implementation rules:

1. Check for an existing owner policy **before replacing payload**. Reject a different owner, unknown schema, symlink, unsafe parent, or insecure file. A postinstall-only owner check is too late if the payload already overwrote policy.
2. On first install, create the policy atomically in a root-owned directory with no user-writable ancestors. Reinstall for the same owner is idempotent and preserves state. Installation hooks must not truncate an existing policy to manufacture a match.
3. At every daemon start, validate the policy and its parents. Reject mismatch between any build-bound owner and the installed policy. Never treat a missing policy as permission for the first socket client to claim ownership.
4. Expose no ordinary owner-change endpoint and accept no owner override from an environment variable or mutable app preferences. `--owner-uid` may remain a separately documented foreground development interface; the installed `--service` route must use the installed policy.
5. Keep the current owner-UID-or-root trust model explicit. Any process running as that owner can exercise the allowed protocol. This is not proof that the caller is the Tailmulti app. Signature/audit-token validation can narrow that later.
6. Refuse ordinary reassignment, including through a newly generated package. Administrative account deletion, UID reuse, or transfer needs a reviewed migration policy. Consider storing and checking the account's stable directory identity in addition to UID; numeric UID alone is not permanent ownership across account recreation.

These rules are proposed security requirements based on our existing peer-UID interface, not capabilities supplied automatically by Installer. They avoid adding a second admin-authentication flow merely to discover an owner already fixed by the approved local package.

For a future generic SMAppService package, the missing owner handoff still needs a design. One supported building block is Authorization Services: a product-specific right, authenticated in the GUI and validated by the privileged daemon using an external authorization form. Bind the intended owner to authenticated peer credentials, make provisioning atomic, and reject reassignments. Authorization forms are transient capabilities and must never be persisted or logged. This does not require the deprecated privilege-execution function. It is additional implementation work, not a feature of SMAppService approval. [Apple Authorization Services tasks][authorization-tasks], [local Authorization headers][authorization], [local AuthorizationDB header][authorizationdb]

### Root control-only service and explicit activation

Introduce `--service` with a root-only, fixed-path startup path. It loads the owner policy and state lock, creates authenticated control, and reports `installedInactive`. It must not call `host.Open`, bind the DNS listener, create a Tailscale account engine, or restore enabled profiles on first installation. This requires refactoring the ordering in [`main.go`](../engine/cmd/tailmultid/main.go).

Add narrow typed operations such as setup status, preflight, activate, and deactivate. Names are proposals. They must accept neither shell commands nor arbitrary paths. Preflight reports conflicts read-only; activation reruns those checks immediately before mutation because conditions can change. Only the authorized owner or root may invoke them. Return enough installation/build/protocol information to detect a wrong or stale helper without exposing peers or account secrets.

Before Activate, explain the concrete effects already documented in [ACTIVATION.md](ACTIVATION.md): fixed state/socket locations, loopback DNS listener, one utun, synthetic address pools, retained reject guards, and marked resolver files. Installation approval does not consent to those changes. Do not stop another VPN or replace global DNS automatically.

Recommended first development behavior: launchd restarts the **control service**, but every fresh process starts networking inactive. Keep account credentials and account preferences persisted; gate restoration of their enabled engines behind an explicit activation for that process. This avoids silently converting installation persistence into network activation after a crash or reboot. A later clearly labeled automatic-resume preference may persist that consent after recovery tests establish safe behavior.

The daemon should keep IPC alive when activation fails so the app can show the conflict and retry after the user resolves it. Deactivate should stop account engines, withdraw packet delivery, and close utun while respecting the existing retained-guard and resolver policy. A package receipt, successful bootstrap, or socket connection must never be presented as successful live connectivity.

### What can be staged without a distribution identity

Apple's `productbuild` manual explicitly makes archive signing optional and describes signing later with `productsign`. This supports producing a local unsigned package for inspection without notarization or an Installer identity. It does not establish that every downloaded, quarantined, or managed-machine package will install without additional policy intervention. Stop at that boundary if blocked; do not disable Gatekeeper or remove quarantine as an automatic setup step. [local productbuild manual][productbuild]

Use separate labels for the outer package, helper executable, and app signatures. If the package is unsigned but the helper is development-signed, say exactly that. A locally signed or ad-hoc payload is still a local development artifact. Unsigned packaging provides no trusted publisher guarantee; the local build and review context, followed by the explicit Installer decision, is the limited provenance model. [Apple certificate types][certificates], [Apple TN2206][codesigning]

The reported code-signing identities can be used only under appropriate authorization in a later build task. No private-key export is needed. For a strictly offline build, be explicit about timestamp behavior: `productbuild` documents that trusted timestamping contacts a server and can be disabled for local staging. Do not turn an offline experiment into a notarization upload. [local productbuild manual][productbuild]

## Continuing enrollment without restarting the app

Proposed state sequence:

```text
not installed -> Installer opened -> waiting for authenticated helper
              -> installed inactive -> preflight -> explicit activation
              -> real engine ready -> add/connect account -> browser sign-in
```

The same app-level coordinator should observe both installer/service state and daemon state. On return to the foreground, and with bounded polling while setup is visible, reconnect to the fixed socket and validate root peer UID plus protocol/build/installation policy. Do not block the main actor, assume Installer success from its process exit, or repeatedly reopen Installer. If the user cancels, keep setup pending and offer retry.

Once the real daemon reports active and healthy, enable the existing Add, Connect, and browser sign-in actions. The app already requests a transient login URL through authenticated IPC and opens it with `NSWorkspace`; it need not restart to do so. Keep separate browser sign-ins for each account. Preview-only labels must not be silently imported as authenticated accounts. If the app started in preview, replace its immutable client explicitly, cancel old requests, and clear stale account selection/status first. [current app model](../macos/Sources/Tailmulti/TailmultiApp.swift), [current client](../macos/Sources/TailmultiKit/ControlClient.swift)

For a future SMAppService flow, substitute registration and System Settings approval for the Installer step. Re-read `status` when the app becomes active and during pending approval, then perform the same authenticated daemon handshake. `.enabled` alone must not enable enrollment. [local SMAppService header][smheader]

## Persistence, repair, and uninstall

Proposed local recovery rules:

- A missing daemon or disabled background item produces setup/recovery UI, not a fallback root shell. For legacy package jobs, `statusForLegacyPlist(at:)` can help distinguish disabled approval from connection failures. The SDK notes that legacy `/Library/LaunchDaemons` jobs are bootstrapped without the new SMAppService approval step because installation already crosses a protected filesystem boundary. Still test background-item controls on each supported OS. [local SMAppService header][smheader]
- Persist owner policy and runtime state separately. Reboot, app exit, and daemon restart do not reassign ownership. A newly launched app resumes polling the real service.
- Repair may reinstall only the matching owner's reviewed payload. Verify existing paths and stop/deactivate the old service before replacing executable files. Refuse an unknown newer installation rather than perform an implicit downgrade.
- Approval revocation, crashes, and uninstall can interrupt cleanup. launchd restart policy cannot prove route/DNS isolation. Existing broader reject routes and marked resolver files intentionally survive ordinary shutdown. Preserve that policy until approved live tests support a change. [ACTIVATION.md](ACTIVATION.md)
- Control-only startup is not yet a complete fail-closed reboot policy. Current reject guards last for the boot, while retained state and resolver files can survive longer. Reboot may therefore leave cached or retained synthetic destinations without those guards until activation. Before promising persistent isolation, design and obtain consent for a minimal guard-restoration phase or another proven recovery mechanism. Guard restoration itself changes host routes, so it cannot be hidden inside a supposedly network-inactive installation test. [ACTIVATION.md](ACTIVATION.md)
- Separate **Deactivate networking**, **Uninstall service**, and **Erase account data**. Do not erase identities or address leases merely to repair service registration. Deleting the app is not an uninstaller for a legacy system job.
- An app-driven uninstall needs its own reviewed privileged operation or user-approved uninstall package. It must stop the exact job and remove only validated Tailmulti-owned paths. Retained resolver/guard cleanup needs the cached-destination precautions in the activation guide. No broad route flush, resolver-directory deletion, or background-item database reset.
- For SMAppService updates, perform graceful daemon preparation before unregistering and wait for asynchronous completion before registering the replacement. Do not update executable bytes in place under a running job. [local SMAppService header][smheader], [Apple update guidance][updates]

## Alternatives and decision

| Route | Fit for this project now | Boundary |
| --- | --- | --- |
| Local Installer package plus legacy system LaunchDaemon | Recommended first implementation. Fixed root-owned payload, build-bound owner, native Installer approval, separate activation. | Staging is possible unsigned. Actual installation, background behavior, and runtime remain untested. |
| Developer ID Installer-signed package | Suitable later packaging for external distribution. | Requires the correct Installer identity; executable signing and notarization remain separate. No upload or publication is authorized. |
| SMAppService | Preferred modern API when keeping the daemon inside the app bundle. | Current SDK notarization requirement and ad-hoc identity concerns prevent claiming an established offline route. Development-signed trials need explicit approval and testing. |
| `SMJobBless` | No reason to adopt for a macOS 14+ project. | Deprecated since macOS 13; introduces older signing/installation machinery. `SMAuthorizedClients` and `SMPrivilegedExecutables` belong to this older mechanism, not SMAppService. |
| `AuthorizationExecuteWithPrivileges` | Reject for this implementation. | Deprecated since macOS 10.7. Authorization Services rights validation remains useful without this execution function. |
| AppleScript, `osascript`, programmatic sudo, setuid, GUI as root | Outside the chosen design and user constraints. | They are not substitutes for the reviewed Installer or service flow. |

Sources for API comparison: [Apple privilege guidance][privileges], [local ServiceManagement header][smlegacy], [local Authorization header][authorization], [Apple DTS on mixed API instructions][mixed-api], [Apple certificate types][certificates].

## Validation boundaries and next work

### Completed in this research

Read project source, local Apple SDK headers and manual pages, Apple documentation, Apple-authored DTS posts, and the official sample archive. Local metadata reports Xcode 27.1 build 27A9275 and macOS 27.0 build 26A428. The header cross-check also used the installed 26.5 SDK. This is source evidence, not a build, install, or runtime test.

The official package sample is an **agent** example. Its source calls `SMAppService.agent`, its plist uses `BundleProgram` and `MachServices`, and its package script marks the bundle nonrelocatable. Its build script does not add a package signing option. It does not implement Tailmulti owner provisioning, a root network daemon, or safe network recovery. Do not copy its global background-item reset or broad sample cleanup instructions into product recovery. [official sample source][sample-zip]

### Offline work that can be implemented and checked next

- Stage the package without running Installer or its hooks. Inspect expanded payload, ownership metadata, modes, fixed absolute paths, identifiers, minimum OS, script contents, and package signing state. Verify that no script downloads anything or invokes the network runtime.
- Verify local owner-policy validation, same-owner reinstall, owner-mismatch refusal **before payload replacement**, symlink refusal, state preservation, and refusal to claim missing policy over IPC.
- Test control-only startup ordering and activation state transitions with temporary paths and injected host adapters. Include unauthorized UID, malformed policy, stale helper, port/route/resolver conflict, canceled setup, daemon death, and reconnect scenarios.
- Test the app coordinator with simulated Installer/SMAppService states. Such tests validate UI behavior only. They do not establish macOS registration, admin approval, launchd identity tracking, or network behavior.
- Separate package staging from installation in build tooling. The default build must never open Installer, bootstrap a job, register SMAppService, or activate networking.

### Requires explicit user participation or host trials

1. Installer approval, system file writes, launchd bootstrap, and service persistence. Verify the exact outer-package/payload signatures and trust policy on this host before describing success.
2. Root service startup without DNS, utun, routes, profile engines, or account enrollment. Verify correct owner binding, non-owner denial, root server identity, and continued app operation.
3. Explicit activation after preflight, then separate account logins and the existing two-tailnet acceptance procedure.
4. Crash, reboot, sleep/wake, background-item disable/re-enable, helper repair, owner mismatch, and uninstall. Test macOS 14 separately; SDK availability on macOS 27 does not prove behavior on the minimum supported OS.
5. Any SMAppService development-signed/notarized experiment. Record actual status/error behavior and approval persistence rather than inferring it from signature validity.

No live network milestone is established by this research. The immediate deliverable is this recommendation and source record. Package staging belongs to the next implementation step; installation and activation remain separate later decisions.

## Source record

All external sources below are Apple-owned documentation or Apple staff guidance. Non-Apple forum replies were not used as authority. Archived documents are marked by their `/library/archive/` URLs and checked against current local headers where relevant.

- [SMAppService header][smheader], SHA-256 `859ec2a7c4471dc1ac1e36b4de67e76d0a62ce4e4d145c942fd80cc4c2d8f8ff`.
- [launchd.plist manual][launchplist], SHA-256 `1c5f5041c1d3492988bfa6f9dc6d969dfd072d20af2703c28d9a8f6ed6aaadcb`.
- [Authorization header][authorization], SHA-256 `b6ed0db365724614ca40a74eef2a2a9d008e012e75a3a9b3379f0d924dc483e8`.
- [AuthorizationDB header][authorizationdb], SHA-256 `1a9c3db25860c9b1b9f5310973990325979a474aa773fed6457f110ed9387317`.
- [Official sample archive][sample-zip], SHA-256 `d765c4c235ba703da37431d97124627475c5e2f9407834f892415ea170478cc5`. Inspected `README.md`, `Commands.swift`, the agent source/plist, `project.pbxproj`, `Packaging/generatePackage.zsh`, and `Packaging/Scripts/postinstall`. No source was copied into the implementation.

[smheader]: /Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX27.0.sdk/System/Library/Frameworks/ServiceManagement.framework/Headers/SMAppService.h
[smheader265]: /Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk/System/Library/Frameworks/ServiceManagement.framework/Headers/SMAppService.h
[smlegacy]: /Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX27.0.sdk/System/Library/Frameworks/ServiceManagement.framework/Headers/ServiceManagement.h
[launchplist]: /Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX27.0.sdk/usr/share/man/man5/launchd.plist.5
[getpeereid]: /Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX27.0.sdk/usr/share/man/man3/getpeereid.3
[authorization]: /Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX27.0.sdk/System/Library/Frameworks/Security.framework/Headers/Authorization.h
[authorizationdb]: /Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX27.0.sdk/System/Library/Frameworks/Security.framework/Headers/AuthorizationDB.h
[pkgbuild]: /usr/share/man/man1/pkgbuild.1
[productbuild]: /usr/share/man/man1/productbuild.1
[installer]: /usr/share/man/man8/installer.8
[privileges]: https://developer.apple.com/forums/thread/708765
[signing-advice]: https://developer.apple.com/forums/thread/799910
[getting-started]: https://developer.apple.com/forums/thread/802443
[migration]: https://developer.apple.com/documentation/servicemanagement/updating-helper-executables-from-earlier-versions-of-macos
[sample]: https://developer.apple.com/documentation/servicemanagement/updating-your-app-package-installer-to-use-the-new-service-management-api
[sample-zip]: https://docs-assets.developer.apple.com/published/2527b6fa1f54/UpdatingYourAppPackageInstallerToUseTheNewServiceManagementAPI.zip
[bundleprogram]: https://developer.apple.com/forums/thread/771162
[mixed-api]: https://developer.apple.com/forums/thread/792826
[certificates]: https://developer.apple.com/help/account/certificates/certificates-overview/
[developer-id]: https://developer.apple.com/developer-id/
[notarization]: https://developer.apple.com/videos/play/wwdc2019/703/
[codesigning]: https://developer.apple.com/library/archive/technotes/tn2206/_index.html
[launchd-guide]: https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html
[distribution]: https://developer.apple.com/library/archive/documentation/DeveloperTools/Reference/DistributionDefinitionRef/Chapters/Distribution_XML_Ref.html
[authorization-tasks]: https://developer.apple.com/library/archive/documentation/Security/Conceptual/authorization_concepts/03authtasks/authtasks.html
[workspace-open]: https://developer.apple.com/documentation/appkit/nsworkspace/open(_:)
[workspace-header]: /Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX27.0.sdk/System/Library/Frameworks/AppKit.framework/Headers/NSWorkspace.h
[updates]: https://developer.apple.com/forums/thread/781548
