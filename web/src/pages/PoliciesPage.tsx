import { useMutation, useQueryClient } from "@tanstack/react-query";
import { BrainCircuit, Copy, Pencil, Plus, Settings, Trash2, X } from "lucide-react";
import { ReactNode, useEffect, useMemo, useState } from "react";
import { api } from "../api";
import { Field, Modal, ModalActions, Panel, Select, SimpleTable, Tag, Toggle } from "../components/ui";
import { useI18n } from "../i18n";
import { formSubmit, policyPayload } from "../lib/forms";
import type { ConsoleData, LLMConfig, Policy, PolicyRule, PromptResource, Target, UserGroup } from "../types";
import { tagColor } from "../utils";

type ResourceMode = "" | "llms" | "prompts";

export function PoliciesPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const [modal, setModal] = useState(false);
  const [resourceMode, setResourceMode] = useState<ResourceMode>("");
  const [selected, setSelected] = useState<string[]>([]);
  const [drawerID, setDrawerID] = useState("");
  const queryClient = useQueryClient();
  const drawerPolicy = data.policies.find((policy) => policy.id === drawerID) || null;
  const create = useMutation({
    mutationFn: api.createPolicy,
    onSuccess: async () => {
      setModal(false);
      await queryClient.invalidateQueries();
    },
  });
  const remove = useMutation({
    mutationFn: api.deletePolicy,
    onSuccess: async () => {
      setSelected([]);
      if (drawerID) setDrawerID("");
      await queryClient.invalidateQueries();
    },
  });
  const copy = useMutation({ mutationFn: api.copyPolicy, onSuccess: async () => queryClient.invalidateQueries() });

  const toggleSelected = (id: string, checked: boolean) => {
    setSelected((current) => checked ? [...current, id] : current.filter((item) => item !== id));
  };
  const batchDelete = async () => {
    for (const id of selected) {
      await remove.mutateAsync(id);
    }
  };

  return (
    <>
      <div className="policy-page">
        <section className="resource-head policy-head">
          <div><small>{t("policy")}</small><h2>{t("commandPolicy")}</h2><p>{t("policyBody")}</p></div>
          <div className="resource-actions">
            <button type="button" onClick={() => setResourceMode("llms")}><BrainCircuit />{t("policyManageLLM")}</button>
            <button type="button" onClick={() => setResourceMode("prompts")}><Settings />{t("policyManagePrompts")}</button>
            <button type="button" className="primary" onClick={() => setModal(true)}><Plus />{t("policyCreate")}</button>
          </div>
        </section>
        {selected.length > 0 && <div className="batch-bar policy-batch">
          <span className="selection-summary">{selected.length} {t("commonSelected")}</span>
          <details className="batch-menu">
            <summary>{t("commonBatchAction")}</summary>
            <button type="button" className="danger" onClick={batchDelete}><Trash2 />{t("policyBatchDelete")}</button>
          </details>
        </div>}
        <Panel title={t("policyList")} subtitle={t("policyListBody")}>
          <SimpleTable headers={["", t("commonName"), t("policyResources"), t("commonBind"), t("policyCapabilities"), t("commonActions")]} rows={data.policies.map((policy) => [
            <input type="checkbox" checked={selected.includes(policy.id)} onChange={(event) => toggleSelected(policy.id, event.target.checked)} />,
            <PolicyNameCell policy={policy} data={data} />,
            <PolicyResourceCell policy={policy} data={data} />,
            <PolicyBindingSummary policy={policy} data={data} />,
            <PolicyCapabilities policy={policy} />,
            <span className="inline-actions">
              <button type="button" onClick={() => setDrawerID(policy.id)}><Pencil />{t("commonEdit")}</button>
              <button type="button" onClick={() => copy.mutate(policy.id)}><Copy />{t("policyCopy")}</button>
              <button type="button" className="danger" onClick={() => remove.mutate(policy.id)}><Trash2 />{t("commonDelete")}</button>
            </span>,
          ])} />
        </Panel>
      </div>
      {modal && <PolicyFormModal data={data} onClose={() => setModal(false)} onSubmit={(body) => create.mutate({ ...body, owner_type: "organization", owner_id: data.activeOrg.id })} />}
      {drawerPolicy && <PolicyDrawer data={data} policy={drawerPolicy} onClose={() => setDrawerID("")} />}
      {resourceMode === "llms" && <LLMManagerModal data={data} onClose={() => setResourceMode("")} />}
      {resourceMode === "prompts" && <PromptManagerModal data={data} onClose={() => setResourceMode("")} />}
    </>
  );
}

