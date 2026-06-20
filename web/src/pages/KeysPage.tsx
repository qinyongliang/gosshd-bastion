import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useState } from "react";
import { api } from "../api";
import { Empty, Field, Modal, ModalActions, Panel, SimpleTable } from "../components/ui";
import { useI18n } from "../i18n";
import { formSubmit, formatDate } from "../lib/forms";
import type { ConsoleData } from "../types";

export function KeysPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const [modal, setModal] = useState(false);
  const queryClient = useQueryClient();
  const create = useMutation({ mutationFn: api.createKey, onSuccess: async () => { setModal(false); await queryClient.invalidateQueries({ queryKey: ["keys"] }); } });
  const remove = useMutation({ mutationFn: api.deleteKey, onSuccess: async () => queryClient.invalidateQueries({ queryKey: ["keys"] }) });
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
      {modal && <Modal title={t("keysAddTitle")} onClose={() => setModal(false)}>
        <form className="stack" onSubmit={(event) => formSubmit(event, (body) => create.mutate({ name: body.name, authorized_key: body.authorized_key }))}>
          <Field label={t("commonTitle")} name="name" required />
          <label className="field"><span>{t("keysTableKey")}</span><textarea name="authorized_key" placeholder="ssh-ed25519 AAAA..." required /></label>
          <ModalActions onCancel={() => setModal(false)} submit={t("addPublicKey")} />
        </form>
      </Modal>}
    </>
  );
}
