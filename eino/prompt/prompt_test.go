package prompt

import (
	"strings"
	"testing"
)

func TestBuildSystemPrompt_RAGContext(t *testing.T) {
	out := BuildSystemPrompt(SystemParams{
		BasePrompt:       "你是助手。",
		RAGStatus:        "当前本地知识库状态：已初始化，源文件数 3，切片数 12。",
		RAGContext:       "《规范》切片 2：xxx",
		StrictContextOnly: false,
		AnswerModePrompt: "\n\n当前回答模式：学习问答。优先使用本地资料。",
	})
	for _, want := range []string{
		"你是助手。",
		"当前本地知识库状态：已初始化，源文件数 3，切片数 12。",
		"参考资料：",
		"请优先基于以上资料回答",
		"当前回答模式：学习问答。",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少期望片段：%q\n完整输出：\n%s", want, out)
		}
	}
}

func TestBuildSystemPrompt_StrictNoContext(t *testing.T) {
	out := BuildSystemPrompt(SystemParams{
		BasePrompt:        "你是助手。",
		StrictContextOnly: true,
		AnswerModePrompt: "\n\n当前回答模式：严格资料。",
	})
	for _, want := range []string{
		"本轮没有检索到可用参考资料",
		"严格模式：只能依据本轮参考资料回答",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少期望片段：%q\n完整输出：\n%s", want, out)
		}
	}
}

func TestBuildSystemPrompt_Order(t *testing.T) {
	out := BuildSystemPrompt(SystemParams{
		BasePrompt:        "BASE",
		RAGStatus:         "STATUS",
		RAGContext:        "CTX",
		StrictContextOnly: true,
		AnswerModePrompt:  "MODE",
		SkillsPrompt:      "SKILL",
	})
	idxBase := strings.Index(out, "BASE")
	idxStatus := strings.Index(out, "STATUS")
	idxCtx := strings.Index(out, "CTX")
	idxMode := strings.Index(out, "MODE")
	idxStrict := strings.Index(out, "严格模式：只能依据本轮参考资料回答")
	idxSkill := strings.Index(out, "SKILL")
	// 顺序应为：BASE < STATUS < CTX < MODE < 严格模式 < SKILL
	if !(idxBase < idxStatus && idxStatus < idxCtx && idxCtx < idxMode && idxMode < idxStrict && idxStrict < idxSkill) {
		t.Errorf("装配顺序错误：\n%s", out)
	}
}

func TestBuildSystemPrompt_NoSkillsWhenEmpty(t *testing.T) {
	out := BuildSystemPrompt(SystemParams{
		BasePrompt:       "BASE",
		AnswerModePrompt: "MODE",
	})
	if strings.Contains(out, "SKILL") {
		t.Errorf("空 SkillsPrompt 不应出现 SKILL 标记：\n%s", out)
	}
}
