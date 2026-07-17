package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// ============================================================
// 工具集合（扩展二）：离线转换/计算 + 国内可稳定访问的在线工具
// 全部免密钥；在线工具优先选用在大陆网络可用的数据源。
// ============================================================

// ------------------------------------------------------------
// 1. json_formatter —— JSON 格式化/压缩/校验
// ------------------------------------------------------------

type JSONFormatInput struct {
	JSON   string `json:"json" jsonschema_description:"要处理的 JSON 文本"`
	Action string `json:"action" jsonschema_description:"format=美化缩进；minify=压缩去空白；validate=仅校验是否合法"`
}

func GetJSONFormatter() (tool.BaseTool, error) {
	return utils.InferTool(
		"json_formatter",
		"JSON 处理：美化缩进（format）、压缩去空白（minify）、或校验是否合法（validate）。当用户给出或得到一段 JSON 需要查看、压缩或验证时使用。",
		func(ctx context.Context, input *JSONFormatInput) (string, error) {
			if input == nil || input.JSON == "" {
				return "", fmt.Errorf("json 内容必填")
			}
			action := input.Action
			if action == "" {
				action = "format"
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "json_formatter",
				Message: "调用工具 json_formatter: " + action,
			})
			var raw interface{}
			if err := json.Unmarshal([]byte(input.JSON), &raw); err != nil {
				if action == "validate" {
					return "JSON 不合法: " + err.Error(), nil
				}
				return "", fmt.Errorf("JSON 解析失败: %w", err)
			}
			var out string
			switch action {
			case "validate":
				out = "JSON 合法 ✓"
			case "minify":
				b, err := json.Marshal(raw)
				if err != nil {
					return "", err
				}
				out = string(b)
			default: // format
				var buf bytes.Buffer
				if err := json.Indent(&buf, mustJSONMarshal(raw), "", "  "); err != nil {
					return "", err
				}
				out = buf.String()
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_result", Name: "json_formatter",
				Result: truncateRunes(out, 2000), Message: "json_formatter 返回结果",
			})
			return out, nil
		},
	)
}

func mustJSONMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// ------------------------------------------------------------
// 2. base_converter —— 进制转换（2-36）
// ------------------------------------------------------------

type BaseConvInput struct {
	Value    string `json:"value" jsonschema_description:"要转换的数值（字符串形式）"`
	FromBase int    `json:"from_base" jsonschema_description:"原始进制，2-36，例如 2/8/10/16"`
	ToBase   int    `json:"to_base" jsonschema_description:"目标进制，2-36，例如 2/8/10/16"`
}

func GetBaseConverter() (tool.BaseTool, error) {
	return utils.InferTool(
		"base_converter",
		"进制转换工具：在 2~36 进制之间互转（支持二进制、八进制、十进制、十六进制及任意进制）。当用户做进制换算、解析机器码、颜色码、位运算相关问题时调用。",
		func(ctx context.Context, input *BaseConvInput) (string, error) {
			if input == nil || input.Value == "" {
				return "", fmt.Errorf("value 必填")
			}
			fb, tb := input.FromBase, input.ToBase
			if fb < 2 || fb > 36 || tb < 2 || tb > 36 {
				return "", fmt.Errorf("进制需在 2-36 之间")
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "base_converter",
				Message: fmt.Sprintf("调用工具 base_converter: %s (%d->%d)", input.Value, fb, tb),
			})
			n := new(big.Int)
			if _, ok := n.SetString(strings.TrimSpace(input.Value), fb); !ok {
				return "", fmt.Errorf("无法按 %d 进制解析 %q", fb, input.Value)
			}
			out := fmt.Sprintf("%s(%d) = %s(%d)", input.Value, fb, n.Text(tb), tb)
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_result", Name: "base_converter", Result: out, Message: "base_converter 返回结果",
			})
			return out, nil
		},
	)
}

// ------------------------------------------------------------
// 3. timestamp_converter —— 时间戳 ↔ 日期
// ------------------------------------------------------------

type TSInput struct {
	Value    string `json:"value" jsonschema_description:"时间戳(秒或毫秒) 或 日期字符串(YYYY-MM-DD / YYYY-MM-DD HH:MM:SS)"`
	Action   string `json:"action" jsonschema_description:"to_date=时间戳转日期；to_ts=日期转时间戳(秒)"`
	Timezone string `json:"timezone" jsonschema_description:"时区名，如 Asia/Shanghai、UTC，默认 Asia/Shanghai"`
}

