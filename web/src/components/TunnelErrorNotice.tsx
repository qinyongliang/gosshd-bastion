import { AlertCircle } from "lucide-react";
import { useI18n } from "../i18n";
import type { Tunnel } from "../types";

const words = {
  "zh-CN": {
    failed: "隧道连接失败",
    listenFailed: "入口监听失败",
    portInUse: "入口监听端口已占用",
    sshDenied: "SSH 远程监听被拒绝",
    machine: "入口机器",
    address: "监听地址",
    occupiedHelp: "请检查并停止占用该端口的旧监听，或修改隧道的入口端口。",
    deniedHelp: "可能原因：端口被占用、SSH 转发权限受限，或监听地址不可用。请检查入口机器的监听端口及 SSH 转发配置。",
    listenHelp: "请检查入口机器是否可连接，以及监听地址和端口是否可用。",
    details: "技术信息",
  },
  en: {
    failed: "Tunnel connection failed",
    listenFailed: "Entry listener failed",
    portInUse: "Entry port is already in use",
    sshDenied: "SSH remote listening was denied",
    machine: "Entry machine",
    address: "Listen address",
    occupiedHelp: "Check and stop the old listener using this port, or change the tunnel's entry port.",
    deniedHelp: "Possible causes: an occupied port, restricted SSH forwarding, or an unavailable bind address. Check the entry machine's listeners and SSH forwarding configuration.",
    listenHelp: "Check that the entry machine is reachable and the listen address and port are available.",
    details: "Technical details",
  },
};

export function TunnelErrorNotice({ tunnel }: { tunnel: Tunnel }) {
  const { locale } = useI18n();
  const w = words[locale];
  if (!tunnel.error) return null;
  const detail = tunnel.error_diagnostic;
  const entry = detail?.stage === "entry_listen";
  const denied = entry && (detail.code === "ssh_forward_denied" || tunnel.error.includes("tcpip-forward request denied by peer"));
  const occupied = entry && detail.code === "port_in_use";
  const title = occupied ? w.portInUse : denied ? w.sshDenied : entry ? w.listenFailed : w.failed;
  const help = occupied ? w.occupiedHelp : denied ? w.deniedHelp : entry ? w.listenHelp : "";
  return (
    <div className="tunnel-runtime-error" role="status">
      <div className="tunnel-error-heading"><AlertCircle size={16} aria-hidden="true" /><strong>{title}</strong></div>
      {entry && <div className="tunnel-error-context">
        <span>{w.machine}: {detail.machine}</span>
        <span>{w.address}: <code>{detail.address}</code></span>
      </div>}
      {help ? <p>{help}</p> : <p>{tunnel.error}</p>}
      {help && <details><summary>{w.details}</summary><code>{tunnel.error}</code></details>}
    </div>
  );
}