function PolicyNameCell({ policy, data }: { policy: Policy; data: ConsoleData }) {
  const { t } = useI18n();
  return <span className="policy-name-cell">
    <strong>{policy.name}</strong>
    <small>{policy.default_action === "allow" ? t("commonAllow") : t("commonDeny")}</small>
    <PolicyResourceBadges policy={policy} data={data} />
  </span>;
}

function PolicyResourceCell({ policy, data }: { policy: Policy; data: ConsoleData }) {
  const { t } = useI18n();
  const llm = data.llms.find((item) => item.id === policy.llm_config_id);
  const prompt = data.prompts.find((item) => item.id === policy.llm_prompt_id);
  return <span className="policy-resource-cell">
    <span><b>{t("policyLLM")}</b>{llm?.name || t("commonNoLLM")}</span>
    <span><b>{t("policyPrompt")}</b>{prompt?.title || t("commonDefault")}</span>
  </span>;
}

function PolicyResourceBadges({ policy, data }: { policy: Policy; data: ConsoleData }) {
  const { t } = useI18n();
  const llm = data.llms.find((item) => item.id === policy.llm_config_id);
  const prompt = data.prompts.find((item) => item.id === policy.llm_prompt_id);
  return <span className="policy-chip-row">
    {llm && <span className="pill">{llm.name}</span>}
    {prompt && <span className="pill">{prompt.title}</span>}
    {!llm && !prompt && <span className="muted">{t("policyNoResource")}</span>}
  </span>;
}

function PolicyBindingSummary({ policy, data }: { policy: Policy; data: ConsoleData }) {
  const { t } = useI18n();
  const targetIDs = policy.target_ids || [];
  const targets = targetIDs.map((id) => data.targets.find((target) => target.id === id)).filter(Boolean) as Target[];
  const groups = (policy.user_group_ids || []).map((id) => data.groups.find((group) => group.id === id)).filter(Boolean) as UserGroup[];
  const tags = policy.target_tags || [];
  const hasBindings = targets.length || tags.length || groups.length;
  return <span className="policy-binding-summary">
    {targets.map((target) => <span key={target.id} className="binding-pill"><b>{t("policyBindServiceShort")}</b>{target.name}<small>{target.alias}</small></span>)}
    {tags.map((tag) => <span key={tag} className="binding-pill"><b>{t("policyBindTagShort")}</b>{tag}</span>)}
    {groups.map((group) => <span key={group.id} className="binding-pill"><b>{t("policyBindGroupShort")}</b>{group.name}</span>)}
    {!hasBindings && <span className="muted">{t("policyNoBindings")}</span>}
  </span>;
}

function PolicyCapabilities({ policy }: { policy: Policy }) {
  const { t } = useI18n();
  const items = [
    policy.allow_interactive && t("policyTerminal"),
    policy.allow_port_forward && t("policyForward"),
    policy.allow_upload && t("policyUpload"),
    policy.allow_download && t("policyDownload"),
    policy.allow_manual_review && t("policyManualReview"),
  ].filter(Boolean) as string[];
  return <span className="capability-row">{items.length ? items.map((item) => <span key={item}>{item}</span>) : <span>{t("policyNoExtraCapabilities")}</span>}</span>;
}

