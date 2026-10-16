# Android Agent APK and optional desktop control

## One package, two installation routes

The recommended non-root installation is **gosshd-agent-android.apk** for Android 11 / API 30 or newer, ARM64 and ARMv7. It includes the Go Agent and the optional accessibility backend. Termux, root, Shizuku and a persistent ADB connection are **not** required to run the built-in Agent.

The foreground service starts the fixed Agent executable installed with the APK, supervises its process with bounded restart backoff, and owns the optional loopback desktop bridge. Commands run as the ordinary application UID inside Android's app sandbox, not as root or the ADB shell user. Installing this APK does not grant full-device filesystem or shell access.

### Manual installation on the phone

1. Obtain the APK, SHA256 file and public signing certificate from the project's tagged GitHub Release. The existing **Android Agent APK** workflow builds and signs these assets; a push to main or manual dispatch also produces a signed Actions artifact. Pull requests produce an explicitly debug-signed test APK.
2. Check the APK checksum, transfer/download it to the phone, open it and confirm Android's package installer prompts locally.
3. Open **GOSSHD Agent**. Enter the HTTPS server address and the **enrollment token** generated in that server's private-node / Agent installation guide. A server address alone cannot authorize registration to an account.
4. Tap **Save and start Agent** and allow notifications. The service notification and screen display the Agent's connection status. Server credentials are kept in private app data, never passed in ADB arguments or Intent extras. Plain HTTP is accepted only for loopback development.
5. Optionally enable **Restore Agent / control after boot** and adjust battery optimization / vendor auto-start settings.

The Bastion installation guide shows the APK download link, server address and a copy control for the enrollment token. It keeps the standalone Termux/root-shell commands under advanced installation options.

### ADB installation from Windows, Linux or macOS

Download the same APK to the computer, enable USB debugging on the phone, connect it and approve the local debugging prompt. These commands work on all three hosts with Android Platform Tools installed:

```sh
adb devices
adb install -r ./gosshd-agent-android.apk
adb shell am start -n com.gosshd.desktop/.ui.MainActivity
```

Then enter the server address and enrollment token **in the app**, exactly as for manual installation. ADB can be disconnected. Do not put registration credentials into shell commands, URLs or copied terminal transcripts.

The Release also includes device-checking host helpers. Windows PowerShell 5.1+:

```powershell
powershell -NoProfile -File ./install-gosshd-android-adb.ps1 -ApkPath ./gosshd-agent-android.apk
```

Linux/macOS:

```sh
sh ./install-gosshd-android-adb.sh --apk ./gosshd-agent-android.apk
```

Repository equivalents are scripts/android-desktop/install-adb.ps1 and scripts/android-desktop/install-adb.sh. With multiple devices, pass -Serial on Windows or --serial on Linux/macOS. The helpers reject unauthorized devices and Android versions below API 30, preserve app data with install -r, and never uninstall on an update failure. For optional accessibility activation, add -EnableAccessibility or --enable-accessibility; this grants the fixed activation permission, but the owner still taps **Activate with ADB grant** in the app. Agent-only installation does not change accessibility permissions or settings.

A debug APK cannot replace a release APK signed with a different certificate. Use a matching signing identity for upgrades. Uninstalling clears the stored credentials, identity and pairing token.

## Optional non-root desktop control

The Agent can run without accessibility. To enable native node actions, gestures, screenshots and Unicode editing:

1. Enable the app's accessibility service manually in Android Settings, or use one of the local ADB/Shizuku activation choices below.
2. Turn on **Enable desktop control (optional)** in the app. The built-in Agent pairs automatically using a private local token file; no token copying or Termux step is needed.
3. Use the existing Bastion target/account authorization and desktop session tools. Remote RPC cannot enable accessibility, change local credentials or turn on desktop control.

Activation choices:

- **Manual:** tap Enable accessibility manually and enable GOSSHD Agent in system settings. Some sideload/ROM restricted-settings policies require owner action in the app's system details page.
- **ADB:** run the copied fixed grant command, then tap Activate with ADB grant. The implementation merges this component with other enabled services instead of replacing them. The optional host-helper flag performs the grant only.
- **Shizuku:** install/start official Shizuku, tap Activate with Shizuku and approve its local manager dialog. Its short-lived UserService only enables this app's fixed accessibility component. ADB-started Shizuku must be restarted after reboot, but a live Shizuku process is not required for ongoing accessibility operations.

The bridge binds only to 127.0.0.1:28765 and supports at most four authenticated local connections. Mutual HMAC-SHA256 challenge-response authenticates both endpoints without sending the raw pairing token. Rotating the token disconnects clients and updates the built-in Agent automatically. External local clients must re-pair.

