import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useEffect, useState } from "react";

import { api } from "../api";
import { dateLocale, useI18n } from "../i18n";
import type { Runtime, Target, TemporarySSHAuthorization } from "../types";
import { CommandBox, CopyButton, ErrorMessage, Modal } from "./ui";

export function TemporarySSHAuthorizationsModal({ target, runtime, onClose }: { target: Target; runtime: Runtime; onClose: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const queryKey = ["temporary-ssh-authorizations", target.id];
  const authorizations = useQuery({ queryKey, queryFn: () => api.temporarySSHAuthorizations(target.id) });
  const [name, setName] = useState("");
  const [hours, setHours] = useState("24");
  const [deleting, setDeleting] = useState<TemporarySSHAuthorization | null>(null);
  const [now, setNow] = useState(Date.now());
  const refresh = () => queryClient.invalidateQueries({ queryKey });
  const create = useMutation({
    mutationFn: () => api.createTemporarySSHAuthorization(target.id, { name: name.trim(), duration_seconds: Math.round(Number(hours) * 3600) }),
    onSuccess: async () => { setName(""); await refresh(); },
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.deleteTemporarySSHAuthorization(target.id, id),
    onSuccess: async () => { setDeleting(null); await refresh(); },
  });

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    create.mutate();
  }

  return <Modal title={`${t("temporarySSHAuthorizationsTitle")} · ${target.name}`} onClose={onClose} wide className="temporary-ssh-modal">
    <div className="temporary-ssh-manager">
      <p className="temporary-ssh-description">{t("temporarySSHAuthorizationsBody")}</p>
      <form className="temporary-ssh-create" onSubmit={submit}>
        <label className="field"><span>{t("temporarySSHAuthorizationName")}</span><input value={name} onChange={(event) => setName(event.target.value)} disabled={create.isPending} /></label>
        <label className="field"><span>{t("temporarySSHAuthorizationLifetime")}</span><input type="number" min="0.01" step="0.01" required value={hours} onChange={(event) => setHours(event.target.value)} disabled={create.isPending} /></label>
        <button type="submit" className="primary" disabled={create.isPending}>{t(create.isPending ? "temporarySSHAuthorizationCreating" : "temporarySSHAuthorizationCreate")}</button>
      </form>
      <ErrorMessage error={create.error} />
      <ErrorMessage error={authorizations.error} />
      {authorizations.isError && <button type="button" disabled={authorizations.isFetching} onClick={() => void authorizations.refetch()}>{t("temporarySSHAuthorizationRetry")}</button>}
      {authorizations.isPending && <p role="status">{t("loading")}</p>}
      {authorizations.isSuccess && !authorizations.data.authorizations.length && <p className="temporary-ssh-empty">{t("temporarySSHAuthorizationEmpty")}</p>}
      <div className="temporary-ssh-list">
        {(authorizations.data?.authorizations || []).map((authorization) => <AuthorizationCard key={authorization.id} authorization={authorization} runtime={runtime} now={now} onRefresh={refresh} onDelete={() => { remove.reset(); setDeleting(authorization); }} deleting={remove.isPending} />)}
      </div>
    </div>
    {deleting && <Modal title={t("temporarySSHAuthorizationDeleteTitle")} onClose={() => { if (!remove.isPending) setDeleting(null); }} stacked closeOnEscape={!remove.isPending}>
      <p>{t("temporarySSHAuthorizationDeleteConfirm")}</p>
      <ErrorMessage error={remove.error} />
      <div className="form-actions">
        <button type="button" disabled={remove.isPending} onClick={() => setDeleting(null)}>{t("cancel")}</button>
        <button type="button" className="danger" disabled={remove.isPending} onClick={() => remove.mutate(deleting.id)}>{t(remove.isPending ? "temporarySSHAuthorizationDeleting" : "commonDelete")}</button>
      </div>
    </Modal>}
  </Modal>;
}

function AuthorizationCard({ authorization, runtime, now, onRefresh, onDelete, deleting }: { authorization: TemporarySSHAuthorization; runtime: Runtime; now: number; onRefresh: () => Promise<void>; onDelete: () => void; deleting: boolean }) {
  const { t, locale } = useI18n();
  const [hours, setHours] = useState("24");
  const renew = useMutation({
    mutationFn: () => api.renewTemporarySSHAuthorization(authorization.target_id, authorization.id, { duration_seconds: Math.round(Number(hours) * 3600) }),
    onSuccess: onRefresh,
  });
  const expired = Date.parse(authorization.expires_at) <= now;
  const command = `ssh -p ${runtime.ssh_port || 22} ${authorization.token}@${runtime.ssh_host || location.hostname}`;

  return <article className="temporary-ssh-card" data-authorization-id={authorization.id}>
    <div className="temporary-ssh-card-head">
      <strong>{authorization.name || t("temporarySSHAuthorizationUnnamed")}</strong>
      <span className={`badge ${expired ? "danger" : "success"}`}>{t(expired ? "temporarySSHAuthorizationExpired" : "temporarySSHAuthorizationActive")}</span>
    </div>
    <div className="temporary-ssh-token"><code>{authorization.token}</code><CopyButton value={authorization.token} label={t("temporarySSHAuthorizationCopyUUID")} /></div>
    <p className="temporary-ssh-expiry">{t("temporarySSHAuthorizationExpires")} <time dateTime={authorization.expires_at}>{new Date(authorization.expires_at).toLocaleString(dateLocale(locale))}</time></p>
    <CommandBox label={t("temporarySSHAuthorizationCommand")} value={command} copyLabel={t("copyConnectionCommand")} />
    <form className="temporary-ssh-renew" onSubmit={(event) => { event.preventDefault(); renew.mutate(); }}>
      <label className="field"><span>{t("temporarySSHAuthorizationRenewHours")}</span><input type="number" min="0.01" step="0.01" required value={hours} onChange={(event) => setHours(event.target.value)} disabled={renew.isPending || deleting} /></label>
      <button type="submit" disabled={renew.isPending || deleting}>{t(renew.isPending ? "temporarySSHAuthorizationRenewing" : "temporarySSHAuthorizationRenew")}</button>
      <button type="button" className="danger" disabled={renew.isPending || deleting} onClick={onDelete}>{t("commonDelete")}</button>
    </form>
    <ErrorMessage error={renew.error} />
  </article>;
}