function PolicyFormModal({ data, onClose, onSubmit }: { data: ConsoleData; onClose: () => void; onSubmit: (body: Record<string, unknown>) => void }) {
  const { t } = useI18n();
  return <Modal title={t("policyCreateTitle")} onClose={onClose} closeOnEscape={false}>
    <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => onSubmit(policyPayload(body)))}>
      <Field label={t("commonName")} name="name" required />
      <Select label={t("policyDefaultAction")} name="default_action" defaultValue="deny" options={[["deny", t("commonDeny")], ["allow", t("commonAllow")]]} />
      <Select label={t("policyLLM")} name="llm_config_id" options={[["", t("commonNotUse")], ...data.llms.map((item): [string, string] => [item.id, item.name])]} />
      <Select label={t("policyPrompt")} name="llm_prompt_id" options={[["", t("commonDefault")], ...data.prompts.map((item): [string, string] => [item.id, item.title])]} />
      <label className="field span-two"><span>{t("policyIPAllowlist")}</span><textarea name="ip_allowlist" placeholder="private, 10.0.0.0/8, 192.168.1.1-192.168.1.20" /></label>
      <Toggle name="allow_interactive" label={t("policyAllowInteractive")} />
      <Toggle name="allow_port_forward" label={t("policyAllowPortForward")} />
      <Toggle name="allow_upload" label={t("policyAllowUpload")} />
      <Toggle name="allow_download" label={t("policyAllowDownload")} />
      <ManualReviewControl />
      <ModalActions onCancel={onClose} submit={t("commonCreate")} />
    </form>
  </Modal>;
}

function PolicyDrawer({ data, policy, onClose }: { data: ConsoleData; policy: Policy; onClose: () => void }) {
  const { t } = useI18n();
  const [resourceMode, setResourceMode] = useState<ResourceMode>("");
  const queryClient = useQueryClient();
  const invalidate = async () => queryClient.invalidateQueries();
  const update = useMutation({ mutationFn: (body: Record<string, unknown>) => api.updatePolicy(policy.id, body), onSuccess: invalidate });
  const attachResource = async (overrides: Partial<Policy>) => {
    await api.updatePolicy(policy.id, policyToPayload(policy, overrides));
    await invalidate();
  };

  return <div className="policy-drawer-host">
    <DrawerShell title={policy.name} subtitle={t("policyEditBody")} onClose={onClose}>
      <section className="section-block embedded">
        <div className="policy-section-title"><div><h3>{t("commonBaseConfig")}</h3><p>{t("policyBaseConfigBody")}</p></div></div>
        <form className="policy-editor-form" onSubmit={(event) => formSubmit(event, (body) => update.mutate(policyPayload(body)))}>
          <Field label={t("commonName")} name="name" defaultValue={policy.name} required />
          <Select label={t("policyDefaultAction")} name="default_action" defaultValue={policy.default_action} options={[["deny", t("commonDeny")], ["allow", t("commonAllow")]]} />
          <ResourceSelect label={t("policyLLM")} name="llm_config_id" value={policy.llm_config_id || ""} emptyLabel={t("commonNotUse")} items={data.llms.map((item): [string, string] => [item.id, item.name])} onCreate={() => setResourceMode("llms")} onManage={() => setResourceMode("llms")} />
          <ResourceSelect label={t("policyPrompt")} name="llm_prompt_id" value={policy.llm_prompt_id || ""} emptyLabel={t("commonDefault")} items={data.prompts.map((item): [string, string] => [item.id, item.title])} onCreate={() => setResourceMode("prompts")} onManage={() => setResourceMode("prompts")} />
          <label className="field span-two"><span>{t("policyIPAllowlist")}</span><textarea name="ip_allowlist" defaultValue={policy.ip_allowlist || ""} placeholder="private, 10.0.0.0/8, 192.168.1.1-192.168.1.20" /></label>
          <div className="policy-toggle-grid span-two">
            <Toggle name="allow_interactive" label={t("policyAllowInteractive")} defaultChecked={policy.allow_interactive} />
            <Toggle name="allow_port_forward" label={t("policyAllowPortForward")} defaultChecked={policy.allow_port_forward} />
            <Toggle name="allow_upload" label={t("policyAllowUpload")} defaultChecked={policy.allow_upload} />
            <Toggle name="allow_download" label={t("policyAllowDownload")} defaultChecked={policy.allow_download} />
            <ManualReviewControl defaultChecked={policy.allow_manual_review} defaultSeconds={policy.manual_review_timeout_seconds} />
          </div>
          <ModalActions submit={t("save")} />
        </form>
      </section>
      <PolicyRulesEditor policy={policy} />
      <PolicyBindingsEditor data={data} policy={policy} />
    </DrawerShell>
    {resourceMode === "llms" && <LLMManagerModal data={data} stacked onClose={() => setResourceMode("")} onCreated={(config) => attachResource({ llm_config_id: config.id })} />}
    {resourceMode === "prompts" && <PromptManagerModal data={data} stacked onClose={() => setResourceMode("")} onCreated={(prompt) => attachResource({ llm_prompt_id: prompt.id })} />}
  </div>;
}

