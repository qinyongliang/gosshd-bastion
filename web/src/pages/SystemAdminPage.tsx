import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { ArrowUpRight, Building2, Fingerprint, KeyRound, Palette, Search, Settings2, ShieldCheck, Users } from "lucide-react";
import { Select, Switch } from "antd";
import { api } from "../api";
import { ActionMenu, BrandMark, ConfirmDialog, Drawer, Empty, ErrorMessage, Field, Modal, ModalActions, SimpleTable, Toggle, UserCell } from "../components/ui";
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
    <div className="admin-workspace">
      <section className="resource-head system-admin-head">
        <div><small>{appDescription(data.runtime)}</small><h2>{t("systemAdminTitle")}</h2><p>{t("systemAdminBody")}</p></div>
      </section>
      <div className="identity-grid system-admin-grid">
        <button type="button" className="admin-card" onClick={() => setModal("users")}><span className="admin-card-icon"><Users /></span><span className="admin-card-copy"><strong>{t("adminAccountsTitle")}</strong><span>{t("adminAccountsBody")}</span></span><ArrowUpRight className="admin-card-arrow" /></button>
        <button type="button" className="admin-card" onClick={() => setModal("orgs")}><span className="admin-card-icon"><Building2 /></span><span className="admin-card-copy"><strong>{t("adminOrgsTitle")}</strong><span>{t("adminOrgsBody")}</span></span><ArrowUpRight className="admin-card-arrow" /></button>
      </div>
      <div className="admin-settings-grid">
        {([ ["branding", Palette, "adminBrandingSettings"], ["auth", ShieldCheck, "adminAuthSettings"], ["dingtalk", Fingerprint, "adminProviderDingTalk"], ["ldap", Settings2, "adminProviderLDAP"] ] as const).map(([id, Icon, label]) => <button key={id} type="button" className="admin-setting-card" onClick={() => setModal(id)}><Icon /><strong>{t(label)}</strong><ArrowUpRight /></button>)}
      </div>
      {modal === "users" && <AdminUsersModal users={adminUsers.data?.users || []} loading={adminUsers.isLoading} error={adminUsers.error} currentUserID={data.user.id} onClose={() => setModal("")} />}
      {modal === "orgs" && <AdminOrgsModal orgs={(adminOrgs.data?.organizations || []).filter((org) => !org.is_personal)} loading={adminOrgs.isLoading} error={adminOrgs.error} onClose={() => setModal("")} />}
      {modal === "branding" && <BrandingSettingsModal data={data} onClose={() => setModal("")} />}
      {modal === "auth" && <AuthSettingsModal onClose={() => setModal("")} />}
      {modal === "dingtalk" && <ProviderModal title={t("adminProviderDingTalk")} action={api.updateDingTalkSettings} onClose={() => setModal("")} />}
      {modal === "ldap" && <ProviderModal title={t("adminProviderLDAP")} action={api.updateLDAPSettings} onClose={() => setModal("")} />}
    </div>
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
  return <Modal title={t("adminBrandingSettings")} onClose={onClose} closeOnEscape={false} className="admin-settings-modal">
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
      <ErrorMessage error={mutation.error} />
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
  return <Modal title={t("adminAuthSettings")} onClose={onClose} closeOnEscape={false} className="admin-settings-modal">
    {!settings.data ? <p>{t("loading")}</p> : <form className="stack" onSubmit={(event) => formSubmit(event, (body) => mutation.mutate(body))}>
      <Toggle name="public_registration" label={t("adminPublicRegistration")} defaultChecked={Boolean(auth.public_registration)} />
      <ErrorMessage error={mutation.error} />
      <ModalActions onCancel={onClose} submit={t("save")} />
    </form>}
  </Modal>;
}

