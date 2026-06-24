import { Fatal } from "../components/ui";
import type { ConsoleData } from "../types";
import { ConnectWorkspace } from "./ConnectPage";

export function LocalTerminalPage({ data }: { data: ConsoleData }) {
  const localTarget = data.targets.find((target) => target.id === data.runtime.local_terminal_target_id)
    || data.targets.find((target) => target.target_type === "agent" && target.alias === "local-terminal");

  if (!localTarget) {
    return <Fatal error={new Error("Local terminal target is unavailable")} />;
  }

  return <ConnectWorkspace data={data} target={localTarget} targets={data.targets} />;
}