function DrawerShell({ title, subtitle, children, onClose }: { title: string; subtitle?: string; children: ReactNode; onClose: () => void }) {
  const { t } = useI18n();
  return <div className="drawer-layer">
    <button className="drawer-scrim" type="button" tabIndex={-1} aria-hidden="true" onClick={onClose} />
    <aside className="drawer policy-drawer">
      <header className="surface-head"><div><h2>{title}</h2>{subtitle && <p>{subtitle}</p>}</div><button className="icon-button" type="button" aria-label={t("close")} onClick={onClose}><X /></button></header>
      <div className="surface-body policy-drawer-body">{children}</div>
    </aside>
  </div>;
}

function ResourceSelect({ label, name, value, emptyLabel, items, onCreate, onManage }: { label: string; name: string; value: string; emptyLabel: string; items: (readonly [string, string])[]; onCreate: () => void; onManage: () => void }) {
  const { t } = useI18n();
  return <label className="field resource-select-field">
    <span>{label}</span>
    <div className="resource-select-row">
      <select key={`${name}-${value}`} name={name} defaultValue={value}>
        <option value="">{emptyLabel}</option>
        {items.map(([id, text]) => <option key={id} value={id}>{text}</option>)}
      </select>
      <button type="button" onClick={onCreate}><Plus />{t("commonNew")}</button>
      <button type="button" onClick={onManage}><Settings />{t("commonManage")}</button>
    </div>
  </label>;
}

function ManualReviewControl({ defaultChecked = false, defaultSeconds = 30 }: { defaultChecked?: boolean; defaultSeconds?: number }) {
  const { t } = useI18n();
  const [enabled, setEnabled] = useState(Boolean(defaultChecked));
  const seconds = Math.max(5, Math.min(300, Number(defaultSeconds || 30)));
  return <div className="manual-review-policy-control span-two">
    <label className="toggle-row">
      <input type="checkbox" name="allow_manual_review" defaultChecked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
      <span>{t("policyAllowManualReview")}</span>
    </label>
    {enabled ? (
      <label className="field manual-review-timeout-field">
        <span>{t("policyManualReviewTimeout")}</span>
        <input name="manual_review_timeout_seconds" type="number" min="5" max="300" step="1" defaultValue={seconds} />
      </label>
    ) : (
      <input type="hidden" name="manual_review_timeout_seconds" value={seconds} />
    )}
  </div>;
}

