#!/system/bin/sh
# Run with adb shell after owner installs the companion. Preserve other services.
set -eu
own='com.gosshd.desktop/com.gosshd.desktop.access.DesktopAccessibilityService'
current=$(settings get secure enabled_accessibility_services)
case "$current" in ''|null) next="$own";; *) case ":$current:" in *":$own:"*) next="$current";; *) next="$current:$own";;esac;;esac
settings put secure enabled_accessibility_services "$next"
settings put secure accessibility_enabled 1
pm grant com.gosshd.desktop android.permission.WRITE_SECURE_SETTINGS
am start -n com.gosshd.desktop/.ui.MainActivity
printf '%s\n' 'Accessibility activation requested. Enable local control and pair inside the app.'
