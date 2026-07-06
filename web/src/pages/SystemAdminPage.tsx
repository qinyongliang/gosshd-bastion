import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api } from "../api";
import { BrandMark, Drawer, Field, Modal, ModalActions, SimpleTable, Toggle, UserCell } from "../components/ui";
import { useI18n } from "../i18n";
import { appDescription, appIcon, appName } from "../lib/branding";
import { formSubmit, roleText } from "../lib/forms";
import type { AdminOrg, AdminUser, ConsoleData } from "../types";

export function SystemAdminPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const [modal, setModal] = useState<"" | "users" | "orgs" | "branding" | "auth" | "dingtalk" | "ldap">("");
  const adminUsers = useQuery({ queryKey: ["admin-users"], queryFn: api.adminUsers, enabled: data.user.is_system_admin && modal === "users" });
  const adminOrgs = useQuery({ queryKey: ["admin-orgs"], queryFn: api.adminOrgs, enabled: data.user.is_system_admin && modal === "orgs" });
  return (
    <>
      <section className="resource-head system-admin-head">
        <div><small>{appDescription(data.runtime)}</small><h2>{t("systemAdminTitle")}</h2><p>{t("systemAdminBody")}</p></div>
        <div className="resource-actions">
          <button type="button" onClick={() => setModal("branding")}>{t("adminBrandingSettings")}</button>
          <button type="button" onClick={() => setModal("auth")}>{t("adminAuthSettings")}</button>
          <button type="button" onClick={() => setModal("dingtalk")}>{t("adminProviderDingTalk")}</button>
          <button type="button" className="primary" onClick={() => setModal("ldap")}>{t("adminProviderLDAP")}</button>
        </div>
      </section>
      <div className="identity-grid system-admin-grid">
        <button type="button" className="admin-card" onClick={() => setModal("users")}><strong>{t("adminAccountsTitle")}</strong><span>{t("adminAccountsBody")}</span></button>
        <button type="button" className="admin-card" onClick={() => setModal("orgs")}><strong>{t("adminOrgsTitle")}</strong><span>{t("adminOrgsBody")}</span></button>
      </div>
      {modal === "users" && <AdminUsersModal users={adminUsers.data?.users || []} currentUserID={data.user.id} onClose={() => setModal("")} />}
      {modal === "orgs" && <AdminOrgsModal orgs={(adminOrgs.data?.organizations || []).filter((org) => !org.is_personal)} onClose={() => setModal("")} />}
      {modal === "branding" && <BrandingSettingsModal data={data} onClose={() => setModal("")} />}
      {modal === "auth" && <AuthSettingsModal onClose={() => setModal("")} />}
      {modal === "dingtalk" && <ProviderModal title={t("adminProviderDingTalk")} action={api.updateDingTalkSettings} onClose={() => setModal("")} />}
      {modal === "ldap" && <ProviderModal title={t("adminProviderLDAP")} action={api.updateLDAPSettings} onClose={() => setModal("")} />}
    </>
  );
}

function BrandingSettingsModal({ data, onClose }: { data: ConsoleData; onClose: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const settings = useQuery({ queryKey: ["admin-settings"], queryFn: api.adminSettings });
  const branding = (settings.data?.branding || {}) as { app_name?: string; app_description?: string; app_icon?: string };
  const [iconValue, setIconValue] = useState("");
  useEffect(() => {
    if (settings.data) setIconValue(branding.app_icon || data.runtime.app_icon || "");
  }, [settings.data, branding.app_icon, data.runtime.app_icon]);
  const mutation = useMutation({
    mutationFn: (body: Record<string, string>) => api.updateBrandingSettings({
      app_name: body.app_name,
      app_description: body.app_description,
      app_icon: body.app_icon,
    }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-settings"] }),
        queryClient.invalidateQueries({ queryKey: ["me"] }),
        queryClient.invalidateQueries({ queryKey: ["providers"] }),
      ]);
      onClose();
    },
  });
  const onIconFile = (file?: File) => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => setIconValue(String(reader.result || ""));
    reader.readAsDataURL(file);
  };
  return <Modal title={t("adminBrandingSettings")} onClose={onClose} closeOnEscape={false}>
    {!settings.data ? <p>{t("loading")}</p> : <form className="stack" onSubmit={(event) => formSubmit(event, (body) => mutation.mutate(body))}>
      <div className="branding-icon-editor">
        <BrandMark branding={{ app_icon: iconValue || appIcon(data.runtime) }} />
        <label className="field">
          <span>{t("adminAppIcon")}</span>
          <input type="file" accept="image/png,image/jpeg,image/webp,image/x-icon" onChange={(event) => onIconFile(event.target.files?.[0])} />
        </label>
      </div>
      <input type="hidden" name="app_icon" value={iconValue} />
      <Field label={t("adminAppName")} name="app_name" defaultValue={branding.app_name || appName(data.runtime)} required />
      <Field label={t("adminAppDescription")} name="app_description" defaultValue={branding.app_description || appDescription(data.runtime)} required />
      <button type="button" onClick={() => setIconValue("")}>{t("adminUseDefaultIcon")}</button>
      <ModalActions onCancel={onClose} submit={t("save")} />
    </form>}
  </Modal>;
}