function PolicyRulesEditor({ policy }: { policy: Policy }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const rule = useMutation({ mutationFn: (body: Record<string, string>) => api.addRule(policy.id, body), onSuccess: async () => queryClient.invalidateQueries() });
  const remove = useMutation({ mutationFn: (ruleID: string) => api.deleteRule(policy.id, ruleID), onSuccess: async () => queryClient.invalidateQueries() });
  return <section className="section-block embedded">
    <div className="policy-section-title"><div><h3>{t("commonRules")}</h3><p>{t("policyRulesBody")}</p></div></div>
    <div className="policy-list-stack">
      {(policy.rules || []).map((item) => <RuleRow key={item.id} rule={item} onDelete={() => remove.mutate(item.id)} />)}
      {(policy.rules || []).length === 0 && <div className="policy-empty-line">{t("policyNoRules")}</div>}
    </div>
    <form className="policy-add-row" onSubmit={(event) => formSubmit(event, (body) => rule.mutate(body))}>
      <Select label={t("policyRuleType")} name="rule_type" options={[["whitelist", t("policyRuleWhitelist")], ["blacklist", t("policyRuleBlacklist")]]} />
      <Select label={t("policyMatch")} name="pattern_type" options={[["exact", t("policyMatchExact")], ["prefix", t("policyMatchPrefix")], ["contains", t("policyMatchContains")]]} />
      <Field label={t("policyRuleCommand")} name="pattern" required />
      <button type="submit" className="primary"><Plus />{t("addRule")}</button>
    </form>
  </section>;
}

function RuleRow({ rule, onDelete }: { rule: PolicyRule; onDelete: () => void }) {
  const { t } = useI18n();
  return <div className="policy-rule-card">
    <span className={rule.rule_type === "whitelist" ? "badge success" : "badge danger"}>{rule.rule_type === "whitelist" ? t("policyRuleWhitelist") : t("policyRuleBlacklist")}</span>
    <span className="badge info">{matchText(rule.pattern_type, t)}</span>
    <code>{rule.pattern}</code>
    <button type="button" className="icon-button danger" onClick={onDelete} aria-label={t("commonDelete")}><Trash2 /></button>
  </div>;
}

function PolicyBindingsEditor({ data, policy }: { data: ConsoleData; policy: Policy }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const invalidate = async () => queryClient.invalidateQueries();
  const bindTarget = useMutation({ mutationFn: (id: string) => api.bindTarget(policy.id, id), onSuccess: invalidate });
  const unbindTarget = useMutation({ mutationFn: (id: string) => api.unbindTarget(policy.id, id), onSuccess: invalidate });
  const bindTag = useMutation({ mutationFn: (tag: string) => api.bindTargetTag(policy.id, { owner_type: "organization", owner_id: data.activeOrg.id, tag }), onSuccess: invalidate });
  const unbindTag = useMutation({ mutationFn: (tag: string) => api.unbindTargetTag(policy.id, tag), onSuccess: invalidate });
  const bindGroup = useMutation({ mutationFn: (id: string) => api.bindGroup(policy.id, id), onSuccess: invalidate });
  const unbindGroup = useMutation({ mutationFn: (id: string) => api.unbindGroup(policy.id, id), onSuccess: invalidate });
  const targetIDs = policy.target_ids || [];
  const boundTargets = targetIDs.map((id) => data.targets.find((target) => target.id === id)).filter(Boolean) as Target[];
  const boundTags = policy.target_tags || [];
  const boundGroups = (policy.user_group_ids || []).map((id) => data.groups.find((group) => group.id === id)).filter(Boolean) as UserGroup[];
  const allTags = useMemo(() => [...new Set(data.targets.flatMap((item) => item.tags || []))].sort(), [data.targets]);

  return <section className="section-block embedded">
    <div className="policy-section-title"><div><h3>{t("commonBind")}</h3><p>{t("policyBindingsBody")}</p></div></div>
    <div className="policy-binding-grid">
      <RelationBlock
        title={t("policyBindService")}
        empty={t("policyNoBoundServices")}
        options={data.targets.filter((target) => !targetIDs.includes(target.id)).map((target): [string, string] => [target.id, `${target.name} / ${target.alias}`])}
        onAdd={(id) => bindTarget.mutate(id)}
        items={boundTargets.map((target) => <RemovableItem key={target.id} title={target.name} detail={target.alias} onRemove={() => unbindTarget.mutate(target.id)} />)}
      />
      <RelationBlock
        title={t("policyBindTag")}
        empty={t("policyNoBoundTags")}
        options={allTags.filter((tag) => !boundTags.includes(tag)).map((tag): [string, string] => [tag, tag])}
        onAdd={(tag) => bindTag.mutate(tag)}
        items={boundTags.map((tag) => <RemovableItem key={tag} title={<Tag tag={tag} color={tagColor(tag, tagColorMap(data.targets))} />} onRemove={() => unbindTag.mutate(tag)} />)}
      />
      <RelationBlock
        title={t("policyBindGroup")}
        empty={t("policyNoBoundGroups")}
        options={data.groups.filter((group) => !(policy.user_group_ids || []).includes(group.id)).map((group): [string, string] => [group.id, group.name])}
        onAdd={(id) => bindGroup.mutate(id)}
        items={boundGroups.map((group) => <RemovableItem key={group.id} title={group.name} detail={group.is_default ? t("membersGroups") : group.slug} onRemove={() => unbindGroup.mutate(group.id)} />)}
      />
    </div>
  </section>;
}

