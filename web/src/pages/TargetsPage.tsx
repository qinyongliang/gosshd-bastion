import { useMutation, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { Plus, TerminalSquare, Trash2 } from "lucide-react";
import { type FormEvent, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
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
  const [drawerTargetID, setDrawerTargetID] = useState("");
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null);
  const [tip, setTip] = useState("");
  const tipTimerRef = useRef<number | null>(null);
  const filtered = data.targets.filter((target) => [target.name, target.alias, target.host, target.remote_username, ...(target.tags || [])].join(" ").toLowerCase().includes(query.toLowerCase()));
  const drawerTarget = data.targets.find((target) => target.id === drawerTargetID) || null;
  const refreshTargets = () => void queryClient.invalidateQueries({ queryKey: ["targets"] });
  const removeTarget = useMutation({
    mutationFn: api.deleteTarget,
    onSuccess: async (_, id) => {
      if (drawerTargetID === id) setDrawerTargetID("");
      await queryClient.invalidateQueries({ queryKey: ["targets"] });
      await queryClient.invalidateQueries({ queryKey: ["policies"] });
    },
  });

  function deleteTarget(target: Target) {
    if (!window.confirm(t("serviceDeleteConfirm"))) return;
    removeTarget.mutate(target.id);
  }

  function showTip(message: string) {
    if (tipTimerRef.current) window.clearTimeout(tipTimerRef.current);
    setTip(message);
    tipTimerRef.current = window.setTimeout(() => setTip(""), 1800);
  }

  useEffect(() => {
    refreshTargets();
  }, [data.activeOrg.id]);

  useEffect(() => () => {
    if (tipTimerRef.current) window.clearTimeout(tipTimerRef.current);
  }, []);

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
          target.target_type === "agent" ? <span><strong>{t("privateNode")}</strong><small>{targetEndpoint(target)}</small></span> : targetEndpoint(target),
          target.auth_type === "private_key" ? t("serviceAuthPrivateKey") : t("serviceAuthPassword"),
          <TagList target={target} />,
          <span className="inline-actions">
            <CopyButton value={`ssh -p ${data.runtime.ssh_port || 22} ${target.alias}@${data.runtime.ssh_host || location.hostname}`} />
            <Link className="button-link" to={`/targets/${target.id}/connect`}><TerminalSquare />{t("connect")}</Link>
            <button type="button" onClick={() => setDrawerTargetID(target.id)}>{t("commonEdit")}</button>
            <button type="button" className="danger" onClick={() => deleteTarget(target)} disabled={removeTarget.isPending}><Trash2 />{t("commonDelete")}</button>
          </span>,
        ])} /> : <Empty title={t("serviceEmptyTitle")} body={t("serviceEmptyBody")} />}
      </Panel>
      {modal && <TargetCreateModal data={data} onClose={() => setModal(false)} onEnrollment={(out) => { setModal(false); setEnrollment(out); }} />}
      {drawerTarget && <TargetDrawer data={data} target={drawerTarget} onClose={() => setDrawerTargetID("")} onEnrollment={setEnrollment} onSaved={() => showTip(t("serviceSaveSuccess"))} />}
      {enrollment && <InstallDrawer enrollment={enrollment} onClose={() => { setEnrollment(null); refreshTargets(); }} />}
      {tip && <div className="page-toast" role="status">{tip}</div>}
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

  return <Modal title={t("serviceCreateTitle")} onClose={onClose} closeOnEscape={false}>
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

function TargetDrawer({ data, target, onClose, onEnrollment, onSaved }: { data: ConsoleData; target: Target; onClose: () => void; onEnrollment: (enrollment: Enrollment) => void; onSaved: () => void }) {
  if (target.target_type === "agent") {
    return <PrivateNodeDrawer data={data} target={target} onClose={onClose} onEnrollment={onEnrollment} onSaved={onSaved} />;
  }
  return <DirectTargetDrawer data={data} target={target} onClose={onClose} onSaved={onSaved} />;
}

