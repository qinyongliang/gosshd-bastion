import { ChevronDown } from "lucide-react";
import { type ReactNode, useId, useState } from "react";
import type { Enrollment } from "../api";
import { useI18n } from "../i18n";
import { detectInstallPlatform, type InstallPlatform } from "../lib/install-platform";
import { CommandBox, CopyButton } from "./ui";

export function AgentInstallGuide({ enrollment }: { enrollment: Enrollment }) {
  const { t } = useI18n();
  const [active, setActive] = useState<InstallPlatform | null>(() => detectInstallPlatform(navigator.userAgent));
  const toggle = (platform: InstallPlatform) => setActive((current) => current === platform ? null : platform);
  return <div className="service-install-commands agent-install-accordion">
    <PlatformSection title="Windows" open={active === "windows"} onToggle={() => toggle("windows")}>
      <p className="muted">{t("serviceWindowsSupport")}</p>
      <CommandBox label={t("serviceWindowsCMDRunOnce")} value={enrollment.install_cmd || ""} />
      <CommandBox label={t("serviceWindowsCMDService")} value={enrollment.service_cmd || ""} />
      <CommandBox label={t("serviceWindowsRunOnce")} value={enrollment.install_ps1 || ""} />
      <CommandBox label={t("serviceWindowsService")} value={enrollment.service_ps1 || ""} />
      <CommandBox label={t("serviceWindowsPwshRunOnce")} value={enrollment.install_pwsh || ""} />
      <CommandBox label={t("serviceWindowsPwshService")} value={enrollment.service_pwsh || ""} />
    </PlatformSection>
    <PlatformSection title="Linux" open={active === "linux"} onToggle={() => toggle("linux")}>
      <CommandBox label={t("serviceRunOnce")} value={enrollment.install_sh || ""} />
      <CommandBox label={t("serviceLinuxService")} value={enrollment.service_sh || ""} />
    </PlatformSection>
    <PlatformSection title="macOS" open={active === "macos"} onToggle={() => toggle("macos")}>
      <CommandBox label={t("serviceRunOnce")} value={enrollment.install_sh || ""} />
      <CommandBox label={t("serviceMacOSService")} value={enrollment.service_sh || ""} />
    </PlatformSection>
    <PlatformSection title="Android (ARMv7 / ARM64)" open={active === "android"} onToggle={() => toggle("android")}>
      <p className="muted">{t("serviceAndroidAPKSupport")}</p>
      {enrollment.android_apk_url && <p><a href={enrollment.android_apk_url} target="_blank" rel="noreferrer">{t("serviceAndroidDownloadAPK")}</a></p>}
      <CommandBox label={t("serviceAndroidServerAddress")} value={enrollment.server_url || window.location.origin} />
      {enrollment.token && <div className="command-box"><span>{t("serviceAndroidEnrollmentToken")}</span><input aria-label={t("serviceAndroidEnrollmentToken")} type="password" readOnly value={enrollment.token} autoComplete="off" /><CopyButton value={enrollment.token} /></div>}
      <p className="muted">{t("serviceAndroidManualSetup")}</p>
      <CommandBox label={t("serviceAndroidADBInstall")} value={'adb install -r ./gosshd-agent-android.apk\nadb shell am start -n com.gosshd.desktop/.ui.MainActivity'} />
      <details className="agent-install-advanced">
        <summary>{t("serviceAndroidAdvanced")}</summary>
        <p className="muted">{t("serviceAndroidSupport")}</p>
        <CommandBox label={t("serviceRunOnce")} value={enrollment.install_android || enrollment.install_sh || ""} />
        <CommandBox label={t("serviceAndroidService")} value={enrollment.service_android || ""} />
        <p className="muted">{t("serviceAndroidBoot")}</p>
      </details>
    </PlatformSection>
  </div>;
}

function PlatformSection({ title, open, onToggle, children }: { title: string; open: boolean; onToggle: () => void; children: ReactNode }) {
  const id = useId();
  return <section className="agent-install-platform">
    <h3><button type="button" id={id + "-heading"} aria-expanded={open} aria-controls={id + "-content"} onClick={onToggle}>
      <span>{title}</span><ChevronDown aria-hidden="true" />
    </button></h3>
    {open && <div id={id + "-content"} role="region" aria-labelledby={id + "-heading"} className="agent-install-platform-content">{children}</div>}
  </section>;
}