function RelationBlock({ title, empty, options, items, onAdd }: { title: string; empty: string; options: (readonly [string, string])[]; items: ReactNode[]; onAdd: (value: string) => void }) {
  const { t } = useI18n();
  const [value, setValue] = useState("");
  const add = () => {
    if (!value) return;
    onAdd(value);
    setValue("");
  };
  return <div className="relation-block">
    <div className="relation-block-head"><strong>{title}</strong></div>
    <div className="relation-list">{items.length ? items : <span className="policy-empty-line">{empty}</span>}</div>
    <div className="relation-add-row">
      <label className="field">
        <span>{t("add")}</span>
        <select value={value} disabled={!options.length} onChange={(event) => setValue(event.target.value)}>
          <option value="">{options.length ? t("commonSelectPlaceholder") : t("policyNoMoreOptions")}</option>
          {options.map(([id, text]) => <option key={id} value={id}>{text}</option>)}
        </select>
      </label>
      <button type="button" disabled={!value} onClick={add}><Plus />{t("add")}</button>
    </div>
  </div>;
}

function RemovableItem({ title, detail, onRemove }: { title: ReactNode; detail?: string; onRemove: () => void }) {
  const { t } = useI18n();
  return <span className="removable-item"><span><span className="removable-title">{title}</span>{detail && <small>{detail}</small>}</span><button type="button" className="small danger relation-remove" aria-label={t("commonDelete")} onClick={onRemove}><X />{t("commonDelete")}</button></span>;
}