func GetTimestampConverter() (tool.BaseTool, error) {
	return utils.InferTool(
		"timestamp_converter",
		"时间戳与日期互转工具：把 Unix 时间戳（自动识别秒/毫秒）转成可读日期，或把日期字符串转成时间戳。支持指定时区（默认 Asia/Shanghai）。当用户处理日志时间、时间戳、过期时间等场景时调用。",
		func(ctx context.Context, input *TSInput) (string, error) {
			if input == nil || input.Value == "" {
				return "", fmt.Errorf("value 必填")
			}
			loc, err := time.LoadLocation(strings.TrimSpace(input.Timezone))
			if err != nil || input.Timezone == "" {
				loc = time.FixedZone("CST", 8*3600)
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "timestamp_converter",
				Message: "调用工具 timestamp_converter: " + input.Action,
			})
			out, err := convertTimestamp(input.Value, input.Action, loc)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type: "tool_result", Name: "timestamp_converter",
					Result: "error: " + err.Error(), Message: "timestamp_converter 失败",
				})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_result", Name: "timestamp_converter", Result: out, Message: "timestamp_converter 返回结果",
			})
			return out, nil
		},
	)
}

func convertTimestamp(value, action string, loc *time.Location) (string, error) {
	switch action {
	case "to_date":
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return "", fmt.Errorf("时间戳应为整数: %w", err)
		}
		if n > 1e11 { // 毫秒
			n = n / 1000
		}
		t := time.Unix(n, 0).In(loc)
		return fmt.Sprintf("%d → %s（%s）", n, t.Format("2006-01-02 15:04:05"), loc.String()), nil
	case "to_ts", "":
		v := strings.TrimSpace(value)
		var t time.Time
		var err error
		if strings.Contains(v, " ") {
			t, err = time.ParseInLocation("2006-01-02 15:04:05", v, loc)
		} else {
			t, err = time.ParseInLocation("2006-01-02", v, loc)
		}
		if err != nil {
			return "", fmt.Errorf("日期解析失败(应为 YYYY-MM-DD 或 YYYY-MM-DD HH:MM:SS): %w", err)
		}
		return fmt.Sprintf("%s → 时间戳 %d（秒）", v, t.Unix()), nil
	default:
		return "", fmt.Errorf("未知 action: %s（支持 to_date/to_ts）", action)
	}
}

// ------------------------------------------------------------
// 4. color_converter —— 颜色格式互转（HEX/RGB/HSL）
// ------------------------------------------------------------

type ColorInput struct {
	Color  string `json:"color" jsonschema_description:"颜色值，支持 #ff0000、255,0,0、hsl(0,100%,50%) 形式"`
	Target string `json:"target" jsonschema_description:"目标格式：hex、rgb、hsl"`
}

func GetColorConverter() (tool.BaseTool, error) {
	return utils.InferTool(
		"color_converter",
		"颜色格式转换工具：在 HEX(#rrggbb)、RGB(r,g,b)、HSL(h,s%,l%) 之间互转。当用户处理前端样式、设计稿取色、配色问题时调用。",
		func(ctx context.Context, input *ColorInput) (string, error) {
			if input == nil || input.Color == "" || input.Target == "" {
				return "", fmt.Errorf("color 与 target 必填")
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "color_converter",
				Message: "调用工具 color_converter: " + input.Color + " -> " + input.Target,
			})
			r, g, b, err := parseColor(input.Color)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type: "tool_result", Name: "color_converter",
					Result: "error: " + err.Error(), Message: "color_converter 失败",
				})
				return "", err
			}
			out := formatColor(r, g, b, strings.ToLower(input.Target))
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_result", Name: "color_converter", Result: out, Message: "color_converter 返回结果",
			})
			return out, nil
		},
	)
}

