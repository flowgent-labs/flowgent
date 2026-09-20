# AuthGuard 集成边界

Flowgent 不再实现认证、身份联邦、Session、Role、Policy、API Key 或授权决策；这些
职责全部归 AuthGuard 及其 Envoy Gateway 集成所有。

Flowgent API 仅使用官方 AuthGuard Go adapters SDK 校验网关注入的签名访问上下文，
并按 HTTP 动作调用 `CurrentScopeForAction`，防止 READ 上下文被复用于 UPDATE/DELETE。
Adapter 把
`urn:iam:prod:flowgent:global:<tenant>:namespace/<namespace>/**` Grant 编译为
嵌入官方 `model.SqlScope` 的 `FlowgentSqlScope`，由 Repository 附加到业务查询。每个
Repository 声明自身的实体资源路径，例如 `flows/{agentflow_id}` 或
`runs/{agentflow_run_id}/tasks/{id}`。Adapter 先根据签名请求中的 `resource_urn`
选择匹配映射，再由 SDK 编译 allow/deny Grant，避免共表别名互相越权。读取、更新、删除
均附加该谓词；创建与 upsert 则通过候选行查询或事务校验授权范围。Helm 关闭集成时，
`FlowgentSqlScope` 使用空实现，因此 Flowgent 不会强制依赖 AuthGuard 运行时。

API Service 默认是 `ClusterIP`。公网请求必须经过 AuthGuard 管理的 Gateway 并进入
`:9999` 受保护监听器；缺少、无效、过期、动作不匹配或冲突的 AuthGuard Context
一律失败关闭。Controller、JM、TM、Sandbox 与 Notifier 使用独立的 `:9990` 内部
控制面监听器，其 dummy SDK scope 仅用于维持分布式执行，不实现任何 Flowgent AuthN，
也不得挂载到公网 Gateway。

LDAP Discovery、GitHub Login、Hosted Login、Principal 生命周期、Role、Policy 与
授权审计均在 AuthGuard 中配置和运维。Flowgent Helm Chart 只负责是否部署 AuthGuard
依赖，并提供 Adapter Resource Mapping 与 SDK 验签密钥环境变量。

[English](authguard-integration.md)