function LLMManagerModal({ data, onClose, onCreated, stacked = false }: { data: ConsoleData; onClose: () => void; onCreated?: (config: LLMConfig) => void | Promise<void>; stacked?: boolean }) {
  const { t } = useI18n();
  const [selectedID, setSelectedID] = useState(data.llms[0]?.id || "__new__");
  const queryClient = useQueryClient();
  const editing = selectedID === "__new__" ? null : data.llms.find((config) => config.id === selectedID) || null;
  useEffect(() => {
    if (selectedID === "__new__") return;
    if (!data.llms.some((config) => config.id === selectedID)) {
      setSelectedID(data.llms[0]?.id || "__new__");
    }
  }, [data.llms, selectedID]);
  const save = useMutation({
    mutationFn: (body: Record<string, string>) => {
      const payload = {
        owner_type: "organization",
        owner_id: data.activeOrg.id,
        name: body.name,
        base_url: body.base_url,
        api_key: body.api_key || "",
        model: body.model,
        timeout_seconds: Number(body.timeout_seconds || 10),
      };
      return editing ? api.updateLLMConfig(editing.id, payload) : api.createLLMConfig(payload);
    },
    onSuccess: async (out) => {
      await queryClient.invalidateQueries();
      if (!editing && onCreated) {
        await onCreated(out.config);
        onClose();
        return;
      }
      onClose();
    },
  });
  const remove = useMutation({
    mutationFn: api.deleteLLMConfig,
    onSuccess: async (_out, id) => {
      if (selectedID === id) setSelectedID(data.llms.find((config) => config.id !== id)?.id || "__new__");
      await queryClient.invalidateQueries();
    },
  });

  return <Modal title={t("policyLLMConfigTitle")} onClose={onClose} wide stacked={stacked} className="resource-modal" closeOnEscape={false}>
    <div className="resource-manager">
      <ResourceListPanel title={t("policyLLMConfigTitle")} createLabel={t("policyCreateLLM")} onCreate={() => setSelectedID("__new__")}>
        {data.llms.map((config) => <ResourceRow key={config.id} active={selectedID === config.id} title={config.name} detail={`${config.model} · ${config.base_url}`} meta={`${config.timeout_seconds}s`} onSelect={() => setSelectedID(config.id)} onDelete={() => remove.mutate(config.id)} />)}
        {data.llms.length === 0 && <div className="policy-empty-line">{t("policyNoLLMConfigs")}</div>}
      </ResourceListPanel>
      <form key={editing?.id || "__new__"} className="resource-form" onSubmit={(event) => formSubmit(event, (body) => save.mutate(body))}>
        <div className="resource-editor-title"><h3>{editing ? t("commonEdit") : t("policyCreateLLM")}</h3>{editing && <small>{editing.model}</small>}</div>
        <Field label={t("policyLLMName")} name="name" defaultValue={editing?.name || ""} required />
        <Field label={t("policyLLMBaseURL")} name="base_url" defaultValue={editing?.base_url || ""} required />
        <Field label={t("policyLLMModel")} name="model" defaultValue={editing?.model || ""} required />
        <Field label={t("policyLLMAPIKey")} name="api_key" type="password" placeholder={editing ? t("policyKeepAPIKey") : ""} />
        <Field label={t("policyLLMTimeout")} name="timeout_seconds" type="number" defaultValue={String(editing?.timeout_seconds || 10)} />
        <ModalActions onCancel={editing ? () => setSelectedID("__new__") : undefined} submit={editing ? t("save") : t("commonCreate")} />
      </form>
    </div>
  </Modal>;
}

function PromptManagerModal({ data, onClose, onCreated, stacked = false }: { data: ConsoleData; onClose: () => void; onCreated?: (prompt: PromptResource) => void | Promise<void>; stacked?: boolean }) {
  const { t } = useI18n();
  const [selectedID, setSelectedID] = useState(data.prompts[0]?.id || "__new__");
  const queryClient = useQueryClient();
  const editing = selectedID === "__new__" ? null : data.prompts.find((prompt) => prompt.id === selectedID) || null;
  useEffect(() => {
    if (selectedID === "__new__") return;
    if (!data.prompts.some((prompt) => prompt.id === selectedID)) {
      setSelectedID(data.prompts[0]?.id || "__new__");
    }
  }, [data.prompts, selectedID]);
  const save = useMutation({
    mutationFn: (body: Record<string, string>) => {
      const payload = { owner_type: "organization", owner_id: data.activeOrg.id, title: body.title, content: body.content };
      return editing ? api.updatePrompt(editing.id, payload) : api.createPrompt(payload);
    },
    onSuccess: async (out) => {
      await queryClient.invalidateQueries();
      if (!editing && onCreated) {
        await onCreated(out.prompt);
        onClose();
        return;
      }
      onClose();
    },
  });
  const remove = useMutation({
    mutationFn: api.deletePrompt,
    onSuccess: async (_out, id) => {
      if (selectedID === id) setSelectedID(data.prompts.find((prompt) => prompt.id !== id)?.id || "__new__");
      await queryClient.invalidateQueries();
    },
  });

  return <Modal title={t("policyPromptConfigTitle")} onClose={onClose} wide stacked={stacked} className="resource-modal" closeOnEscape={false}>
    <div className="resource-manager">
      <ResourceListPanel title={t("policyPromptConfigTitle")} createLabel={t("policyCreatePrompt")} onCreate={() => setSelectedID("__new__")}>
        {data.prompts.map((prompt) => <ResourceRow key={prompt.id} active={selectedID === prompt.id} title={prompt.title} detail={prompt.content} meta={prompt.is_readonly ? t("policyReadonlyPrompt") : ""} onSelect={() => setSelectedID(prompt.id)} onDelete={() => remove.mutate(prompt.id)} disabled={prompt.is_readonly} />)}
        {data.prompts.length === 0 && <div className="policy-empty-line">{t("policyNoPrompts")}</div>}
      </ResourceListPanel>
      <form key={editing?.id || "__new__"} className="resource-form" onSubmit={(event) => formSubmit(event, (body) => save.mutate(body))}>
        <div className="resource-editor-title"><h3>{editing ? t("commonEdit") : t("policyCreatePrompt")}</h3>{editing?.is_readonly && <small>{t("policyReadonlyPrompt")}</small>}</div>
        <Field label={t("commonTitle")} name="title" defaultValue={editing?.title || ""} required disabled={Boolean(editing?.is_readonly)} />
        <label className="field"><span>{t("policyPromptContent")}</span><textarea name="content" defaultValue={editing?.content || ""} required disabled={Boolean(editing?.is_readonly)} /></label>
        {editing?.is_readonly ? <div className="policy-empty-line">{t("policyReadonlyPrompt")}</div> : <ModalActions onCancel={editing ? () => setSelectedID("__new__") : undefined} submit={editing ? t("save") : t("commonCreate")} />}
      </form>
    </div>
  </Modal>;
}

