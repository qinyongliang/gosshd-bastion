import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useState } from "react";
import { api } from "../api";
import { Drawer, Field, Modal, ModalActions, Panel, Select, SelectButton, SimpleTable, Toggle } from "../components/ui";
import { useI18n } from "../i18n";
import { formSubmit, policyPayload } from "../lib/forms";
import type { ConsoleData, Policy } from "../types";

export function PoliciesPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const [modal, setModal] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [drawer, setDrawer] = useState<Policy | null>(null);
  const queryClient = useQueryClient();
  const create = useMutation({ mutationFn: api.createPolicy, onSuccess: async () => { setModal(false); await queryClient.invalidateQueries(); } });
  const remove = useMutation({ mutationFn: api.deletePolicy, onSuccess: async () => { setSelected([]); await queryClient.invalidateQueries(); } });
  const copy = useMutation({ mutationFn: api.copyPolicy, onSuccess: async () => queryClient.invalidateQueries() });
  return (
    <>
      <section className="resource-head">
        <div><small>{t("policy")}</small><h2>{t("commandPolicy")}</h2><p>{t("policyBody")}</p></div>
        <button type="button" className="primary" onClick={() => setModal(true)}><Plus />{t("policyCreate")}</button>
      </section>
      {selected.length > 0 && <div className="batch-bar"><select defaultValue="" onChange={(event) => { if (event.target.value === "delete") selected.forEach((id) => remove.mutate(id)); }}><option value="">{t("commonBatchAction")}</option><option value="delete">{t("policyBatchDelete")}</option></select></div>}
      <Panel title={t("policyList")} subtitle="">
        <SimpleTable headers={["", t("commonName"), t("commonDefault"), t("policyCapabilities"), t("commonActions")]} rows={data.policies.map((policy) => [
          <input type="checkbox" checked={selected.includes(policy.id)} onChange={(event) => setSelected(event.target.checked ? [...selected, policy.id] : selected.filter((id) => id !== policy.id))} />,
          <button type="button" className="row-link" onClick={() => setDrawer(policy)}><strong>{policy.name}</strong><small>{policy.llm_config_id ? t("policyLLM") : t("commonNoLLM")}</small></button>,
          policy.default_action === "allow" ? t("commonAllow") : t("commonDeny"),
          <span className="capability-row">{policy.allow_interactive && `${t("policyTerminal")} `}{policy.allow_port_forward && `${t("policyForward")} `}{policy.allow_upload && `${t("policyUpload")} `}{policy.allow_download && t("policyDownload")}</span>,
          <span className="inline-actions"><button type="button" onClick={() => copy.mutate(policy.id)}>{t("policyCopy")}</button><button type="button" onClick={() => remove.mutate(policy.id)}>{t("commonDelete")}</button></span>,
        ])} />
      </Panel>
      {modal && <PolicyFormModal data={data} onClose={() => setModal(false)} onSubmit={(body) => create.mutate({ ...body, owner_type: "organization", owner_id: data.activeOrg.id })} />}
      {drawer && <PolicyDrawer data={data} policy={drawer} onClose={() => setDrawer(null)} />}
    </>
  );
}

function PolicyFormModal({ data, onClose, onSubmit, policy }: { data: ConsoleData; onClose: () => void; onSubmit: (body: Record<string, unknown>) => void; policy?: Policy }) {
  const { t } = useI18n();
  return <Modal title={policy ? t("policyEditTitle") : t("policyCreateTitle")} onClose={onClose}>
    <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => onSubmit(policyPayload(body)))}>
      <Field label={t("commonName")} name="name" defaultValue={policy?.name || ""} required />
      <Select label={t("policyDefaultAction")} name="default_action" defaultValue={policy?.default_action || "deny"} options={[["deny", t("commonDeny")], ["allow", t("commonAllow")]]} />
      <Select label={t("policyLLM")} name="llm_config_id" defaultValue={policy?.llm_config_id || ""} options={[["", t("commonNotUse")], ...data.llms.map((item): [string, string] => [item.id, item.name])]} />
      <Select label={t("policyPrompt")} name="llm_prompt_id" defaultValue={policy?.llm_prompt_id || ""} options={[["", t("commonDefault")], ...data.prompts.map((item): [string, string] => [item.id, item.title])]} />
      <label className="field span-two"><span>{t("policyIPAllowlist")}</span><textarea name="ip_allowlist" defaultValue={policy?.ip_allowlist || ""} placeholder="private, 10.0.0.0/8, 192.168.1.1-192.168.1.20" /></label>
      <Toggle name="allow_interactive" label={t("policyAllowInteractive")} defaultChecked={policy?.allow_interactive} />
      <Toggle name="allow_port_forward" label={t("policyAllowPortForward")} defaultChecked={policy?.allow_port_forward} />
      <Toggle name="allow_upload" label={t("policyAllowUpload")} defaultChecked={policy?.allow_upload} />
      <Toggle name="allow_download" label={t("policyAllowDownload")} defaultChecked={policy?.allow_download} />
      <ModalActions onCancel={onClose} submit={policy ? t("save") : t("commonCreate")} />
    </form>
  </Modal>;
}

