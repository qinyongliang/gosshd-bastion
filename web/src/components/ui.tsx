import clsx from "clsx";
import { Button as AntButton, Drawer as AntDrawer, Dropdown as AntDropdown, Input as AntInput, Modal as AntModal, Pagination as AntPagination, Select as AntSelect, Spin } from "antd";
import enUS from "antd/locale/en_US";
import zhCN from "antd/locale/zh_CN";
import { Activity, Check, Copy, MoreHorizontal, Play, Search, X } from "lucide-react";
import { Children, cloneElement, isValidElement, ReactNode, useEffect, useState, type CSSProperties } from "react";
import { Link, useLocation } from "react-router-dom";
import { dateLocale, useI18n } from "../i18n";
import type { AuditLog, Member, Target } from "../types";
import { copyText, tagColor } from "../utils";
import { formatDate } from "../lib/forms";
import { appIcon, type Branding } from "../lib/branding";
import { HighlightedCommand } from "./HighlightedCommand";

export function AuditTable({ logs, onReplay, onLiveOutput, compact = false }: { logs: AuditLog[]; onReplay?: (log: AuditLog) => void; onLiveOutput?: (log: AuditLog) => void; compact?: boolean }) {
  const { t } = useI18n();
  const [detail, setDetail] = useState<{ title: string; value: string; mono?: boolean } | null>(null);
  const columns = [
    ...(!compact ? [["user", "auditTableUser"], ["key", "auditTableKey"]] : []),
    ["target", "auditTableTarget"], ["command", "auditTableCommand"], ["decision", "auditTableDecision"],
    ["reason", "auditTableReason"], ["exit", "auditTableExit"], ["duration", "auditTableDuration"], ["started", "auditTableStarted"],
    ...(onReplay || onLiveOutput ? [["actions", "commonActions"]] : []),
  ];
  const headers = columns.map(([, label]) => t(label));
  const openDetail = (title: string, value: string, mono = false) => {
    const trimmed = value.trim();
    if (!trimmed || trimmed === "-") return;
    setDetail({ title, value: trimmed, mono });
  };
  const rows = logs.map((log) => {
      const userPrimary = log.user_display_name || log.user_email || "-";
      const userSecondary = log.user_email && log.user_email !== userPrimary ? log.user_email : "";
      const keyName = log.public_key_name === "Temporary SSH authorization" && !log.public_key_fingerprint ? t("temporarySSHAuthorizationUnnamed") : log.public_key_name || "-";
      const row: ReactNode[] = compact ? [
        <AuditTextCell title={t("auditTableTarget")} primary={log.target_name || log.target_alias || "-"} secondary={log.target_endpoint || ""} onOpen={openDetail} />,
        <AuditTextCell title={t("auditTableCommand")} primary={log.command || "-"} mono lines={2} onOpen={openDetail} />,
        <span className={clsx("badge", log.policy_decision === "allow" ? "success" : "danger")}>{log.policy_decision === "allow" ? t("commonAllow") : t("commonDeny")}</span>,
        <AuditTextCell title={t("auditTableReason")} primary={log.policy_reason || "-"} lines={2} onOpen={openDetail} />,
        String(log.exit_code ?? ""),
        formatAuditDuration(log, t("auditRunning")),
        <AuditStartedAt value={log.started_at} />,
      ] : [
        <AuditTextCell title={t("auditTableUser")} primary={userPrimary} secondary={userSecondary} onOpen={openDetail} />,
        <AuditTextCell title={t("auditTableKey")} primary={keyName} onOpen={openDetail} />,
        <AuditTextCell title={t("auditTableTarget")} primary={log.target_name || log.target_alias || "-"} secondary={log.target_endpoint || ""} onOpen={openDetail} />,
        <AuditTextCell title={t("auditTableCommand")} primary={log.command || "-"} mono lines={2} onOpen={openDetail} />,
        <span className={clsx("badge", log.policy_decision === "allow" ? "success" : "danger")}>{log.policy_decision === "allow" ? t("commonAllow") : t("commonDeny")}</span>,
        <AuditTextCell title={t("auditTableReason")} primary={log.policy_reason || "-"} lines={2} onOpen={openDetail} />,
        String(log.exit_code ?? ""),
        formatAuditDuration(log, t("auditRunning")),
        <AuditStartedAt value={log.started_at} />,
      ];
      if (onReplay || onLiveOutput) {
        row.push(log.running && onLiveOutput
          ? <button type="button" className="small primary" onClick={() => onLiveOutput(log)}><Activity />{t("auditLiveOutput")}</button>
          : log.has_recording && onReplay
            ? <button type="button" className="small" onClick={() => onReplay(log)}><Play />{t("auditReplay")}</button>
            : <span className="muted">-</span>);
      }
      return row;
    });
  return <>
    <div className={clsx("audit-table-compact", compact ? "audit-table-client" : "audit-table-full")}>
      <div className="table-wrap"><table>
        <colgroup>{columns.map(([key]) => <col key={key} className={`audit-col-${key}`} />)}</colgroup>
        <thead><tr>{headers.map((label, index) => <th key={columns[index][0]} scope="col">{label}</th>)}</tr></thead>
        <tbody>{rows.map((row, index) => <tr key={logs[index].id}>{row.map((cell, columnIndex) => <td key={columns[columnIndex][0]} data-label={headers[columnIndex]} data-column={columns[columnIndex][0]}>{cell}</td>)}</tr>)}</tbody>
      </table></div>
    </div>
    {detail && <Modal title={detail.title} onClose={() => setDetail(null)} wide className="audit-detail-modal">
      {detail.mono ? <HighlightedCommand command={detail.value} className="audit-detail-content" /> : <pre className="audit-detail-content">{detail.value}</pre>}
    </Modal>}
  </>;
}

