import { useMutation, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { Plus } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { api, type Enrollment } from "../api";
import { CommandBox, CopyButton, Drawer, Empty, Field, Metric, Modal, ModalActions, Panel, Select, SimpleTable, Tag, TagList, Toolbar } from "../components/ui";
import { useI18n } from "../i18n";
import { formSubmit, formValues } from "../lib/forms";
import type { ConsoleData, Target } from "../types";
import { splitTags, tagColor, targetEndpoint } from "../utils";

export function TargetsPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [query, setQuery] = useState("");
  const [modal, setModal] = useState(false);
  const [drawer, setDrawer] = useState<Target | null>(null);
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null);
  const filtered = data.targets.filter((target) => [target.name, target.alias, target.host, target.remote_username, ...(target.tags || [])].join(" ").toLowerCase().includes(query.toLowerCase()));
  const refreshTargets = () => void queryClient.invalidateQueries({ queryKey: ["targets"] });

  useEffect(() => {
    refreshTargets();
  }, [data.activeOrg.id]);

  return (
    <>
      <section className="resource-head">
        <div><small>{t("shellProduct")}</small><h2>{t("services")}</h2><p>{t("servicePageBody")}</p></div>
        <button type="button" className="primary" onClick={() => setModal(true)}><Plus />{t("addService")}</button>
      </section>
      <div className="metrics">
        <Metric label={t("serviceTotal")} value={data.targets.length} />
        <Metric label={t("serviceDirect")} value={data.targets.filter((item) => item.target_type === "direct").length} />
        <Metric label={t("privateNode")} value={data.targets.filter((item) => item.target_type === "agent").length} />
        <Metric label={t("commonTag")} value={new Set(data.targets.flatMap((item) => item.tags || [])).size} />
      </div>
      <Toolbar query={query} setQuery={setQuery} />
      <Panel title={t("serviceTableService")} subtitle="">
        {filtered.length ? <SimpleTable headers={[t("serviceTableService"), t("serviceTableAlias"), t("commonEndpoint"), t("commonAuth"), t("commonTag"), t("commonActions")]} rows={filtered.map((target) => [
          <strong>{target.name}</strong>,
          <code>{target.alias}</code>,
          target.target_type === "agent" ? t("privateNode") : targetEndpoint(target),
          target.auth_type === "private_key" ? t("serviceAuthPrivateKey") : t("serviceAuthPassword"),
          <TagList target={target} />,
          <span className="inline-actions">
            <CopyButton value={`ssh -p ${data.runtime.ssh_port || 22} ${target.alias}@${data.runtime.ssh_host || location.hostname}`} />
            <button type="button" onClick={() => setDrawer(target)}>{t("commonEdit")}</button>
          </span>,
        ])} /> : <Empty title={t("serviceEmptyTitle")} body={t("serviceEmptyBody")} />}
      </Panel>
      {modal && <TargetCreateModal data={data} onClose={() => setModal(false)} onEnrollment={(out) => { setModal(false); setEnrollment(out); }} />}
      {drawer && <TargetDrawer data={data} target={drawer} onClose={() => setDrawer(null)} />}
      {enrollment && <InstallDrawer enrollment={enrollment} onClose={() => { setEnrollment(null); refreshTargets(); }} />}
    </>
  );
}

function TargetCreateModal({ data, onClose, onEnrollment }: { data: ConsoleData; onClose: () => void; onEnrollment: (enrollment: Enrollment) => void }) {
  const { t } = useI18n();
  const [mode, setMode] = useState<"direct" | "private">("direct");
  const [step, setStep] = useState(0);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const queryClient = useQueryClient();
  const create = useMutation({ mutationFn: api.createTarget, onSuccess: async () => { onClose(); await queryClient.invalidateQueries(); } });
  const enroll = useMutation({ mutationFn: api.enrollPrivateNode, onSuccess: async (out) => { await queryClient.invalidateQueries(); onEnrollment(out); } });

  function next(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const values = { ...draft, ...formValues(event.currentTarget) };
    setDraft(values);
    if (mode === "private") {
      enroll.mutate({ owner_type: "organization", owner_id: data.activeOrg.id, label: values.alias || values.name || "private-node", default_host: "127.0.0.1", default_port: 22 });
      return;
    }
    if (step < 2) {
      setStep(step + 1);
      return;
    }
    create.mutate({
      owner_type: "organization",
      owner_id: data.activeOrg.id,
      target_type: "direct",
      name: values.name,
      alias: values.alias,
      host: values.host,
      port: Number(values.port || 22),
      remote_username: values.remote_username,
      auth_type: values.auth_type || "password",
      secret: values.secret || "",
      tags: splitTags(values.tags || ""),
      proxy_target_id: values.proxy_target_id || "",
    });
  }

  return <Modal title={t("serviceCreateTitle")} onClose={onClose}>
    <div className="tabs" role="tablist">
      <button type="button" role="tab" aria-selected={mode === "direct"} className={clsx(mode === "direct" && "active")} onClick={() => { setMode("direct"); setStep(0); }}>{t("serviceServerTab")}</button>
      <button type="button" role="tab" aria-selected={mode === "private"} className={clsx(mode === "private" && "active")} onClick={() => { setMode("private"); setStep(0); }}>{t("privateNode")}</button>
    </div>
    <form className="grid two" onSubmit={next}>
      {mode === "private" ? <>
        <Field label={t("serviceAlias")} name="alias" defaultValue={draft.alias} required />
        <p className="span-two muted">{t("servicePrivateHint")}</p>
      </> : <>
        {step === 0 && <>
          <Field label={t("serviceName")} name="name" defaultValue={draft.name} required />
          <Field label={t("serviceAlias")} name="alias" defaultValue={draft.alias} required />
          <Field label={t("commonTag")} name="tags" defaultValue={draft.tags} placeholder="test, common" />
        </>}
        {step === 1 && <>
          <Field label={t("targetHost")} name="host" defaultValue={draft.host} required />
          <Field label={t("targetPort")} name="port" defaultValue={draft.port || "22"} required />
          <Field label={t("serviceRemoteUser")} name="remote_username" defaultValue={draft.remote_username} required />
        </>}
        {step === 2 && <>
          <Select label={t("serviceAuthType")} name="auth_type" defaultValue={draft.auth_type || "password"} options={[["password", t("serviceAuthPassword")], ["private_key", t("serviceAuthPrivateKey")]]} />
          <label className="field"><span>{t("serviceAuthSecret")}</span><textarea name="secret" defaultValue={draft.secret} /></label>
          <Select label={t("serviceAdvancedProxy")} name="proxy_target_id" defaultValue={draft.proxy_target_id || ""} options={[["", t("commonNotUse")], ...data.targets.map((target): [string, string] => [target.id, `${target.name} (${target.alias})`])]} />
        </>}
      </>}
      <ModalActions onCancel={onClose} submit={mode === "private" ? t("serviceCreateInstallToken") : step < 2 ? t("commonNext") : t("addService")} />
    </form>
  </Modal>;
}

