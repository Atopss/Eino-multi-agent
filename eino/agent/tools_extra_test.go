package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func runTool(t *testing.T, tl tool.BaseTool, name string, args string) string {
	t.Helper()
	ctx := context.Background()
	info, err := tl.Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != name {
		t.Fatalf("期望工具名 %s, 实际 %s", name, info.Name)
	}
	it, ok := tl.(tool.InvokableTool)
	if !ok {
		t.Fatalf("%s 不是 InvokableTool", name)
	}
	out, err := it.InvokableRun(ctx, args)
	if err != nil {
		t.Fatalf("%s 执行失败: %v", name, err)
	}
	t.Logf("[%s] -> %s", name, out)
	return out
}

func getTool(t *testing.T, name string) tool.BaseTool {
	t.Helper()
	tools, err := GetAllTools(true)
	if err != nil {
		t.Fatalf("GetAllTools: %v", err)
	}
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		info, _ := tl.Info(context.Background())
		names = append(names, info.Name)
		if info.Name == name {
			return tl
		}
	}
	t.Fatalf("未找到工具 %s; 当前工具: %v", name, names)
	return nil
}

func TestExtraToolsRegistered(t *testing.T) {
	tools, err := GetAllTools(true)
	if err != nil {
		t.Fatalf("GetAllTools: %v", err)
	}
	expected := []string{
		"get_weather", "get_current_time", "calculator", "web_search", "fetch_url",
		"unit_converter", "date_calculator", "text_tools", "random_generator",
		"wikipedia_summary", "currency_converter",
	}
	got := map[string]bool{}
	for _, tl := range tools {
		info, _ := tl.Info(context.Background())
		got[info.Name] = true
	}
	for _, e := range expected {
		if !got[e] {
			t.Fatalf("缺少工具: %s", e)
		}
	}
	t.Logf("注册工具总数=%d", len(tools))
}

func TestUnitConverter(t *testing.T) {
	out := runTool(t, getTool(t, "unit_converter"), "unit_converter",
		`{"value":100,"from":"公里","to":"英里"}`)
	if !strings.Contains(out, "英里") {
		t.Fatalf("换算结果异常: %s", out)
	}
	out = runTool(t, getTool(t, "unit_converter"), "unit_converter",
		`{"value":212,"from":"华氏度","to":"摄氏度"}`)
	if !strings.Contains(out, "摄氏度") {
		t.Fatalf("温度换算异常: %s", out)
	}
}

func TestDateCalculator(t *testing.T) {
	out := runTool(t, getTool(t, "date_calculator"), "date_calculator",
		`{"operation":"diff","date1":"2026-01-01","date2":"2026-01-11"}`)
	if !strings.Contains(out, "10 天") {
		t.Fatalf("日期差异常: %s", out)
	}
	out = runTool(t, getTool(t, "date_calculator"), "date_calculator",
		`{"operation":"weekday","date1":"2026-07-16"}`)
	if !strings.Contains(out, "星期") {
		t.Fatalf("星期查询异常: %s", out)
	}
}

func TestTextTools(t *testing.T) {
	out := runTool(t, getTool(t, "text_tools"), "text_tools",
		`{"operation":"base64_encode","text":"hello"}`)
	if !strings.Contains(out, "aGVsbG8=") {
		t.Fatalf("base64 异常: %s", out)
	}
	out = runTool(t, getTool(t, "text_tools"), "text_tools",
		`{"operation":"md5","text":"abc"}`)
	if !strings.Contains(out, "900150983cd24fb0d6963f7d28e17f72") {
		t.Fatalf("md5 异常: %s", out)
	}
	out = runTool(t, getTool(t, "text_tools"), "text_tools",
		`{"operation":"count","text":"hello world"}`)
	if !strings.Contains(out, "单词数: 2") {
		t.Fatalf("count 异常: %s", out)
	}
}

func TestRandomGenerator(t *testing.T) {
	out := runTool(t, getTool(t, "random_generator"), "random_generator",
		`{"kind":"uuid"}`)
	if len(strings.ReplaceAll(out, "-", "")) != 32 {
		t.Fatalf("uuid 异常: %s", out)
	}
	out = runTool(t, getTool(t, "random_generator"), "random_generator",
		`{"kind":"number","min":1,"max":6}`)
	if out == "" {
		t.Fatalf("随机数空")
	}
}