function AdminUsersModal({ users, loading, error, currentUserID, onClose }: { users: AdminUser[]; loading: boolean; error: unknown; currentUserID: string; onClose: () => void }) {
  const { t } = useI18n();
  const [resetUser, setResetUser] = useState<AdminUser | null>(null);
  const [deleteUser, setDeleteUser] = useState<AdminUser | null>(null);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("all");
  const filtered = users.filter((user) => `${user.display_name} ${user.email}`.toLowerCase().includes(query.toLowerCase().trim()) && (status === "all" || Boolean(user.disabled_at) === (status === "disabled")));
  const queryClient = useQueryClient();
  const update = useMutation({ mutationFn: ({ id, body }: { id: string; body: Record<string, unknown> }) => api.updateAdminUser(id, body), onSuccess: async () => queryClient.invalidateQueries() });
  const remove = useMutation({ mutationFn: (id: string) => api.deleteAdminUser(id), onSuccess: async () => queryClient.invalidateQueries() });
  return <Modal title={t("adminAccountsTitle")} onClose={onClose} wide className="admin-list-modal">
    <div className="admin-list-intro"><span>{t("adminAccountsBody")}</span><span className="admin-count">{filtered.length} / {users.length}</span></div>
    <div className="admin-list-toolbar"><label className="admin-search"><Search /><input aria-label={t("commonSearchPlaceholder")} placeholder={t("commonSearchPlaceholder")} value={query} onChange={(event) => setQuery(event.target.value)} /></label><Select aria-label={t("commonStatus")} value={status} onChange={setStatus} options={[{value: "all", label: t("commonAll")}, {value: "enabled", label: t("adminUserEnabled")}, {value: "disabled", label: t("adminUserDisabled")}]} /></div>
    <ErrorMessage error={error || update.error || remove.error} />
    {loading ? <div className="admin-loading">{t("loading")}</div> : filtered.length ? <div className="admin-users-table">
    <SimpleTable headers={[t("commonEmail"), t("adminLoginSource"), t("commonStatus"), t("adminSystemAdminColumn"), t("commonActions")]} rows={filtered.map((user) => [
      <div className="admin-user-identity"><span className="admin-avatar">{(user.display_name || user.email).slice(0, 1).toUpperCase()}</span><UserCell member={{ user_id: user.id, email: user.email, display_name: user.display_name, role: "member" }} /></div>,
      <span className="admin-provider-label">{user.auth_provider === "local" ? t("commonLocal") : user.auth_provider}</span>,
      user.disabled_at ? <span className="badge danger">{t("adminUserDisabled")}</span> : <span className="badge info">{t("adminUserEnabled")}</span>,
      <Switch aria-label={`${t("adminSystemAdminColumn")}: ${user.email}`} checked={user.is_system_admin} loading={update.isPending && update.variables?.id === user.id} onChange={(checked) => update.mutate({ id: user.id, body: { is_system_admin: checked } })} />,
      <ActionMenu label={t("commonActions")} alwaysDropdown>
        <button type="button" onClick={() => update.mutate({ id: user.id, body: { disabled: !user.disabled_at } })} disabled={user.id === currentUserID && !user.disabled_at}>{user.disabled_at ? t("adminEnableUser") : t("adminDisableUser")}</button>
        <button type="button" disabled={user.auth_provider !== "local"} onClick={() => setResetUser(user)}>{t("adminResetPassword")}</button>
        <button type="button" className="danger" disabled={user.id === currentUserID} onClick={() => setDeleteUser(user)}>{t("commonDelete")}</button>
      </ActionMenu>,
    ])} />
    </div> : <Empty title={t("adminNoResults")} body={t("adminNoResultsBody")} />}
    {deleteUser && <ConfirmDialog title={t("commonDelete")} body={t("adminDeleteUserConfirm")} confirmLabel={t("commonDelete")} danger onConfirm={() => remove.mutate(deleteUser.id)} onClose={() => setDeleteUser(null)} />}
    {resetUser && <ResetPasswordModal user={resetUser} onClose={() => setResetUser(null)} />}
  </Modal>;
}

function ResetPasswordModal({ user, onClose }: { user: AdminUser; onClose: () => void }) {
  const { t } = useI18n();
  const reset = useMutation({ mutationFn: (body: Record<string, string>) => api.resetAdminUserPassword(user.id, body), onSuccess: onClose });
  return <Modal title={t("adminResetPasswordTitle")} onClose={onClose} stacked closeOnEscape={false} className="admin-settings-modal">
    <form className="stack" onSubmit={(event) => formSubmit(event, (body) => reset.mutate(body))}>
      <div className="admin-form-subject"><KeyRound /><div><strong>{user.display_name || user.email}</strong><small>{user.email}</small></div></div>
      <Field label={t("adminNewPassword")} name="password" type="password" required />
      <ErrorMessage error={reset.error} />
      <ModalActions onCancel={onClose} submit={t("adminSaveNewPassword")} />
    </form>
  </Modal>;
}