function DirectTargetDrawer({ data, target, onClose, onSaved }: { data: ConsoleData; target: Target; onClose: () => void; onSaved: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const update = useMutation({
    mutationFn: (body: Record<string, unknown>) => api.updateTarget(target.id, body),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["targets"] });
      await queryClient.invalidateQueries({ queryKey: ["policies"] });
      onSaved();
      onClose();
    },
  });
  const remove = useMutation({
    mutationFn: () => api.deleteTarget(target.id),
    onSuccess: async () => {
      onClose();
      await queryClient.invalidateQueries({ queryKey: ["targets"] });
      await queryClient.invalidateQueries({ queryKey: ["policies"] });
    },
  });
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
    <TagColorEditor data={data} target={target} />
    <section className="notice-card compact danger-zone">
      <h3>{t("serviceDeleteTitle")}</h3>
      <p>{t("serviceDeleteBody")}</p>
      <button type="button" className="danger" onClick={() => { if (window.confirm(t("serviceDeleteConfirm"))) remove.mutate(); }} disabled={remove.isPending}><Trash2 />{t("commonDelete")}</button>
    </section>
  </Drawer>;
}

function PrivateNodeDrawer({ data, target, onClose, onEnrollment, onSaved }: { data: ConsoleData; target: Target; onClose: () => void; onEnrollment: (enrollment: Enrollment) => void; onSaved: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const update = useMutation({
    mutationFn: (body: Record<string, unknown>) => api.updateTarget(target.id, body),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["targets"] });
      await queryClient.invalidateQueries({ queryKey: ["policies"] });
      onSaved();
      onClose();
    },
  });
  const remove = useMutation({
    mutationFn: () => api.deleteTarget(target.id),
    onSuccess: async () => {
      onClose();
      await queryClient.invalidateQueries({ queryKey: ["targets"] });
      await queryClient.invalidateQueries({ queryKey: ["policies"] });
    },
  });
  const replace = useMutation({
    mutationFn: () => api.enrollPrivateNode({ owner_type: "organization", owner_id: data.activeOrg.id, label: target.alias || target.name || "private-node", default_host: "127.0.0.1", default_port: 22 }),
    onSuccess: async (out) => {
      await queryClient.invalidateQueries({ queryKey: ["targets"] });
      onEnrollment(out);
    },
  });

  return <Drawer title={target.name} subtitle={t("servicePrivateEditBody")} onClose={onClose}>
    <section className="section-block embedded">
      <h3>{t("servicePrivateMetadataTitle")}</h3>
      <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => update.mutate({
        name: body.name,
        alias: body.alias,
        tags: splitTags(body.tags || ""),
      }))}>
        <Field label={t("serviceName")} name="name" defaultValue={target.name} required />
        <Field label={t("serviceAlias")} name="alias" defaultValue={target.alias} required />
        <Field label={t("commonTag")} name="tags" defaultValue={(target.tags || []).join(", ")} />
        <ModalActions onCancel={onClose} submit={t("save")} />
      </form>
    </section>
    <TagColorEditor data={data} target={target} />
    <section className="notice-card compact">
      <h3>{t("servicePrivateReplaceTitle")}</h3>
      <p>{t("servicePrivateReplaceBody")}</p>
      <button type="button" className="primary" onClick={() => replace.mutate()} disabled={replace.isPending}>{t("servicePrivateReplaceAction")}</button>
    </section>
    <section className="notice-card compact danger-zone">
      <h3>{t("serviceDeleteTitle")}</h3>
      <p>{t("serviceDeleteBody")}</p>
      <button type="button" className="danger" onClick={() => { if (window.confirm(t("serviceDeleteConfirm"))) remove.mutate(); }} disabled={remove.isPending}><Trash2 />{t("commonDelete")}</button>
    </section>
  </Drawer>;
}

function TagColorEditor({ data, target }: { data: ConsoleData; target: Target }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const color = useMutation({ mutationFn: (body: Record<string, unknown>) => api.updateTargetTagColor(body), onSuccess: async () => queryClient.invalidateQueries({ queryKey: ["targets"] }) });
  const tags = target.tags || [];
  return <section className="section-block embedded">
    <h3>{t("serviceTagColors")}</h3>
    {tags.length ? tags.map((tag) => <div className="tag-color-row" key={tag}>
      <Tag tag={tag} color={tagColor(tag, target.tag_colors)} />
      <div className="tag-color-swatches">
        {["gray", "red", "orange", "yellow", "green", "blue", "purple"].map((item) => <button key={item} type="button" aria-label={`${t("serviceTagColorSet")} ${tag} ${t(`tagColor${item[0].toUpperCase()}${item.slice(1)}`)}`} className={`tag-color-${item}`} onClick={() => color.mutate({ owner_type: "organization", owner_id: data.activeOrg.id, name: tag, color: item })}>{item}</button>)}
      </div>
    </div>) : <p className="muted">{t("serviceNoTagsForColors")}</p>}
  </section>;
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
