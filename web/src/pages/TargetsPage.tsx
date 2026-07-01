import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { CheckSquare, ChevronDown, ChevronRight, Copy, Edit3, Folder, FolderPlus, KeyRound, Move, Play, Plus, Search, Settings, Square, TerminalSquare, Trash2 } from "lucide-react";
import { type CSSProperties, type FormEvent, useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";

import { api, type Enrollment } from "../api";
import { CommandBox, CopyButton, Drawer, Empty, Field, Metric, Modal, ModalActions, Panel, Select, SimpleTable, Tag, TagList, Toggle, Toolbar } from "../components/ui";
import { useI18n } from "../i18n";
import { appDescription } from "../lib/branding";
import { formSubmit, formValues } from "../lib/forms";
import type { ConsoleData, Target, TargetFolder } from "../types";
import { splitTags, tagColor, targetEndpoint } from "../utils";

let connectWindowRef: Window | null = null;

export function TargetsPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const [query, setQuery] = useState("");
  const [modal, setModal] = useState(false);
  const [credentialModal, setCredentialModal] = useState(false);
  const [folderModal, setFolderModal] = useState<{ parent_id?: string } | null>(null);
  const [settingsModal, setSettingsModal] = useState(false);
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [moveModal, setMoveModal] = useState<"move" | "copy" | null>(null);
  const [commandModal, setCommandModal] = useState(false);
  const [collapsedFolders, setCollapsedFolders] = useState<Set<string>>(() => new Set());
  const [drawerTargetID, setDrawerTargetID] = useState("");
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null);
  const [tip, setTip] = useState("");
  const tipTimerRef = useRef<number | null>(null);
  const filtered = data.targets.filter((target) => {
    const folderPath = targetFolderPath(target, data.targetFolders);
    return [folderPath, `${folderPath}/${target.alias}`, `${folderPath}/${target.name}`, target.name, target.alias, target.host, target.remote_username, ...(target.tags || [])].join(" ").toLowerCase().includes(query.toLowerCase());
  });
  const drawerTarget = data.targets.find((target) => target.id === drawerTargetID) || null;
  const selectedTargetIDs = selectedTargets(selected, data);
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

  function openConnectWindow(id: string) {
    openConnectPath(`/targets/${id}/connect`, id);
  }

  function openConnectPath(path: string, id: string) {
    const attachExisting = data.userSettings.connect_attach_existing;
    if (window.parent && window.parent !== window) {
      window.parent.postMessage({ type: "gosshd-open-connect", path, targetID: id, openMode: data.userSettings.connect_open_mode, attachExisting }, "*");
      return;
    }
    if (data.userSettings.connect_open_mode === "tab") {
      if (attachExisting && openInExistingConnectWindow(path, id)) return;
      const opened = window.open(path, attachExisting ? "gosshd-connect" : "_blank", attachExisting ? "" : "noopener=yes,noreferrer=yes");
      if (attachExisting) connectWindowRef = opened;
      return;
    }
    const width = 1200;
    const height = 800;
    const left = Math.max(0, Math.round((window.screen.width - width) / 2));
    const top = Math.max(0, Math.round((window.screen.height - height) / 2));
    const features = [
      `width=${width}`,
      `height=${height}`,
      `left=${left}`,
      `top=${top}`,
      "resizable=yes",
      "scrollbars=yes",
      "status=yes",
      ...(!attachExisting ? ["noopener=yes", "noreferrer=yes"] : []),
    ].join(",");
    if (attachExisting && openInExistingConnectWindow(path, id)) return;
    const opened = window.open(path, attachExisting ? "gosshd-connect" : `connect-${id}`, features);
    if (attachExisting) connectWindowRef = opened;
  }

  function toggleSelected(id: string) {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  }

  function setManySelected(ids: string[], checked: boolean) {
    setSelected((current) => {
      const next = new Set(current);
      for (const id of ids) {
        if (checked) next.add(id); else next.delete(id);
      }
      return next;
    });
  }

  async function runBatchCommand(command: string) {
    await api.recordBatchCommandHistory({
      owner_type: "organization",
      owner_id: data.activeOrg.id,
      command,
    });
    await queryClient.invalidateQueries({ queryKey: ["batch-command-histories", data.activeOrg.id] });
    for (const id of selectedTargetIDs) {
      openConnectPath(`/targets/${id}/connect?command=${encodeURIComponent(command)}`, id);
    }
    setCommandModal(false);
  }

  useEffect(() => {
    refreshTargets();
  }, [data.activeOrg.id]);

  useEffect(() => {
    if (searchParams.get("new") !== "1") return;
    setModal(true);
    setSearchParams({}, { replace: true });
  }, [searchParams, setSearchParams]);

  useEffect(() => () => {
    if (tipTimerRef.current) window.clearTimeout(tipTimerRef.current);
  }, []);

  return (
    <>
      <section className="resource-head">
        <div><small>{appDescription(data.runtime)}</small><h2>{t("services")}</h2><p>{t("servicePageBody")}</p></div>
        {!data.runtime.client_mode && <div className="inline-actions">
          <button type="button" onClick={() => setSettingsModal(true)}><Settings />{t("serviceOpenSettings")}</button>
          <button type="button" onClick={() => { setSelecting((value) => !value); setSelected(new Set()); }}>{selecting ? <CheckSquare /> : <Square />}{selecting ? t("serviceBatchDone") : t("serviceBatchSelect")}</button>
          <button type="button" onClick={() => setCredentialModal(true)}><KeyRound />{t("serviceCredentials")}</button>
          <button type="button" onClick={() => setFolderModal({})}><FolderPlus />{t("serviceNewFolder")}</button>
          <button type="button" className="primary" onClick={() => setModal(true)}><Plus />{t("addService")}</button>
        </div>}
      </section>
      {!data.runtime.client_mode && <div className="metrics">
        <Metric label={t("serviceTotal")} value={data.targets.length} />
        <Metric label={t("serviceDirect")} value={data.targets.filter((item) => item.target_type === "direct").length} />
        <Metric label={t("privateNode")} value={data.targets.filter((item) => item.target_type === "agent").length} />
        <Metric label={t("commonTag")} value={new Set(data.targets.flatMap((item) => item.tags || [])).size} />
      </div>}
      <Toolbar query={query} setQuery={setQuery} />
      {selecting && <div className="batch-toolbar">
        <span>{t("serviceBatchSelected").replace("{0}", String(selectedTargetIDs.length))}</span>
        <button type="button" onClick={() => setMoveModal("move")} disabled={!selectedTargetIDs.length}><Move />{t("serviceBatchMove")}</button>
        <button type="button" onClick={() => setMoveModal("copy")} disabled={!selectedTargetIDs.length}><Copy />{t("serviceBatchCopy")}</button>
        <button type="button" onClick={() => setCommandModal(true)} disabled={!selectedTargetIDs.length}><Play />{t("serviceBatchCommand")}</button>
      </div>}
      <Panel title={t("serviceTableService")} subtitle="">
        {filtered.length || data.targetFolders.length ? (
          <TargetTree
            data={data}
            targets={filtered}
            collapsed={collapsedFolders}
            onToggle={(id) => setCollapsedFolders((current) => {
              const next = new Set(current);
              if (next.has(id)) next.delete(id); else next.add(id);
              return next;
            })}
            onNewFolder={(parentID) => setFolderModal({ parent_id: parentID })}
            onOpen={openConnectWindow}
            onEdit={(id) => setDrawerTargetID(id)}
            onDelete={deleteTarget}
            deleting={removeTarget.isPending}
            selecting={selecting}
            selected={selected}
            onSelect={toggleSelected}
            onSelectMany={setManySelected}
          />
        ) : <Empty title={t("serviceEmptyTitle")} body={t("serviceEmptyBody")} />}
      </Panel>
      {modal && <TargetCreateModal data={data} onClose={() => setModal(false)} onEnrollment={(out) => { setModal(false); setEnrollment(out); }} />}
      {credentialModal && <CredentialManagerModal data={data} onClose={() => setCredentialModal(false)} />}
      {folderModal && <FolderModal data={data} parentID={folderModal.parent_id || ""} onClose={() => setFolderModal(null)} />}
      {settingsModal && <ConnectOpenSettingsModal data={data} onClose={() => setSettingsModal(false)} />}
      {moveModal && <TargetMoveCopyModal data={data} targetIDs={selectedTargetIDs} action={moveModal} onClose={() => setMoveModal(null)} />}
      {commandModal && <BatchCommandModal data={data} onClose={() => setCommandModal(false)} onSubmit={runBatchCommand} />}
      {drawerTarget && <TargetDrawer data={data} target={drawerTarget} onClose={() => setDrawerTargetID("")} onEnrollment={setEnrollment} onSaved={() => showTip(t("serviceSaveSuccess"))} />}
      {enrollment && <InstallDrawer enrollment={enrollment} onClose={() => { setEnrollment(null); refreshTargets(); }} />}
      {tip && <div className="page-toast" role="status">{tip}</div>}
    </>
  );
}

