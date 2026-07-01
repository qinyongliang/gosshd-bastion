import { lazy, Suspense } from "react";
import { Fatal, Loading } from "../components/ui";
import type { ConsoleData } from "../types";

const ConnectWorkspace = lazy(() => import("./ConnectPage").then((module) => ({ default: module.ConnectWorkspace })));

export function LocalTerminalPage({ data }: { data: ConsoleData }) {
  const localTarget = data.targets.find((target) => target.id === data.runtime.local_terminal_target_id)
    || data.targets.find((target) => target.target_type === "agent" && target.alias === "local-terminal");

  if (!localTarget) {
    return <Fatal error={new Error("Local terminal target is unavailable")} />;
  }

  return (
    <Suspense fallback={<Loading />}>
      <ConnectWorkspace data={data} target={localTarget} targets={data.targets} />
    </Suspense>
  );
}