function PolicyDrawer({ data, policy, onClose }: { data: ConsoleData; policy: Policy; onClose: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const update = useMutation({ mutationFn: (body: Record<string, unknown>) => api.updatePolicy(policy.id, body), onSuccess: async () => queryClient.invalidateQueries() });
  const rule = useMutation({ mutationFn: (body: Record<string, string>) => api.addRule(policy.id, body), onSuccess: async () => queryClient.invalidateQueries() });
  const bindTarget = useMutation({ mutationFn: (id: string) => api.bindTarget(policy.id, id), onSuccess: async () => queryClient.invalidateQueries() });
  const bindTag = useMutation({ mutationFn: (tag: string) => api.bindTargetTag(policy.id, { owner_type: "organization", owner_id: data.activeOrg.id, tag }), onSuccess: async () => queryClient.invalidateQueries() });
  const bindGroup = useMutation({ mutationFn: (id: string) => api.bindGroup(policy.id, id), onSuccess: async () => queryClient.invalidateQueries() });
  return <Drawer title={policy.name} subtitle={t("policyEditBody")} onClose={onClose}>
    <PolicyFormInline data={data} policy={policy} onSubmit={(body) => update.mutate(body)} />
    <section className="section-block embedded">
      <h3>{t("commonRules")}</h3>
      <form className="grid three" onSubmit={(event) => formSubmit(event, (body) => rule.mutate(body))}>
        <Select label={t("policyRuleType")} name="rule_type" options={[["whitelist", t("policyRuleWhitelist")], ["blacklist", t("policyRuleBlacklist")]]} />
        <Select label={t("policyMatch")} name="pattern_type" options={[["exact", t("policyMatchExact")], ["prefix", t("policyMatchPrefix")], ["contains", t("policyMatchContains")]]} />
        <Field label={t("policyRuleCommand")} name="pattern" required />
        <ModalActions submit={t("addRule")} />
      </form>
    </section>
    <section className="section-block embedded">
      <h3>{t("commonBind")}</h3>
      <div className="grid three">
        <SelectButton label={t("policyBindService")} items={data.targets.map((item): [string, string] => [item.id, item.name])} onSelect={(id) => bindTarget.mutate(id)} />
        <SelectButton label={t("policyBindTag")} items={[...new Set(data.targets.flatMap((item) => item.tags || []))].map((tag): [string, string] => [tag, tag])} onSelect={(tag) => bindTag.mutate(tag)} />
        <SelectButton label={t("policyBindGroup")} items={data.groups.map((item): [string, string] => [item.id, item.name])} onSelect={(id) => bindGroup.mutate(id)} />
      </div>
    </section>
  </Drawer>;
}

function PolicyFormInline({ data, policy, onSubmit }: { data: ConsoleData; policy: Policy; onSubmit: (body: Record<string, unknown>) => void }) {
  const { t } = useI18n();
  return <section className="section-block embedded">
    <h3>{t("commonBaseConfig")}</h3>
    <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => onSubmit(policyPayload(body)))}>
      <Field label={t("commonName")} name="name" defaultValue={policy.name} required />
      <Select label={t("policyDefaultAction")} name="default_action" defaultValue={policy.default_action} options={[["deny", t("commonDeny")], ["allow", t("commonAllow")]]} />
      <Select label={t("policyLLM")} name="llm_config_id" defaultValue={policy.llm_config_id || ""} options={[["", t("commonNotUse")], ...data.llms.map((item): [string, string] => [item.id, item.name])]} />
      <Select label={t("policyPrompt")} name="llm_prompt_id" defaultValue={policy.llm_prompt_id || ""} options={[["", t("commonDefault")], ...data.prompts.map((item): [string, string] => [item.id, item.title])]} />
      <label className="field span-two"><span>{t("policyIPAllowlist")}</span><textarea name="ip_allowlist" defaultValue={policy.ip_allowlist || ""} /></label>
      <Toggle name="allow_interactive" label={t("policyAllowInteractive")} defaultChecked={policy.allow_interactive} />
      <Toggle name="allow_port_forward" label={t("policyAllowPortForward")} defaultChecked={policy.allow_port_forward} />
      <Toggle name="allow_upload" label={t("policyAllowUpload")} defaultChecked={policy.allow_upload} />
      <Toggle name="allow_download" label={t("policyAllowDownload")} defaultChecked={policy.allow_download} />
      <ModalActions submit={t("save")} />
    </form>
  </section>;
}
