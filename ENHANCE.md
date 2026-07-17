# Eino 增强路线图

> 目标：对标 Cursor/OpenCode 等 AI 编程助手，补齐"Plan→Execute→Diff"闭环体验

---

## P0: Plan-before-execute 模式

### 目标
收到用户任务后，agent 先输出执行计划（Markdown 列表），用户确认后再执行。把"黑盒执行"变成"可控执行"。

### 后端改动
- `RunOptions` 新增 `PlanMode bool` 字段
- `RunStream()` 中当 `PlanMode=true` 时，先用 `a.Generate()` 让 LLM 生成计划（纯文本、不调工具）
- 将计划通过 `StreamEvent{Type: "plan", Delta: 计划文本}` 流出
- 计划末尾附带 `plannedActions`（从计划文本中解析出的步骤列表 JSON）
- 正常流程继续（ReAct 循环执行）

### 前端改动
- `ChatComposer` 左侧底部新增 **Plan 模式** 切换按钮（ClipboardList 图标）
- `ChatMessage` 收到 `type=plan` 事件后渲染 `<PlanCard />`
- `PlanCard` 组件：展示计划步骤列表 + "批准执行"/"取消"
- `chat.ts` store 新增 `plan` 状态处理

### 影响范围
- `eino/agent/types.go` — StreamEvent / RunOptions 加 PlanMode
- `eino/agent/chat.go` — RunStream 加 plan 生成逻辑
- `web/src/types/api.ts` — StreamEvent 加 plan 相关字段
- `web/src/stores/chat.ts` — 处理 plan SSE 事件
- `web/src/components/ChatComposer.vue` — 加 plan 模式按钮
- `web/src/components/ChatMessage.vue` — 渲染 PlanCard
- **NEW** `web/src/components/PlanCard.vue` — 新组件

---

## P0: 侧边栏文件浏览器

### 目标
左侧边栏增加"文件"标签页，展示本地文件系统目录树，双击预览文件内容。

### 前端改动
- `AppSidebar` 顶部新增 `[会话] [文件]` Tab 切换
- `FileBrowser` 组件：
  - 面包屑导航（当前路径）
  - 目录树（利用已有 `/api/browse?path=` 接口）
  - 文件双击 → 展示内容预览（弹窗/内嵌）
  - 文本文件渲染代码高亮
  - 图片文件直接预览

### 影响范围
- `web/src/components/AppSidebar.vue` — 加 Tab 切换
- **NEW** `web/src/components/FileBrowser.vue` — 新组件

---

## P1: 对话内 Diff 视图

### 目标
当 agent 修改文件后，消息气泡中用 diff 视图展示改动（绿色增行 / 红色删行），而不是纯文本代码块。

### 前端改动
- `DiffView` 组件：
  - 解析 unified diff 格式（`---` / `+++` / `@@` / `+` / `-`）
  - 绿色/红色行渲染
  - 折叠不变行的上下文（`@@ ... @@` 标记可折叠）
  - 行号显示
- `ChatMessage` 检测助手回复中的 ` ```diff ... ``` ` 代码块，自动替换为 DiffView

### 影响范围
- **NEW** `web/src/components/DiffView.vue` — 新组件
- `web/src/components/ChatMessage.vue` — 检测 diff 代码块并替换

---

## P1: 终端面板

### 目标
Computer Daemon 执行命令时，展示实时终端输出流（模拟 CLI 体验）。

### 改动
- Computer Daemon `/exec` 端点改为 SSE 流式输出
- 前端 `TerminalPanel` 组件：黑色终端背景 + 绿色等宽字体 + 实时滚动

---

## P2: @文件引用

### 目标
输入框中 `@filename.ts` 自动搜索项目文件并注入上下文。

### 改动
- 输入框监听 `@` 触发文件搜索下拉
- 选中后读取文件内容注入到消息 payload

---

## P2: 内联编辑能力

### 目标
在对话中直接编辑文件（内联编辑器），而非通过完整文件覆盖。

### 改动
- 消息气泡内代码块增加"编辑"按钮
- 点击后在原位展开 Monaco/CodeMirror 编辑器
- 保存时通过 Computer Daemon 写入文件

---

## P3: Qdrant 向量数据库支持

### 时机
等到以下条件满足时任选其一再加：
- 本地文档 > 5000 个、检索开始变慢
- 需要多用户/多机器共享向量索引
- 想提供"用户可选向量库"的高级功能

### 改动
- 封装 `VectorStore` 接口（内存 / Qdrant / Chroma 统一接口）
- 新增 `qdrant/` 包实现 Qdrant gRPC 客户端
- 在设置面板提供"向量库类型"下拉选择

---

## 清理记录

已删除临时文件：
- `temp_resp.txt` ~ `temp_resp4.txt` — 调试响应
- `temp_test.json` — 调试数据
- `eino/eino.exe` — 旧版编译产物
- `*.err.log / *.out.log / *.log` — 运行时日志
- `backend_restart.* / frontend_restart.*` — 重启日志
- `backend_test.log` — 测试日志
