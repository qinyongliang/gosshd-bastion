# Public APK signing identity

Only the public release certificate and its SHA256 DER fingerprint belong here. Private keystores, key passwords and signing credentials must never be added to Git or uploaded as build artifacts.

GitHub Actions uses the repository Secrets documented in [Android accessibility setup](../../../docs/android-accessibility.md#maintained-release-signing-on-github). Preserve the maintained signing identity for APK updates; rotate deliberately, never by regenerating a key during a build.
