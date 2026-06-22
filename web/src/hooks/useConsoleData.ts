import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../api";
import type { ConsoleData, Organization, Runtime, User } from "../types";
import { ownerFromOrg } from "../utils";

const activeOrgStorage = "gosshd_active_org";

export function useConsoleData({ user, orgs, runtime }: { user: User; orgs: Organization[]; runtime: Runtime }): ConsoleData | null {
  const [activeOrgID, setActiveOrgIDState] = useState(() => window.localStorage.getItem(activeOrgStorage) || "");
  const activeOrg = orgs.find((org) => org.id === activeOrgID) || orgs[0];
  const owner = ownerFromOrg(activeOrg);
  const queryClient = useQueryClient();
  const keys = useQuery({ queryKey: ["keys"], queryFn: api.keys });
  const targets = useQuery({ queryKey: ["targets", owner], queryFn: () => api.targets(owner!), enabled: Boolean(owner) });
  const members = useQuery({ queryKey: ["members", activeOrg?.id], queryFn: () => api.orgMembers(activeOrg.id), enabled: Boolean(activeOrg) });
  const groups = useQuery({ queryKey: ["groups", activeOrg?.id], queryFn: () => api.groups(activeOrg.id), enabled: Boolean(activeOrg) });
  const policies = useQuery({ queryKey: ["policies", owner], queryFn: () => api.policies(owner!), enabled: Boolean(owner) });
  const llms = useQuery({ queryKey: ["llms", owner], queryFn: () => api.llmConfigs(owner!), enabled: Boolean(owner) });
  const prompts = useQuery({ queryKey: ["prompts", owner], queryFn: () => api.prompts(owner!), enabled: Boolean(owner) });
  const audit = useQuery({
    queryKey: ["audit", activeOrg?.id],
    queryFn: () => api.audit({ organization_id: activeOrg.id, page: 1, page_size: 20 }),
    enabled: Boolean(activeOrg),
  });

  if (!activeOrg) return null;

  return {
    user,
    orgs,
    activeOrg,
    setActiveOrgID(id) {
      window.localStorage.setItem(activeOrgStorage, id);
      setActiveOrgIDState(id);
    },
    runtime,
    keys: keys.data?.keys || [],
    members: members.data?.members || [],
    groups: groups.data?.groups || [],
    targets: targets.data?.targets || [],
    policies: policies.data?.policies || [],
    llms: llms.data?.configs || [],
    prompts: prompts.data?.prompts || [],
    auditPage: { total: audit.data?.total || 0, page: audit.data?.page || 1, page_size: audit.data?.page_size || 20, logs: audit.data?.logs || [] },
    refetchAll: () => void queryClient.invalidateQueries(),
  };
}
