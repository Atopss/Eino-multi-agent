package agent

import (
	"context"
	"strings"
	"testing"
)

func TestMoreToolsRegistered(t *testing.T) {
	tools, err := GetAllTools(true)
	if err != nil {
		t.Fatalf("GetAllTools: %v", err)
	}
	expected := []string{
		"get_weather", "get_current_time", "calculator", "web_search", "fetch_url",
		"unit_converter", "date_calculator", "text_tools", "random_generator",
		"wikipedia_summary", "currency_converter",
		"json_formatter", "base_converter", "timestamp_converter", "color_converter",
		"roman_numeral", "percentage_calc", "bmi_calculator", "regex_tester",
		"translate", "my_ip", "dns_lookup", "http_check",
		"crypto_price", "stock_quote", "hot_trends",
	}
	got := map[string]bool{}
	for _, tl := range tools {
		info, _ := tl.Info(context.Background())
		got[info.Name] = true
	}
	for _, e := range expected {
		if !got[e] {
			t.Fatalf("缺少工具: %s（已注册 %d 个）", e, len(tools))
		}
	}
	t.Logf("注册工具总数=%d", len(tools))
}

func TestJSONFormatter(t *testing.T) {
	out := runTool(t, getTool(t, "json_formatter"), "json_formatter",
		`{"json":"{\"a\":1}","action":"format"}`)
	if !strings.Contains(out, "\n") {
		t.Fatalf("format 异常: %s", out)
	}
	out = runTool(t, getTool(t, "json_formatter"), "json_formatter",
		`{"json":"not json","action":"validate"}`)
	if !strings.Contains(out, "不合法") {
		t.Fatalf("validate 异常: %s", out)
	}
}

func TestBaseConverter(t *testing.T) {
	out := runTool(t, getTool(t, "base_converter"), "base_converter",
		`{"value":"255","from_base":10,"to_base":16}`)
	if !strings.Contains(out, "ff") {
		t.Fatalf("进制转换异常: %s", out)
	}
}

func TestTimestampConverter(t *testing.T) {
	out := runTool(t, getTool(t, "timestamp_converter"), "timestamp_converter",
		`{"value":"1700000000","action":"to_date","timezone":"Asia/Shanghai"}`)
	if !strings.Contains(out, "2023") {
		t.Fatalf("时间戳转换异常: %s", out)
	}
	out = runTool(t, getTool(t, "timestamp_converter"), "timestamp_converter",
		`{"value":"2026-07-16","action":"to_ts","timezone":"Asia/Shanghai"}`)
	if !strings.Contains(out, "时间戳") {
		t.Fatalf("日期转时间戳异常: %s", out)
	}
}

func TestColorConverter(t *testing.T) {
	out := runTool(t, getTool(t, "color_converter"), "color_converter",
		`{"color":"#ff0000","target":"rgb"}`)
	if !strings.Contains(out, "rgb(255, 0, 0)") {
		t.Fatalf("颜色转换异常: %s", out)
	}
}

func TestRomanNumeral(t *testing.T) {
	out := runTool(t, getTool(t, "roman_numeral"), "roman_numeral",
		`{"value":"2024"}`)
	if !strings.Contains(out, "MMXXIV") {
		t.Fatalf("阿拉伯转罗马异常: %s", out)
	}
	out = runTool(t, getTool(t, "roman_numeral"), "roman_numeral",
		`{"value":"MMXXIV"}`)
	if !strings.Contains(out, "2024") {
		t.Fatalf("罗马转阿拉伯异常: %s", out)
	}
}

func TestPercentageCalc(t *testing.T) {
	out := runTool(t, getTool(t, "percentage_calc"), "percentage_calc",
		`{"operation":"of","a":200,"b":15}`)
	if !strings.Contains(out, "30") {
		t.Fatalf("百分比异常: %s", out)
	}
}

func TestBMICalculator(t *testing.T) {
	out := runTool(t, getTool(t, "bmi_calculator"), "bmi_calculator",
		`{"weight_kg":70,"height_cm":175}`)
	if !strings.Contains(out, "BMI") {
		t.Fatalf("BMI 异常: %s", out)
	}
}

func TestRegexTester(t *testing.T) {
	out := runTool(t, getTool(t, "regex_tester"), "regex_tester",
		`{"pattern":"\\d+","text":"abc123def456","action":"findall"}`)
	if !strings.Contains(out, "123") || !strings.Contains(out, "456") {
		t.Fatalf("正则异常: %s", out)
	}
}
