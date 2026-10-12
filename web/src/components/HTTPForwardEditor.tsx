import { Button, Card, Input, InputNumber, Select, Switch, Tag } from "antd";
import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { MachinePicker } from "./MachinePicker";
import { useI18n } from "../i18n";
import type { ConsoleData, HTTPForwardConfig, HTTPForwardLocation, HTTPForwardServer, TunnelConfig } from "../types";
import "./forwarding.css";
const id = () => crypto.randomUUID();
export const newHTTPLocation = (exit: string): HTTPForwardLocation => ({id:id(),name:"",match:"prefix",path:"/",exit_target_id:exit,scheme:"http",host:"127.0.0.1",port:80,strip_prefix:false,preserve_host:false});
export const newHTTPServer = (exit:string):HTTPForwardServer => ({id:id(),name:"",hosts:["_"],locations:[newHTTPLocation(exit)]});
function HostInput({hosts,onChange,label}:{hosts:string[];onChange:(v:string[])=>void;label:string}) {
 const [value,setValue]=useState(hosts.join(", "));
 return <Input aria-label={label} value={value} onChange={e=>{setValue(e.target.value);onChange(e.target.value.split(/[,\s]+/).filter(Boolean));}} placeholder="app.example.com, *.example.org"/>;
}
export function HTTPForwardEditor({data,config,onChange}:{data:ConsoleData;config:TunnelConfig;onChange:(patch:Partial<TunnelConfig>)=>void}) {
 const {locale}=useI18n(); const zh=locale==="zh-CN";const text=(cn:string,en:string)=>zh?cn:en;
 const http=config.http || {servers:[]}; const exit=data.user.is_system_admin?"":data.targets[0]?.id||"__select__";
 const setHTTP=(v:HTTPForwardConfig)=>onChange({http:v});
 const moveServer=(index:number,delta:number)=>{const servers=[...http.servers];[servers[index],servers[index+delta]]=[servers[index+delta],servers[index]];setHTTP({servers});};
 const moveLocation=(index:number,location:number,delta:number)=>{const locations=[...http.servers[index].locations];[locations[location],locations[location+delta]]=[locations[location+delta],locations[location]];updateServer(index,{locations});};
 const updateServer=(i:number,p:Partial<HTTPForwardServer>)=>setHTTP({servers:http.servers.map((s,n)=>n===i?{...s,...p}:s)});
 const updateLocation=(i:number,j:number,p:Partial<HTTPForwardLocation>)=>updateServer(i,{locations:http.servers[i].locations.map((l,n)=>n===j?{...l,...p}:l)});
 const picker=(value:string,label:string,change:(id:string)=>void)=><MachinePicker targets={data.targets} folders={data.targetFolders} currentTargetID={value} label={label} variant="field" additionalOptions={data.user.is_system_admin?[{value:"",label:text("堡垒机","Bastion")}]:[]} onOpenTarget={change}/>;
 return <div className="http-forward-editor">
  <Card size="small" title={text("监听入口 · 所有 server 块共享","Listener · shared by all server blocks")}>
   <div className="forward-grid">
    <label className="field"><span>{text("入口机器","Entry machine")}</span>{picker(config.entry_target_id,text("入口机器","Entry machine"),v=>onChange({entry_target_id:v}))}</label>
    <label className="field"><span>{text("监听 IP","Listen IP")}</span><Input required aria-label="HTTP listen IP" value={config.listen_host} onChange={e=>onChange({listen_host:e.target.value})}/></label>
    <label className="field"><span>{text("监听端口","Listen port")}</span><InputNumber min={1} max={65535} aria-label="HTTP listen port" value={config.listen_port} onChange={v=>onChange({listen_port:v||80})}/></label>
   </div>
   <p className="forward-help">{text("入口优先在 Agent 执行，127.0.0.1 仅本机访问。每条配置独占监听端口；共享端口的多个 Host 请放在此配置的不同 server 块。","Agent entries execute at the edge. One listener per configuration; put hosts sharing a port in separate server blocks here.")}</p>
  </Card>
  {http.servers.map((server,i)=><Card key={server.id} className="forward-server-block" size="small" title={<><Tag color="blue">server {i+1}</Tag>{server.name||text("虚拟主机","Virtual host")}</>} extra={<><Button size="small" aria-label={text("上移 server 块","Move server up")} disabled={i===0} onClick={()=>moveServer(i,-1)}>↑</Button><Button size="small" aria-label={text("下移 server 块","Move server down")} disabled={i===http.servers.length-1} onClick={()=>moveServer(i,1)}>↓</Button><Button danger type="text" aria-label={text("删除 server 块","Remove server block")} icon={<Trash2 size={15}/>} disabled={http.servers.length===1} onClick={()=>setHTTP({servers:http.servers.filter((_,n)=>n!==i)})}/></>}>
   <div className="forward-grid two">
    <label className="field"><span>{text("块名称","Block name")}</span><Input maxLength={128} value={server.name} onChange={e=>updateServer(i,{name:e.target.value})} placeholder={text("例如：业务 API","e.g. Business API")}/></label>
    <label className="field"><span>server_name · {text("多个 Host 用逗号分隔，_ 为默认","comma-separated hosts; _ is default")}</span><HostInput hosts={server.hosts} label="server_name" onChange={hosts=>updateServer(i,{hosts})}/></label>
   </div>
   {server.locations.map((l,j)=><Card key={l.id} className="forward-location-block" size="small" title={<><Tag color="cyan">location {j+1}</Tag>{l.name||l.match+" "+l.path}</>} extra={<><Button size="small" aria-label={text("上移 location 规则","Move location up")} disabled={j===0} onClick={()=>moveLocation(i,j,-1)}>↑</Button><Button size="small" aria-label={text("下移 location 规则","Move location down")} disabled={j===server.locations.length-1} onClick={()=>moveLocation(i,j,1)}>↓</Button><Button danger type="text" aria-label={text("删除 location 块","Remove location block")} icon={<Trash2 size={14}/>} disabled={server.locations.length===1} onClick={()=>updateServer(i,{locations:server.locations.filter((_,n)=>n!==j)})}/></>}>
    <div className="forward-grid">
     <label className="field"><span>{text("规则名称","Rule name")}</span><Input value={l.name} maxLength={128} onChange={e=>updateLocation(i,j,{name:e.target.value})}/></label>
     <label className="field"><span>{text("匹配方式","Match modifier")}</span><Select aria-label={text("匹配方式","Match modifier")} value={l.match||"prefix"} options={[
      {value:"prefix",label:text("前缀（最长匹配）","Prefix (longest match)")},{value:"=",label:text("= 精确匹配","= Exact")},{value:"^~",label:text("^~ 优先前缀","^~ Preferred prefix")},{value:"~",label:text("~ 正则","~ Regex")},{value:"~*",label:text("~* 忽略大小写正则","~* Case-insensitive regex")}
     ]} onChange={match=>updateLocation(i,j,{match,strip_prefix:match.startsWith("~")?false:l.strip_prefix})}/></label>
     <label className="field"><span>{text("路径 / 正则","Path / regex")}</span><Input required maxLength={4096} value={l.path} onChange={e=>updateLocation(i,j,{path:e.target.value})}/></label>
    </div>
    <div className="forward-grid upstream">
     <label className="field"><span>{text("出口 Agent / SSH 节点","Exit Agent / SSH node")}</span>{picker(l.exit_target_id,text("出口机器","Exit machine"),v=>updateLocation(i,j,{exit_target_id:v}))}</label>
     <label className="field"><span>{text("协议","Scheme")}</span><Select value={l.scheme} options={[{value:"http",label:"HTTP"},{value:"https",label:"HTTPS"}]} onChange={scheme=>updateLocation(i,j,{scheme})}/></label>
     <label className="field"><span>{text("服务 IP / 域名","Service IP / hostname")}</span><Input required value={l.host} onChange={e=>updateLocation(i,j,{host:e.target.value})}/></label>
     <label className="field"><span>{text("服务端口","Service port")}</span><InputNumber min={1} max={65535} value={l.port} onChange={v=>updateLocation(i,j,{port:v||80})}/></label>
    </div>
    <div className="forward-switches">
     <label><Switch size="small" checked={l.strip_prefix} disabled={l.match.startsWith("~")} onChange={strip_prefix=>updateLocation(i,j,{strip_prefix})}/>{text("去掉匹配前缀","Strip matched prefix")}</label>
     <label><Switch size="small" checked={l.preserve_host} onChange={preserve_host=>updateLocation(i,j,{preserve_host})}/>{text("保留原始 Host","Preserve original Host")}</label>
    </div>
    <div className="forward-headers">
     <strong>proxy_set_header</strong>
     {Object.entries(l.headers||{}).map(([key,value],n)=><div className="forward-header-row" key={n}>
      <Input aria-label={text("请求头名称","Header name")} value={key} placeholder="X-Service" onChange={e=>{const pairs=Object.entries(l.headers||{});pairs[n]=[e.target.value,value];updateLocation(i,j,{headers:Object.fromEntries(pairs)});}}/>
      <Input aria-label={text("请求头值","Header value")} value={value} onChange={e=>updateLocation(i,j,{headers:{...l.headers,[key]:e.target.value}})}/>
      <Button danger type="text" aria-label={text("删除请求头","Remove header")} icon={<Trash2 size={14}/>} onClick={()=>{const headers={...l.headers};delete headers[key];updateLocation(i,j,{headers});}}/>
     </div>)}
     <Button size="small" icon={<Plus size={14}/>} disabled={Object.hasOwn(l.headers||{},"")} onClick={()=>updateLocation(i,j,{headers:{...l.headers,"":""}})}>{text("添加请求头","Add header")}</Button>
    </div>
   </Card>)}
   <Button icon={<Plus size={15}/>} onClick={()=>updateServer(i,{locations:[...server.locations,newHTTPLocation(exit)]})}>{text("添加 location 规则","Add location rule")}</Button>
  </Card>)}
  <Button type="dashed" block icon={<Plus size={16}/>} onClick={()=>setHTTP({servers:[...http.servers,newHTTPServer(exit)]})}>{text("添加 server 块","Add server block")}</Button>
  <p className="forward-help">{text("优先级：精确 → 最长 ^~ 前缀 → 按顺序首个正则 → 最长普通前缀。正则使用 Go RE2，不支持 PCRE 的回溯或前后查找。出口的 127.0.0.1 指出口节点自身。","Priority: exact → longest ^~ prefix → first matching regex → longest prefix. Go RE2, not full PCRE. Upstream 127.0.0.1 refers to the exit node.")}</p>
 </div>;
}
export function HTTPForwardSummary({config}:{config:TunnelConfig}){
 return <div className="forward-summary"><code>{config.listen_host}:{config.listen_port}</code>{config.http?.servers.map(s=><div key={s.id}><Tag color="blue">{s.name||"server"}</Tag><strong>{s.hosts.join(", ")}</strong><div>{s.locations.map(l=><Tag key={l.id}>{l.name||l.match+" "+l.path} → {l.scheme}://{l.host}:{l.port}</Tag>)}</div></div>)}</div>;
}
