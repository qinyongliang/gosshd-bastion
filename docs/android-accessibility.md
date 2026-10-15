# Android dual-mode desktop control

The existing root/ADB shell backend remains supported. The optional companion APK adds a separate **non-root accessibility backend**. Neither mode changes Bastion identity, target authorization or desktop audit policies.

| | Root / ADB shell | Accessibility companion |
| --- | --- | --- |
| Runtime UID | root 0 or shell 2000 | ordinary application UID; Termux Agent can pair |
| Android | API 24+ | API 30+ (Android 11+) for native accessibility screenshots |
| Input | shell gestures, keys, printable ASCII | native node actions, gestures, Unicode editable text and set_value |
| Setup | authorized root/shell Agent | local accessibility enablement plus explicit pairing |
| ADB/Shizuku | optional shell runtime | fast activation only; not required for continuing accessibility operations |

## Installation routes by actual su state

The modules in `scripts/android-desktop/` are a local wrapper around the updated, saved owner-scoped Agent installer. Prepare the verified companion APK and keep all scripts together:

```sh
sh scripts/android-desktop/install.sh ./saved-agent-installer.sh ./gosshd-desktop-companion.apk install
```

- Existing UID 0: executes `root.sh`, selecting the shell backend.
- Ordinary UID: probes `su -c 'id -u'` with a five-second timeout. Only a successful result of `0` selects root and re-executes the known root installer module.
- Missing su, denied permission, timeout or unavailable timeout utility: executes `accessibility.sh` as the ordinary Termux user. Merely finding a su executable is not treated as a grant.
- `GOSSHD_ANDROID_INSTALL_MODE=accessibility` explicitly avoids elevation; `root` explicitly requires a successful grant; default is `auto`.

The root module reuses the existing native root controller and optional existing root-framework boot hook. The non-root module launches the owner-mediated package installer, asks for a local pairing token if missing, and reuses the existing Termux/runit service. Android unknown-source prompts and permission grants remain local. ADB/Shizuku activation alone does not create a persistent Termux Agent.

The ordinary Android installer stays SSH-only by default. These desktop wrappers explicitly pass `GOSSHD_DESKTOP_ENABLE=1` and backend configuration; the updated installer persists the selected `--desktop-enable`, `--android-backend` and private token-file path in its existing run script. No enrollment token or pairing token is printed in logs.

## Three local activation choices

1. **Manual:** open the companion and select **Enable accessibility manually**, then enable GOSSHD Desktop in Android settings.
2. **ADB:** run the companion's copied grant command, then tap **Activate with ADB grant**. Alternatively run the supplied fixed script:

   ```sh
   adb shell pm grant com.gosshd.desktop android.permission.WRITE_SECURE_SETTINGS
   adb push scripts/android-desktop/activate-adb.sh /data/local/tmp/gosshd-activate.sh
   adb shell sh /data/local/tmp/gosshd-activate.sh
   ```

3. **Shizuku:** install/start official Shizuku locally, tap **Activate with Shizuku**, and approve its manager permission dialog. A short-lived UserService can only merge this app's accessibility component and enable accessibility. It has no general command-execution RPC. The grant and activation complete asynchronously; the UI shows the connection state and result.

Both fast activation paths preserve other enabled accessibility services. None silently turns on the paired control bridge. The owner enables **paired local control** separately. Non-root Shizuku started by ADB must be restarted after a device reboot; accessibility can keep working once activated without a live Shizuku process. Restricted-settings/permission policies on vendor ROMs may require owner action rather than programmatic activation.

## Pair the local Agent

Enable **paired local control** in the APK, copy its random pairing token, and save it in a private token file inside Termux (mode 600). Do not put the token in command history, source code, logs or public documentation. Launch the Agent with:

```sh
gosshd-agent --server https://bastion.example.com \
  --enrollment-token '<registration-token>' --desktop-enable \
  --android-backend accessibility \
  --android-bridge-token-file "$HOME/.gosshd/desktop-pairing.token"
```

For a standalone MCP host use the same two Android backend flags with `--allow-read --allow-control`.

`--android-backend shell` preserves the original backend. `auto` selects accessibility when a local pairing file is configured, otherwise shell; it does not silently obtain root or grant accessibility. Installation-time su routing and runtime backend selection are separate responsibilities.

