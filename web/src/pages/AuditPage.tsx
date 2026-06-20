import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { useState } from "react";
import { api } from "../api";
import { AuditTable, Empty, Panel } from "../components/ui";
import { useI18n } from "../i18n";
import { formSubmit } from "../lib/forms";
import type { ConsoleData } from "../types";

export function AuditPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const [filters, setFilters] = useState({ query: "", started_from: "", started_to: "", page: 1, page_size: 20 });
  const audit = useQuery({ queryKey: ["audit-page", filters], queryFn: () => api.audit(filters) });
  const logs = audit.data?.logs || data.auditPage.logs;
  return (
    <>
      <section className="resource-head">
        <div><small>{t("auditPageEyebrow")}</small><h2>{t("auditPageTitle")}</h2><p>{t("auditPageBody")}</p></div>
      </section>
      <form className="toolbar" onSubmit={(event) => formSubmit(event, (body) => setFilters({ query: body.query || "", started_from: body.started_from || "", started_to: body.started_to || "", page: 1, page_size: 20 }))}>
        <Search />
        <input name="query" placeholder={t("auditSearchPlaceholder")} />
        <input name="started_from" type="datetime-local" />
        <input name="started_to" type="datetime-local" />
        <button type="submit">{t("search")}</button>
      </form>
      <Panel title={t("auditList")} subtitle="">
        {logs.length ? <AuditTable logs={logs} /> : <Empty title={t("auditEmptyTitle")} body={t("auditEmptyBody")} />}
      </Panel>
      <div className="pager">
        <button type="button" disabled={filters.page <= 1} onClick={() => setFilters({ ...filters, page: filters.page - 1 })}>{t("commonPrevious")}</button>
        <span>{t("commonPage")} {audit.data?.page || 1}</span>
        <button type="button" disabled={(audit.data?.total || 0) <= filters.page * filters.page_size} onClick={() => setFilters({ ...filters, page: filters.page + 1 })}>{t("commonNext")}</button>
      </div>
    </>
  );
}