function AuditStartedAt({ value }: { value: string }) {
  const { locale } = useI18n();
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return <span>{value || "-"}</span>;
  return <time className="audit-started-at" dateTime={value} title={formatDate(value)}>
    <span>{new Intl.DateTimeFormat(dateLocale(locale), { dateStyle: "short" }).format(date)}</span>
    <span>{new Intl.DateTimeFormat(dateLocale(locale), { timeStyle: "medium" }).format(date)}</span>
  </time>;
}

function AuditTextCell({ title, primary, secondary = "", mono = false, lines = 1, onOpen }: { title: string; primary: string; secondary?: string; mono?: boolean; lines?: 1 | 2; onOpen: (title: string, value: string, mono?: boolean) => void }) {
  const full = [primary, secondary].filter(Boolean).join("\n");
  return <button type="button" className={clsx("audit-cell", mono && "mono", lines === 2 && "two-lines")} title={full} onClick={() => onOpen(title, full, mono)}>
    {mono ? <HighlightedCommand command={primary} className="audit-cell-command" /> : <strong>{primary}</strong>}
    {secondary && <small>{secondary}</small>}
  </button>;
}

function formatAuditDuration(log: AuditLog, runningLabel: string) {
  const duration = log.recording_duration_ms || (log.ended_at ? new Date(log.ended_at).getTime() - new Date(log.started_at).getTime() : undefined);
  if (duration === undefined || !Number.isFinite(duration) || duration < 0) return log.ended_at ? "-" : runningLabel;
  if (duration < 1000) return `${duration}ms`;
  if (duration < 60_000) return `${(duration / 1000).toFixed(1)}s`;
  const minutes = Math.floor(duration / 60_000);
  return `${minutes}m ${Math.round((duration % 60_000) / 1000)}s`;
}

export function NavButton({ to, label, icon, onClick }: { to: string; label: string; icon: ReactNode; onClick: () => void }) {
  const location = useLocation();
  const active = to === "/" ? location.pathname === "/" : location.pathname.startsWith(to);
  return <Link className={clsx("nav-link", active && "active")} to={to} onClick={onClick}>{icon}{label}</Link>;
}

export function ActionMenu({ label, children }: { label: string; children: ReactNode }) {
  const [desktop, setDesktop] = useState(false);
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const workspace = document.querySelector(".workspace");
    if (!workspace) return;
    const observer = new ResizeObserver(([entry]) => setDesktop(entry.contentRect.width >= 1200));
    observer.observe(workspace);
    return () => observer.disconnect();
  }, []);
  if (desktop) return <div className="action-menu-inline">{children}</div>;
  const menuChildren = Children.map(children, (child) => {
    if (!isValidElement<{ onClick?: (event: React.MouseEvent) => void; "data-keep-menu"?: string }>(child)) return child;
    return cloneElement(child, {
      onClick: (event) => {
        child.props.onClick?.(event);
        if (child.props["data-keep-menu"] !== "true") setOpen(false);
      },
    });
  });
  return <AntDropdown open={open} onOpenChange={setOpen} trigger={["click"]} placement="bottomRight" destroyOnHidden popupRender={() => <div className="action-menu-popover antd-action-menu" role="menu">{menuChildren}</div>}>
    <button type="button" className="action-menu-trigger" aria-label={label} aria-haspopup="menu" aria-expanded={open}><MoreHorizontal /><span>{label}</span></button>
  </AntDropdown>;
}