func parseColor(s string) (r, g, b uint8, err error) {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "#") || (len(s) == 6 && isHex(s)):
		h := strings.TrimPrefix(low, "#")
		if len(h) == 3 { // #rgb -> #rrggbb
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) != 6 {
			return 0, 0, 0, fmt.Errorf("非法 hex 颜色: %s", s)
		}
		v, e := strconv.ParseUint(h, 16, 32)
		if e != nil {
			return 0, 0, 0, fmt.Errorf("非法 hex 颜色: %s", s)
		}
		return uint8(v >> 16), uint8(v >> 8), uint8(v), nil
	case strings.HasPrefix(low, "hsl"):
		re := regexp.MustCompile(`(?i)hsl\(\s*([\d.]+)\s*,\s*([\d.]+)%\s*,\s*([\d.]+)%\s*\)`)
		m := re.FindStringSubmatch(s)
		if len(m) < 4 {
			return 0, 0, 0, fmt.Errorf("非法 hsl 颜色: %s", s)
		}
		h, _ := strconv.ParseFloat(m[1], 64)
		sv, _ := strconv.ParseFloat(m[2], 64)
		lv, _ := strconv.ParseFloat(m[3], 64)
		return hslToRgb(h, sv/100, lv/100)
	default:
		parts := strings.Split(s, ",")
		if len(parts) != 3 {
			return 0, 0, 0, fmt.Errorf("无法识别颜色格式: %s", s)
		}
		ri, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		gi, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		bi, e3 := strconv.Atoi(strings.TrimSpace(parts[2]))
		if e1 != nil || e2 != nil || e3 != nil {
			return 0, 0, 0, fmt.Errorf("非法 rgb 颜色: %s", s)
		}
		return uint8(clampByte(ri)), uint8(clampByte(gi)), uint8(clampByte(bi)), nil
	}
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func clampByte(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

func formatColor(r, g, b uint8, target string) string {
	switch target {
	case "rgb":
		return fmt.Sprintf("rgb(%d, %d, %d)", r, g, b)
	case "hsl":
		h, s, l := rgbToHsl(r, g, b)
		return fmt.Sprintf("hsl(%.0f, %.0f%%, %.0f%%)", h, s*100, l*100)
	case "hex", "":
		return fmt.Sprintf("#%02X%02X%02X", r, g, b)
	default:
		return fmt.Sprintf("#%02X%02X%02X", r, g, b)
	}
}

func rgbToHsl(r, g, b uint8) (h, s, l float64) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	max := math.Max(rf, math.Max(gf, bf))
	min := math.Min(rf, math.Min(gf, bf))
	l = (max + min) / 2
	if max == min {
		return 0, 0, l
	}
	d := max - min
	if l > 0.5 {
		s = d / (2 - max - min)
	} else {
		s = d / (max + min)
	}
	switch max {
	case rf:
		h = (gf - bf) / d
		if gf < bf {
			h += 6
		}
	case gf:
		h = (bf-rf)/d + 2
	case bf:
		h = (rf-gf)/d + 4
	}
	h *= 60
	return h, s, l
}

func hslToRgb(h, s, l float64) (uint8, uint8, uint8, error) {
	if h < 0 || h > 360 || s < 0 || s > 1 || l < 0 || l > 1 {
		return 0, 0, 0, fmt.Errorf("hsl 取值越界")
	}
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return uint8(clampByte(int((r + m) * 255))),
		uint8(clampByte(int((g + m) * 255))),
		uint8(clampByte(int((b + m) * 255))), nil
}

// ------------------------------------------------------------
// 5. roman_numeral —— 阿拉伯 ↔ 罗马数字
// ------------------------------------------------------------

type RomanInput struct {
	Value string `json:"value" jsonschema_description:"阿拉伯数字(1-3999) 或 罗马数字(如 MMXXIV、xiv)"`
}

func GetRomanNumeral() (tool.BaseTool, error) {
	return utils.InferTool(
		"roman_numeral",
		"阿拉伯数字与罗马数字互转工具（范围 1-3999）。当用户需要把数字写成罗马数字（如用于序号、章节、钟表、纪念日）或识别罗马数字时调用。",
		func(ctx context.Context, input *RomanInput) (string, error) {
			if input == nil || input.Value == "" {
				return "", fmt.Errorf("value 必填")
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "roman_numeral",
				Message: "调用工具 roman_numeral: " + input.Value,
			})
			v := strings.TrimSpace(input.Value)
			if isAllDigits(v) {
				n, err := strconv.Atoi(v)
				if err != nil || n <= 0 || n > 3999 {
					return "", fmt.Errorf("阿拉伯数字需在 1-3999: %s", v)
				}
				out := fmt.Sprintf("%d = %s", n, arabicToRoman(n))
				appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "roman_numeral", Result: out, Message: "roman_numeral 返回结果"})
				return out, nil
			}
			n, err := romanToArabic(strings.ToUpper(v))
			if err != nil {
				return "", err
			}
			out := fmt.Sprintf("%s = %d", v, n)
			appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "roman_numeral", Result: out, Message: "roman_numeral 返回结果"})
			return out, nil
		},
	)
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func arabicToRoman(n int) string {
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i := 0; n > 0; i++ {
		for n >= vals[i] {
			b.WriteString(syms[i])
			n -= vals[i]
		}
	}
	return b.String()
}

func romanToArabic(s string) (int, error) {
	m := map[rune]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}
	total := 0
	prev := 0
	for _, c := range s {
		v, ok := m[c]
		if !ok {
			return 0, fmt.Errorf("非法罗马数字字符: %c", c)
		}
		if v > prev {
			total += v - 2*prev
		} else {
			total += v
		}
		prev = v
	}
	return total, nil
}

// ------------------------------------------------------------
// 6. percentage_calc —— 百分比计算
// ------------------------------------------------------------