function AuthSettingsModal({ onClose }: { onClose: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const settings = useQuery({ queryKey: ["admin-settings"], queryFn: api.adminSettings });
  const mutation = useMutation({
    mutationFn: (body: Record<string, string>) => api.updateAuthSettings({ public_registration: body.public_registration === "on" }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-settings"] });
      onClose();
    },
  });
  const auth = (settings.data?.auth || {}) as { public_registration?: boolean };
  return <Modal title={t("adminAuthSettings")} onClose={onClose} closeOnEscape={false}>
    {!settings.data ? <p>{t("loading")}</p> : <form className="stack" onSubmit={(event) => formSubmit(event, (body) => mutation.mutate(body))}>
      <Toggle name="public_registration" label={t("adminPublicRegistration")} defaultChecked={Boolean(auth.public_registration)} />
      <ModalActions onCancel={onClose} submit={t("save")} />
    </form>}
  </Modal>;
}

function AdminUsersModal({ users, currentUserID, onClose }: { users: AdminUser[]; currentUserID: string; onClose: () => void }) {
  const { t } = useI18n();
  const [resetUser, setResetUser] = useState<AdminUser | null>(null);
  const queryClient = useQueryClient();
  const update = useMutation({ mutationFn: ({ id, body }: { id: string; body: Record<string, unknown> }) => api.updateAdminUser(id, body), onSuccess: async () => queryClient.invalidateQueries() });
  const remove = useMutation({ mutationFn: (id: string) => api.deleteAdminUser(id), onSuccess: async () => queryClient.invalidateQueries() });
  return <Modal title={t("adminAccountsTitle")} onClose={onClose} wide>
    <SimpleTable headers={[t("commonEmail"), t("adminLoginSource"), t("commonStatus"), t("adminSystemAdminColumn"), t("commonActions")]} rows={users.map((user) => [
      <UserCell member={{ user_id: user.id, email: user.email, display_name: user.display_name, role: "member" }} />,
      user.auth_provider === "local" ? t("commonLocal") : user.auth_provider,
      user.disabled_at ? <span className="badge danger">{t("adminUserDisabled")}</span> : <span className="badge info">{t("adminUserEnabled")}</span>,
      <select defaultValue={user.is_system_admin ? "admin" : "user"} onChange={(event) => update.mutate({ id: user.id, body: { is_system_admin: event.target.value === "admin" } })}><option value="user">{t("adminUser")}</option><option value="admin">{t("adminRole")}</option></select>,
      <span className="inline-actions">
        <button type="button" onClick={() => update.mutate({ id: user.id, body: { disabled: !user.disabled_at } })} disabled={user.id === currentUserID && !user.disabled_at}>{user.disabled_at ? t("adminEnableUser") : t("adminDisableUser")}</button>
        <button type="button" disabled={user.auth_provider !== "local"} onClick={() => setResetUser(user)}>{t("adminResetPassword")}</button>
        <button type="button" className="danger" disabled={user.id === currentUserID} onClick={() => { if (window.confirm(t("adminDeleteUserConfirm"))) remove.mutate(user.id); }}>{t("commonDelete")}</button>
      </span>,
    ])} />
    {resetUser && <ResetPasswordModal user={resetUser} onClose={() => setResetUser(null)} />}
  </Modal>;
}

function ResetPasswordModal({ user, onClose }: { user: AdminUser; onClose: () => void }) {
  const { t } = useI18n();
  const reset = useMutation({ mutationFn: (body: Record<string, string>) => api.resetAdminUserPassword(user.id, body), onSuccess: onClose });
  return <Modal title={t("adminResetPasswordTitle")} onClose={onClose} stacked closeOnEscape={false}>
    <form className="stack" onSubmit={(event) => formSubmit(event, (body) => reset.mutate(body))}>
      <p>{user.display_name || user.email}</p>
      <Field label={t("adminNewPassword")} name="password" type="password" required />
      <ModalActions onCancel={onClose} submit={t("adminSaveNewPassword")} />
    </form>
  </Modal>;
}

function AdminOrgsModal({ orgs, onClose }: { orgs: AdminOrg[]; onClose: () => void }) {
  const { t } = useI18n();
  const [selected, setSelected] = useState<AdminOrg | null>(null);
  const queryClient = useQueryClient();
  const remove = useMutation({
    mutationFn: (id: string) => api.deleteAdminOrg(id),
    onSuccess: async (_, id) => {
      if (selected?.id === id) setSelected(null);
      await queryClient.invalidateQueries();
    },
  });
  return <Modal title={t("adminOrgsTitle")} onClose={onClose} wide>
    <SimpleTable headers={[t("orgs"), t("commonRole"), t("commonActions")]} rows={orgs.map((org) => [
      <strong>{org.name}</strong>,
      roleText(org.role, t),
      <span className="inline-actions">
        <button type="button" onClick={() => setSelected(org)}>{t("members")}</button>
        <button type="button" className="danger" onClick={() => { if (window.confirm(t("adminDeleteOrgConfirm"))) remove.mutate(org.id); }}>{t("commonDelete")}</button>
      </span>,
    ])} />
    {selected && <AdminOrgDrawer org={selected} onClose={() => setSelected(null)} />}
  </Modal>;
}

function AdminOrgDrawer({ org, onClose }: { org: AdminOrg; onClose: () => void }) {
  const { t } = useI18n();
  const members = useQuery({ queryKey: ["admin-org-members", org.id], queryFn: () => api.adminOrgMembers(org.id) });
  const queryClient = useQueryClient();
  const update = useMutation({ mutationFn: ({ userID, role }: { userID: string; role: string }) => api.adminUpdateOrgMember(org.id, userID, { role }), onSuccess: async () => queryClient.invalidateQueries() });
  return <Drawer title={org.name} subtitle={t("adminOrgMembersTitle")} onClose={onClose}>
    <div className="member-card-list">
      {(members.data?.members || []).map((member) => <article className="member-card" key={member.user_id}>
        <UserCell member={member} />
        <span>{roleText(member.role, t)}</span>
        {member.role === "owner" ? <span className="badge info">{t("roleOwner")}</span> : <span className="inline-actions">
          <button type="button" onClick={() => update.mutate({ userID: member.user_id, role: "admin" })}>{t("roleAdmin")}</button>
          <button type="button" onClick={() => update.mutate({ userID: member.user_id, role: "member" })}>{t("roleMember")}</button>
        </span>}
      </article>)}
    </div>
  </Drawer>;
}

function ProviderModal({ title, action, onClose }: { title: string; action: (body: Record<string, unknown>) => Promise<void>; onClose: () => void }) {
  const { t } = useI18n();
  const mutation = useMutation({ mutationFn: action, onSuccess: onClose });
  return <Modal title={title} onClose={onClose} closeOnEscape={false}>
    <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => mutation.mutate(body))}>
      <Field label={t("adminProviderEnable")} name="enabled" />
      <Field label={t("adminProviderClientID")} name="client_id" />
      <Field label={t("adminProviderClientSecret")} name="client_secret" />
      <Field label={t("adminProviderRedirect")} name="redirect_url" />
      <ModalActions onCancel={onClose} submit={t("save")} />
    </form>
  </Modal>;
}