function TargetTree({
  data,
  targets,
  collapsed,
  onToggle,
  onNewFolder,
  onOpen,
  onEdit,
  onDelete,
  deleting,
  selecting,
  selected,
  onSelect,
  onSelectMany,
}: {
  data: ConsoleData;
  targets: Target[];
  collapsed: Set<string>;
  onToggle: (id: string) => void;
  onNewFolder: (parentID: string) => void;
  onOpen: (id: string) => void;
  onEdit: (id: string) => void;
  onDelete: (target: Target) => void;
  deleting: boolean;
  selecting: boolean;
  selected: Set<string>;
  onSelect: (id: string) => void;
  onSelectMany: (ids: string[], checked: boolean) => void;
}) {
  const roots = data.targetFolders.filter((folder) => !folder.parent_id);
  const rootTargets = targets.filter((target) => !target.folder_id);
  return <div className="target-tree">
    {roots.map((folder) => <FolderNode key={folder.id} data={data} folder={folder} targets={targets} collapsed={collapsed} onToggle={onToggle} onNewFolder={onNewFolder} onOpen={onOpen} onEdit={onEdit} onDelete={onDelete} deleting={deleting} selecting={selecting} selected={selected} onSelect={onSelect} onSelectMany={onSelectMany} />)}
    {rootTargets.map((target) => <TargetTreeRow key={target.id} data={data} target={target} onOpen={onOpen} onEdit={onEdit} onDelete={onDelete} deleting={deleting} selecting={selecting} selected={selected.has(target.id)} onSelect={() => onSelect(target.id)} />)}
  </div>;
}

