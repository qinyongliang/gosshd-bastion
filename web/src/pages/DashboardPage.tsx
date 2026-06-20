import { ListChecks, Server, Shield, Users } from "lucide-react";
import { AuditTable, Empty, Metric, Panel, SummaryCard } from "../components/ui";
import { useI18n } from "../i18n";
import type { ConsoleData } from "../types";

export function DashboardPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  return (
    <>
      <div className="signal-panel">
        <Panel title={t("dashboardAccessTitle")} subtitle={t("dashboardAccessBody")}>
          <div className="access-summary-grid">
            <SummaryCard index="01" title={t("dashboardKeyCardTitle")} body={t("dashboardKeyCardBody")} />
            <SummaryCard index="02" title={t("dashboardPolicyCardTitle")} body={t("dashboardPolicyCardBody")} />
            <SummaryCard index="03" title={t("dashboardTargetCardTitle")} body={t("dashboardTargetCardBody")} />
            <SummaryCard index="04" title={t("dashboardAuditCardTitle")} body={t("dashboardAuditCardBody")} />
          </div>
        </Panel>
        <Panel title={t("dashboardControlTitle")} subtitle={t("dashboardControlBody")}>
          <div className="summary-list">
            <span><strong>{data.keys.length}</strong><small>{t("keys")}</small></span>
            <span><strong>{data.targets.length}</strong><small>{t("services")}</small></span>
            <span><strong>{data.policies.length}</strong><small>{t("commandPolicy")}</small></span>
            <span><strong>{data.auditPage.total}</strong><small>{t("auditRecords")}</small></span>
          </div>
        </Panel>
      </div>
      <div className="metrics">
        <Metric icon={<Server />} label={t("services")} value={data.targets.length} />
        <Metric icon={<Shield />} label={t("policy")} value={data.policies.length} />
        <Metric icon={<Users />} label={t("membersGroups")} value={data.groups.length} />
        <Metric icon={<ListChecks />} label={t("auditRecords")} value={data.auditPage.total} />
      </div>
      <Panel title={t("auditRecentTitle")} subtitle={t("auditRecentBody")}>
        {data.auditPage.logs.length ? <AuditTable logs={data.auditPage.logs.slice(0, 5)} /> : <Empty title={t("auditEmptyTitle")} body={t("auditEmptyBody")} />}
      </Panel>
    </>
  );
}
