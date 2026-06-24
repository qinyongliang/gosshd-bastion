import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
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
import { LocalTerminalPage } from "./pages/LocalTerminalPage";
import { MembersPage } from "./pages/MembersPage";
import { OrganizationsPage } from "./pages/OrganizationsPage";
import { PoliciesPage } from "./pages/PoliciesPage";
import { SystemAdminPage } from "./pages/SystemAdminPage";
import { TargetsPage } from "./pages/TargetsPage";
import type { ConsoleData, Organization, Runtime, User } from "./types";

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
  const location = useLocation();
  if (!data) return <Fatal error={new Error("No organization available")} />;

  const isConnectPage = /^\/targets\/[^/]+\/connect\/?$/.test(location.pathname);
  const isClientTerminalPage = runtime.client_mode && location.pathname === "/local-terminal";
  const isClientMode = Boolean(data.runtime.client_mode);

  return (
    <>
      {!isConnectPage && !isClientTerminalPage && <ManualReviewPoller data={data} />}
      <Routes>
        <Route path="/targets/:targetID/connect" element={<ConnectPage data={data} />} />
        {isClientMode && <Route path="/local-terminal" element={<LocalTerminalPage />} />}
        <Route path="*" element={isClientMode ? (
          <ClientDesktopFrame>
            <ClientRoutes data={data} />
          </ClientDesktopFrame>
        ) : (
          <Shell data={data}>
            <ManagedRoutes data={data} />
          </Shell>
        )} />
      </Routes>
    </>
  );
}

function ClientDesktopFrame({ children }: { children: ReactNode }) {
  return <main className="client-desktop-content">{children}</main>;
}

function ClientRoutes({ data }: { data: ConsoleData }) {
  return (
    <Routes>
      <Route path="/" element={<Navigate to="/local-terminal" replace />} />
      <Route path="/local-terminal" element={<LocalTerminalPage />} />
      <Route path="/targets" element={<TargetsPage data={data} />} />
      <Route path="/agents" element={<Navigate to="/targets" replace />} />
      <Route path="/policies" element={<PoliciesPage data={data} />} />
      <Route path="/audit" element={<AuditPage data={data} />} />
      <Route path="*" element={<Navigate to="/targets" replace />} />
    </Routes>
  );
}

function ManagedRoutes({ data }: { data: ConsoleData }) {
  return (
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
  );
}