type PctInput struct {
	Operation string  `json:"operation" jsonschema_description:"of=X的Y%是多少；percent=A是B的百分之几；change=从A到B的变化率%；increase=A增加B%；decrease=A减少B%"`
	A        float64 `json:"a" jsonschema_description:"数值 A"`
	B        float64 `json:"b" jsonschema_description:"数值 B 或 百分比值"`
}

func GetPercentageCalc() (tool.BaseTool, error) {
	return utils.InferTool(
		"percentage_calc",
		"百分比计算工具：支持 求X的Y%是多少(of)、A是B的百分之几(percent)、从A到B的变化率%(change)、A增加/减少B%(increase/decrease)。当用户做折扣、涨幅、占比、增长率等计算时调用。",
		func(ctx context.Context, input *PctInput) (string, error) {
			if input == nil || input.Operation == "" {
				return "", fmt.Errorf("operation 必填")
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "percentage_calc",
				Message: "调用工具 percentage_calc: " + input.Operation,
			})
			out, err := calcPercentage(input.Operation, input.A, input.B)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "percentage_calc", Result: "error: " + err.Error(), Message: "percentage_calc 失败"})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "percentage_calc", Result: out, Message: "percentage_calc 返回结果"})
			return out, nil
		},
	)
}

func calcPercentage(op string, a, b float64) (string, error) {
	switch op {
	case "of":
		return fmt.Sprintf("%.4g 的 %.4g%% = %.6g", a, b, a*b/100), nil
	case "percent":
		if b == 0 {
			return "", fmt.Errorf("分母 B 不能为 0")
		}
		return fmt.Sprintf("%.4g 是 %.4g 的 %.4g%%", a, b, a/b*100), nil
	case "change":
		if a == 0 {
			return "", fmt.Errorf("原值不能为 0")
		}
		return fmt.Sprintf("从 %.4g 到 %.4g 的变化率为 %.4g%%", a, b, (b-a)/a*100), nil
	case "increase":
		return fmt.Sprintf("%.4g 增加 %.4g%% = %.6g", a, b, a*(1+b/100)), nil
	case "decrease":
		return fmt.Sprintf("%.4g 减少 %.4g%% = %.6g", a, b, a*(1-b/100)), nil
	default:
		return "", fmt.Errorf("未知操作: %s", op)
	}
}

// ------------------------------------------------------------
// 7. bmi_calculator —— BMI 计算（中国标准）
// ------------------------------------------------------------

type BMIInput struct {
	WeightKg float64 `json:"weight_kg" jsonschema_description:"体重，单位公斤(kg)"`
	HeightCm float64 `json:"height_cm" jsonschema_description:"身高，单位厘米(cm)"`
}

func GetBMICalculator() (tool.BaseTool, error) {
	return utils.InferTool(
		"bmi_calculator",
		"BMI 身体质量指数计算（采用中国成人标准分类），输入身高(cm)与体重(kg)，返回 BMI 数值、体型分类与健康建议。当用户询问胖瘦、体重健康、BMI 时调用。",
		func(ctx context.Context, input *BMIInput) (string, error) {
			if input == nil || input.HeightCm <= 0 || input.WeightKg <= 0 {
				return "", fmt.Errorf("身高(cm)与体重(kg)均需为正数")
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "bmi_calculator",
				Message: "调用工具 bmi_calculator",
			})
			m := input.HeightCm / 100
			bmi := input.WeightKg / (m * m)
			var cat, advice string
			switch {
			case bmi < 18.5:
				cat, advice = "偏瘦", "建议适当增加营养摄入，规律运动增肌。"
			case bmi < 24:
				cat, advice = "正常", "保持当前体重与良好生活习惯即可。"
			case bmi < 28:
				cat, advice = "超重", "建议控制饮食热量、增加有氧运动，防止进一步增重。"
			default:
				cat, advice = "肥胖", "建议制定减重计划，控制膳食并坚持运动，必要时咨询专业医师。"
			}
			out := fmt.Sprintf("身高 %.1fcm / 体重 %.1fkg → BMI = %.1f，体型分类：%s。%s", input.HeightCm, input.WeightKg, bmi, cat, advice)
			appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "bmi_calculator", Result: out, Message: "bmi_calculator 返回结果"})
			return out, nil
		},
	)
}

// ------------------------------------------------------------
// 8. regex_tester —— 正则测试（匹配/查找/替换/分组）
// ------------------------------------------------------------

type RegexInput struct {
	Pattern string `json:"pattern" jsonschema_description:"正则表达式"`
	Text    string `json:"text" jsonschema_description:"待匹配文本"`
	Action  string `json:"action" jsonschema_description:"match=是否整体匹配；findall=列出所有匹配；replace=替换(需 replace 字段)；groups=捕获组"`
	Replace string `json:"replace" jsonschema_description:"替换后的内容（action=replace 时使用）"`
}

