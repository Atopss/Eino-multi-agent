# Eino 演进方案（Phase 3 及后续）

> 状态：方案已定稿，分批执行中。
> 定位：本项目目标是"内部使用 / 上架对外"，因此稳定性、多用户隔离、资源可控是底线，而非可选项。

## 背景与原则

- 已交付：阶段一（核心功能）、阶段二（底层能力标准化：可观测性 / RAG 抽象 / Prompt 模板 / Memory 组件）。
- 本方案聚焦 **Phase 3（收尾加固）**，并附 **上架路线图**（本轮不做）。
- 执行方式：**分批实施，每批做完自测（build / vet / test）并向用户报告，再决定是否继续下一批**。

---

## 批次 1 · 资源防泄漏（纯后端自动，零新依赖，无前端改动）

**目的**：多用户场景下，上下文无限膨胀与会话锁只增不减会被放大成"跑几天就卡 / 内存涨 / API 烧钱"。这两个改动不需要任何人工操作即生效。

- `eino/config/config.go`
  - `RuntimeConfig` 新增字段 `MaxSessionHistory int`（默认 60，约 30 轮对话）。
  - `applyEnvFallbacks` 补默认值与 `MAX_SESSION_HISTORY` 环境变量覆盖。
- `eino/server/chat_handlers.go`
  - `loadSessionHistory`：读历史后若 `len(hist) > MaxSessionHistory`，截断到最近 N 条 → 防上下文无限膨胀、防 API 成本暴涨、防响应变慢。
- `eino/server/server.go`
  - `chatLocks`（当前 `sync.Map`，只 `LoadOrStore` 从不删）增加"超阈值懒清理"：条目超过上限（如 4096）时，比对 `s.sessions` 活跃列表，删除已不存在的孤儿锁 → 彻底堵住锁泄漏，不依赖任何前端操作。

**批次 1 预期**：长时间 / 多人运行下内存平稳、响应不退化。

---

## 批次 2 · 会话删除补全锁清理（后端端点已存在）

> 经代码核查：后端 `handleSessionDelete`（`server.go:2602`）**已实现**，通过 `sessionKey` 复用归属前缀调用 `s.sessions.DeleteSession(key)`，并已由路由注册、走鉴权中间件。`routing.go` 中对应路由为 `/api/session/delete`。

- 唯一缺口：删除时**未清理 `s.chatLocks.Delete(key)`**，导致锁仍残留在 `sync.Map` 中。
- 改动：`eino/server/server.go` 的 `handleSessionDelete` 在 `DeleteSession` 后补 `s.chatLocks.Delete(key)`。

**批次 2 预期**：会话删除与锁回收彻底闭环，孤儿锁清理（批次 1）也能借删除事件即时释放。

---

## 批次 3 · 配置热加载（已实现，**零新依赖**）

> 经取舍：不引入第三方库 `fsnotify`，改用 Go 标准库内置的**定时轮询文件 mtime** 方案——效果一致（改配置免重启），且无外部依赖、维护成本更低。

- `eino/server/server.go`
  - `Server` 新增 `stopCh chan struct{}`（优雅关闭轮询 goroutine，防长期运行泄漏）。
  - 新增 `startConfigWatcher()`：`New()` 收尾时启动，每 3s 轮询 `config.json` / `agents.json` 的修改时间；任一变更即去抖（静置 500ms 再确认一次，过滤原子写入/临时文件抖动）后，在后台调用已有的 `rebuild()`（自带 `RWMutex` 保护，并发安全）。
  - 新增 `Stop()`：关闭 `stopCh` 并 `s.srv.Close()`，供进程退出时优雅收尾。
- 行为对齐网页"保存设置"（同样走 `rebuild()`），因此保存配置时 watcher 不会与之冲突（幂等）。

**批次 3 预期**：改 `config.json` / `agents.json` 后免重启自动生效。

---

## 上架路线图（分批执行中）

现状盘点：账号底层（`UserStore`/`users` 表/`LoginHandler`/JWT 签发）、限流（`RateLimiter` 令牌桶）其实**已写好但未接通**；审计、HTTPS、备份**完全缺失**。支点只有一个——让 `AuthMiddleware` 真正校验 token（A1 已完成）。

