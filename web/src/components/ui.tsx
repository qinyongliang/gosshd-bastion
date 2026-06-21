import clsx from "clsx";
import { Activity, Copy, Search, X } from "lucide-react";
import { ReactNode, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { useI18n } from "../i18n";
import type { AuditLog, Member, Target } from "../types";
import { copyText, tagColor } from "../utils";
import { formatDate } from "../lib/forms";

export function AuditTable({ logs }: { logs: AuditLog[] }) {
  const { t } = useI18n();
  return <SimpleTable headers={[t("auditTableUser"), t("auditTableKey"), t("auditTableTarget"), t("auditTableCommand"), t("auditTableType"), t("auditTableDecision"), t("auditTableReason"), t("auditTableExit"), t("auditTableStarted")]} rows={logs.map((log) => [
    <span><strong>{log.user_display_name || log.user_email || "-"}</strong><small>{log.user_email || ""}</small></span>,
    <span><strong>{log.public_key_name || "-"}</strong><small>{log.public_key_fingerprint || ""}</small></span>,
    <span><strong>{log.target_name || log.target_alias || "-"}</strong><small>{log.target_endpoint || ""}</small></span>,
    <code>{log.command || "-"}</code>,
    log.request_type,
    <span className={clsx("badge", log.policy_decision === "allow" ? "success" : "danger")}>{log.policy_decision === "allow" ? t("commonAllow") : t("commonDeny")}</span>,
    log.policy_reason || "-",
    String(log.exit_code ?? ""),
    formatDate(log.started_at),
  ])} />;
}

export function NavButton({ to, label, icon, onClick }: { to: string; label: string; icon: ReactNode; onClick: () => void }) {
  const location = useLocation();
  const active = to === "/" ? location.pathname === "/" : location.pathname.startsWith(to);
  return <Link className={clsx("nav-link", active && "active")} to={to} onClick={onClick}>{icon}{label}</Link>;
}

export function Panel({ title, subtitle, children }: { title: string; subtitle?: string; children: ReactNode }) {
  return <section className="panel"><div className="panel-head"><div><h2>{title}</h2>{subtitle && <p>{subtitle}</p>}</div></div>{children}</section>;
}

export function SummaryCard({ index, title, body }: { index: string; title: string; body: string }) {
  return <section className="access-summary-card"><span>{index}</span><strong>{title}</strong><small>{body}</small></section>;
}

export function Metric({ label, value, icon }: { label: string; value: number; icon?: ReactNode }) {
  return <div className="metric">{icon || <Activity />}<span>{label}</span><strong>{value}</strong></div>;
}

export function Modal({ title, children, onClose, wide = false, stacked = false }: { title: string; children: ReactNode; onClose: () => void; wide?: boolean; stacked?: boolean }) {
  const { t } = useI18n();
  return <div className={clsx("overlay", stacked && "stacked")}><section className={clsx("modal", wide && "wide")} role="dialog" aria-label={title}>
    <header className="surface-head"><div><h2>{title}</h2></div><button className="icon-button" type="button" aria-label={t("close")} onClick={onClose}><X /></button></header>
    <div className="surface-body modal-body-list">{children}</div>
  </section></div>;
}

export function Drawer({ title, subtitle, children, onClose }: { title: string; subtitle?: string; children: ReactNode; onClose: () => void }) {
  const { t } = useI18n();
  return <div className="drawer-layer">
    <button className="drawer-scrim" type="button" tabIndex={-1} aria-hidden="true" onClick={onClose} />
    <aside className="drawer">
      <header className="surface-head"><div><h2>{title}</h2>{subtitle && <p>{subtitle}</p>}</div><button className="icon-button" type="button" aria-label={t("close")} onClick={onClose}><X /></button></header>
      <div className="surface-body">{children}</div>
    </aside>
  </div>;
}

export function Field({ label, name, type = "text", defaultValue = "", required = false, placeholder = "", disabled = false }: { label: string; name: string; type?: string; defaultValue?: string; required?: boolean; placeholder?: string; disabled?: boolean }) {
  return <label className="field"><span>{label}</span><input name={name} type={type} defaultValue={defaultValue} required={required} placeholder={placeholder} disabled={disabled} /></label>;
}

export function Select({ label, name, options, defaultValue = "" }: { label: string; name: string; options: (readonly [string, string])[]; defaultValue?: string }) {
  return <label className="field"><span>{label}</span><select name={name} defaultValue={defaultValue}>{options.map(([value, text]) => <option key={value} value={value}>{text}</option>)}</select></label>;
}

export function Toggle({ name, label, defaultChecked }: { name: string; label: string; defaultChecked?: boolean }) {
  return <label className="toggle-row"><input type="checkbox" name={name} defaultChecked={defaultChecked} /><span>{label}</span></label>;
}

export function ModalActions({ onCancel, submit }: { onCancel?: () => void; submit: string }) {
  const { t } = useI18n();
  return <div className="form-actions span-two">{onCancel && <button type="button" onClick={onCancel}>{t("cancel")}</button>}<button type="submit" className="primary">{submit}</button></div>;
}

export function Segmented({ value, items, onChange }: { value: string; items: (readonly [string, string])[]; onChange: (value: string) => void }) {
  return <div className="theme-switch">{items.map(([id, label]) => <button key={id} type="button" className={clsx(value === id && "active")} onClick={() => onChange(id)}>{label}</button>)}</div>;
}

export function Toolbar({ query, setQuery, children }: { query: string; setQuery: (value: string) => void; children?: ReactNode }) {
  const { t } = useI18n();
  return <div className="toolbar"><Search /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("commonSearchPlaceholder")} />{children}<button type="button" onClick={() => setQuery("")}>{t("commonClearFilters")}</button></div>;
}

