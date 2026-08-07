import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, UserPlus } from "lucide-react";
import { useState } from "react";
import { api } from "../api";
import { CommandBox, Field, Metric, Modal, ModalActions, Panel, Select, SimpleTable } from "../components/ui";
import { useI18n } from "../i18n";
import { appDescription } from "../lib/branding";
import { formSubmit, roleText } from "../lib/forms";
import type { ConsoleData, Organization } from "../types";

export function OrganizationsPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const [modal, setModal] = useState<"" | "create" | "join">("");
  const [inviteOrg, setInviteOrg] = useState<Organization | null>(null);
  const [inviteCode, setInviteCode] = useState("");
  const [neverExpires, setNeverExpires] = useState(true);
  const queryClient = useQueryClient();
  const create = useMutation({ mutationFn: api.createOrg, onSuccess: async (out) => { data.setActiveOrgID(out.organization.id); setModal(""); await queryClient.invalidateQueries(); } });
  const join = useMutation({ mutationFn: (body: Record<string, string>) => api.joinOrg(body.code), onSuccess: async (out) => { data.setActiveOrgID(out.organization.id); setModal(""); await queryClient.invalidateQueries(); } });
  const invite = useMutation({
    mutationFn: ({ orgID, body }: { orgID: string; body: Record<string, string> }) => api.invite(orgID, {
      role: body.role,
      expires_at: body.expires_at ? new Date(body.expires_at).toISOString() : "",
    }),
    onSuccess: (out) => setInviteCode(out.code),
  });
  const openInvite = (org: Organization) => {
    invite.reset();
    setInviteOrg(org);
    setInviteCode("");
    setNeverExpires(true);
  };
  const closeInvite = () => setInviteOrg(null);
  return (
    <>
      <section className="resource-head org-head">
        <div><small>{appDescription(data.runtime)}</small><h2>{t("orgs")}</h2><p>{t("orgPageBody")}</p></div>
        <div className="resource-actions">
          <button type="button" onClick={() => setModal("join")}>{t("orgJoin")}</button>
          <button type="button" className="primary" onClick={() => setModal("create")}><Plus />{t("orgCreate")}</button>
        </div>
      </section>
      <div className="metrics org-metrics">
        <Metric label={t("orgMetricTotal")} value={data.orgs.length} />
        <Metric label={t("orgMetricShared")} value={data.orgs.filter((item) => !item.is_personal).length} />
        <Metric label={t("orgMetricPersonal")} value={data.orgs.filter((item) => item.is_personal).length} />
      </div>
      <Panel title={t("orgListTitle")} subtitle={t("orgListBody")} className="org-list-panel">
        <SimpleTable headers={[t("commonName"), t("orgType"), t("commonRole"), t("commonActions")]} rows={data.orgs.map((org) => [
          <strong>{org.name}</strong>,
          org.is_personal ? t("orgPersonal") : t("orgShared"),
          roleText(org.role, t),
          <span className="row-actions">
            <button type="button" onClick={() => data.setActiveOrgID(org.id)}>{t("commonSwitch")}</button>
            {!org.is_personal && (org.role === "owner" || org.role === "admin") && <button type="button" onClick={() => openInvite(org)}><UserPlus />{t("orgInviteCreate")}</button>}
          </span>,
        ])} />
      </Panel>
      {modal === "create" && <Modal title={t("orgCreateTitle")} onClose={() => setModal("")} closeOnEscape={false}>
        <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => create.mutate(body))}>
          <Field label={t("orgName")} name="name" required />
          <Field label={t("orgSlug")} name="slug" required />
          <ModalActions onCancel={() => setModal("")} submit={t("orgCreate")} />
        </form>
      </Modal>}
      {modal === "join" && <Modal title={t("orgJoinTitle")} onClose={() => setModal("")} closeOnEscape={false}>
        <form className="stack" onSubmit={(event) => formSubmit(event, (body) => join.mutate(body))}>
          <Field label={t("orgJoinCode")} name="code" required />
          <ModalActions onCancel={() => setModal("")} submit={t("orgJoin")} />
        </form>
      </Modal>}
      {inviteOrg && <Modal title={t("orgInviteCreateTitle")} onClose={closeInvite} closeOnEscape={false}>
        {inviteCode ? <CommandBox label={t("orgJoinCode")} value={inviteCode} copyLabel={t("commonCopy")} /> : <form className="stack" onSubmit={(event) => formSubmit(event, (body) => invite.mutate({ orgID: inviteOrg.id, body }))}>
          <Select label={t("commonRole")} name="role" defaultValue="member" options={inviteOrg.role === "owner" ? [["member", t("roleMember")], ["admin", t("roleAdmin")]] : [["member", t("roleMember")]]} />
          <label className="toggle-row"><input type="checkbox" checked={neverExpires} onChange={(event) => setNeverExpires(event.target.checked)} /><span>{t("orgInviteNeverExpires")}</span></label>
          {!neverExpires && <Field label={t("orgInviteExpiresAt")} name="expires_at" type="datetime-local" required />}
          {invite.error && <div className="status error">{invite.error.message}</div>}
          <ModalActions onCancel={closeInvite} submit={t("orgInviteCreate")} />
        </form>}
      </Modal>}
    </>
  );
}