func GetRegexTester() (tool.BaseTool, error) {
	return utils.InferTool(
		"regex_tester",
		"正则表达式测试工具：检验正则是否匹配（match）、列出全部匹配（findall）、执行替换（replace）或提取捕获组（groups）。当用户需要验证、调试正则表达式或从文本中抽取模式化内容时调用。",
		func(ctx context.Context, input *RegexInput) (string, error) {
			if input == nil || input.Pattern == "" || input.Text == "" {
				return "", fmt.Errorf("pattern 与 text 必填")
			}
			action := input.Action
			if action == "" {
				action = "findall"
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "regex_tester",
				Message: "调用工具 regex_tester: " + action,
			})
			re, err := regexp.Compile(input.Pattern)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "regex_tester", Result: "error: " + err.Error(), Message: "regex_tester 失败"})
				return "", fmt.Errorf("正则编译失败: %w", err)
			}
			var out string
			switch action {
			case "match":
				out = fmt.Sprintf("是否匹配: %v", re.MatchString(input.Text))
			case "findall":
				ms := re.FindAllString(input.Text, -1)
				out = fmt.Sprintf("共 %d 处匹配:\n- %s", len(ms), strings.Join(ms, "\n- "))
			case "replace":
				out = re.ReplaceAllString(input.Text, input.Replace)
			case "groups":
				m := re.FindStringSubmatch(input.Text)
				if m == nil {
					out = "无匹配 / 无捕获组"
				} else {
					var b strings.Builder
					fmt.Fprintf(&b, "整体匹配: %s\n", m[0])
					for i := 1; i < len(m); i++ {
						fmt.Fprintf(&b, "组 %d: %s\n", i, m[i])
					}
					out = b.String()
				}
			default:
				return "", fmt.Errorf("未知 action: %s", action)
			}
			appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "regex_tester", Result: truncateRunes(out, 1000), Message: "regex_tester 返回结果"})
			return out, nil
		},
	)
}

// ------------------------------------------------------------
// 9. translate —— 翻译（MyMemory 免密钥）
// ------------------------------------------------------------

type TranslateInput struct {
	Text string `json:"text" jsonschema_description:"要翻译的文本"`
	From string `json:"from" jsonschema_description:"源语言，auto=自动检测，或 zh-CN/en/ja/ko/fr 等，默认 zh-CN"`
	To   string `json:"to" jsonschema_description:"目标语言，如 zh-CN、en、ja，默认 en"`
}

func GetTranslator() (tool.BaseTool, error) {
	return utils.InferTool(
		"translate",
		"文本翻译工具（数据来自 MyMemory 免费接口，免密钥，支持多语言互译与自动检测）。当用户需要把一段文字翻译成中文/英文或其他语言时使用。",
		func(ctx context.Context, input *TranslateInput) (string, error) {
			if input == nil || strings.TrimSpace(input.Text) == "" {
				return "", fmt.Errorf("text 必填")
			}
			from := strings.TrimSpace(input.From)
			if from == "" {
				from = "zh-CN"
			}
			if strings.EqualFold(from, "auto") {
				from = "Autodetect"
			}
			to := strings.TrimSpace(input.To)
			if to == "" {
				to = "en"
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "translate",
				Message: "调用工具 translate: " + from + "->" + to,
			})
			out, err := translateText(input.Text, from, to)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "translate", Result: "error: " + err.Error(), Message: "translate 失败"})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "translate", Result: truncateRunes(out, 1000), Message: "translate 返回结果"})
			return out, nil
		},
	)
}

