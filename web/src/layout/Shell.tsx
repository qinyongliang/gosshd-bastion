import { useMutation, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { KeyRound, LayoutDashboard, ListChecks, LockKeyhole, Menu, Server, Settings, Shield, Users, X } from "lucide-react";
import { ComponentType, ReactNode, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import { NavButton, Segmented } from "../components/ui";
import { useI18n } from "../i18n";
import { pageTitle } from "../lib/forms";
import { useTheme } from "../theme";
import type { ConsoleData } from "../types";

export function Shell({ data, children }: { data: ConsoleData; children: ReactNode }) {
  const { t, locale, setLocale } = useI18n();
  const { theme, setTheme } = useTheme();
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const isClientMode = Boolean(data.runtime.client_mode);
  const logout = useMutation({
    mutationFn: api.logout,
    onSuccess: async () => {
      await queryClient.clear();
      navigate("/");
      window.location.reload();
    },
  });
  const nav: Array<[string, string, ComponentType<{ className?: string }>]> = isClientMode
    ? [
        ["/targets", t("services"), Server],
        ["/policies", t("commandPolicy"), Shield],
        ["/audit", t("audit"), ListChecks],
      ]
    : [
        ["/", t("dashboard"), LayoutDashboard],
        ["/orgs", t("orgs"), Users],
        ["/org-admin", t("members"), Users],
        ["/keys", t("authorization"), KeyRound],
        ["/targets", t("services"), Server],
        ["/policies", t("commandPolicy"), Shield],
        ["/audit", t("audit"), ListChecks],
      ];
  if (!isClientMode && data.user.is_system_admin) nav.push(["/system-admin", t("settings"), Settings]);

  return (
    <section className={clsx("console", sidebarOpen && "sidebar-open")}>
      <aside className="sidebar">
        <div className="brand-row">
          <div className="mark">g</div>
          <strong>gosshd</strong>
          <button className="mobile-sidebar-close icon-button" type="button" aria-label={t("close")} onClick={() => setSidebarOpen(false)}><X /></button>
        </div>
        {!isClientMode && <div className="user-block">
          <strong>{data.user.display_name || data.user.email}</strong>
          <span>{data.user.email}</span>
          {data.user.is_system_admin && <span className="pill">{t("admin")}</span>}
        </div>}
        <nav className="side-nav">
          {nav.map(([to, label, Icon]) => <NavButton key={to} to={to} label={label} icon={<Icon />} onClick={() => setSidebarOpen(false)} />)}
        </nav>
        {!isClientMode && <OrgSwitcher data={data} />}
        {!isClientMode && <button type="button" className="logout-button" onClick={() => logout.mutate()}><LockKeyhole />{t("logout")}</button>}
      </aside>
      <button className="sidebar-scrim" aria-label={t("close")} onClick={() => setSidebarOpen(false)} />
      <section className="workspace">
        <header className="topbar">
          <button className="mobile-menu-button icon-button" type="button" aria-label={t("openMenu")} onClick={() => setSidebarOpen(true)}><Menu /></button>
          <div>
            <small>{t("shellProduct")}</small>
            <h1>{pageTitle(t)}</h1>
            {!isClientMode && <span className="context-line">{data.activeOrg.name}</span>}
          </div>
          <div className="top-actions">
            <Segmented value={theme} items={[["dark", t("themeDark")], ["light", t("themeLight")]]} onChange={(value) => setTheme(value as "light" | "dark")} />
            <Segmented value={locale} items={[["en", "EN"], ["zh-CN", t("languageChinese")]]} onChange={(value) => setLocale(value as "en" | "zh-CN")} />
          </div>
        </header>
        <div className="hud-line">
          <span className="hud-pill"><i className="hud-dot" />{t("statusIngress")}</span>
          <span className="hud-pill">{t("statusPolicy")}</span>
          <span className="hud-pill">{t("statusAudit")}</span>
        </div>
        {children}
      </section>
    </section>
  );
}

function OrgSwitcher({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  return <select className="org-switcher" value={data.activeOrg.id} onChange={(event) => data.setActiveOrgID(event.target.value)}>
    {data.orgs.map((org) => <option key={org.id} value={org.id}>{org.name} {org.is_personal ? t("personal") : ""}</option>)}
  </select>;
}
