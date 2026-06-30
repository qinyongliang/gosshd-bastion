import { useMutation, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { KeyRound, LayoutDashboard, ListChecks, LockKeyhole, Menu, Server, Settings, Shield, Users, X } from "lucide-react";
import { ComponentType, ReactNode, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import { Field, Modal, ModalActions, NavButton, Segmented } from "../components/ui";
import { useI18n } from "../i18n";
import { appDescription, appName } from "../lib/branding";
import { formSubmit, pageTitle } from "../lib/forms";
import { useTheme } from "../theme";
import type { ConsoleData } from "../types";

export function Shell({ data, children }: { data: ConsoleData; children: ReactNode }) {
  const { t, locale, setLocale } = useI18n();
  const { theme, setTheme } = useTheme();
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [passwordOpen, setPasswordOpen] = useState(false);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const isClientMode = Boolean(data.runtime.client_mode);
  const name = appName(data.runtime);
  const description = appDescription(data.runtime);
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
          <strong>{name}</strong>
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
        {!isClientMode && data.user.auth_provider === "local" && <button type="button" className="logout-button" onClick={() => setPasswordOpen(true)}><KeyRound />{t("changePassword")}</button>}
        {!isClientMode && <button type="button" className="logout-button" onClick={() => logout.mutate()}><LockKeyhole />{t("logout")}</button>}
      </aside>
      <button className="sidebar-scrim" aria-label={t("close")} onClick={() => setSidebarOpen(false)} />
      <section className="workspace">
        <header className="topbar">
          <button className="mobile-menu-button icon-button" type="button" aria-label={t("openMenu")} onClick={() => setSidebarOpen(true)}><Menu /></button>
          <div>
            <small>{description}</small>
            <h1>{pageTitle(t)}</h1>
            {!isClientMode && <span className="context-line">{data.activeOrg.name}</span>}
          </div>
          <div className="top-actions">
            <Segmented value={theme} items={[["dark", t("themeDark")], ["light", t("themeLight")]]} onChange={(value) => setTheme(value as "light" | "dark")} />
            <Segmented value={locale} items={[["en", "EN"], ["zh-CN", t("languageChinese")]]} onChange={(value) => setLocale(value as "en" | "zh-CN")} />
          </div>
        </header>
        {children}
      </section>
      {passwordOpen && <ChangePasswordModal onClose={() => setPasswordOpen(false)} />}
    </section>
  );
}

function ChangePasswordModal({ onClose }: { onClose: () => void }) {
  const { t } = useI18n();
  const mutation = useMutation({ mutationFn: api.changeOwnPassword, onSuccess: onClose });
  return <Modal title={t("changePassword")} onClose={onClose} closeOnEscape={false}>
    <form className="stack" onSubmit={(event) => formSubmit(event, (body) => mutation.mutate(body))}>
      <Field label={t("currentPassword")} name="current_password" type="password" required />
      <Field label={t("newPassword")} name="new_password" type="password" required />
      <Field label={t("confirmPassword")} name="confirm_password" type="password" required />
      <ModalActions onCancel={onClose} submit={t("save")} />
    </form>
  </Modal>;
}

function OrgSwitcher({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  return <select className="org-switcher" value={data.activeOrg.id} onChange={(event) => data.setActiveOrgID(event.target.value)}>
    {data.orgs.map((org) => <option key={org.id} value={org.id}>{org.name} {org.is_personal ? t("personal") : ""}</option>)}
  </select>;
}
