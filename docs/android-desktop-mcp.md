# Android desktop control MCP

Two backends are available: the original root/ADB shell backend below, and the [no-root accessibility companion](android-accessibility.md) with ADB/Shizuku activation, Unicode/native text actions and foreground lifecycle. The installer wrapper selects root only after a successful su grant; otherwise it selects the auxiliary path.

The **shell backend**, on Android API 24+ (Android 7+), uses the same `desktop_*` tools as Windows/macOS. Run the Agent or standalone MCP host as **root (UID 0) or adb shell (UID 2000)** with an awake, locally unlocked primary display. Non-root Termux can run the SSH Agent but cannot use this desktop backend. The module does not install an APK, enable accessibility services, switch input methods, obtain root or automate a lock screen.

## Module boundaries

- `internal/desktop/contract`: leaf wire types and validation; existing `desktop` types remain aliases, preserving callers and JSON schemas.
- `internal/desktop/native/android`: device state, window inventory, bounded UI XML, PNG capture, gestures/keys, literal text, launching and cancelable command runner. No imports from Agent/server/store or the MCP SDK.
- `internal/desktop/process_android.go`: Android build selection. `process_embedded.go` owns the private native stream and cancels it on close/disconnect.
- Existing Agent transport, actor-owned sessions, authorization, MCP images and audit metadata are reused. No public helper listener is added.

The runner executes fixed `/system/bin` tools with separate argv values; remote callers cannot specify shell scripts, executables, dump paths or elevation commands. Command diagnostics are not written to audit. UI XML exists briefly in a random mode-0700 device directory, removed after each operation. Screenshots are held in memory.

## Agent setup

Install/build the normal Android ARMv7 or ARM64 Agent. In an already authorized root or adb shell session, run:

```sh
gosshd-agent --server https://bastion.example.com \
  --enrollment-token '<registration-token>' \
  --desktop-enable
```

Leave `--desktop-helper` empty: Android embeds the backend in the Go binary. Desktop stays disabled without `--desktop-enable`. For an existing root background installation, add the flag to its local Agent invocation and restart its existing controller; the installer does not silently enable screen control.

Enable the operator token's existing `desktop` tool group and obtain normal target desktop read/control authorization. Select the Android **Agent** target, open `desktop_session_open`, inspect returned permission status, enumerate windows, observe a returned id/app pair, act once, then observe again. Direct SSH targets do not gain screen control.

Root or shell UID alone does not prove that automation works. Status probes actual PNG/UI access without injecting input. SELinux, vendor changes, missing tools and protected screens can deny operations. Off, locked or unknown device state refuses input. The owner handles local unlocking and OS security prompts.

## Execution modes

Omitted/default and auto modes allow primary-screen gesture control. Android's shell backend has no independent semantic/background input or window capture: explicit background controls, window observations and screenshots return `background_not_supported` without sending gestures. Status reports supported_modes default/auto and background_supported false. Existing Windows/macOS modes and capture metadata remain unchanged.

## Operations and limitations

| Tool | Android behavior |
| --- | --- |
| status | UID eligibility plus capture/UI/power/lock-state probes; no input |
| list_displays | Primary logical display 0, accounting for rotation/current geometry |
| list_windows / list_apps | Visible primary-display inventory; apps are those with targetable windows |
| get_window_state | Focused window, optional cropped/scaled PNG and bounded UI tree; session-local snapshot |
| screenshot | Primary display or focused window; consumes any earlier actionable snapshot |
| click | Left tap/double tap; element index uses the visible center |
| drag / scroll | Swipe within observed window/element bounds; positive delta scrolls forward |
| press_key | Android keys including Back/Home; chords only when `input keycombination` is advertised |
| type_text | Printable ASCII including literal spaces, percent signs and punctuation; Unicode/newline/tab rejected before injection |
| launch_app | Installed package identifier such as `com.example.app`, with launcher confirmation |
| activate_window | Already focused window or its real returned activity component; observe inactive metadata with include_text=false and include_screenshot=false first |
| perform_action | Advertised invoke/toggle/secondary gesture equivalents only; secondary is a long press |
| set_value | Explicitly unsupported; the shell backend has no semantic editable-value interface |
| request_permissions | Reports local root/adb shell prerequisites; no escalation or grant changes |

The shell backend cannot call arbitrary `AccessibilityNodeInfo.performAction` methods. Its element operations are **gesture equivalents**, requiring a new observation to confirm the result. Password nodes and foreign-package nodes are excluded. Custom surfaces/vendor apps can omit controls. A window PNG is a crop of the visible display; overlays and protected surfaces remain subject to system capture behavior.