export function Panel({ title, subtitle, children, className = "" }: { title: string; subtitle?: string; children: ReactNode; className?: string }) {
  return <section className={clsx("panel", className)}><div className="panel-head"><div><h3>{title}</h3>{subtitle && <p>{subtitle}</p>}</div></div>{children}</section>;
}

export function SummaryCard({ index, title, body }: { index: string; title: string; body: string }) {
  return <section className="access-summary-card"><span>{index}</span><strong>{title}</strong><small>{body}</small></section>;
}

export function Metric({ label, value, icon }: { label: string; value: number; icon?: ReactNode }) {
  return <div className="metric"><div className="metric-label">{icon || <Activity />}<span>{label}</span></div><strong>{value}</strong></div>;
}

export function Modal({ title, children, onClose, wide = false, stacked = false, className = "", closeOnEscape = true }: { title: string; children: ReactNode; onClose: () => void; wide?: boolean; stacked?: boolean; className?: string; closeOnEscape?: boolean }) {
  return <AntModal open onCancel={onClose} closeIcon={<X />} title={title} footer={null} keyboard={closeOnEscape} maskClosable={closeOnEscape} width={wide ? 1000 : 560} className={clsx(className, stacked && "stacked")} destroyOnHidden>{children}</AntModal>;
}

export function Drawer({ title, subtitle, children, onClose }: { title: string; subtitle?: string; children: ReactNode; onClose: () => void }) {
  return <AntDrawer open onClose={onClose} closeIcon={<X />} title={<div><strong>{title}</strong>{subtitle && <small className="drawer-subtitle">{subtitle}</small>}</div>} placement="right" width={Math.min(720, typeof window === "undefined" ? 720 : window.innerWidth)} destroyOnHidden>{children}</AntDrawer>;
}

export function Field({ label, name, type = "text", defaultValue = "", required = false, placeholder = "", disabled = false }: { label: string; name: string; type?: string; defaultValue?: string; required?: boolean; placeholder?: string; disabled?: boolean }) {
  return <label className="field"><span>{label}</span><AntInput name={name} type={type} defaultValue={defaultValue} required={required} placeholder={placeholder} disabled={disabled} /></label>;
}

export function Select({ label, name, options, defaultValue = "" }: { label: string; name: string; options: (readonly [string, string])[]; defaultValue?: string }) {
  const [value, setValue] = useState(defaultValue);
  return <label className="field"><span>{label}</span><AntSelect value={value} onChange={setValue} options={options.map(([itemValue, text]) => ({ value: itemValue, label: text }))} /><input type="hidden" name={name} value={value} readOnly /></label>;
}

export function Toggle({ name, label, defaultChecked }: { name: string; label: string; defaultChecked?: boolean }) {
  return <label className="toggle-row"><input type="checkbox" name={name} defaultChecked={defaultChecked} /><span>{label}</span></label>;
}

export function ModalActions({ onCancel, submit }: { onCancel?: () => void; submit: string }) {
  const { t } = useI18n();
  return <div className="form-actions span-two">{onCancel && <AntButton onClick={onCancel}>{t("cancel")}</AntButton>}<AntButton htmlType="submit" type="primary">{submit}</AntButton></div>;
}

export function Segmented({ value, items, onChange }: { value: string; items: (readonly [string, string])[]; onChange: (value: string) => void }) {
  return <div className="theme-switch">{items.map(([id, label]) => <button key={id} type="button" className={clsx(value === id && "active")} onClick={() => onChange(id)}>{label}</button>)}</div>;
}

export function Toolbar({ query, setQuery, children }: { query: string; setQuery: (value: string) => void; children?: ReactNode }) {
  const { t } = useI18n();
  return <div className="toolbar"><Search /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("commonSearchPlaceholder")} />{children}<button type="button" onClick={() => setQuery("")}>{t("commonClearFilters")}</button></div>;
}

