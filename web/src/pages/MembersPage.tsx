import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { api } from "../api";
import { Field, Modal, ModalActions, Panel, Select, SimpleTable, Toolbar, UserCell } from "../components/ui";
import { useI18n } from "../i18n";
import { formSubmit, formatDate, roleText, sortMembers } from "../lib/forms";
import type { ConsoleData } from "../types";

export function MembersPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<"role" | "name" | "newest">("role");
  const [modal, setModal] = useState<"" | "add" | "groups" | "transfer">("");
  const queryClient = useQueryClient();
  const add = useMutation({ mutationFn: (body: Record<string, string>) => api.addOrgMember(data.activeOrg.id, body), onSuccess: async () => { setModal(""); await queryClient.invalidateQueries(); } });
  const group = useMutation({ mutationFn: (body: Record<string, string>) => api.createGroup(data.activeOrg.id, body), onSuccess: async () => queryClient.invalidateQueries() });
  const update = useMutation({ mutationFn: ({ userID, role }: { userID: string; role: string }) => api.updateOrgMember(data.activeOrg.id, userID, { role }), onSuccess: async () => queryClient.invalidateQueries() });
  const transfer = useMutation({ mutationFn: (body: Record<string, string>) => api.transferOrgOwner(data.activeOrg.id, body.user_id), onSuccess: async () => { setModal(""); await queryClient.invalidateQueries(); } });
  const members = useMemo(() => sortMembers(data.members, query, sort), [data.members, query, sort]);
  return (
    <>
      <section className="resource-head members-head">
        <div><small>{t("orgs")}</small><h2>{t("members")}</h2><p>{t("membersBody")}</p></div>
        <div className="resource-actions">
          <button type="button" onClick={() => setModal("groups")}>{t("membersGroups")}</button>
          <button type="button" onClick={() => setModal("transfer")}>{t("membersTransfer")}</button>
          <button type="button" className="primary" onClick={() => setModal("add")}><Plus />{t("addMember")}</button>
        </div>
      </section>
      <div className="member-access-summary">
        <span><strong>{data.members.length}</strong><small>{t("members")}</small></span>
        <span><strong>{data.members.filter((item) => item.role === "owner" || item.role === "admin").length}</strong><small>{t("roleAdmin")}</small></span>
        <span><strong>{data.groups.length}</strong><small>{t("membersGroups")}</small></span>
      </div>
      <Toolbar query={query} setQuery={setQuery}>
        <select value={sort} onChange={(event) => setSort(event.target.value as typeof sort)}>
          <option value="role">{t("membersSortRole")}</option>
          <option value="name">{t("membersSortName")}</option>
          <option value="newest">{t("membersSortNewest")}</option>
        </select>
      </Toolbar>
      <Panel title={t("members")} subtitle={t("membersCannotOwnerAgain")} className="members-table-panel">
        <SimpleTable headers={[t("auditTableUser"), t("commonRole"), t("membersJoinedAt"), t("commonActions")]} rows={members.map((member) => [
          <UserCell member={member} />,
          roleText(member.role, t),
          formatDate(member.created_at || member.joined_at),
          member.role === "owner" ? <span className="badge info">{t("roleOwner")}</span> : <span className="inline-actions">
            <button type="button" onClick={() => update.mutate({ userID: member.user_id, role: "admin" })}>{t("roleAdmin")}</button>
            <button type="button" onClick={() => update.mutate({ userID: member.user_id, role: "member" })}>{t("roleMember")}</button>
          </span>,
        ])} />
      </Panel>
      {modal === "add" && <Modal title={t("membersAddTitle")} onClose={() => setModal("")} closeOnEscape={false}>
        <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => add.mutate(body))}>
          <Field label={t("commonEmail")} name="email" />
          <Field label={t("membersUserID")} name="user_id" />
          <Select label={t("commonRole")} name="role" options={[["member", t("roleMember")], ["admin", t("roleAdmin")]]} />
          <ModalActions onCancel={() => setModal("")} submit={t("addMember")} />
        </form>
      </Modal>}
      {modal === "groups" && <Modal title={t("membersGroupTitle")} onClose={() => setModal("")} closeOnEscape={false}>
        <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => group.mutate(body))}>
          <Field label={t("membersGroupName")} name="name" required />
          <Field label="group-slug" name="slug" required />
          <ModalActions onCancel={() => setModal("")} submit={t("addUserGroup")} />
        </form>
        <SimpleTable headers={[t("commonName"), "Slug"]} rows={data.groups.map((item) => [item.name, item.slug])} />
      </Modal>}
      {modal === "transfer" && <Modal title={t("membersTransferTitle")} onClose={() => setModal("")} closeOnEscape={false}>
        <form className="stack" onSubmit={(event) => formSubmit(event, (body) => transfer.mutate(body))}>
          <Select label={t("membersNewOwner")} name="user_id" options={data.members.filter((item) => item.role !== "owner").map((item) => [item.user_id, item.display_name || item.email])} />
          <ModalActions onCancel={() => setModal("")} submit={t("membersTransfer")} />
        </form>
      </Modal>}
    </>
  );
}
