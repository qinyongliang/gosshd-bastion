import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useState } from "react";
import { api } from "../api";
import { Field, Metric, Modal, ModalActions, Panel, SimpleTable } from "../components/ui";
import { useI18n } from "../i18n";
import { formSubmit, roleText } from "../lib/forms";
import type { ConsoleData } from "../types";

export function OrganizationsPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const [modal, setModal] = useState<"" | "create" | "join">("");
  const queryClient = useQueryClient();
  const create = useMutation({ mutationFn: api.createOrg, onSuccess: async (out) => { data.setActiveOrgID(out.organization.id); setModal(""); await queryClient.invalidateQueries(); } });
  const join = useMutation({ mutationFn: (body: Record<string, string>) => api.joinOrg(body.code), onSuccess: async (out) => { data.setActiveOrgID(out.organization.id); setModal(""); await queryClient.invalidateQueries(); } });
  return (
    <>
      <section className="resource-head">
        <div><small>{t("shellProduct")}</small><h2>{t("orgs")}</h2><p>{t("orgPageBody")}</p></div>
        <div className="resource-actions">
          <button type="button" onClick={() => setModal("join")}>{t("orgJoin")}</button>
          <button type="button" className="primary" onClick={() => setModal("create")}><Plus />{t("orgCreate")}</button>
        </div>
      </section>
      <div className="metrics">
        <Metric label={t("orgMetricTotal")} value={data.orgs.length} />
        <Metric label={t("orgMetricShared")} value={data.orgs.filter((item) => !item.is_personal).length} />
        <Metric label={t("orgMetricPersonal")} value={data.orgs.filter((item) => item.is_personal).length} />
      </div>
      <Panel title={t("orgListTitle")} subtitle={t("orgListBody")}>
        <SimpleTable headers={[t("commonName"), t("orgType"), t("commonRole"), t("commonActions")]} rows={data.orgs.map((org) => [
          <strong>{org.name}</strong>,
          org.is_personal ? t("orgPersonal") : t("orgShared"),
          roleText(org.role, t),
          <button type="button" onClick={() => data.setActiveOrgID(org.id)}>{t("commonSwitch")}</button>,
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
    </>
  );
}