func translateText(text, from, to string) (string, error) {
	u := "https://api.mymemory.translated.net/get?q=" + url.QueryEscape(text) + "&langpair=" + url.QueryEscape(from) + "%7C" + url.QueryEscape(to)
	body, err := httpGet(u)
	if err != nil {
		return "", err
	}
	var resp struct {
		ResponseData struct {
			TranslatedText string `json:"translatedText"`
		} `json:"responseData"`
		ResponseStatus int    `json:"responseStatus"`
		ResponseDetail string `json:"responseDetails"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("解析翻译结果失败: %w", err)
	}
	if resp.ResponseStatus != 200 {
		return "", fmt.Errorf("翻译接口返回 %d: %s", resp.ResponseStatus, resp.ResponseDetail)
	}
	if strings.TrimSpace(resp.ResponseData.TranslatedText) == "" {
		return "", fmt.Errorf("翻译结果为空")
	}
	return resp.ResponseData.TranslatedText, nil
}

// ------------------------------------------------------------
// 10. my_ip —— 公网 IP 与归属地（ip.cn，国内可用）
// ------------------------------------------------------------

type MyIPInput struct {
	IP string `json:"ip" jsonschema_description:"可选，留空查询本机公网 IP；填入具体 IP 则查询该 IP 的归属地"`
}

func GetMyIP() (tool.BaseTool, error) {
	return utils.InferTool(
		"my_ip",
		"查询公网 IP 及其归属地（数据来自 ip.cn，国内可用）。留空查询本机出口 IP 与地理位置；也可传入指定 IP 反查归属地。当用户想知道自己的 IP、网络位置或核实某 IP 归属时调用。",
		func(ctx context.Context, input *MyIPInput) (string, error) {
			ip := ""
			if input != nil {
				ip = strings.TrimSpace(input.IP)
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "my_ip",
				Message: "调用工具 my_ip: " + ip,
			})
			out, err := queryMyIP(ip)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "my_ip", Result: "error: " + err.Error(), Message: "my_ip 失败"})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "my_ip", Result: out, Message: "my_ip 返回结果"})
			return out, nil
		},
	)
}

func queryMyIP(ip string) (string, error) {
	u := "https://www.ip.cn/api/index?ip=" + url.QueryEscape(ip) + "&type=0"
	body, err := httpGet(u)
	if err != nil {
		return "", err
	}
	var resp struct {
		IP      string `json:"ip"`
		Address string `json:"address"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("解析 IP 信息失败: %w", err)
	}
	if resp.IP == "" && resp.Address == "" {
		return "", fmt.Errorf("未获取到 IP 信息")
	}
	return fmt.Sprintf("IP: %s\n归属地: %s", resp.IP, resp.Address), nil
}

// ------------------------------------------------------------
// 11. dns_lookup —— 域名 DNS 解析（标准库，离线）
// ------------------------------------------------------------

type DNSInput struct {
	Domain string `json:"domain" jsonschema_description:"要解析的域名，如 example.com"`
	Type   string `json:"type" jsonschema_description:"记录类型：A/AAAA/CNAME/MX/NS/TXT，默认 A"`
}

func GetDNSLookup() (tool.BaseTool, error) {
	return utils.InferTool(
		"dns_lookup",
		"DNS 解析工具：查询域名的 A(IPv4)/AAAA(IPv6)/CNAME/MX(邮件)/NS(权威)/TXT 记录。当用户排查域名解析、邮件配置、CDN 或网络连通性问题时调用。",
		func(ctx context.Context, input *DNSInput) (string, error) {
			if input == nil || strings.TrimSpace(input.Domain) == "" {
				return "", fmt.Errorf("domain 必填")
			}
			dtype := strings.ToUpper(strings.TrimSpace(input.Type))
			if dtype == "" {
				dtype = "A"
			}
			domain := strings.TrimSpace(input.Domain)
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "dns_lookup",
				Message: "调用工具 dns_lookup: " + domain + " " + dtype,
			})
			out, err := dnsLookup(domain, dtype)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "dns_lookup", Result: "error: " + err.Error(), Message: "dns_lookup 失败"})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "dns_lookup", Result: truncateRunes(out, 1500), Message: "dns_lookup 返回结果"})
			return out, nil
		},
	)
}

func dnsLookup(domain, dtype string) (string, error) {
	switch dtype {
	case "A":
		addrs, err := net.LookupHost(domain)
		if err != nil {
			return "", err
		}
		return "A 记录:\n- " + strings.Join(addrs, "\n- "), nil
	case "AAAA":
		addrs, err := net.LookupIP(domain)
		if err != nil {
			return "", err
		}
		var v6 []string
		for _, ip := range addrs {
			if ip.To4() == nil {
				v6 = append(v6, ip.String())
			}
		}
		return "AAAA 记录:\n- " + strings.Join(v6, "\n- "), nil
	case "CNAME":
		cname, err := net.LookupCNAME(domain)
		if err != nil {
			return "", err
		}
		return "CNAME: " + cname, nil
	case "MX":
		mx, err := net.LookupMX(domain)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, m := range mx {
			fmt.Fprintf(&b, "- %s (优先级 %d)\n", m.Host, m.Pref)
		}
		return "MX 记录:\n" + strings.TrimSpace(b.String()), nil
	case "NS":
		ns, err := net.LookupNS(domain)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, n := range ns {
			fmt.Fprintf(&b, "- %s\n", n.Host)
		}
		return "NS 记录:\n" + strings.TrimSpace(b.String()), nil
	case "TXT":
		txt, err := net.LookupTXT(domain)
		if err != nil {
			return "", err
		}
		return "TXT 记录:\n- " + strings.Join(txt, "\n- "), nil
	default:
		return "", fmt.Errorf("不支持的记录类型: %s（支持 A/AAAA/CNAME/MX/NS/TXT）", dtype)
	}
}

// ------------------------------------------------------------
// 12. http_check —— 网址可达性/状态码检测
// ------------------------------------------------------------