### 阶段 A · 账号真正接通
- **A1 ✅ 双模式鉴权 + 登录端点**
  - `AuthMiddleware(mode, secret, next)`：`local`（默认，注入固定匿名用户，向后兼容）/ `jwt`（校验 `Authorization: Bearer`，失败 401 并注入 claims 用户）。
  - `config` 新增 `AuthMode`（env `AUTH_MODE`，默认 `local`）、`TokenTTLHours`（env `TOKEN_TTL_HOURS`，默认 24）。
  - `initDB` 启动 `EnsureAdmin()` 引导初始管理员；`AUTH_MODE=jwt` 但缺 `JWT_SECRET` 时自动回退 local 并告警。
  - 路由注册公开 `/api/auth/login`（按 IP 限流防爆破），所有受保护/管理员端点改走双模式中间件。
- **A2 后端 ✅**：注册端点 `/api/auth/register`（管理员专属，避免公开注册被滥用）+ `adminOnly` 叠加 `AdminGuard`（jwt 模式校验 `is_admin`，local 模式放行）。
- **A2 前端 ✅**：`web/src/views/Login.vue` 登录/注册页（沿用项目 design token）；`api/client.ts` 增加 token 持久化、所有请求带 `Authorization`、非鉴权接口 401 自动跳 `/#/login`；`router` 增加 `/login` 路由。

### 阶段 B · 配额（依赖 A 的 user_id）✅
- **与 RPS 限流解耦**：RPS 限流（`RateLimiter` 令牌桶）防突发洪峰、按 IP/用户维度、秒级；配额控制"单用户一天的总用量"，防被他人/脚本刷爆成本。
- **`db.go`**：新增 `quota_usage` 表（`user_id` + `day` 自然日主键，`requests`/`tokens` 累加，幂等建表）。
- **`config.go`**：新增 `QuotaDailyRequests`（默认 500，env `QUOTA_DAILY_REQUESTS`）、`QuotaDailyTokens`（默认 200000，env `QUOTA_DAILY_TOKENS`）。
- **`server/quota.go`**：`QuotaStore`（原子 `INSERT...ON CONFLICT DO UPDATE` 累加）、`QuotaMiddleware`（开工前预检，普通 jwt 用户当日用尽即 `429 daily quota exceeded`）、`recordUsage`（chat 成功后按 `输入+输出` 字节 `/4` 估算 Token 累加）、`/api/quota` 查询端点。
- **豁免规则**：`local` 模式（固定匿名用户）与 `is_admin` 管理员不受配额约束，便于运维。
- **接入点**：`/api/chat` 与 `/api/chat/stream` 套 `QuotaMiddleware`；四个 chat 成功路径（`handleChat` 编排/多模态/单智能体 + `handleChatStream` 编排/单智能体）在生成完成后调用 `recordUsage`。
- **Token 估算说明**：以输入消息 + 附件(base64) + 输出 `result.Reply` 的字节数 `/4` 近似（中文会偏保守、英文接近真实）。作为配额上限足够用；后续如需精确可接入 tokenizer。

### 阶段 C · 操作审计日志
- `audit_log` 表（user_id / action / target / detail / ip / ts）+ 中间件/关键 handler 记录登录、RAG 上传扫描、agent 增删、删会话、改设置、批权限。

### 阶段 D · 传输安全
- 可选 `ListenAndServeTLS` + 证书 env；API Key 落库加密。

### 阶段 E · 数据备份与恢复
- SQLite 定时备份（保留 N 份）+ 恢复脚本。

---

## 进度

| 批次 | 内容 | 状态 |
|------|------|------|
| 批次 1 | 资源防泄漏（历史截断 + 孤儿锁清理） | ✅ 已完成（build/vet/config 测试通过） |
| 批次 2 | 会话删除补全锁清理（handleSessionDelete 补 chatLocks.Delete） | ✅ 已完成（build/vet 通过） |
| 批次 3 | 配置热加载（轮询 mtime，零新依赖） | ✅ 已完成（build/vet 通过） |
| 上架 A1 | 双模式鉴权（local/jwt）+ 登录端点接通 | ✅ 已完成（build/vet 通过） |
| 上架 A2 后端 | 注册端点 + 管理员 AdminGuard 区分 | ✅ 已完成（build/vet 通过） |
| 上架 A2 前端 | Vue 登录页 + Token 存储/携带 | ✅ 已完成（vue-tsc 0 报错） |
| 上架 B | 配额（quota 表 + 每日请求/Token 限流，与 RPS 解耦） | ✅ 已完成（go build 通过） |
| 上架 C | 审计日志 | 待执行 |
| 上架 D | 传输安全 HTTPS | 待执行 |
| 上架 E | 备份恢复 | 待执行 |
