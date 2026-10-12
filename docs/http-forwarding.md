# TCP / HTTP 边缘转发

## 管理与配置

隧道管理提供 TCP 和 HTTP 两个标签，新建时可切换类型。HTTP 配置由一个入口监听器和多个 server/location 块组成；块可命名、增加、删除和排序。

- 每条配置独占入口机器的一个 IP/端口。需要共享端口的 Host 放在同一配置的不同 server 块内。
- server_name 支持一个/多个精确域名、*.example.com 左侧通配符；_ 或 * 是显式默认站点。未匹配 Host 且没有默认站点时返回 404。
- location 支持普通最长前缀、= 精确、^~ 优先前缀、~ 正则和 ~* 忽略大小写正则。
- 匹配顺序：精确 → 最长前缀是 ^~ 时直接采用 → 按配置顺序第一个匹配正则 → 最长普通前缀。
- 正则使用 Go RE2，不是 Nginx 的完整 PCRE；不支持反向引用、前后查找，也不导入任意 Nginx 配置/指令。
- 每个 location 独立配置出口 Agent/SSH 节点、目标 IP/域名/端口、HTTP/HTTPS 上游、去掉前缀、保留 Host、静态 proxy_set_header。
- 出口的 127.0.0.1 表示出口节点自身，其他地址从出口节点的网络访问。
- 自动设置可信 X-Forwarded-For / X-Forwarded-Host / X-Forwarded-Proto / X-Real-IP，丢弃客户端伪造的转发链。
- HTTP 入口不提供 TLS 证书/HTTPS 监听配置；HTTPS 上游验证证书。HTTP 流式响应与 WebSocket 升级使用共享代理运行时。

示例（POST /api/tunnels，组织管理员；空机器 ID 仅系统管理员可用）：

~~~json
{
  "organization_id": "组织 ID",
  "protocol": "http",
  "name": "业务入口",
  "entry_target_id": "入口 Agent / SSH 目标 ID",
  "listen_host": "0.0.0.0",
  "listen_port": 8080,
  "duration_seconds": 0,
  "http": {
    "servers": [{
      "id": "business",
      "name": "业务站点",
      "hosts": ["app.example.com", "alias.example.com"],
      "locations": [{
        "id": "api",
        "name": "API",
        "match": "^~",
        "path": "/api/",
        "exit_target_id": "出口 Agent / SSH 目标 ID",
        "scheme": "http",
        "host": "10.0.0.20",
        "port": 9000,
        "strip_prefix": true,
        "preserve_host": false,
        "headers": {"X-Service": "business"}
      }]
    }]
  }
}
~~~

新配置默认停止，调用启用操作后部署。修改已启用配置保留绝对到期时间、按 revision 更新并关闭旧连接。旧配置未设置 protocol 时保留原 TCP 实现；在界面编辑保存为 TCP，或通过 API/MCP 明确设置 protocol=tcp，才迁移到新的边缘实现。已有程序需升级服务器和参与转发的 Agent，旧 Agent 不支持新的同步协议。

## 执行位置与断服边界

- Agent 入口（包括经 Agent 访问的 SSH 入口）执行监听、HTTP 路由、连接池、代理和异步日志。
- 同一 Agent 的本机/网络服务或其 SSH 链路直接在该 Agent 执行，不经过服务器数据面。
- 不同 Agent 之间，为配置中的固定出口建立持久可多路复用通道，复用原有 WebRTC 直连协商与可靠通道；出口只接受服务器授权的 location ID，不能由入口传入任意目标地址。
- 已确认直连的多路复用通道在服务器会话断开后继续工作，新的 HTTP/TCP 请求仍可开新的出口流，而不只是保持原有连接。
- 服务器/纯 SSH 入口，或没有 Agent 承载的出口，由服务器执行必要部分。
- 单个配置的 HTTP 路由预编译；Host/精确路径有索引，普通前缀按长度排序。每个 location 复用 HTTP Transport/上游连接，SSH 跳转链复用 SSH client，复制缓冲池为 32 KiB。
- 相同 revision 不重启 Agent 监听器；周期同步只发送变更配置、状态、确认与日志/统计批次。
- Agent 缓存配置使用本地 AES-GCM 加密，密钥保存在其专属目录；普通 Agent 启动时加载未到期规则。监听器的生命周期独立于服务器控制流。

**不能保证的情况：** 尚未建立直连、直连已经丢失、或出口必须使用服务器中转时，服务器中断不能继续建立该出口连接。没有信令服务器也无法重新协商一个全新的跨 Agent 直连。Agent 进程重启会丢失旧直连，但本地/委派 SSH 规则可从缓存恢复，跨 Agent 通道等待信令恢复。

停止、修改、权限撤销在离线 Agent 上只能等其重新连接后应用；限时配置使用已经下发的绝对到期时间自主停止，不因重连续期。应保持节点时钟同步。控制面超过 15 秒未收到状态时显示“离线·状态待确认”，不把最后报告的直连状态当作已验证的当前运行状态。

## 日志、统计与保留

服务器数据目录下的 forward-logs/<配置 UUID>.sqlite 保存对应配置的数据；原主库和原审计库不存放新转发的访问明细。Agent 在身份文件旁的 forwarding/<服务器与 Agent 身份哈希>/logs/ 下保存独立日志与待确认队列；嵌入式 Agent 使用自己的工作目录。