type HTTPCheckInput struct {
	URL    string `json:"url" jsonschema_description:"要检测的网址，需以 http:// 或 https:// 开头"`
	Method string `json:"method" jsonschema_description:"请求方法，GET 或 HEAD，默认 GET"`
}

func GetHTTPCheck() (tool.BaseTool, error) {
	return utils.InferTool(
		"http_check",
		"检测网址是否可访问，返回 HTTP 状态码、是否成功(2xx)、最终跳转地址与响应耗时。当用户确认某个网站/接口是否在线、排查链接失效或监控可用性时调用。",
		func(ctx context.Context, input *HTTPCheckInput) (string, error) {
			if input == nil || !strings.HasPrefix(strings.ToLower(input.URL), "http") {
				return "", fmt.Errorf("需要提供合法的 http(s) URL")
			}
			method := strings.ToUpper(strings.TrimSpace(input.Method))
			if method == "" {
				method = "GET"
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "http_check",
				Message: "调用工具 http_check: " + input.URL,
			})
			client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return nil
			}}
			req, err := http.NewRequest(method, input.URL, nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; EinoAgent/1.0)")
			start := time.Now()
			resp, err := client.Do(req)
			elapsed := time.Since(start)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "http_check", Result: "error: " + err.Error(), Message: "http_check 失败"})
				return "", fmt.Errorf("请求失败: %w", err)
			}
			defer resp.Body.Close()
			ok := resp.StatusCode >= 200 && resp.StatusCode < 300
			out := fmt.Sprintf("状态码: %d\n状态: %s\n成功: %v\n最终地址: %s\n耗时: %s",
				resp.StatusCode, resp.Status, ok, resp.Request.URL.String(), elapsed.Round(time.Millisecond))
			appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "http_check", Result: out, Message: "http_check 返回结果"})
			return out, nil
		},
	)
}

// ------------------------------------------------------------
// 13. crypto_price —— 加密货币行情（CoinGecko 免密钥）
// ------------------------------------------------------------

type CryptoInput struct {
	Coins string `json:"coins" jsonschema_description:"币种 id，逗号分隔，如 bitcoin,ethereum,dogecoin,solana"`
	Vs    string `json:"vs" jsonschema_description:"计价货币，usd 或 cny，默认 usd"`
}

func GetCryptoPrice() (tool.BaseTool, error) {
	return utils.InferTool(
		"crypto_price",
		"加密货币行情查询（数据来自 CoinGecko 免费接口，免密钥）：输入币种 id（如 bitcoin、ethereum）查询实时价格与 24 小时涨跌幅。当用户询问比特币、以太坊等加密货币价格时调用。",
		func(ctx context.Context, input *CryptoInput) (string, error) {
			if input == nil || strings.TrimSpace(input.Coins) == "" {
				return "", fmt.Errorf("coins 必填，例如 bitcoin,ethereum")
			}
			vs := strings.ToLower(strings.TrimSpace(input.Vs))
			if vs == "" {
				vs = "usd"
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "crypto_price",
				Message: "调用工具 crypto_price: " + input.Coins,
			})
			out, err := cryptoPrice(input.Coins, vs)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "crypto_price", Result: "error: " + err.Error(), Message: "crypto_price 失败"})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "crypto_price", Result: truncateRunes(out, 1500), Message: "crypto_price 返回结果"})
			return out, nil
		},
	)
}

func cryptoPrice(coins, vs string) (string, error) {
	u := "https://api.coingecko.com/api/v3/simple/price?ids=" + url.QueryEscape(coins) +
		"&vs_currencies=" + url.QueryEscape(vs) + "&include_24hr_change=true"
	body, err := httpGet(u)
	if err != nil {
		return "", err
	}
	var data map[string]map[string]float64
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("解析行情失败: %w", err)
	}
	if len(data) == 0 {
		return "未查询到对应币种行情（请确认币种 id 是否正确，如 bitcoin/ethereum）。", nil
	}
	var b strings.Builder
	for id, m := range data {
		price := m[vs]
		chg := m[vs+"_24h_change"]
		fmt.Fprintf(&b, "%s: %.6g %s（24h %+.2f%%）\n", id, price, strings.ToUpper(vs), chg)
	}
	return strings.TrimSpace(b.String()), nil
}

// ------------------------------------------------------------
// 14. stock_quote —— A股/港股/美股行情（腾讯 gtimg，国内可用）
// ------------------------------------------------------------

type StockInput struct {
	Symbols string `json:"symbols" jsonschema_description:"股票代码，逗号分隔。沪市 sh+代码(如 sh600519)，深市 sz+代码(如 sz000001)，港股 hk+代码(如 hk00700)，美股 us+代码(如 usAAPL)"`
}

