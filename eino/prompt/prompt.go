// Package prompt 提供可复用的提示词装配能力。
//
// 把「system prompt 的拼装」从 agent 的业务逻辑中抽离为纯函数，
// 既便于单元测试（不同 RAG / 严格模式 / 技能组合下的输出可预期），
// 也避免 prompt 构造逻辑散落在调用处、各 agent 各写一套导致漂移。
//
// 本包不依赖 agent / rag / skills，只接收已经解析好的字符串参数，
// 保证依赖方向单一（上层依赖本包，本包不反向依赖上层）。
package prompt

import "strings"

// SystemParams 是 BuildSystemPrompt 的入参。
// 所有字段都是「已解析好的纯字符串」，本包不负责任何业务语义判断，
// 业务语义（如回答模式文案、技能提示）由上层在调用前解析完毕。
type SystemParams struct {
	// BasePrompt 基础系统提示（通常来自 agent 配置）。
	BasePrompt string
	// RAGStatus 知识库状态文案；非空时追加到提示末尾之前。
	// 例：「当前本地知识库状态：已初始化，源文件数 N，切片数 M。」
	RAGStatus string
	// RAGContext 检索到的参考资料正文；非空则追加「参考资料：…」段落。
	RAGContext string
	// StrictContextOnly 严格模式：只能依据资料回答。
	StrictContextOnly bool
	// AnswerModePrompt 按回答模式预解析好的提示文案（应自带前导换行）。
	AnswerModePrompt string
	// SkillsPrompt 技能提示文案；非空时追加到提示末尾。
	SkillsPrompt string
}

// BuildSystemPrompt 按固定顺序装配 system prompt：
//
//	BasePrompt
//	+ [RAGStatus]
//	+ [参考资料段落 | 严格模式无资料提示]
//	+ AnswerModePrompt
//	+ [严格模式约束]
//	+ [SkillsPrompt]
//
// 装配顺序与历史内联实现保持一致，确保存量行为不漂移。
func BuildSystemPrompt(p SystemParams) string {
	var b strings.Builder
	b.WriteString(p.BasePrompt)

	if p.RAGStatus != "" {
		b.WriteString("\n\n")
		b.WriteString(p.RAGStatus)
	}

	if p.RAGContext != "" {
		b.WriteString("\n\n参考资料：\n")
		b.WriteString(p.RAGContext)
		b.WriteString("\n\n请优先基于以上资料回答。每条关键结论后尽量标注来源，例如：[文件名 切片 N]。资料不足时先说明资料不足，再补充通用知识，并明确哪些内容来自资料库外。")
	} else if p.StrictContextOnly {
		b.WriteString("\n\n本轮没有检索到可用参考资料。用户要求只基于知识库回答，所以请直接说明资料库中没有找到相关内容，不要使用资料库外知识补充。")
	}

	b.WriteString(p.AnswerModePrompt)

	if p.StrictContextOnly {
		b.WriteString("\n\n严格模式：只能依据本轮参考资料回答；如果参考资料不足，请明确说资料库没有足够信息，不要自由发挥。")
	}

	if p.SkillsPrompt != "" {
		b.WriteString(p.SkillsPrompt)
	}

	return b.String()
}