export function Pagination({ page, pageSize, total, onChange, disabled = false, compact = false }: {
  page: number; pageSize: number; total: number; onChange: (page: number, pageSize: number) => void; disabled?: boolean; compact?: boolean;
}) {
  const { t, locale } = useI18n();
  return <nav className={clsx("pager", "app-pagination", compact && "compact")} aria-label={t("paginationNavigation")} onKeyDown={(event) => {
    if (event.key === "Enter" && event.target instanceof HTMLInputElement) event.preventDefault();
  }}>
    <AntPagination current={page} pageSize={pageSize} total={total} disabled={disabled}
      pageSizeOptions={[10, 20, 50, 100]} showSizeChanger={{ "aria-label": t("paginationPageSize") }}
      showQuickJumper={{ goButton: true }} showLessItems
      locale={{ ...(locale === "zh-CN" ? zhCN.Pagination : enUS.Pagination), jump_to_confirm: t("paginationGo") }}
      showTotal={(count: number) => t("paginationTotal").replace("{count}", String(count))}
      onChange={(nextPage: number, nextSize: number) => onChange(nextSize === pageSize ? nextPage : 1, nextSize)}
      itemRender={(_page: number, type: string, element: ReactNode) => {
        if ((type === "prev" || type === "next") && isValidElement<{ children?: ReactNode; "aria-label"?: string }>(element)) {
          const label = t(type === "prev" ? "paginationPreviousPage" : "paginationNextPage");
          return cloneElement(element, { children: label, "aria-label": label });
        }
        return element;
      }} />
  </nav>;
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

export function CopyButton({ value, label }: { value: string; label?: string }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1800);
    return () => window.clearTimeout(timer);
  }, [copied, value]);
  return <button type="button" className="copy-anchor" data-value={value} onClick={async () => {
    try {
      await copyText(value);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }}>
    {copied ? <Check /> : <Copy />}<span>{copied ? t("copied") : label || t("copyConnectionCommand")}</span>
  </button>;
}

export function CommandBox({ label, value, copyLabel }: { label: string; value: string; copyLabel?: string }) {
  const multiline = value.includes("\n") || value.trim().startsWith("{") || value.trim().startsWith("[");
  return <div className={clsx("command-box", multiline && "multiline")}><span>{label}</span><HighlightedCommand command={value} className="command-box-code" /><CopyButton value={value} label={copyLabel} /></div>;
}

export function SelectButton({ label, items, onSelect }: { label: string; items: (readonly [string, string])[]; onSelect: (value: string) => void }) {
  const { t } = useI18n();
  return <label className="field"><span>{label}</span><select defaultValue="" onChange={(event) => { if (event.target.value) onSelect(event.target.value); event.target.value = ""; }}><option value="">{t("commonSelectPlaceholder")}</option>{items.map(([value, text]) => <option key={value} value={value}>{text}</option>)}</select></label>;
}

export function ErrorMessage({ error }: { error: unknown }) {
  if (!error) return null;
  return <p className="form-error" role="alert">{error instanceof Error ? error.message : String(error)}</p>;
}

export function InlineNotice({ tone = "info", children }: { tone?: "info" | "success" | "danger"; children: ReactNode }) {
  return <div className={clsx("inline-notice", tone)} role={tone === "danger" ? "alert" : "status"}>{children}</div>;
}

export function ConfirmDialog({ title, body, confirmLabel, danger = false, onConfirm, onClose }: { title: string; body: string; confirmLabel: string; danger?: boolean; onConfirm: () => void; onClose: () => void }) {
  return <Modal title={title} onClose={onClose}><div className="confirm-dialog"><p>{body}</p><div className="form-actions"><button type="button" onClick={onClose}>Cancel</button><button type="button" className={danger ? "danger" : "primary"} onClick={() => { onConfirm(); onClose(); }}>{confirmLabel}</button></div></div></Modal>;
}

export function Loading() {
  return <section className="loading-view" style={{ minHeight: "100dvh", display: "grid", placeItems: "center", alignContent: "center", gap: 16, padding: 24, color: "#667085" }}>
    <div style={{ position: "relative", width: 112, height: 112 }}>
      <BrandMark style={{ width: 112, height: 112, borderRadius: 20 }} />
      <Spin size="small" style={{ position: "absolute", right: 8, bottom: 8 }} />
    </div>
    <p style={{ margin: 0, fontSize: 14 }}>Loading...</p>
  </section>;
}

export function BrandMark({ branding, className = "", style }: { branding?: Branding; className?: string; style?: CSSProperties }) {
  return <span className={clsx("mark", className)} style={style}><img src={appIcon(branding)} alt="" style={{ width: style ? "100%" : "72%", height: style ? "100%" : "72%", objectFit: "contain" }} /></span>;
}

export function Fatal({ error }: { error: unknown }) {
  return <section className="auth-screen"><div className="auth-card"><div className="status error">{error instanceof Error ? error.message : String(error)}</div></div></section>;
}
