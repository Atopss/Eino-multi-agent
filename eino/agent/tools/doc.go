// Package tools 汇集智能体可调用的「无状态内置工具」实现（天气、时间、计算、
// 联网搜索、网页抓取，以及各类离线/免密钥的转换与查询工具）。
//
// 设计约束：本包只依赖低层的 eino/trace（执行轨迹）与 eino/toolutil（纯函数），
// 不依赖 agent 包，因此这里的工具天然无法访问 Agent 的会话/权限/模型等内部状态。
// 需要访问 Agent 内部状态的工具（如 computer_action）不在本包，保留在 agent 包内。
//
// 对外入口：各 GetXxx() 工厂函数返回 tool.BaseTool，由 agent 包的 GetAllTools 聚合注册。
package tools