function FolderNode(props: {
  data: ConsoleData;
  folder: TargetFolder;
  targets: Target[];
  collapsed: Set<string>;
  onToggle: (id: string) => void;
  onNewFolder: (parentID: string) => void;
  onOpen: (id: string) => void;
  onEdit: (id: string) => void;
  onDelete: (target: Target) => void;
  deleting: boolean;
  selecting: boolean;
  selected: Set<string>;
  onSelect: (id: string) => void;
  onSelectMany: (ids: string[], checked: boolean) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const remove = useMutation({ mutationFn: api.deleteTargetFolder, onSuccess: async () => queryClient.invalidateQueries() });
  const rename = useMutation({ mutationFn: (name: string) => api.updateTargetFolder(props.folder.id, { name }), onSuccess: async () => queryClient.invalidateQueries(), onError: (error) => window.alert(error instanceof Error ? error.message : String(error)) });
  const children = props.data.targetFolders.filter((folder) => folder.parent_id === props.folder.id);
  const targets = props.targets.filter((target) => target.folder_id === props.folder.id);
  const collapsed = props.collapsed.has(props.folder.id);
  const folderTargetIDs = targetIDsInFolder(props.folder.id, props.data);
  const folderChecked = folderTargetIDs.length > 0 && folderTargetIDs.every((id) => props.selected.has(id));
  const toggleFolder = () => props.onSelectMany(folderTargetIDs, !folderChecked);
  return <div className="target-folder-node">
    <div className={`target-folder-row ${props.selecting ? "selecting" : ""}`}>
      {props.selecting && <button type="button" className="tree-check" onClick={toggleFolder}>{folderChecked ? <CheckSquare /> : <Square />}</button>}
      <button type="button" className="target-folder-main" onClick={() => props.onToggle(props.folder.id)}>
        {collapsed ? <ChevronRight /> : <ChevronDown />}<Folder /><strong>{props.folder.name}</strong>
      </button>
      <span className="inline-actions">
        <button type="button" onClick={() => {
          const name = window.prompt(t("serviceRenameFolder"), props.folder.name);
          if (name && name.trim() && name.trim() !== props.folder.name) rename.mutate(name.trim());
        }} disabled={rename.isPending}><Edit3 />{t("commonEdit")}</button>
        <button type="button" onClick={() => props.onNewFolder(props.folder.id)}><FolderPlus />{t("serviceNewFolder")}</button>
        <button type="button" className="danger" onClick={() => { if (window.confirm(t("serviceDeleteFolderConfirm"))) remove.mutate(props.folder.id); }} disabled={remove.isPending}><Trash2 />{t("commonDelete")}</button>
      </span>
    </div>
    {!collapsed && <div className="target-folder-children">
      {children.map((folder) => <FolderNode key={folder.id} {...props} folder={folder} />)}
      {targets.map((target) => <TargetTreeRow key={target.id} data={props.data} target={target} onOpen={props.onOpen} onEdit={props.onEdit} onDelete={props.onDelete} deleting={props.deleting} selecting={props.selecting} selected={props.selected.has(target.id)} onSelect={() => props.onSelect(target.id)} />)}
    </div>}
  </div>;
}

function TargetTreeRow({ data, target, onOpen, onEdit, onDelete, deleting, selecting, selected, onSelect }: { data: ConsoleData; target: Target; onOpen: (id: string) => void; onEdit: (id: string) => void; onDelete: (target: Target) => void; deleting: boolean; selecting: boolean; selected: boolean; onSelect: () => void }) {
  const { t } = useI18n();
  const credential = data.credentials.find((item) => item.id === target.credential_id);
  return <div className="target-tree-row">
    <div className="target-tree-main">
      {selecting && <button type="button" className="tree-check" onClick={onSelect}>{selected ? <CheckSquare /> : <Square />}</button>}
      {target.target_type === "agent" ? <TerminalSquare /> : <TerminalSquare />}
      <span><strong>{target.name}</strong><code>{target.alias}</code></span>
    </div>
    <span>{target.target_type === "agent" ? t("privateNode") : targetEndpoint(target)}</span>
    <span>{credential ? credential.name : (target.auth_type === "private_key" ? t("serviceAuthPrivateKey") : t("serviceAuthPassword"))}</span>
    <TagList target={target} />
    <span className="inline-actions">
      <CopyButton value={`ssh -p ${data.runtime.ssh_port || 22} ${target.alias}@${data.runtime.ssh_host || location.hostname}`} />
      <button type="button" className="button-link" onClick={() => onOpen(target.id)}><TerminalSquare />{t("connect")}</button>
      <button type="button" onClick={() => onEdit(target.id)}>{t("commonEdit")}</button>
      <button type="button" className="danger" onClick={() => onDelete(target)} disabled={deleting}><Trash2 />{t("commonDelete")}</button>
    </span>
  </div>;
}

function CredentialManagerModal({ data, onClose }: { data: ConsoleData; onClose: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const create = useMutation({ mutationFn: api.createCredential, onSuccess: async () => queryClient.invalidateQueries() });
  const remove = useMutation({ mutationFn: api.deleteCredential, onSuccess: async () => queryClient.invalidateQueries(), onError: (error) => window.alert(error instanceof Error ? error.message : String(error)) });
  return <Modal title={t("serviceCredentials")} onClose={onClose} wide>
    <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => create.mutate({
      owner_type: "organization",
      owner_id: data.activeOrg.id,
      name: body.name,
      username: body.username,
      auth_type: body.auth_type || "password",
      secret: body.secret || "",
    }))}>
      <Field label={t("serviceCredentialName")} name="name" required />
      <Field label={t("serviceRemoteUser")} name="username" required />
      <Select label={t("serviceAuthType")} name="auth_type" defaultValue="password" options={[["password", t("serviceAuthPassword")], ["private_key", t("serviceAuthPrivateKey")]]} />
      <label className="field"><span>{t("serviceAuthSecret")}</span><textarea name="secret" /></label>
      <ModalActions onCancel={onClose} submit={t("add")} />
    </form>
    <SimpleTable headers={[t("serviceCredentialName"), t("serviceRemoteUser"), t("commonAuth"), t("commonActions")]} rows={data.credentials.map((credential) => [
      credential.name,
      credential.username,
      credential.auth_type === "private_key" ? t("serviceAuthPrivateKey") : t("serviceAuthPassword"),
      <button type="button" className="danger" onClick={() => remove.mutate(credential.id)} disabled={remove.isPending}><Trash2 />{t("commonDelete")}</button>,
    ])} />
  </Modal>;
}