function AdminOrgsModal({ orgs, loading, error, onClose }: { orgs: AdminOrg[]; loading: boolean; error: unknown; onClose: () => void }) {
  const { t } = useI18n();
  const [selected, setSelected] = useState<AdminOrg | null>(null);
  const [deleteOrg, setDeleteOrg] = useState<AdminOrg | null>(null);
  const queryClient = useQueryClient();
  const remove = useMutation({
    mutationFn: (id: string) => api.deleteAdminOrg(id),
    onSuccess: async (_, id) => {
      if (selected?.id === id) setSelected(null);
      await queryClient.invalidateQueries();
    },
  });
  return <Modal title={t("adminOrgsTitle")} onClose={onClose} wide className="admin-list-modal">
    <div className="admin-list-intro"><span>{t("adminOrgsBody")}</span><span className="admin-count">{orgs.length}</span></div>
    <ErrorMessage error={error || remove.error} />
    {loading ? <div className="admin-loading">{t("loading")}</div> : !orgs.length ? <Empty title={t("adminNoResults")} body={t("adminOrgsBody")} /> : <div className="admin-orgs-table">
    <SimpleTable headers={[t("orgs"), t("commonRole"), t("commonActions")]} rows={orgs.map((org) => [
      <strong>{org.name}</strong>,
      roleText(org.role, t),
      <span className="inline-actions">
        <button type="button" onClick={() => setSelected(org)}>{t("members")}</button>
        <button type="button" className="danger" onClick={() => setDeleteOrg(org)}>{t("commonDelete")}</button>
      </span>,
    ])} /></div>}
    {deleteOrg && <ConfirmDialog title={t("commonDelete")} body={t("adminDeleteOrgConfirm")} confirmLabel={t("commonDelete")} danger onConfirm={() => remove.mutate(deleteOrg.id)} onClose={() => setDeleteOrg(null)} />}
    {selected && <AdminOrgDrawer org={selected} onClose={() => setSelected(null)} />}
  </Modal>;
}

function AdminOrgDrawer({ org, onClose }: { org: AdminOrg; onClose: () => void }) {
  const { t } = useI18n();
  const members = useQuery({ queryKey: ["admin-org-members", org.id], queryFn: () => api.adminOrgMembers(org.id) });
  const queryClient = useQueryClient();
  const update = useMutation({ mutationFn: ({ userID, role }: { userID: string; role: string }) => api.adminUpdateOrgMember(org.id, userID, { role }), onSuccess: async () => queryClient.invalidateQueries() });
  return <Drawer title={org.name} subtitle={t("adminOrgMembersTitle")} onClose={onClose}>
    <ErrorMessage error={members.error || update.error} />
    <div className="member-card-list admin-member-list">
      {(members.data?.members || []).map((member) => <article className="member-card" key={member.user_id}>
        <div className="admin-user-identity"><span className="admin-avatar">{(member.display_name || member.email).slice(0, 1).toUpperCase()}</span><UserCell member={member} /></div>
        {member.role === "owner" ? <span className="badge info">{t("roleOwner")}</span> : <Select aria-label={`${t("commonRole")}: ${member.email}`} value={member.role} disabled={update.isPending} onChange={(role) => update.mutate({userID: member.user_id, role})} options={[{value:"admin",label:t("roleAdmin")},{value:"member",label:t("roleMember")}]} />}
      </article>)}
    </div>
  </Drawer>;
}

function ProviderModal({ title, action, onClose }: { title: string; action: (body: Record<string, unknown>) => Promise<void>; onClose: () => void }) {
  const { t } = useI18n();
  const mutation = useMutation({ mutationFn: action, onSuccess: onClose });
  return <Modal title={title} onClose={onClose} closeOnEscape={false} className="admin-settings-modal">
    <form className="stack" onSubmit={(event) => formSubmit(event, (body) => mutation.mutate(body))}>
      <Field label={t("adminProviderEnable")} name="enabled" />
      <Field label={t("adminProviderClientID")} name="client_id" />
      <Field label={t("adminProviderClientSecret")} name="client_secret" type="password" />
      <Field label={t("adminProviderRedirect")} name="redirect_url" />
      <ErrorMessage error={mutation.error} />
      <ModalActions onCancel={onClose} submit={t("save")} />
    </form>
  </Modal>;
}