访问明细包含来源 IP、Method、Host、请求 URI（包含 query）、server/location ID、上游、响应 code、实际转发载荷上下行字节、耗时、全局/来源并发、直接/中转载荷和错误。HTTP/SSE/WebSocket 在请求结束、TCP 在连接结束后形成明细；在途字节和当前并发另行报告。字节统计主要是转发载荷，不是网卡层全部协议开销。

- WAL SQLite、250ms/256 条批次、4096 个槽位的非阻塞队列；请求线程不等待 SQLite。
- 极端过载可丢弃访问明细，并暴露 dropped/write_failures。紧凑流量计数保留在内存并由后台重试；持续磁盘故障导致未落盘数据仍有进程终止风险。
- Agent 定期（约 5 秒）批量上报，积压时连续排空多个批次。服务器按 event UUID 去重、提交后确认；Agent 确认游标持久化，重连可重放未确认记录。
- 入口迁移后，原 Agent 的已授权配置历史仍可补报，不覆盖新入口的运行状态。
- **访问明细** 全局默认保留 7 天，可在系统管理中设为 1–365 天，传播到 Agent；后台分批删除并回收空闲页，停止配置的历史文件也会维护。
- **累计流量/请求数/峰值与五分钟流量桶** 独立保存，不按访问日志保留期删除。Agent 累计统计和增量流量桶独立上报；即使离线超过访问明细保留期，统计仍能恢复。
- 访问明细查询和“保留期内来源”只反映仍保留的明细；历史来源/流量趋势使用独立流量桶。

请求 URI 可能包含敏感 query 参数，应在应用侧避免把密码/长期令牌放在 URL，并合理设置日志保留时间和组织管理员访问权限。

## API 与 MCP

原隧道列表/创建/更新/启用/停止/删除接口及对应 MCP 工具接受 protocol 与 http 配置，共用组织、跳转链、系统管理员权限和校验逻辑。

- GET /api/tunnels/{id}/logs：组织管理员，before 序号分页；limit 默认 100、最大 500；source_ip/path/route_id/status/from/to 筛选。日志时间筛选用 Unix 毫秒。
- GET /api/tunnels/{id}/traffic：复用原统计接口，时间筛选用 Unix 秒、五分钟桶；新转发从独立配置库读取。
- GET/PUT /api/admin/settings/forward：系统管理员，retention_days。
- MCP tunnel_create / tunnel_update / tunnel_list / tunnel_get / tunnel_enable / tunnel_stop / tunnel_delete / tunnel_traffic 支持新类型。
- MCP forward_logs 查询访问明细、来源、累计统计、在途并发、上报时间、日志写入健康；forward_log_settings_get / forward_log_settings_update 管理全局保留时长。
- 新工具纳入原 tunnel 工具组；每次调用刷新用户状态，日志仍受对应组织管理员权限约束，全局设置仅系统管理员。

## 验证

单元/集成用例覆盖 Host/路径优先级、非法规则、代理改写/可信转发头、独立数据库、明细保留与统计分离、去重/确认/长断服统计补报、队列非阻塞和过载计数、加密缓存恢复、自主到期、Web/MCP 权限、服务器与 Agent 入口、委派 SSH、已建立直连断服后的新 HTTP/TCP 连接。浏览器用例覆盖中英文、桌面/手机、多块添加与排序、真实请求和日志界面。


### 最终验收与发布（2026-10-12）

- TCP/HTTP/委派 SSH 的已建立直连断服后新连接、WebSocket 升级、日志与统计用例通过；Linux `go test ./internal/...` 全套通过。
- 浏览器：中文/英文、1440 桌面/390 手机、多块排序、真实 Host 代理与日志查询通过；`pnpm check` / `pnpm build` 通过，保留原有 Monaco 大 chunk 提示。
- 初始 Windows 全套运行存在两个已记录的基线失败：ConPTY 测试期望英文 ping 回复、批量复制/移动测试返回 502；不将跳过用例的运行描述为无条件全套通过。
- Windows 本机 CGO=0/无 GCC 的限制已通过 WSL2/Linux GCC 验证补足：forward / tunnel / agent / server 的针对性 race 测试通过，Release 流水线的 race 与浏览器验收也通过。
- 真实 server/entry Agent/exit Agent 进程在隔离 Linux 网络命名空间中完成 4096/4096 请求（最高 128 并发），验证直连→UDP 故障/中转→直连恢复→服务器中断→恢复补报，流量与访问明细逐项核对，dropped/write_failures 均为 0。该结果不是公网/NAT 长期生产容量承诺；详见 补充验收（内部验证记录仅保留在本地）。
- `v0.1.167-bastion` 已发布并部署到 amd64 主站与 ARM64 站点；健康、版本、原配置/审计数据、SQLite integrity、前端资源与三个常用平台的 Agent 下载 SHA256 均已核验。
- 本次仅升级服务器和预热 Agent 下载缓存，未创建生产 HTTP 转发规则，也未逐台验收现有 Agent 升级状态；使用新边缘协议的 Agent 仍须运行支持该协议的版本。
- 完整发布、备份与收尾记录见 [v167 发布部署记录](superpowers/plans/2026-10-12-v167-release-deployment.md)，架构边界见 [架构审查](http-forward-architecture.md)。