function FolderModal({ data, parentID, onClose }: { data: ConsoleData; parentID: string; onClose: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const create = useMutation({ mutationFn: api.createTargetFolder, onSuccess: async () => { await queryClient.invalidateQueries(); onClose(); } });
  return <Modal title={t("serviceNewFolder")} onClose={onClose}>
    <form className="stack" onSubmit={(event) => formSubmit(event, (body) => create.mutate({
      owner_type: "organization",
      owner_id: data.activeOrg.id,
      parent_id: parentID,
      name: body.name,
    }))}>
      <Field label={t("serviceFolderName")} name="name" required />
      <ModalActions onCancel={onClose} submit={t("serviceNewFolder")} />
    </form>
  </Modal>;
}

function ConnectOpenSettingsModal({ data, onClose }: { data: ConsoleData; onClose: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const update = useMutation({ mutationFn: api.updateMySettings, onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ["my-settings"] }); onClose(); } });
  return <Modal title={t("serviceOpenSettings")} onClose={onClose}>
    <form className="stack" onSubmit={(event) => formSubmit(event, (body) => update.mutate({
      connect_open_mode: body.connect_open_mode || "popup",
      connect_attach_existing: body.connect_attach_existing === "on",
    }))}>
      <Select label={t("serviceConnectOpenMode")} name="connect_open_mode" defaultValue={data.userSettings.connect_open_mode} options={[["popup", t("serviceConnectOpenPopup")], ["tab", t("serviceConnectOpenTab")]]} />
      <Toggle name="connect_attach_existing" label={t("serviceConnectAttachExisting")} defaultChecked={data.userSettings.connect_attach_existing} />
      <ModalActions onCancel={onClose} submit={t("save")} />
    </form>
  </Modal>;
}

