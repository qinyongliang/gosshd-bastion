import { useMutation, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { KeyRound, LayoutDashboard, ListChecks, LockKeyhole, Menu, Server, Settings, Shield, Users } from "lucide-react";
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
  const logout = useMutation({
    mutationFn: api.logout,
    onSuccess: async () => {
      await queryClient.clear();
      navigate("/");
      window.location.reload();
    },
  });
  const nav: Array<[string, string, ComponentType<{ className?: string }>]> = [
    ["/", t("dashboard"), LayoutDashboard],
    ["/orgs", t("orgs"), Users],
    ["/org-admin", t("members"), Users],
    ["/keys", t("keys"), KeyRound],
    ["/targets", t("services"), Server],
    ["/policies", t("commandPolicy"), Shield],
    ["/audit", t("audit"), ListChecks],
  ];
  if (data.user.is_system_admin) nav.push(["/system-admin", t("settings"), Settings]);

  return (
    <section className="console">
      <aside className={clsx("sidebar", sidebarOpen && "open")}>
        <div className="brand-row"><div className="mark">g</div><strong>gosshd</strong></div>
        <div className="sidebar-user">
          <strong>{data.user.display_name || data.user.email}</strong>
          <span>{data.user.email}</span>
          {data.user.is_system_admin && <span className="pill">{t("admin")}</span>}
        </div>
        <nav className="side-nav">
          {nav.map(([to, label, Icon]) => <NavButton key={to} to={to} label={label} icon={<Icon />} onClick={() => setSidebarOpen(false)} />)}
        </nav>
        <OrgSwitcher data={data} />
        <button type="button" onClick={() => logout.mutate()}><LockKeyhole />{t("logout")}</button>
      </aside>
      {sidebarOpen && <button className="sidebar-backdrop" aria-label="Close menu" onClick={() => setSidebarOpen(false)} />}
      <section className="workspace">
        <header className="topbar">
          <button className="mobile-menu" type="button" onClick={() => setSidebarOpen(true)}><Menu /></button>
          <div>
            <small>AI 服务堡垒机</small>
            <h1>{pageTitle()}</h1>
            <span>{data.activeOrg.name}</span>
          </div>
          <div className="topbar-actions">
            <Segmented value={theme} items={[["dark", "黑", "Black"], ["light", "白", "White"]]} onChange={(value) => setTheme(value as "light" | "dark")} />
            <Segmented value={locale} items={[["en", "EN", "EN"], ["zh-CN", "中文", "中文"]]} onChange={(value) => setLocale(value as "en" | "zh-CN")} />
          </div>
        </header>
        <div className="hud-line">
          <span className="hud-pill"><i className="hud-dot" />SSH ingress online</span>
          <span className="hud-pill">policy guard ready</span>
          <span className="hud-pill">audit isolated</span>
        </div>
        {children}
      </section>
    </section>
  );
}

function OrgSwitcher({ data }: { data: ConsoleData }) {
  return <select className="org-switcher" value={data.activeOrg.id} onChange={(event) => data.setActiveOrgID(event.target.value)}>
    {data.orgs.map((org) => <option key={org.id} value={org.id}>{org.name} {org.is_personal ? "个人" : ""}</option>)}
  </select>;
}
