import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Plus } from "lucide-react";
import { useState } from "react";
import { api } from "../api";
import { CommandBox, Empty, Field, Modal, ModalActions, Panel, SimpleTable } from "../components/ui";
import { useI18n } from "../i18n";
import { formSubmit, formatDate } from "../lib/forms";
import type { ConsoleData, MCPTokenCreateResponse } from "../types";

export function KeysPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const [modal, setModal] = useState(false);
  const [mcpModal, setMCPModal] = useState(false);
  const [createdMCP, setCreatedMCP] = useState<MCPTokenCreateResponse | null>(null);
  const queryClient = useQueryClient();
  const create = useMutation({ mutationFn: api.createKey, onSuccess: async () => { setModal(false); await queryClient.invalidateQueries({ queryKey: ["keys"] }); } });
  const remove = useMutation({ mutationFn: api.deleteKey, onSuccess: async () => queryClient.invalidateQueries({ queryKey: ["keys"] }) });
  const mcpTokens = useQuery({ queryKey: ["mcp-tokens"], queryFn: api.mcpTokens });
  const createMCP = useMutation({ mutationFn: api.createMCPToken, onSuccess: async (out) => { setCreatedMCP(out); await queryClient.invalidateQueries({ queryKey: ["mcp-tokens"] }); } });
  const removeMCP = useMutation({ mutationFn: api.deleteMCPToken, onSuccess: async () => queryClient.invalidateQueries({ queryKey: ["mcp-tokens"] }) });
  const closeMCPModal = () => {
    setMCPModal(false);
    setCreatedMCP(null);
  };
  const openMCPModal = () => {
    setCreatedMCP(null);
    setMCPModal(true);
  };
  const tokens = mcpTokens.data?.tokens || [];
  return (
    <>
      <section className="resource-head">
        <div><small>{t("keysPageEyebrow")}</small><h2>{t("keys")}</h2><p>{t("keysPageBody")}</p></div>
        <button type="button" className="primary" onClick={() => setModal(true)}><Plus />{t("addPublicKey")}</button>
      </section>
      <Panel title={t("keysListTitle")} subtitle={t("keysListBody")}>
        {data.keys.length ? <SimpleTable headers={[t("commonName"), t("keysFingerprint"), t("commonCreatedAt"), t("commonActions")]} rows={data.keys.map((key) => [
          <strong>{key.name}</strong>,
          <code>{key.fingerprint}</code>,
          formatDate(key.created_at),
          <button type="button" onClick={() => remove.mutate(key.id)}>{t("commonDelete")}</button>,
        ])} /> : <Empty title={t("keysEmptyTitle")} body={t("keysEmptyBody")} />}
      </Panel>
      <section className="panel mcp-token-panel">
        <div className="panel-head">
          <div><h2>{t("mcpTokensTitle")}</h2><p>{t("mcpTokensBody")}</p></div>
          <button type="button" className="primary" onClick={openMCPModal}><KeyRound />{t("mcpTokenGenerate")}</button>
        </div>
        {!mcpTokens.data ? <p>{t("loading")}</p> : tokens.length ? <SimpleTable headers={[t("commonName"), t("commonCreatedAt"), t("mcpLastUsedAt"), t("commonActions")]} rows={tokens.map((token) => [
          <strong>{token.name}</strong>,
          formatDate(token.created_at),
          token.last_used_at ? formatDate(token.last_used_at) : "-",
          <button type="button" onClick={() => removeMCP.mutate(token.id)}>{t("commonDelete")}</button>,
        ])} /> : <Empty title={t("mcpTokensEmptyTitle")} body={t("mcpTokensEmptyBody")} />}
      </section>
      {modal && <Modal title={t("keysAddTitle")} onClose={() => setModal(false)} closeOnEscape={false}>
        <form className="stack" onSubmit={(event) => formSubmit(event, (body) => create.mutate({ name: body.name, authorized_key: body.authorized_key }))}>
          <Field label={t("commonTitle")} name="name" required />
          <label className="field"><span>{t("keysTableKey")}</span><textarea name="authorized_key" placeholder="ssh-ed25519 AAAA..." required /></label>
          <ModalActions onCancel={() => setModal(false)} submit={t("addPublicKey")} />
        </form>
      </Modal>}
      {mcpModal && <Modal title={t("mcpTokenCreateTitle")} onClose={closeMCPModal} wide closeOnEscape={false}>
        {createdMCP ? <div className="stack">
          <CommandBox label={t("mcpTokenValue")} value={createdMCP.token_value} copyLabel={t("commonCopy")} />
          <CommandBox label={t("mcpJSON")} value={JSON.stringify(createdMCP.mcp_json, null, 2)} copyLabel={t("commonCopy")} />
          <div className="form-actions span-two"><button type="button" className="primary" onClick={closeMCPModal}>{t("close")}</button></div>
        </div> : <form className="stack" onSubmit={(event) => formSubmit(event, (body) => createMCP.mutate({ name: body.name }))}>
          <Field label={t("commonName")} name="name" placeholder="Codex" />
          <ModalActions onCancel={closeMCPModal} submit={t("mcpTokenGenerate")} />
        </form>}
      </Modal>}
    </>
  );
}