function TargetMoveCopyModal({ data, targetIDs, action, onClose }: { data: ConsoleData; targetIDs: string[]; action: "move" | "copy"; onClose: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [folderID, setFolderID] = useState("");
  const move = useMutation({
    mutationFn: async () => {
      for (const id of targetIDs) {
        if (action === "copy") {
          const copied = await api.copyTarget(id);
          if (folderID) await api.updateTarget(copied.target.id, { folder_id: folderID });
        } else {
          await api.updateTarget(id, { folder_id: folderID });
        }
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["targets"] });
      onClose();
    },
  });
  return <Modal title={action === "copy" ? t("serviceBatchCopy") : t("serviceBatchMove")} onClose={onClose} wide>
    <form className="stack" onSubmit={(event) => { event.preventDefault(); move.mutate(); }}>
      <div className="folder-picker-tree">
        <button type="button" className={!folderID ? "active" : ""} onClick={() => setFolderID("")}>Root</button>
        {data.targetFolders.filter((folder) => !folder.parent_id).map((folder) => (
          <FolderPickNode key={folder.id} folder={folder} folders={data.targetFolders} selectedID={folderID} onSelect={setFolderID} depth={0} />
        ))}
      </div>
      <ModalActions onCancel={onClose} submit={move.isPending ? t("loading") : t("save")} />
    </form>
  </Modal>;
}

function FolderPickNode({ folder, folders, selectedID, onSelect, depth }: { folder: TargetFolder; folders: TargetFolder[]; selectedID: string; onSelect: (id: string) => void; depth: number }) {
  const children = folders.filter((item) => item.parent_id === folder.id);
  return <>
    <button type="button" className={selectedID === folder.id ? "active" : ""} style={{ "--tree-depth": depth } as CSSProperties} onClick={() => onSelect(folder.id)}><Folder />{folder.name}</button>
    {children.map((child) => <FolderPickNode key={child.id} folder={child} folders={folders} selectedID={selectedID} onSelect={onSelect} depth={depth + 1} />)}
  </>;
}