function ResourceListPanel({ title, createLabel, onCreate, children }: { title: string; createLabel: string; onCreate: () => void; children: ReactNode }) {
  return <section className="resource-list-panel">
    <header className="resource-list-head"><strong>{title}</strong><button type="button" onClick={onCreate}><Plus />{createLabel}</button></header>
    <div className="resource-list">{children}</div>
  </section>;
}

function ResourceRow({ title, detail, meta, active, onSelect, onDelete, disabled = false }: { title: string; detail: string; meta?: string; active?: boolean; onSelect: () => void; onDelete: () => void; disabled?: boolean }) {
  const { t } = useI18n();
  return <div className={`resource-row${active ? " active" : ""}`}>
    <button type="button" className="resource-row-main" onClick={onSelect}>
      <strong>{title}</strong><small>{detail}</small>{meta && <em>{meta}</em>}
    </button>
    <span className="inline-actions">
      <button type="button" onClick={onSelect} disabled={disabled}><Pencil />{t("commonEdit")}</button>
      <button type="button" className="danger" onClick={onDelete} disabled={disabled}><Trash2 />{t("commonDelete")}</button>
    </span>
  </div>;
}

function policyToPayload(policy: Policy, overrides: Partial<Policy> = {}) {
  return {
    name: policy.name,
    default_action: policy.default_action,
    llm_config_id: policy.llm_config_id || "",
    llm_prompt_id: policy.llm_prompt_id || "",
    ip_allowlist: policy.ip_allowlist || "",
    allow_interactive: Boolean(policy.allow_interactive),
    allow_port_forward: Boolean(policy.allow_port_forward),
    allow_upload: Boolean(policy.allow_upload),
    allow_download: Boolean(policy.allow_download),
    allow_manual_review: Boolean(policy.allow_manual_review),
    manual_review_timeout_seconds: policy.manual_review_timeout_seconds || 30,
    ...overrides,
  };
}

function tagColorMap(targets: Target[]) {
  return targets.reduce<Record<string, string>>((out, target) => ({ ...out, ...(target.tag_colors || {}) }), {});
}

function matchText(type: string, t: (key: string) => string) {
  if (type === "prefix") return t("policyMatchPrefix");
  if (type === "contains") return t("policyMatchContains");
  return t("policyMatchExact");
}