func GetStockQuote() (tool.BaseTool, error) {
	return utils.InferTool(
		"stock_quote",
		"股票行情查询（数据来自腾讯财经公开接口，国内可用，免密钥）：支持 A股（沪 sh/深 sz）、港股（hk）、美股（us）。返回名称、现价、涨跌幅、成交量等。当用户查询股票价格、大盘个股行情时调用。",
		func(ctx context.Context, input *StockInput) (string, error) {
			if input == nil || strings.TrimSpace(input.Symbols) == "" {
				return "", fmt.Errorf("symbols 必填，例如 sh600519,sz000001")
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "stock_quote",
				Message: "调用工具 stock_quote: " + input.Symbols,
			})
			out, err := stockQuote(input.Symbols)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "stock_quote", Result: "error: " + err.Error(), Message: "stock_quote 失败"})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "stock_quote", Result: truncateRunes(out, 2000), Message: "stock_quote 返回结果"})
			return out, nil
		},
	)
}

func stockQuote(symbols string) (string, error) {
	parts := strings.Split(symbols, ",")
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) == 0 {
		return "", fmt.Errorf("无有效代码")
	}
	u := "https://qt.gtimg.cn/q=" + url.QueryEscape(strings.Join(cleaned, ","))
	body, err := httpGet(u)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(body), ";")
	var b strings.Builder
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "v_") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		val := strings.Trim(line[eq+1:], `"`)
		fields := strings.Split(val, "~")
		if len(fields) < 33 {
			continue
		}
		name := fields[1]
		code := fields[2]
		price := fields[3]
		prevClose := fields[4]
		open := fields[5]
		change := fields[31]
		changePct := fields[32]
		volume := fields[6]
		fmt.Fprintf(&b, "%s(%s): 现价 %s，昨收 %s，今开 %s，涨跌 %s (%s%%)，成交量 %s 手\n",
			name, code, price, prevClose, open, change, changePct, volume)
	}
	res := strings.TrimSpace(b.String())
	if res == "" {
		return "未查询到对应股票行情（请确认代码格式，如 sh600519）。", nil
	}
	return res, nil
}

// ------------------------------------------------------------
// 15. hot_trends —— 平台热榜（oioweb.cn 免密钥，国内可用）
// ------------------------------------------------------------

type HotInput struct {
	Source string `json:"source" jsonschema_description:"平台：weibo/zhihu/douyin/bilibili/baidu/toutiao，默认 weibo"`
}

func GetHotTrends() (tool.BaseTool, error) {
	return utils.InferTool(
		"hot_trends",
		"获取各大平台实时热榜（数据来自 oioweb.cn 免费接口，国内可用）：支持微博、知乎、抖音、B站、百度、今日头条热榜。当用户想了解当下热点、热搜话题、社会舆情时调用。",
		func(ctx context.Context, input *HotInput) (string, error) {
			src := "weibo"
			if input != nil && input.Source != "" {
				src = strings.ToLower(strings.TrimSpace(input.Source))
			}
			typeCN, ok := map[string]string{
				"weibo": "微博", "zhihu": "知乎热榜", "douyin": "抖音热点",
				"bilibili": "B站排行榜", "baidu": "百度热点", "toutiao": "今日头条",
			}[src]
			if !ok {
				typeCN = "微博"
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type: "tool_call", Name: "hot_trends",
				Message: "调用工具 hot_trends: " + typeCN,
			})
			out, err := hotTrends(typeCN)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "hot_trends", Result: "error: " + err.Error(), Message: "hot_trends 失败"})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{Type: "tool_result", Name: "hot_trends", Result: truncateRunes(out, 2000), Message: "hot_trends 返回结果"})
			return out, nil
		},
	)
}

func hotTrends(typeCN string) (string, error) {
	u := "https://api.oioweb.cn/api/common/HotList?type=" + url.QueryEscape(typeCN)
	body, err := httpGet(u)
	if err != nil {
		return "", err
	}
	var resp struct {
		Code int `json:"code"`
		Data []struct {
			Title string `json:"title"`
			Hot   string `json:"hot"`
			URL   string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("解析热榜失败: %w", err)
	}
	if len(resp.Data) == 0 {
		return "（" + typeCN + "）暂无可读取的热榜内容。", nil
	}
	n := 10
	if len(resp.Data) < n {
		n = len(resp.Data)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "【%s 热榜】\n", typeCN)
	for i := 0; i < n; i++ {
		item := resp.Data[i]
		fmt.Fprintf(&b, "%d. %s", i+1, item.Title)
		if item.Hot != "" {
			fmt.Fprintf(&b, "（热度 %s）", item.Hot)
		}
		if item.URL != "" {
			fmt.Fprintf(&b, "\n   %s", item.URL)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}
