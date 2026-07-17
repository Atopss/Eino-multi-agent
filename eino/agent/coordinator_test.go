package agent

import (
	"testing"

	"eino/config"
)

// TestCoordinatorAgent 固化协调者选取逻辑：
//   - 协调者恒为参与列表里第一个真实存在的智能体（即列表首个，跳过不存在的）；
//   - 列表整体为空 / 全部候选缺失时应报错，避免编排拿到 nil 协调者。
func TestCoordinatorAgent(t *testing.T) {
	o := &Orchestrator{} // 方法不依赖 o 内部字段，零值即可

	mk := func(name string) *Agent {
		return &Agent{config: config.AgentConfig{Name: name}}
	}

	t.Run("首个即协调者", func(t *testing.T) {
		agents := map[string]*Agent{
			"A": mk("A"),
			"B": mk("B"),
			"C": mk("C"),
		}
		a, name, err := o.coordinatorAgent(agents, []string{"A", "B", "C"})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if name != "A" {
			t.Fatalf("应选首位 A，实际 %q", name)
		}
		if a != agents["A"] {
			t.Fatal("返回的不是 A 的实例")
		}
	})

	t.Run("首个缺失_跳过选下一个", func(t *testing.T) {
		agents := map[string]*Agent{
			"B": mk("B"),
		}
		a, name, err := o.coordinatorAgent(agents, []string{"ghost", "B"})
		if err != nil || name != "B" {
			t.Fatalf("应跳过缺失的 ghost 选中 B，实际 name=%q err=%v", name, err)
		}
		if a != agents["B"] {
			t.Fatal("返回的不是 B 的实例")
		}
	})

	t.Run("空列表_报错", func(t *testing.T) {
		agents := map[string]*Agent{"A": mk("A")}
		if _, _, err := o.coordinatorAgent(agents, []string{}); err == nil {
			t.Fatal("空列表应报错")
		}
	})

	t.Run("候选整体缺失_报错", func(t *testing.T) {
		agents := map[string]*Agent{"A": mk("A")}
		if _, _, err := o.coordinatorAgent(agents, []string{"ghost"}); err == nil {
			t.Fatal("候选智能体缺失应报错")
		}
	})
}
