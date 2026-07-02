# Nexus3 + SonatypeIQ Skill 设计草稿

**Date:** 2026-05-30
**Decision:** 合并 `examples/mcp-nexus3/` + `examples/mcp-sonatypeiq/` → skill

---

## 背景

Nexus3 企业版集成 SonatypeIQ 后：
- Nexus3 UI 的 Maven 组件详情页显示 `firewall.status: allowed | denied (quarantined)`
- SonatypeIQ UI 显示建议升级版本，或 "无可升级版本"（需管理员 waive）
- Nexus3 REST API swagger 的 `/service/rest/v1/search` **不返回** firewall.status
- Nexus3 开源版 / 个人部署 **没有** SonatypeIQ 集成

因此无法用标准 MCP 工具（纯 swagger 调用）获取 firewall status。必须结合：
1. Nexus3 REST API → 组件版本列表
2. SonatypeIQ Web UI API（非 swagger）→ firewall.status per version
3. SonatypeIQ upgrade suggestions → 推荐版本
4. 合并过滤 → top-5 allowed versions

## 设计决策

### Skill 只返回数据，不修改项目

Flowgent 的确定性 DAG 原则：每一步可审计、可回滚。

```
❌ skill 内部直接改 pom.xml — 黑盒副作用，DAG 不可见
✅ skill 输出 top-5 versions — 父 flow 的后续节点显式修改
```

父 flow 收到结果后的链路：
```
fetch-safe-deps (skill)
  → agent: 分析选择目标版本
  → human approval gate
  → sandbox: 执行修改 pom.xml
  → commit + PR
```

### 脚本设计：单脚本 + subcmd

`scripts/nexus3-iq.sh get-available-versions <groupId> <artifactId> <currentVersion>`

```
get-available-versions:
  1. query-nexus3: GET /service/rest/v1/search?group=...&name=...&sort=version
     → 所有已发布版本列表
  2. query-iq-firewall: POST SonatypeIQ Web UI API
     → firewall.status per version (allow/deny)
  3. query-iq-upgrade: SonatypeIQ upgrade suggestions API
     → 推荐升级版本（可能为空，需 waive）
  4. merge + filter:
     - 排除 firewall.status=deny (quarantined)
     - 排除 snapshots + current version
     - 优先 SonatypeIQ 推荐版本
     - 补充 allowed 的最新版本
     - 返回 top-5
  5. 输出 JSON: {"versions":[{"version":"...","firewall":"allow","recommended":true,"source":"iq"|"nexus3"}]}
```

### 特殊处理

- **Waive**: 如果 SonatypeIQ 显示 "no upgrade available"（所有版本都被隔离），返回空列表 + 警告
- **只返回 allowed 版本**: 企业环境 IQ 隔离的组件不能使用
- **单人部署 fallback**: 无 IQ license → firewall.status 全部 unknown → 全部返回（标记 warning）

## 执行计划

1. 删除 `examples/mcp-nexus3/`
2. 删除 `examples/mcp-sonatypeiq/`
3. 更新 `examples/skills/nexus3-retrieval/`
   - 重写 `scripts/` → `nexus3-iq.sh get-available-versions`
   - 更新 `skill.yaml` — 使用单个 sandbox node 调用脚本
   - 更新 `SKILL.md` — 文档
4. 更新 `Makefile` — 移除 sonatypeiq/nexus3 MCP 构建目标
5. 更新 `etc/flowgent.yaml.sample` — 移除对应 MCP 配置
6. 更新 `docs/` 中相关引用
