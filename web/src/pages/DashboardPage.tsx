import { ListChecks, Server, Shield, Users } from "lucide-react";
import { AuditTable, Empty, Metric, Panel, SummaryCard } from "../components/ui";
import { useI18n } from "../i18n";
import type { ConsoleData } from "../types";

export function DashboardPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  return (
    <>
      <DashboardCockpit data={data} />
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

function DashboardCockpit({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const primaryTarget = data.targets[0];
  const secondaryTarget = data.targets[1];
  const recentLog = data.auditPage.logs[0];
  const reviewPolicies = data.policies.filter((policy) => policy.allow_manual_review).length;
  const targetName = primaryTarget?.name || primaryTarget?.alias || t("dashboardCockpitNoTarget");
  const targetEndpoint = primaryTarget?.endpoint || [primaryTarget?.host, primaryTarget?.port].filter(Boolean).join(":") || "-";
  const command = recentLog?.command || "journalctl -u gosshd --since 10m";
  const decision = recentLog?.policy_decision === "deny" ? t("commonDeny") : t("commonAllow");

  return (
    <section className="dashboard-cockpit-panel" aria-label={t("dashboardCockpitTitle")}>
      <div className="dashboard-cockpit-copy">
        <span>{t("dashboardCockpitEyebrow")}</span>
        <h2>{t("dashboardCockpitTitle")}</h2>
        <p>{t("dashboardCockpitBody")}</p>
      </div>
      <div className="dashboard-cockpit">
        <aside className="dashboard-cockpit-rail">
          <strong>{t("services")}</strong>
          <span className="dashboard-server-row active"><i /> <b>{targetName}</b><small>{targetEndpoint}</small></span>
          <span className="dashboard-server-row"><i /> <b>{secondaryTarget?.name || t("dashboardCockpitAgent")}</b><small>{secondaryTarget?.alias || t("dashboardCockpitAgentState")}</small></span>
        </aside>
        <section className="dashboard-terminal-preview">
          <div className="dashboard-terminal-toolbar">
            <span>{targetName}</span>
            <b>{t("dashboardCockpitReviewTimer")}</b>
          </div>
          <pre><code>{`# ${t("dashboardCockpitCommandComment")}
$ ${command}
policy: ${decision}
reason: ${recentLog?.policy_reason || t("dashboardCockpitReason")}
suggestion: ${t("dashboardCockpitSuggestion")}`}</code></pre>
        </section>
        <aside className="dashboard-review-panel">
          <span>{t("dashboardCockpitReviewCard")}</span>
          <strong>{t("dashboardCockpitReviewTitle")}</strong>
          <p>{t("dashboardCockpitReviewBody")}</p>
          <div className="dashboard-review-actions">
            <b>{reviewPolicies}</b>
            <small>{t("dashboardCockpitReviewMetric")}</small>
          </div>
        </aside>
      </div>
    </section>
  );
}