function BatchCommandModal({ data, onClose, onSubmit }: { data: ConsoleData; onClose: () => void; onSubmit: (command: string) => Promise<void> }) {
  const { t } = useI18n();
  const [command, setCommand] = useState("");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");
  const histories = useQuery({
    queryKey: ["batch-command-histories", data.activeOrg.id, query, page],
    queryFn: () => api.batchCommandHistories({
      owner_type: "organization",
      owner_id: data.activeOrg.id,
      query,
      page,
      page_size: 10,
    }),
  });
  const historyRows = histories.data?.histories || [];
  const total = histories.data?.total || 0;
  const pageSize = histories.data?.page_size || 10;
  return <Modal title={t("serviceBatchCommand")} onClose={onClose} wide>
    <form className="stack" onSubmit={async (event) => {
      event.preventDefault();
      const nextCommand = command.trim();
      if (!nextCommand) return;
      setSubmitting(true);
      setSubmitError("");
      try {
        await onSubmit(nextCommand);
      } catch (error) {
        setSubmitError(error instanceof Error ? error.message : String(error));
      } finally {
        setSubmitting(false);
      }
    }}>
      <label className="field"><span>{t("serviceBatchCommandInput")}</span><textarea name="command" required value={command} onChange={(event) => setCommand(event.target.value)} /></label>
      <div className="batch-command-history">
        <div className="batch-command-history-head">
          <strong>{t("serviceBatchCommandHistory")}</strong>
          <label>
            <Search />
            <input value={query} onChange={(event) => { setQuery(event.target.value); setPage(1); }} placeholder={t("serviceBatchCommandHistorySearch")} />
          </label>
        </div>
        <div className="batch-command-history-list">
          {histories.isLoading ? <p className="muted">{t("loading")}</p> : historyRows.length ? historyRows.map((item) => (
            <button key={item.id} type="button" onClick={() => setCommand(item.command)}>
              <code>{item.command}</code>
              <span>{t("serviceBatchCommandHistoryCount").replace("{0}", String(item.execute_count))}</span>
            </button>
          )) : <p className="muted">{t("serviceBatchCommandHistoryEmpty")}</p>}
        </div>
        <div className="pager compact">
          <button type="button" disabled={page <= 1 || histories.isFetching} onClick={() => setPage(page - 1)}>{t("commonPrevious")}</button>
          <span>{t("commonPage")} {page}</span>
          <button type="button" disabled={total <= page * pageSize || histories.isFetching} onClick={() => setPage(page + 1)}>{t("commonNext")}</button>
        </div>
      </div>
      {submitError && <p className="form-error">{submitError}</p>}
      <ModalActions onCancel={onClose} submit={submitting ? t("loading") : t("serviceBatchCommandRun")} />
    </form>
  </Modal>;
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
      credential_id: values.credential_id || "",
      folder_id: values.folder_id || "",
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
          <Select label={t("serviceFolder")} name="folder_id" defaultValue={draft.folder_id || ""} options={folderOptions(data)} />
          <Field label={t("commonTag")} name="tags" defaultValue={draft.tags} placeholder="test, common" />
        </>}
        {step === 1 && <>
          <Field label={t("targetHost")} name="host" defaultValue={draft.host} required />
          <Field label={t("targetPort")} name="port" defaultValue={draft.port || "22"} required />
          <Field label={t("serviceRemoteUser")} name="remote_username" defaultValue={draft.remote_username} required />
        </>}
        {step === 2 && <>
          <Select label={t("serviceCredential")} name="credential_id" defaultValue={draft.credential_id || ""} options={[["", t("serviceCredentialManual")], ...data.credentials.map((credential): [string, string] => [credential.id, `${credential.name} (${credential.username})`])]} />
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
      credential_id: body.credential_id || "",
      folder_id: body.folder_id || "",
    }))}>
      <Field label={t("serviceName")} name="name" defaultValue={target.name} required />
      <Field label={t("serviceAlias")} name="alias" defaultValue={target.alias} required />
      <Field label={t("targetHost")} name="host" defaultValue={target.host} disabled={target.target_type === "agent"} />
      <Field label={t("targetPort")} name="port" defaultValue={String(target.port || 22)} disabled={target.target_type === "agent"} />
      <Field label={t("serviceRemoteUser")} name="remote_username" defaultValue={target.remote_username} disabled={target.target_type === "agent"} />
      <Select label={t("serviceCredential")} name="credential_id" defaultValue={target.credential_id || ""} options={[["", t("serviceCredentialManual")], ...data.credentials.map((credential): [string, string] => [credential.id, `${credential.name} (${credential.username})`])]} />
      <Select label={t("serviceAuthType")} name="auth_type" defaultValue={target.auth_type} options={[["password", t("serviceAuthPassword")], ["private_key", t("serviceAuthPrivateKey")]]} />
      <label className="field"><span>{t("serviceAuthSecret")}</span><textarea name="secret" /></label>
      <Field label={t("commonTag")} name="tags" defaultValue={(target.tags || []).join(", ")} />
      <Select label={t("serviceFolder")} name="folder_id" defaultValue={target.folder_id || ""} options={folderOptions(data)} />
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
        folder_id: body.folder_id || "",
      }))}>
        <Field label={t("serviceName")} name="name" defaultValue={target.name} required />
        <Field label={t("serviceAlias")} name="alias" defaultValue={target.alias} required />
        <Field label={t("commonTag")} name="tags" defaultValue={(target.tags || []).join(", ")} />
        <Select label={t("serviceFolder")} name="folder_id" defaultValue={target.folder_id || ""} options={folderOptions(data)} />
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