The bridge listens only on `127.0.0.1:28765`, and accepts at most four local connections. Mutual HMAC-SHA256 challenge-response authenticates both endpoints without transmitting the raw pairing token. Bounds, snapshots, native nodes and screenshots remain per-session. Rotating the token or stopping control disconnects sessions. Remote MCP cannot change activation, the pairing token or local backend configuration.

Screenshots cannot capture protected surfaces. Secure/password nodes are excluded, locked/off displays reject input, and controls must still advertise native actions. Unicode editing applies only to editable non-secure fields. Auxiliary keys include Back/Home/Recents and editable Ctrl+a, not arbitrary desktop keyboard chords. Coordinate gestures stay within the selected window; UI state must be re-observed after each operation. A command accepted by Android still requires observation of the result.

## Background operation, not an immortality guarantee

- A visible `specialUse` foreground service with persistent notification owns the local bridge. Its **Stop control** action disables restart intent as well as stopping current sessions.
- An opt-in **Restore control after boot** receiver requests recovery after boot. Opening the app can restart a previously enabled bridge; it never re-enables a locally disabled one.
- A bounded partial wake lock is held only for authenticated sessions, renewed by activity, not as a permanent idle loop. It does not unlock or keep the screen on.
- The battery-optimization settings button and vendor auto-start guidance are local owner actions. No root hooks or private process hacks are used for the no-root mode.
- Force-stop, revoked accessibility, vendor task killing and Android background-start restrictions can stop recovery. The owner may need to reopen the app. APK foreground lifecycle does not replace Termux service/battery configuration for the separately installed Go Agent.

## Build and verification

The APK is independent of the Go Agent. Build using SDK platform/build-tools 35 and a JDK:

```powershell
# Local test signing only; produces an explicitly debug APK.
pwsh -File scripts/build-desktop-android-companion.ps1 -Debug
# Production requires a stable user-supplied key; password is read from an environment variable.
pwsh -File scripts/build-desktop-android-companion.ps1 -SigningKey '<private-keystore>' -SigningAlias companion
```

The builder pins official Shizuku API 13.1.5 AARs by SHA256. Generated keys, dependencies and APKs stay under ignored build/dist directories. A production keystore must never be committed. `-TestMCPBinary` is permitted only for a debug APK and embeds an x86_64 test executable for ordinary-UID emulator acceptance; release APKs do not embed this test executable. The fixture builder remains separate.

```sh
go test ./internal/desktop/... -count=1
python3 scripts/android-desktop/install.test.py
```

The opt-in `TestAndroidAccessibilityEmulatorFixture` verifies actual MCP through the debug companion's ordinary app UID. Set `GOSSHD_ANDROID_ACCESSIBILITY_TEST=1`, the explicit owned emulator serial/absolute ADB path and `GOSSHD_ANDROID_COMPANION_MCP` to that debug APK's extracted `/data/app/.../lib/x86_64/libdesktop_mcp.so`. Pair the debug app locally first and save its token privately to its `files/pair.token`; no test token is hard-coded. Physical-device serials are refused. The old `TestAndroidEmulatorFixture` still tests shell/root mode separately.

Sources used for the implementation: official Android AccessibilityService APIs, Android foreground-service documentation, and RikkaApps Shizuku-API's official provider/UserService guidance. Root, shell and accessibility emulator checks do not certify every ARM phone or vendor ROM.

## Maintained release signing on GitHub

The public X.509 certificate and DER SHA256 fingerprint are versioned in `client/android-desktop/signing/`. The private PKCS12 keystore is **not** a repository file or CI artifact. Repository administrators maintain it using encrypted Actions Secrets `ANDROID_DESKTOP_KEYSTORE_B64` and `ANDROID_DESKTOP_KEYSTORE_PASSWORD`. The keystore alias is `companion`. Store an independent private backup before any credential rotation; replacing the key breaks normal installed-app updates.

The `Android companion APK` workflow builds and signs on main-branch changes and manual dispatch, and attaches APK/checksum/public certificate to version-tag releases. Pull requests compile with debug signing and never receive the release secrets. Missing release secrets fail packaging instead of falling back to a debug signature. The build verifies that the APK signer matches the tracked certificate fingerprint, assigns a monotonically increasing workflow run number as versionCode, and deletes its temporary keystore in a finally block. Only the APK, its SHA256 file and public certificate are uploaded.

A previously installed debug APK has a different certificate from the maintained release build; uninstall the debug test app before installing the first official build. Future builds signed with the maintained key support normal upgrades. Debug test keys are never promoted to production signing keys.