export function SimpleTable({ headers, rows }: { headers: string[]; rows: ReactNode[][] }) {
  return <div className="table-wrap"><table><thead><tr>{headers.map((item) => <th key={item}>{item}</th>)}</tr></thead><tbody>{rows.map((row, index) => <tr key={index}>{row.map((cell, cellIndex) => <td key={cellIndex}>{cell}</td>)}</tr>)}</tbody></table></div>;
}

export function Empty({ title, body }: { title: string; body: string }) {
  return <div className="empty-state"><div className="empty-orbit" /><strong>{title}</strong><span>{body}</span></div>;
}

export function UserCell({ member }: { member: Pick<Member, "display_name" | "email" | "user_id" | "role"> }) {
  return <span><strong>{member.display_name || member.email}</strong><small>{member.email}</small></span>;
}

export function TagList({ target }: { target: Target }) {
  return <span className="tag-row">{(target.tags || []).map((tag) => <Tag key={tag} tag={tag} color={tagColor(tag, target.tag_colors)} />)}</span>;
}

export function Tag({ tag, color }: { tag: string; color: string }) {
  return <span className={`tag-chip tag-color-${color}`} data-tag={tag}>{tag}</span>;
}

export function CopyButton({ value }: { value: string }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  return <button type="button" className="copy-anchor" data-value={value} onClick={async () => { await copyText(value); setCopied(true); window.setTimeout(() => setCopied(false), 1300); }}>
    <Copy />{t("copyConnectionCommand")}{copied && <span className="copy-tip">{t("copied")}</span>}
  </button>;
}

export function CommandBox({ label, value }: { label: string; value: string }) {
  return <div className="command-box"><span>{label}</span><code>{value}</code><CopyButton value={value} /></div>;
}

export function SelectButton({ label, items, onSelect }: { label: string; items: (readonly [string, string])[]; onSelect: (value: string) => void }) {
  const { t } = useI18n();
  return <label className="field"><span>{label}</span><select defaultValue="" onChange={(event) => { if (event.target.value) onSelect(event.target.value); event.target.value = ""; }}><option value="">{t("commonSelectPlaceholder")}</option>{items.map(([value, text]) => <option key={value} value={value}>{text}</option>)}</select></label>;
}

export function Loading() {
  return <section className="loading-view"><div className="mark">g</div><p>Loading bastion console...</p></section>;
}

export function Fatal({ error }: { error: unknown }) {
  return <section className="auth-screen"><div className="auth-card"><div className="status error">{error instanceof Error ? error.message : String(error)}</div></div></section>;
}