Window bounds use logical display pixels; element coordinates and input x/y are relative to the observed window. PNGs scale to at most 1600 pixels on the longest side: map points with `x = image_x * bounds.width / screenshot.width` and similarly for y. Focus, origin, size or display changes invalidate input. Only the focused window accepts input. Secondary displays, offscreen windows and unknown geometry fail explicitly.

UI tree defaults: depth 8 / 512 elements; maximum depth 16 / 1024. PNG payloads, decoded image dimensions and command output are bounded. Snapshots expire after 60 seconds and every attempted action consumes them. Session expiry and operation timeouts reuse the existing desktop service. Disconnect cancels commands including a private uiautomator launcher process group.

## Standalone host and builds

The independent MCP host runs **on Android** under the same UID requirements; a workstation executable does not automatically control an adb-connected phone.

```sh
ANDROID_NDK_HOME='<ndk-directory>' bash scripts/build-desktop-android.sh dev
# Agent builds remain separate.
ANDROID_NDK_HOME='<ndk-directory>' bash scripts/build-agent-android.sh dev
```

Windows equivalents (Go and NDK r27c or newer):

```powershell
pwsh -File scripts/build-desktop-android.ps1 -Version dev -NDKPath '<ndk-directory>'
pwsh -File scripts/build-agent-android.ps1 -Version dev -NDKPath '<ndk-directory>'
```

Independent binaries/docs are in `build/desktop/android/<version>/{armv7,arm64}/`. On the device run the selected binary with `--allow-read --allow-control`; open `desktop_session_open({"target_id":"local"})`. Read/control flags retain existing standalone behavior. Establish root access locally before launch.

## Verification

```sh
go test ./internal/desktop/... -count=1
go vet ./internal/desktop/...
go test ./internal/server ./internal/agent ./internal/store -run 'Test(Desktop|MCPDesktop|MCPImages)' -count=1
```

Host tests use synthesized `com.example.*` fixtures and an injected runner. They cover parsing, geometry/crop/scaling, snapshot freshness/isolation, state rejection, UI filtering/limits, literal text, chord support, cancellation and stream shutdown. Cross-compilation is separate from physical device acceptance. No connected-device actions run automatically in these tests.

## Opt-in emulator acceptance

`TestAndroidEmulatorFixture` drives the actual on-device stdio MCP server through ADB and verifies the resulting native UI. It refuses physical-device serials and requires `ro.kernel.qemu=1`. The test APK has no network or private-data permissions and uses only `com.example.gosshd.fixture` controls. Default test runs skip this acceptance test.

Prepare an owned Android API 35 `default;x86_64` AVD, boot it, and build the fixture on Windows with JDK plus SDK platform/build-tools 35:

```powershell
pwsh -File scripts/build-desktop-android-fixture.ps1
```

For an x86_64 emulator, cross-build `cmd/gosshd-desktop-mcp` with `GOOS=android`, `GOARCH=amd64`, `CGO_ENABLED=1` and NDK clang targeting `x86_64-linux-android24`. This is an acceptance binary, not an ARM release artifact. Example from a Unix host with the NDK compiler on PATH:

```sh
GOOS=android GOARCH=amd64 CGO_ENABLED=1 \
  CC='clang --target=x86_64-linux-android24' \
  go build -buildmode=pie -trimpath -o build/desktop/android/emulator/gosshd-desktop-mcp ./cmd/gosshd-desktop-mcp
```

Select the owned emulator explicitly, install `build/desktop/android/fixture/fixture.apk`, push the acceptance executable to `/data/local/tmp/gosshd-desktop-mcp`, then `chmod 700` it. Configure the opt-in test:

```powershell
$env:GOSSHD_ANDROID_EMULATOR_TEST='1'
$env:GOSSHD_ANDROID_ADB='<absolute-sdk-path>/platform-tools/adb.exe'
$env:GOSSHD_ANDROID_EMULATOR_SERIAL='emulator-5554' # Your explicitly selected owned AVD.
$env:GOSSHD_ANDROID_EMULATOR_ARTIFACT_DIR="$PWD/local-only/android-emulator-acceptance"
go test ./internal/desktop/mcptools -run '^TestAndroidEmulatorFixture$' -count=1 -v -timeout=5m
```

The suite resets only the fixture, verifies screenshots/tree, taps, long presses, toggles, slider drag, scroll, literal ASCII, modifier chords, rejected Unicode/bounds/set_value, consumed snapshots, rotation, sleep and session closure. It temporarily rotates/sleeps the owned AVD and restores the default orientation/awake state on exit. It accepts shell UID 2000 or root UID 0; `adb root` is only suitable for a root-capable owned test image.

Logs, generated signing keys, device dumps and screenshots must stay in ignored build/local-only directories. Emulator runtime results do not prove ARM device/vendor compatibility.