function TargetDrawer({ data, target, onClose }: { data: ConsoleData; target: Target; onClose: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const update = useMutation({ mutationFn: (body: Record<string, unknown>) => api.updateTarget(target.id, body), onSuccess: async () => queryClient.invalidateQueries() });
  const color = useMutation({ mutationFn: (body: Record<string, unknown>) => api.updateTargetTagColor(body), onSuccess: async () => queryClient.invalidateQueries() });
  return <Drawer title={target.name} subtitle={t("serviceEditBody")} onClose={onClose}>
    <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => update.mutate({
      name: body.name,
      alias: body.alias,
      host: body.host,
      port: Number(body.port || target.port || 22),
      remote_username: body.remote_username,
      auth_type: body.auth_type,
      secret: body.secret,
      tags: splitTags(body.tags || ""),
      proxy_target_id: body.proxy_target_id || "",
    }))}>
      <Field label={t("serviceName")} name="name" defaultValue={target.name} required />
      <Field label={t("serviceAlias")} name="alias" defaultValue={target.alias} required />
      <Field label={t("targetHost")} name="host" defaultValue={target.host} disabled={target.target_type === "agent"} />
      <Field label={t("targetPort")} name="port" defaultValue={String(target.port || 22)} disabled={target.target_type === "agent"} />
      <Field label={t("serviceRemoteUser")} name="remote_username" defaultValue={target.remote_username} disabled={target.target_type === "agent"} />
      <Select label={t("serviceAuthType")} name="auth_type" defaultValue={target.auth_type} options={[["password", t("serviceAuthPassword")], ["private_key", t("serviceAuthPrivateKey")]]} />
      <label className="field"><span>{t("serviceAuthSecret")}</span><textarea name="secret" /></label>
      <Field label={t("commonTag")} name="tags" defaultValue={(target.tags || []).join(", ")} />
      <Select label={t("serviceAdvancedProxy")} name="proxy_target_id" defaultValue={target.proxy_target_id || ""} options={[["", t("commonNotUse")], ...data.targets.filter((item) => item.id !== target.id).map((item): [string, string] => [item.id, `${item.name} (${item.alias})`])]} />
      <ModalActions onCancel={onClose} submit={t("save")} />
    </form>
    <section className="section-block embedded">
      <h3>{t("serviceTagColors")}</h3>
      {(target.tags || []).map((tag) => <div className="tag-color-row" key={tag}>
        <Tag tag={tag} color={tagColor(tag, target.tag_colors)} />
        <div className="tag-color-swatches">
          {["gray", "red", "orange", "yellow", "green", "blue", "purple"].map((item) => <button key={item} type="button" className={`tag-color-${item}`} onClick={() => color.mutate({ owner_type: "organization", owner_id: data.activeOrg.id, name: tag, color: item })}>{item}</button>)}
        </div>
      </div>)}
    </section>
  </Drawer>;
}

function InstallDrawer({ enrollment, onClose }: { enrollment: Enrollment; onClose: () => void }) {
  const { t } = useI18n();
  return <Drawer title={t("serviceInstallTitle")} subtitle={t("serviceInstallBody")} onClose={onClose}>
    <div className="grid two">
      <section className="section-block embedded">
        <h3>Linux / macOS</h3>
        <CommandBox label={t("serviceRunOnce")} value={enrollment.install_sh || ""} />
        <CommandBox label={t("serviceLinuxService")} value={enrollment.service_sh || ""} />
      </section>
      <section className="section-block embedded">
        <h3>Windows</h3>
        <CommandBox label={t("serviceWindowsRunOnce")} value={enrollment.install_ps1 || ""} />
        <CommandBox label={t("serviceWindowsService")} value={enrollment.service_ps1 || ""} />
      </section>
    </div>
  </Drawer>;
}