Screenshots cannot capture protected surfaces. Password/secure nodes are excluded, locked/off displays reject input, and native operations must be advertised by the observed node. Reobserve after every action or error. Neither Android backend supports strict background mode.

## Background operation and mobile data

- A visible specialUse foreground service owns the Agent subprocess and optional bridge. **Stop service** disables both runtime opt-ins; boot recovery does not override that stop.
- The Agent and desktop-control switches are independent. Turning off desktop control does not stop the Agent connection. Stopping only the Agent can leave the explicitly enabled local bridge available to external clients.
- Boot restoration is opt-in and best-effort. An APK replacement requests restoration only if the service was enabled. Force-stop, revoked accessibility, Doze/network suspension and vendor task killing can still require reopening the app. No root hooks or hidden persistence mechanism is used.
- The wake lock is bounded and used for active paired desktop sessions, not a permanent idle loop. Failed native-process starts and lost Agent connections use bounded exponential backoff rather than a busy loop.
- In accessibility mode, get_window_state omits screenshots unless include_screenshot: true is explicitly requested. Prefer include_text: true for routine semantic work. The explicit screenshot operation still captures an image, bounded to a 1600-pixel longest side, and can consume significant cellular data when relayed to the server.
- The companion bridge itself is loopback-only. The built-in Agent's persistent server connection, requested commands, file transfers and forwards use network data. Idle keepalives are not a zero-data guarantee. There is no periodic screen upload or APK download loop.
- **APK-managed Agents do not download or replace their own executable.** Install a newer APK signed with the maintained identity to update. This avoids redundant binary downloads on mobile networks and keeps the installed application and Agent version together.

## Legacy shell and external companion usage

The existing root/ADB-shell Android backend and standalone Android 7+ Agent binaries remain available. Termux/runit and root installation wrappers are advanced, separate routes, not prerequisites for this APK. See the main README and Android desktop MCP documentation.

For an external Termux Agent, stop the built-in Agent if it is not needed, enable desktop control, and privately save the app's copied pairing token in a mode-600 file. Pass --desktop-enable --android-backend accessibility --android-bridge-token-file to that external Agent. The APK's process supervisor does not manage external Termux services, and its local token is not an enrollment token.

## Build and verification

The SDK-only APK builder needs a JDK, Go from go.mod, Android SDK platform/build-tools 35 and NDK r27c+:

```powershell
./scripts/build-desktop-android-companion.ps1 -Debug
# Optional x86_64 ABI for an explicitly owned emulator:
./scripts/build-desktop-android-companion.ps1 -Debug -IncludeEmulator
# Production signing, with the keystore password provided privately via environment:
./scripts/build-desktop-android-companion.ps1 -SigningKey '<private-keystore>' -SigningAlias companion
```

The canonical outputs are dist/gosshd-agent-android.apk (or -debug.apk), its SHA256 and public certificate. The old gosshd-desktop-companion filenames are retained as identical compatibility aliases, with the same Android package ID and signing identity. The APK bundles ARM64/ARMv7 PIE executables under its installed native library directory; it does not extract executable code to writable app data. Optional debug emulator builds add x86_64. Standalone Android builds remain unchanged by default.

```sh
go test ./cmd/gosshd-android-agent ./internal/agent ./internal/desktop/... -count=1
node --test scripts/android-desktop/adb-install.test.mjs
```

Installer tests use fake ADB and never access a phone. Actual accessibility acceptance remains opt-in and restricted to an explicitly owned emulator fixture. Compilation and host tests do not certify process execution, boot recovery or vendor battery behavior on physical phones.

## GitHub Actions and maintained release signing

The Android Agent APK workflow tests the ADB helpers on Windows, Linux and macOS, installs Go/JDK/SDK/NDK, bundles both ARM Agent binaries, and builds the APK. Main/manual runs require the maintained release secrets. Version-tag runs attach the APK, checksum, public certificate and both host helpers to the GitHub Release. Pull requests build without signing secrets and upload debug-only test artifacts.

Public X.509 certificate and DER SHA256 fingerprint are versioned in client/android-desktop/signing/. Repository administrators maintain the private PKCS12 key using encrypted Actions Secrets ANDROID_DESKTOP_KEYSTORE_B64 and ANDROID_DESKTOP_KEYSTORE_PASSWORD, alias companion. Keep a separate private backup. Never regenerate the key during ordinary builds or promote a debug key to production. Missing release secrets fail the build instead of falling back to debug signing. The builder verifies the signer against the tracked public fingerprint and uses the workflow run number as versionCode. Temporary signing material is deleted and never uploaded.

Android permission and lifecycle references: official Android AccessibilityService, foreground-service and application manifest documentation; Shizuku activation follows the official Shizuku-API provider/UserService contract.
