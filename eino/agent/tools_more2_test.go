package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

// ---- 修复项回归测试 ----

func TestPercentDirection(t *testing.T) {
	out := runTool(t, getTool(t, "percentage_calc"), "percentage_calc", `{"operation":"percent","a":25,"b":200}`)
	if !strings.Contains(out, "12.5") {
		t.Fatalf("percent 方向应为 25 是 200 的 12.5%%，实际: %s", out)
	}
}

func TestBaseConverterBig(t *testing.T) {
	// 16 位 F = 18446744073709551615，超出 int64，验证 big.Int 不溢出
	out := runTool(t, getTool(t, "base_converter"), "base_converter", `{"value":"FFFFFFFFFFFFFFFF","from_base":16,"to_base":10}`)
	if !strings.Contains(out, "18446744073709551615") {
		t.Fatalf("大数进制转换失败: %s", out)
	}
}

func TestDateDiffDirection(t *testing.T) {
	out := runTool(t, getTool(t, "date_calculator"), "date_calculator", `{"operation":"diff","date1":"2026-01-01","date2":"2026-01-10"}`)
	if !strings.Contains(out, "9 天") || !strings.Contains(out, "之后") {
		t.Fatalf("diff 方向输出异常: %s", out)
	}
}

func TestTitleCase(t *testing.T) {
	out := runTool(t, getTool(t, "text_tools"), "text_tools", `{"operation":"title","text":"hello WORLD foo"}`)
	if out != "Hello World Foo" {
		t.Fatalf("title 转换异常: %q", out)
	}
}

func TestUnitTempMix(t *testing.T) {
	it, ok := getTool(t, "unit_converter").(tool.InvokableTool)
	if !ok {
		t.Fatal("unit_converter 不是 InvokableTool")
	}
	if _, err := it.InvokableRun(context.Background(), `{"value":100,"from":"摄氏度","to":"米"}`); err == nil {
		t.Fatal("温度与长度混用应报错")
	}
}

func TestBatch3Registered(t *testing.T) {
	tools, err := GetAllTools(true)
	if err != nil {
		t.Fatalf("GetAllTools: %v", err)
	}
	got := map[string]bool{}
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		info, _ := tl.Info(context.Background())
		got[info.Name] = true
		names = append(names, info.Name)
	}
	expected := []string{
		"qr_code", "url_shorten", "url_expand", "rss_reader",
		"book_search", "ip_lookup", "holiday_cn", "geo_distance",
		"morse_code", "password_strength",
	}
	for _, n := range expected {
		if !got[n] {
			t.Fatalf("缺少工具 %s; 当前工具(%d): %v", n, len(tools), names)
		}
	}
	t.Logf("已注册工具总数: %d", len(tools))
}

func TestGeoDistance(t *testing.T) {
	out := runTool(t, getTool(t, "geo_distance"), "geo_distance",
		`{"lat1":39.9042,"lon1":116.4074,"lat2":31.2304,"lon2":121.4737,"unit":"km"}`)
	if !strings.Contains(out, "公里") {
		t.Fatalf("geo_distance 输出异常: %s", out)
	}
}

func TestMorseCode(t *testing.T) {
	enc := runTool(t, getTool(t, "morse_code"), "morse_code", `{"text":"SOS","mode":"encode"}`)
	if !strings.Contains(enc, "...") {
		t.Fatalf("morse encode 异常: %s", enc)
	}
	dec := runTool(t, getTool(t, "morse_code"), "morse_code", `{"text":"... --- ...","mode":"decode"}`)
	if !strings.Contains(dec, "SOS") {
		t.Fatalf("morse decode 异常: %s", dec)
	}
}

func TestPasswordStrength(t *testing.T) {
	out := runTool(t, getTool(t, "password_strength"), "password_strength", `{"password":"abc123"}`)
	if !strings.Contains(out, "强度评分") {
		t.Fatalf("password_strength 输出异常: %s", out)
	}
}
