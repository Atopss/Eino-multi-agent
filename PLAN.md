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

## 上架路线图（本轮不做，仅展示目标）

既然定位是"给别人用"，Phase 3 只是地基。后续真正上架前还需：

1. **用户账号体系**：注册 / 登录、多租户隔离。
2. **配额与限流**：按用户 / 按 Key 限额，防单用户拖垮全局。
3. **操作审计日志**：谁、何时、调了什么模型 / 工具 / 知识库。
4. **传输安全**：HTTPS、敏感数据加密。
5. **数据备份与恢复**：SQLite 持久化数据的定期备份。

---

## 进度

| 批次 | 内容 | 状态 |
|------|------|------|
| 批次 1 | 资源防泄漏（历史截断 + 孤儿锁清理） | ✅ 已完成（build/vet/config 测试通过） |
| 批次 2 | 会话删除补全锁清理（handleSessionDelete 补 chatLocks.Delete） | ✅ 已完成（build/vet 通过） |
| 批次 3 | 配置热加载（轮询 mtime，零新依赖） | ✅ 已完成（build/vet 通过） |
| 路线图 | 上架五件套 | 本轮不做 |
