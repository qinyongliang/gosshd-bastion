# HTTP/边缘转发架构审查与边界

## 依赖方向

- `internal/protocol/types`：仅 Go 标准库 DTO；HTTP 配置、日志事件、统计与网络诊断的序列化结构。不依赖 server、agent、store、SQLite、WebRTC 或 yamux。
- protocol 与 store 只引用上述 DTO，不再为字段定义导入整个转发引擎。
- forward：匹配、HTTP/TCP 数据面、统计/日志后端；Runtime 只依赖 EventRecorder.Record(Event) 和 DialFunc，不持有 App、Repository 或 AgentRegistry。
- tunnel：已有可靠通道与 WebRTC；网络诊断使用同一 DTO。断服续行是明确 opt-in，不改变旧文件传输/临时隧道生命周期。
- server：授权与期望状态编排；按 forward_plan / forward_sync / forward_links / forward_local / forward_status 分开职责。Manager 只协调状态与生命周期，不解析请求路径、不执行逐请求 SQLite 写入。
- agent：接收已授权且固定目标的部署，持久化规则并驱动共享 Runtime；不允许入口通过 peer 传任意地址。
- Web API 与 MCP 继续调用同一管理服务；未复制第二套权限或配置验证。

## 本次审查的实质调整

1. 移除协议层和配置存储层对完整 forward/tunnel 运行时的依赖；增加静态依赖边界测试。
2. EventRecorder 窄接口替代 Runtime 对 SQLite Logs 具体类型的绑定，便于测试和替换后端。
3. 将约 750 行的混合编排文件分解成上述职责文件，保持既有接口和序列化字段不变，不引入插件框架、复杂基类或循环依赖。
4. 路由规则在初始化时预编译，HTTP Transport、SSH client、边缘多路复用链路和复制缓冲复用；I/O 热路径不访问配置仓库。
5. 访问明细与流量聚合的持久化/确认独立；明细清理不会删累计流量。旧 TCP 配置仍兼容，显式 tcp/http 才进入边缘管理器。

## 审查范围与限制

没有重构无关的 SFTP、认证、终端或旧隧道实现。统计边界、Agent 断服和并发行为有回归/race/真实进程网络测试；性能结果不是生产环境容量承诺。没有用新的通用抽象掩盖现有双管理器的兼容迁移——旧/新数据面划分由明确协议配置决定。