function folderOptions(data: ConsoleData): Array<[string, string]> {
  return [["", "Root"], ...data.targetFolders.map((folder) => [folder.id, targetFolderName(folder, data.targetFolders)] as [string, string])];
}

function targetFolderPath(target: Target, folders: TargetFolder[]) {
  const folder = folders.find((item) => item.id === target.folder_id);
  return folder ? targetFolderName(folder, folders) : "";
}

function targetFolderName(folder: TargetFolder, folders: TargetFolder[]) {
  const byID = new Map(folders.map((item) => [item.id, item]));
  const names: string[] = [];
  const seen = new Set<string>();
  for (let current: TargetFolder | undefined = folder; current;) {
    if (seen.has(current.id)) break;
    seen.add(current.id);
    names.unshift(current.name);
    current = current.parent_id ? byID.get(current.parent_id) : undefined;
  }
  return names.join("/");
}

function selectedTargets(selected: Set<string>, data: ConsoleData) {
  return data.targets.filter((target) => selected.has(target.id)).map((target) => target.id);
}

function targetIDsInFolder(folderID: string, data: ConsoleData) {
  const folderIDs = new Set<string>([folderID]);
  let changed = true;
  while (changed) {
    changed = false;
    for (const folder of data.targetFolders) {
      if (folder.parent_id && folderIDs.has(folder.parent_id) && !folderIDs.has(folder.id)) {
        folderIDs.add(folder.id);
        changed = true;
      }
    }
  }
  return data.targets.filter((target) => target.folder_id && folderIDs.has(target.folder_id)).map((target) => target.id);
}

function openInExistingConnectWindow(path: string, targetID: string) {
  if (typeof window === "undefined") return false;
  let online = false;
  try {
    const raw = window.localStorage.getItem("gosshd-connect-window-online");
    const state = raw ? JSON.parse(raw) as { at?: number } : null;
    online = Boolean(state?.at && Date.now() - state.at < 2500);
  } catch {
    online = false;
  }
  if (!online) return false;
  const command = new URL(path, window.location.origin).searchParams.get("command") || "";
  const message = { type: "gosshd-connect-open-target", targetID, command, at: Date.now(), messageID: `${targetID}:${Date.now()}:${Math.random().toString(36).slice(2)}` };
  if (connectWindowRef?.closed) connectWindowRef = null;
  connectWindowRef?.postMessage(message, window.location.origin);
  try {
    window.localStorage.setItem("gosshd-connect-open", JSON.stringify(message));
  } catch {
    // Ignore storage failures; direct postMessage is the primary path when this page opened the window.
  }
  connectWindowRef?.focus();
  return true;
}
