import { useQuery } from "@tanstack/react-query";
import { Navigate, Route, Routes } from "react-router-dom";
import { ApiError, api } from "./api";
import { ManualReviewPoller } from "./components/ManualReviewPoller";
import { Fatal, Loading } from "./components/ui";
import { useConsoleData } from "./hooks/useConsoleData";
import { Shell } from "./layout/Shell";
import { AuditPage } from "./pages/AuditPage";
import { AuthPage } from "./pages/AuthPage";
import { ConnectPage } from "./pages/ConnectPage";
import { DashboardPage } from "./pages/DashboardPage";
import { KeysPage } from "./pages/KeysPage";
import { MembersPage } from "./pages/MembersPage";
import { OrganizationsPage } from "./pages/OrganizationsPage";
import { PoliciesPage } from "./pages/PoliciesPage";
import { SystemAdminPage } from "./pages/SystemAdminPage";
import { TargetsPage } from "./pages/TargetsPage";
import type { Organization, Runtime, User } from "./types";

export function App() {
  const providers = useQuery({ queryKey: ["providers"], queryFn: api.authProviders });
  const me = useQuery({ queryKey: ["me"], queryFn: api.me });

  if (me.isLoading || providers.isLoading) return <Loading />;
  if (me.error instanceof ApiError && me.error.status === 401) {
    return <AuthPage dingTalkEnabled={Boolean(providers.data?.dingtalk?.enabled)} registrationEnabled={Boolean(providers.data?.registration_enabled)} />;
  }
  if (me.error) return <Fatal error={me.error} />;
  if (!me.data) return <Loading />;
  return <ConsoleApp user={me.data.user} orgs={me.data.organizations} runtime={me.data.runtime} />;
}

function ConsoleApp({ user, orgs, runtime }: { user: User; orgs: Organization[]; runtime: Runtime }) {
  const data = useConsoleData({ user, orgs, runtime });
  if (!data) return <Fatal error={new Error("No organization available")} />;

  return (
    <>
      <ManualReviewPoller data={data} />
      <Routes>
        <Route path="/targets/:targetID/connect" element={<ConnectPage data={data} />} />
        <Route path="*" element={
          <Shell data={data}>
            <Routes>
              <Route path="/" element={<DashboardPage data={data} />} />
              <Route path="/orgs" element={<OrganizationsPage data={data} />} />
              <Route path="/org-admin" element={<MembersPage data={data} />} />
              <Route path="/keys" element={<KeysPage data={data} />} />
              <Route path="/targets" element={<TargetsPage data={data} />} />
              <Route path="/agents" element={<Navigate to="/targets" replace />} />
              <Route path="/policies" element={<PoliciesPage data={data} />} />
              <Route path="/audit" element={<AuditPage data={data} />} />
              <Route path="/system-admin" element={data.user.is_system_admin ? <SystemAdminPage data={data} /> : <Navigate to="/" replace />} />
            </Routes>
          </Shell>
        } />
      </Routes>
    </>
  );
}
