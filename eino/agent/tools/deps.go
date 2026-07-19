package tools

import (
	"eino/toolutil"
	"eino/trace"
)

// 本文件把工具实现所依赖的、原属 agent 包的符号，以「类型别名 / 函数变量」
// 形式在 tools 子包内保持原名可用。这样从 agent 包迁入的工具函数体无需任何改动，
// 同时确立了干净的依赖方向：tools 只依赖低层 eino/trace 与 eino/toolutil，
// 绝不反向依赖 agent，从编译层面杜绝工具触碰 Agent 内部状态。

// ExecutionTraceItem 执行轨迹项（与 trace 包同一类型）。
type ExecutionTraceItem = trace.ExecutionTraceItem

// appendTraceItem 追加一条执行轨迹并按需实时流出。
var appendTraceItem = trace.AppendTraceItem

// truncateRunes / limitString 通用字符串截断 helper（与 toolutil 同一实现）。
var (
	truncateRunes = toolutil.TruncateRunes
	limitString   = toolutil.LimitString
)
