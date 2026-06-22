import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../api";
import { Drawer, Field, Modal, ModalActions, SimpleTable, Toggle, UserCell } from "../components/ui";
import { useI18n } from "../i18n";
import { formSubmit, roleText } from "../lib/forms";
import type { AdminOrg, AdminUser, ConsoleData } from "../types";

export function SystemAdminPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const [modal, setModal] = useState<"" | "users" | "orgs" | "auth" | "dingtalk" | "ldap">("");
  const adminUsers = useQuery({ queryKey: ["admin-users"], queryFn: api.adminUsers, enabled: data.user.is_system_admin && modal === "users" });
  const adminOrgs = useQuery({ queryKey: ["admin-orgs"], queryFn: api.adminOrgs, enabled: data.user.is_system_admin && modal === "orgs" });
  return (
    <>
      <section className="resource-head">
        <div><small>{t("shellProduct")}</small><h2>{t("systemAdminTitle")}</h2><p>{t("systemAdminBody")}</p></div>
        <div className="resource-actions">
          <button type="button" onClick={() => setModal("auth")}>{t("adminAuthSettings")}</button>
          <button type="button" onClick={() => setModal("dingtalk")}>{t("adminProviderDingTalk")}</button>
          <button type="button" className="primary" onClick={() => setModal("ldap")}>{t("adminProviderLDAP")}</button>
        </div>
      </section>
      <div className="identity-grid">
        <button type="button" className="admin-card" onClick={() => setModal("users")}><strong>{t("adminAccountsTitle")}</strong><span>{t("adminAccountsBody")}</span></button>
        <button type="button" className="admin-card" onClick={() => setModal("orgs")}><strong>{t("adminOrgsTitle")}</strong><span>{t("adminOrgsBody")}</span></button>
      </div>
      {modal === "users" && <AdminUsersModal users={adminUsers.data?.users || []} onClose={() => setModal("")} />}
      {modal === "orgs" && <AdminOrgsModal orgs={(adminOrgs.data?.organizations || []).filter((org) => !org.is_personal)} onClose={() => setModal("")} />}
      {modal === "auth" && <AuthSettingsModal onClose={() => setModal("")} />}
      {modal === "dingtalk" && <ProviderModal title={t("adminProviderDingTalk")} action={api.updateDingTalkSettings} onClose={() => setModal("")} />}
      {modal === "ldap" && <ProviderModal title={t("adminProviderLDAP")} action={api.updateLDAPSettings} onClose={() => setModal("")} />}
    </>
  );
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

function AdminUsersModal({ users, onClose }: { users: AdminUser[]; onClose: () => void }) {
  const { t } = useI18n();
  const [resetUser, setResetUser] = useState<AdminUser | null>(null);
  const queryClient = useQueryClient();
  const update = useMutation({ mutationFn: ({ id, is_system_admin }: { id: string; is_system_admin: boolean }) => api.updateAdminUser(id, { is_system_admin }), onSuccess: async () => queryClient.invalidateQueries() });
  return <Modal title={t("adminAccountsTitle")} onClose={onClose} wide>
    <SimpleTable headers={[t("commonEmail"), t("adminLoginSource"), t("adminSystemAdminColumn"), t("commonActions")]} rows={users.map((user) => [
      <UserCell member={{ user_id: user.id, email: user.email, display_name: user.display_name, role: "member" }} />,
      user.auth_provider === "local" ? t("commonLocal") : user.auth_provider,
      <select defaultValue={user.is_system_admin ? "admin" : "user"} onChange={(event) => update.mutate({ id: user.id, is_system_admin: event.target.value === "admin" })}><option value="user">{t("adminUser")}</option><option value="admin">{t("adminRole")}</option></select>,
      <button type="button" disabled={user.auth_provider !== "local"} onClick={() => setResetUser(user)}>{t("adminResetPassword")}</button>,
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
  return <Modal title={t("adminOrgsTitle")} onClose={onClose} wide>
    <SimpleTable headers={[t("orgs"), t("commonRole"), t("commonActions")]} rows={orgs.map((org) => [
      <strong>{org.name}</strong>,
      roleText(org.role, t),
      <button type="button" onClick={() => setSelected(org)}>{t("members")}</button>,
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
