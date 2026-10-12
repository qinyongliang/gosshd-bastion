import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Input, InputNumber, Select, Table, Tabs, Tag } from "antd";
import { RefreshCw } from "lucide-react";
import { useState } from "react";
import { request } from "../api";
import { useI18n } from "../i18n";
import type { Tunnel } from "../types";
import type { ForwardEvent, ForwardLogsResponse } from "../forwardTypes";
import { ErrorMessage, Modal } from "./ui";
import { TunnelTrafficDialog, formatBytes } from "./TunnelTraffic";
import "./forwarding.css";
export function ForwardRetentionSettings(){
 const {locale}=useI18n();const zh=locale==="zh-CN";const qc=useQueryClient();const [days,setDays]=useState<number|null>(null);
 const query=useQuery({queryKey:["forward-settings"],queryFn:()=>request<{retention_days:number}>("/api/admin/settings/forward")});
 const save=useMutation({mutationFn:()=>request("/api/admin/settings/forward",{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify({retention_days:days??query.data?.retention_days??7})}),onSuccess:()=>qc.invalidateQueries({queryKey:["forward-settings"]})});
 return <div><div className="forward-retention"><strong>{zh?"转发访问日志保留":"Forward access-log retention"}</strong><InputNumber aria-label={zh?"日志保留天数":"Log retention days"} min={1} max={365} value={days??query.data?.retention_days??7} onChange={setDays}/><span>{zh?"天":"days"}</span><Button onClick={()=>save.mutate()} loading={save.isPending} disabled={!query.data}>{zh?"保存全局配置":"Save global setting"}</Button><p className="forward-help">{zh?"默认 7 天，同步至所有 Agent；不删除累计流量和请求统计。":"Default 7 days, propagated to all Agents. Lifetime traffic/request totals are not deleted."}</p></div><ErrorMessage error={query.error||save.error}/></div>;
}
export function ForwardLogsDialog({tunnel,onClose}:{tunnel:Tunnel;onClose:()=>void}){
 const {locale}=useI18n();const zh=locale==="zh-CN";const tx=(cn:string,en:string)=>zh?cn:en;
 const [showTraffic,setShowTraffic]=useState(false);
 const [source,setSource]=useState("");const [path,setPath]=useState("");const [route,setRoute]=useState("");const [status,setStatus]=useState<number|null>(null);const [before,setBefore]=useState(0);
 const params=new URLSearchParams({source_ip:source,path,route_id:route,limit:"100",before:String(before)});if(status)params.set("status",String(status));
 const query=useQuery({queryKey:["forward-logs",tunnel.id,params.toString()],queryFn:()=>request<ForwardLogsResponse>("/api/tunnels/"+tunnel.id+"/logs?"+params.toString()),refetchInterval:before?false:5000});
 const data=query.data;const locations=tunnel.http?.servers.flatMap(s=>s.locations)||[];
 const routeName=(id:string)=>tunnel.protocol === "port" ? id.toUpperCase() : locations.find(l=>l.id===id)?.name||locations.find(l=>l.id===id)?.path||id||"—";
 const update=(fn:()=>void)=>{setBefore(0);fn();};
 const columns=[
  {title:tx("时间","Time"),dataIndex:"time",width:170,render:(v:number)=>new Date(v).toLocaleString(locale)},
  {title:tx("来源 IP","Source IP"),dataIndex:"source_ip",width:130},
  {title:"Method / Host / Path",key:"request",width:350,render:(_:unknown,e:ForwardEvent)=><div className="forward-log-path"><Tag>{e.method}</Tag>{e.host}<div>{e.path||"—"}</div></div>},
  {title:tx("匹配路由 / 出口","Matched route / upstream"),key:"route",width:250,render:(_:unknown,e:ForwardEvent)=><div className="forward-log-path"><strong>{routeName(e.route_id)}</strong><div>{e.upstream}</div><Tag>{(e.direct_up+e.direct_down)>0 ? ((e.direct_up+e.direct_down)===(e.up+e.down)?tx("直连","Direct"):tx("混合路径","Mixed")) : (e.direct?tx("直连","Direct"):tx("中转 / 本地","Relay / local"))}</Tag></div>},
  {title:"Code",dataIndex:"status",width:80,render:(v:number,e:ForwardEvent)=><Tag color={v>=500?"red":v>=400?"orange":"green"}>{v||e.method}</Tag>},
  {title:tx("上传 / 下载","Upload / download"),key:"traffic",width:150,render:(_:unknown,e:ForwardEvent)=>formatBytes(e.up)+" / "+formatBytes(e.down)},
  {title:tx("并发 / 耗时","Concurrent / duration"),key:"duration",width:150,render:(_:unknown,e:ForwardEvent)=>e.concurrent+" (IP: "+e.source_concurrent+") / "+e.duration_ms+" ms"},
  {title:tx("错误","Error"),dataIndex:"error",width:180},
 ];
 return <><Modal title={tunnel.name+" · "+tx("转发访问日志","Forward access logs")} wide onClose={onClose}>
  <p className="forward-help">{tx("每个配置独立 SQLite，异步批量写入。","One SQLite per configuration; asynchronous batched writes.")} {tx("保留","Retention")}: {data?.retention_days??7} {tx("天","days")}. {data?.last_report&&<>{tx("Agent 最后上报","Last Agent report")}: {new Date(data.last_report).toLocaleString(locale)}</>}</p>
  {data?.last_report&&Date.now()-new Date(data.last_report).getTime()>15000&&<Alert type="warning" showIcon title={tx("Agent 未连接，以下为最后上报状态；边缘直连可能仍在运行。","Agent disconnected. Showing last reported state; direct edge paths may still be running.")}/>}
  {!!((data?.stats.dropped||0)+(data?.stats.write_failures||0)+(data?.edge_stats?.dropped||0)+(data?.edge_stats?.write_failures||0))&&<Alert type="warning" title={tx("日志队列出现丢弃或写入失败，请检查节点磁盘和负载。","Log drops/write failures detected; check node disk and load.")}/>}
  <div className="forward-log-stats">{[[tx("累计请求 / 连接","Lifetime requests / connections"),data?.stats.requests||0],[tx("当前并发","Current concurrency"),data?.active||0],[tx("峰值并发","Peak concurrency"),Math.max(data?.stats.peak||0,data?.live_peak||0)],[tx("累计流量","Lifetime traffic"),formatBytes((data?.stats.up||0)+(data?.stats.down||0))],[tx("5xx / 转发失败","5xx / failures"),data?.stats.errors||0]].map(([label,value])=><div key={String(label)}><small>{label}</small><strong>{value}</strong></div>)}</div>
  <Button onClick={()=>setShowTraffic(true)}>{tx("流量趋势 / 历史来源统计","Traffic trends / historical sources")}</Button>
  <div className="forward-log-filters">
   <Input aria-label={tx("来源 IP 筛选","Filter source IP")} placeholder={tx("来源 IP","Source IP")} value={source} onChange={e=>update(()=>setSource(e.target.value))}/>
   <Input aria-label={tx("请求 Path 筛选","Filter request path")} placeholder={tx("请求 Path","Request path")} value={path} onChange={e=>update(()=>setPath(e.target.value))}/>
   <Select aria-label={tx("路由筛选","Filter route")} value={route} onChange={v=>update(()=>setRoute(v))} options={[{value:"",label:tx("全部路由","All routes")},...(tunnel.protocol === "port" ? (tunnel.transports||[]).map(p=>({value:p,label:p.toUpperCase()})) : locations.map(l=>({value:l.id,label:l.name||l.path})))]}/>
   <InputNumber aria-label="Response code" placeholder="Code" min={100} max={599} value={status} onChange={v=>update(()=>setStatus(v))}/>
   <Button icon={<RefreshCw size={15}/>} onClick={()=>void query.refetch()}>{tx("刷新","Refresh")}</Button>
  </div>
  <ErrorMessage error={query.error}/>
  <Tabs items={[{key:"requests",label:tx("请求日志","Requests"),children:<><Table size="small" rowKey="id" dataSource={data?.events||[]} columns={columns} loading={query.isLoading} pagination={false} scroll={{x:1550}}/><div className="form-actions"><Button disabled={!before} onClick={()=>setBefore(0)}>{tx("返回最新","Latest")}</Button><Button disabled={!data?.next_before||data.events.length<100} onClick={()=>setBefore(data?.next_before||0)}>{tx("更早记录","Older records")}</Button></div></>},{key:"sources",label:tx("来源统计（保留期内）","Sources (retained window)"),children:<Table size="small" rowKey="source_ip" dataSource={data?.sources||[]} pagination={{pageSize:20}} columns={[{title:"IP",dataIndex:"source_ip"},{title:tx("请求","Requests"),dataIndex:"requests"},{title:tx("上传","Upload"),dataIndex:"up",render:formatBytes},{title:tx("下载","Download"),dataIndex:"down",render:formatBytes},{title:tx("当前并发","Active"),dataIndex:"active"},{title:tx("峰值并发","Peak concurrency"),dataIndex:"peak"},{title:"5xx",dataIndex:"errors"}]}/>}]}/>
 </Modal>{showTraffic&&<TunnelTrafficDialog tunnel={tunnel} onClose={()=>setShowTraffic(false)}/>}</>;
}
