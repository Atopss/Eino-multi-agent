package tools

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// ============================================================
// 工具集合（扩展）：单位换算 / 日期计算 / 文本处理 /
// 随机数生成 / 维基百科摘要 / 汇率换算
// 均为免密钥、可离线或调用免费公开接口，复用 tools.go 的 httpGet。
// ============================================================

// ------------------------------------------------------------
// 1. unit_converter —— 单位换算
// ------------------------------------------------------------

type UnitConverterInput struct {
	Value float64 `json:"value" jsonschema_description:"要换算的数值"`
	From  string  `json:"from" jsonschema_description:"原始单位，例如 米、kg、华氏度、公里、英里、码、公顷、升、mb、km/h"`
	To    string  `json:"to" jsonschema_description:"目标单位，例如 英尺、g、摄氏度、平方米、加仑、mph"`
}

func GetUnitConverter() (tool.BaseTool, error) {
	return utils.InferTool(
		"unit_converter",
		"单位换算工具，支持长度、重量、温度、面积、体积、时间、速度、数据存储之间的换算（例如 公里↔英里、kg↔磅、华氏度↔摄氏度、公顷↔英亩、升↔加仑、km/h↔mph、MB↔GB）。当用户进行物理量、度量衡换算时调用。",
		func(ctx context.Context, input *UnitConverterInput) (string, error) {
			if input == nil {
				return "", fmt.Errorf("value/from/to are required")
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_call",
				Name:    "unit_converter",
				Message: fmt.Sprintf("调用工具 unit_converter: %.6g %s -> %s", input.Value, input.From, input.To),
			})
			out, err := convertUnit(input.Value, input.From, input.To)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:    "tool_result",
					Name:    "unit_converter",
					Result:  "error: " + err.Error(),
					Message: "unit_converter 失败",
				})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_result",
				Name:    "unit_converter",
				Result:  out,
				Message: "unit_converter 返回结果",
			})
			return out, nil
		},
	)
}

// 各单位的「换算到基准单位」的系数。
// 温度单独处理（非倍率关系）。
var unitFactors = map[string]map[string]float64{
	"长度": {
		"米": 1, "m": 1, "厘米": 0.01, "cm": 0.01, "毫米": 0.001, "mm": 0.001,
		"千米": 1000, "km": 1000, "公里": 1000,
		"英里": 1609.344, "mile": 1609.344, "mi": 1609.344,
		"英尺": 0.3048, "ft": 0.3048, "foot": 0.3048, "feet": 0.3048,
		"英寸": 0.0254, "inch": 0.0254, "in": 0.0254,
		"码": 0.9144, "yard": 0.9144, "yd": 0.9144,
		"海里": 1852, "nmi": 1852, "nauticalmile": 1852,
	},
	"重量": {
		"克": 1, "g": 1, "毫克": 0.001, "mg": 0.001,
		"千克": 1000, "kg": 1000, "公斤": 1000,
		"吨": 1e6, "t": 1e6,
		"磅": 453.59237, "lb": 453.59237, "lbs": 453.59237,
		"盎司": 28.349523125, "oz": 28.349523125,
	},
	"面积": {
		"平方米": 1, "m2": 1, "米²": 1, "m²": 1,
		"平方千米": 1e6, "km2": 1e6, "km²": 1e6,
		"平方厘米": 1e-4, "cm2": 1e-4, "cm²": 1e-4,
		"公顷": 1e4, "ha": 1e4,
		"亩": 666.6666667,
		"英亩": 4046.8564224, "acre": 4046.8564224,
		"平方英尺": 0.09290304, "ft2": 0.09290304, "ft²": 0.09290304,
	},
	"体积": {
		"升": 1, "l": 1, "公升": 1,
		"毫升": 0.001, "ml": 0.001,
		"立方米": 1000, "m3": 1000, "m³": 1000,
		"立方厘米": 0.001, "cm3": 0.001, "cm³": 0.001,
		"加仑": 3.785411784, "gallon": 3.785411784, "gal": 3.785411784,
		"美加仑": 3.785411784,
	},
	"时间": {
		"秒": 1, "s": 1, "second": 1, "seconds": 1,
		"分钟": 60, "min": 60, "minute": 60, "minutes": 60,
		"小时": 3600, "h": 3600, "hour": 3600, "hours": 3600,
		"天": 86400, "d": 86400, "day": 86400, "days": 86400,
		"周": 604800, "week": 604800, "weeks": 604800,
	},
	"速度": {
		"米每秒": 1, "m/s": 1, "mps": 1,
		"千米每小时": 0.2777778, "km/h": 0.2777778, "kph": 0.2777778,
		"英里每小时": 0.44704, "mph": 0.44704,
		"节": 0.5144444, "knot": 0.5144444, "knots": 0.5144444,
	},
	"数据存储": {
		"B": 1, "字节": 1, "byte": 1, "bytes": 1,
		"KB": 1024, "千字节": 1024,
		"MB": 1048576, "兆字节": 1048576,
		"GB": 1073741824, "吉字节": 1073741824,
		"TB": 1099511627776, "太字节": 1099511627776,
	},
}

var tempUnits = map[string]bool{"摄氏度": true, "°c": true, "c": true, "华氏度": true, "°f": true, "f": true, "开尔文": true, "k": true, "kelvin": true}

func normalizeUnit(u string) string {
	u = strings.TrimSpace(strings.ToLower(u))
	// 兼容中文与英文混合写法
	repl := map[string]string{
		"摄氏度": "c", "华氏度": "f", "开尔文": "k",
		"千米": "km", "公里": "km", "米": "m", "厘米": "cm", "毫米": "mm",
		"千克": "kg", "公斤": "kg", "克": "g", "毫克": "mg", "吨": "t",
		"毫升": "ml", "升": "l", "公升": "l", "立方米": "m3", "立方厘米": "cm3",
		"平方米": "m2", "平方千米": "km2", "平方厘米": "cm2", "公顷": "ha", "英亩": "acre", "平方英尺": "ft2",
		"分钟": "min", "小时": "h", "天": "d", "周": "week", "秒": "s",
		"米每秒": "m/s", "千米每小时": "km/h", "英里每小时": "mph", "节": "knot",
		"字节": "b", "千字节": "kb", "兆字节": "mb", "吉字节": "gb", "太字节": "tb",
	}
	if r, ok := repl[u]; ok {
		return r
	}
	return u
}

func convertUnit(value float64, fromRaw, toRaw string) (string, error) {
	from := normalizeUnit(fromRaw)
	to := normalizeUnit(toRaw)
	if from == "" || to == "" {
		return "", fmt.Errorf("单位不能为空")
	}
	// 温度分支：温度单位只能与温度单位互转
	if tempUnits[from] || tempUnits[to] {
		if !tempUnits[from] || !tempUnits[to] {
			return "", fmt.Errorf("温度单位只能与温度单位互转（%s 与 %s 不同类）", fromRaw, toRaw)
		}
		c, err := toCelsius(from, value)
		if err != nil {
			return "", err
		}
		res, err := fromCelsius(to, c)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%.4g %s = %.6g %s", value, fromRaw, res, toRaw), nil
	}
	// 倍率分支：找到同一类别下两个单位的系数
	var catFrom, catTo string
	var facFrom, facTo float64
	for cat, units := range unitFactors {
		if f, ok := units[from]; ok {
			catFrom, facFrom = cat, f
		}
		if f, ok := units[to]; ok {
			catTo, facTo = cat, f
		}
	}
	if catFrom == "" {
		return "", fmt.Errorf("无法识别原始单位: %s", fromRaw)
	}
	if catTo == "" {
		return "", fmt.Errorf("无法识别目标单位: %s", toRaw)
	}
	if catFrom != catTo {
		return "", fmt.Errorf("单位类别不一致（%s 与 %s 无法互相换算）", catFrom, catTo)
	}
	base := value * facFrom
	res := base / facTo
	return fmt.Sprintf("%.6g %s = %.6g %s", value, fromRaw, res, toRaw), nil
}

func toCelsius(u string, v float64) (float64, error) {
	switch u {
	case "c", "摄氏度":
		return v, nil
	case "f", "华氏度":
		return (v - 32) * 5 / 9, nil
	case "k", "开尔文":
		return v - 273.15, nil
	}
	return 0, fmt.Errorf("不支持的温度单位: %s", u)
}

func fromCelsius(u string, c float64) (float64, error) {
	switch u {
	case "c", "摄氏度":
		return c, nil
	case "f", "华氏度":
		return c*9/5 + 32, nil
	case "k", "开尔文":
		return c + 273.15, nil
	}
	return 0, fmt.Errorf("不支持的温度单位: %s", u)
}

// ------------------------------------------------------------
// 2. date_calculator —— 日期计算
// ------------------------------------------------------------

type DateCalcInput struct {
	Operation string `json:"operation" jsonschema_description:"操作类型：diff=计算两个日期相差天数；add=在日期上加减天数；weekday=求某日期是星期几"`
	Date1     string `json:"date1" jsonschema_description:"起始日期，格式 YYYY-MM-DD（如 2026-07-16）"`
	Date2     string `json:"date2" jsonschema_description:"结束日期（仅 diff 需要），格式 YYYY-MM-DD"`
	Days      int    `json:"days" jsonschema_description:"加减的天数（仅 add 需要，可为负数）"`
}

func GetDateCalculator() (tool.BaseTool, error) {
	return utils.InferTool(
		"date_calculator",
		"日期计算工具：可计算两个日期相差多少天、在某个日期上加/减若干天得到新日期、或求某天是星期几。输入日期格式为 YYYY-MM-DD。当用户问到日期间隔、倒计时、若干天后是几号、某日是星期几等问题时调用。",
		func(ctx context.Context, input *DateCalcInput) (string, error) {
			if input == nil || input.Operation == "" || input.Date1 == "" {
				return "", fmt.Errorf("operation 与 date1 必填")
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_call",
				Name:    "date_calculator",
				Message: fmt.Sprintf("调用工具 date_calculator: %s %s", input.Operation, input.Date1),
			})
			out, err := calcDate(input.Operation, input.Date1, input.Date2, input.Days)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:    "tool_result",
					Name:    "date_calculator",
					Result:  "error: " + err.Error(),
					Message: "date_calculator 失败",
				})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_result",
				Name:    "date_calculator",
				Result:  out,
				Message: "date_calculator 返回结果",
			})
			return out, nil
		},
	)
}

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("日期格式应为 YYYY-MM-DD，收到: %s", s)
	}
	return t, nil
}

func calcDate(op, d1, d2 string, days int) (string, error) {
	t1, err := parseDate(d1)
	if err != nil {
		return "", err
	}
	weekdayCN := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	switch op {
	case "diff":
		if d2 == "" {
			return "", fmt.Errorf("diff 操作需要 date2")
		}
		t2, err := parseDate(d2)
		if err != nil {
			return "", err
		}
		d := int(t2.Sub(t1).Hours() / 24)
		rel := ""
		switch {
		case d > 0:
			rel = fmt.Sprintf("（%s 在 %s 之后 %d 天）", d2, d1, d)
		case d < 0:
			d = -d
			rel = fmt.Sprintf("（%s 在 %s 之前 %d 天）", d2, d1, d)
		default:
			rel = "（同一天）"
		}
		return fmt.Sprintf("%s 与 %s 相差 %d 天%s", d1, d2, d, rel), nil
	case "add":
		t := t1.AddDate(0, 0, days)
		return fmt.Sprintf("%s %+d 天 = %s（%s）", d1, days, t.Format("2006-01-02"), weekdayCN[int(t.Weekday())]), nil
	case "weekday":
		return fmt.Sprintf("%s 是 %s", d1, weekdayCN[int(t1.Weekday())]), nil
	default:
		return "", fmt.Errorf("未知操作: %s（支持 diff/add/weekday）", op)
	}
}

// ------------------------------------------------------------
// 3. text_tools —— 文本处理
// ------------------------------------------------------------

type TextToolsInput struct {
	Operation string `json:"operation" jsonschema_description:"操作类型：count=字数统计；upper/lower/title=大小写转换；reverse=反转；base64_encode/base64_decode=Base64编解码；url_encode/url_decode=URL编解码；md5/sha256=计算哈希"`
	Text      string `json:"text" jsonschema_description:"要处理的文本"`
}

func GetTextTools() (tool.BaseTool, error) {
	return utils.InferTool(
		"text_tools",
		"文本处理工具：支持字数统计（字符数/单词数/不含空格字符数）、大小写转换（upper/lower/title）、字符串反转、Base64 编解码、URL 编解码、MD5/SHA256 哈希计算。当用户需要对文本做计数、编码、哈希、格式转换时使用。",
		func(ctx context.Context, input *TextToolsInput) (string, error) {
			if input == nil || input.Operation == "" {
				return "", fmt.Errorf("operation 必填")
			}
			if strings.HasPrefix(input.Operation, "count") ||
				strings.HasPrefix(input.Operation, "upper") ||
				strings.HasPrefix(input.Operation, "lower") ||
				strings.HasPrefix(input.Operation, "title") ||
				strings.HasPrefix(input.Operation, "reverse") ||
				strings.HasPrefix(input.Operation, "base64") ||
				strings.HasPrefix(input.Operation, "url") ||
				strings.HasPrefix(input.Operation, "md5") ||
				strings.HasPrefix(input.Operation, "sha256") {
				if input.Text == "" {
					return "", fmt.Errorf("该操作需要 text 参数")
				}
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_call",
				Name:    "text_tools",
				Message: "调用工具 text_tools: " + input.Operation,
			})
			out, err := processText(input.Operation, input.Text)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:    "tool_result",
					Name:    "text_tools",
					Result:  "error: " + err.Error(),
					Message: "text_tools 失败",
				})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_result",
				Name:    "text_tools",
				Result:  truncateRunes(out, 500),
				Message: "text_tools 返回结果",
			})
			return out, nil
		},
	)
}

func processText(op, text string) (string, error) {
	switch op {
	case "count":
		runes := []rune(text)
		noSpace := strings.ReplaceAll(strings.ReplaceAll(text, " ", ""), "\n", "")
		words := len(strings.Fields(text))
		return fmt.Sprintf("字符数(含空格): %d；字符数(不含空格): %d；单词数: %d", len(runes), len([]rune(noSpace)), words), nil
	case "upper":
		return strings.ToUpper(text), nil
	case "lower":
		return strings.ToLower(text), nil
	case "title":
		return toTitleCase(text), nil
	case "reverse":
		r := []rune(text)
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
		return string(r), nil
	case "base64_encode":
		return base64.StdEncoding.EncodeToString([]byte(text)), nil
	case "base64_decode":
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
		if err != nil {
			return "", fmt.Errorf("Base64 解码失败: %w", err)
		}
		return string(b), nil
	case "url_encode":
		return url.QueryEscape(text), nil
	case "url_decode":
		s, err := url.QueryUnescape(text)
		if err != nil {
			return "", fmt.Errorf("URL 解码失败: %w", err)
		}
		return s, nil
	case "md5":
		sum := md5.Sum([]byte(text))
		return hex.EncodeToString(sum[:]), nil
	case "sha256":
		sum := sha256.Sum256([]byte(text))
		return hex.EncodeToString(sum[:]), nil
	default:
		return "", fmt.Errorf("未知操作: %s", op)
	}
}

// toTitleCase 将每个单词首字母大写、其余小写（替代已弃用的 strings.Title）。
func toTitleCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevIsLetter := false
	for _, r := range s {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		switch {
		case isLetter && !prevIsLetter:
			b.WriteString(strings.ToUpper(string(r)))
		case isLetter:
			b.WriteString(strings.ToLower(string(r)))
		default:
			b.WriteRune(r)
		}
		prevIsLetter = isLetter
	}
	return b.String()
}

// ------------------------------------------------------------
// 4. random_generator —— 随机数 / 密码 / UUID / 字符串
// ------------------------------------------------------------

type RandomGenInput struct {
	Kind    string `json:"kind" jsonschema_description:"类型：number=随机整数；password=随机密码；uuid=UUID v4；string=随机字符串"`
	Min     int    `json:"min" jsonschema_description:"随机整数下界（number 用，默认 0）"`
	Max     int    `json:"max" jsonschema_description:"随机整数上界（number 用，默认 100，需大于 min）"`
	Length  int    `json:"length" jsonschema_description:"长度（password/string 用，默认 16）"`
	Count   int    `json:"count" jsonschema_description:"生成个数，默认 1，最多 20"`
	UseSym  bool   `json:"use_symbols" jsonschema_description:"密码是否包含符号（password 用，默认 true）"`
}

func GetRandomGenerator() (tool.BaseTool, error) {
	return utils.InferTool(
		"random_generator",
		"随机数生成工具：可生成指定范围的随机整数、高强度随机密码、UUID v4、或指定长度的随机字符串。当用户需要随机数、验证码、临时口令、唯一 ID 时调用。",
		func(ctx context.Context, input *RandomGenInput) (string, error) {
			if input == nil || input.Kind == "" {
				return "", fmt.Errorf("kind 必填")
			}
			count := input.Count
			if count <= 0 {
				count = 1
			}
			if count > 20 {
				count = 20
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_call",
				Name:    "random_generator",
				Message: "调用工具 random_generator: " + input.Kind,
			})
			var results []string
			for i := 0; i < count; i++ {
				v, err := genRandom(input.Kind, input.Min, input.Max, input.Length, input.UseSym)
				if err != nil {
					appendTraceItem(ctx, ExecutionTraceItem{
						Type:    "tool_result",
						Name:    "random_generator",
						Result:  "error: " + err.Error(),
						Message: "random_generator 失败",
					})
					return "", err
				}
				results = append(results, v)
			}
			out := strings.Join(results, "\n")
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_result",
				Name:    "random_generator",
				Result:  truncateRunes(out, 500),
				Message: "random_generator 返回结果",
			})
			return out, nil
		},
	)
}

func genRandom(kind string, min, max, length int, useSym bool) (string, error) {
	switch kind {
	case "number":
		if max <= min {
			max = min + 100
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d", int(n.Int64())+min), nil
	case "password":
		if length <= 0 {
			length = 16
		}
		return randSecureString(length, useSym)
	case "uuid":
		b := make([]byte, 16)
		if _, err := io.ReadFull(rand.Reader, b); err != nil {
			return "", err
		}
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
	case "string":
		if length <= 0 {
			length = 16
		}
		return randSecureString(length, false)
	default:
		return "", fmt.Errorf("未知类型: %s（支持 number/password/uuid/string）", kind)
	}
}

const (
	alphaNum  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	alphaSym  = alphaNum + "!@#$%^&*()-_=+[]{};:,.?/"
	symbolsOnly = "!@#$%^&*()-_=+[]{};:,.?/"
)

func randSecureString(n int, useSym bool) (string, error) {
	charset := alphaNum
	if useSym {
		charset = alphaSym
	}
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		b.WriteByte(charset[idx.Int64()])
	}
	return b.String(), nil
}

// ------------------------------------------------------------
// 5. wikipedia_summary —— 维基百科摘要（免密钥）
// ------------------------------------------------------------

type WikiInput struct {
	Query string `json:"query" jsonschema_description:"要查询的主题，例如 量子计算、光合作用、第二次世界大战"`
	Lang  string `json:"lang" jsonschema_description:"维基百科语言，默认 zh（中文），也可填 en、ja 等"`
}

func GetWikipediaSummary() (tool.BaseTool, error) {
	return utils.InferTool(
		"wikipedia_summary",
		"查询维基百科条目摘要，获取某个概念、人物、事件、事物的简明百科介绍（免密钥）。当用户需要了解某术语的定义与背景知识、或需要可靠的百科类信息时调用。",
		func(ctx context.Context, input *WikiInput) (string, error) {
			if input == nil || strings.TrimSpace(input.Query) == "" {
				return "", fmt.Errorf("query is required")
			}
			lang := strings.ToLower(strings.TrimSpace(input.Lang))
			if lang == "" {
				lang = "zh"
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_call",
				Name:    "wikipedia_summary",
				Message: "调用工具 wikipedia_summary: " + input.Query,
			})
			out, err := wikiSummary(input.Query, lang)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:    "tool_result",
					Name:    "wikipedia_summary",
					Result:  "error: " + err.Error(),
					Message: "wikipedia_summary 失败",
				})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_result",
				Name:    "wikipedia_summary",
				Result:  truncateRunes(out, 500),
				Message: "wikipedia_summary 返回结果",
			})
			return out, nil
		},
	)
}

// wikiSummary 先用 opensearch 找最佳条目标题，再取 REST 摘要。
// lang 优先使用用户指定的语言；若失败则按顺序回退（中文维基在国内常被屏蔽，
// 故默认把英文维基也列入回退链，提高可用性）。
func wikiSummary(query, lang string) (string, error) {
	candidates := []string{lang}
	if lang != "en" {
		candidates = append(candidates, "en")
	}
	if lang != "zh" {
		candidates = append(candidates, "zh")
	}
	var lastErr error
	for _, lc := range candidates {
		out, err := wikiSummaryLang(query, lc)
		if err == nil {
			return out, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "未找到与「" + query + "」相关的维基百科条目。", nil
}

func wikiSummaryLang(query, lang string) (string, error) {
	searchURL := fmt.Sprintf("https://%s.wikipedia.org/w/api.php?action=opensearch&search=%s&limit=1&format=json",
		lang, url.QueryEscape(query))
	body, err := httpGet(searchURL)
	if err != nil {
		return "", err
	}
	var os []interface{}
	if err := json.Unmarshal(body, &os); err != nil || len(os) < 2 {
		return "", fmt.Errorf("opensearch 解析失败")
	}
	titles, ok := os[1].([]interface{})
	if !ok || len(titles) == 0 {
		return "", fmt.Errorf("无匹配条目")
	}
	title := fmt.Sprint(titles[0])

	summaryURL := fmt.Sprintf("https://%s.wikipedia.org/api/rest_v1/page/summary/%s",
		lang, url.QueryEscape(title))
	sBody, err := httpGet(summaryURL)
	if err != nil {
		return "", err
	}
	var s struct {
		Title   string `json:"title"`
		Extract string `json:"extract"`
		ContentURL struct {
			Desktop struct {
				Page string `json:"page"`
			} `json:"desktop"`
		} `json:"content_urls"`
	}
	if err := json.Unmarshal(sBody, &s); err != nil {
		return "", fmt.Errorf("解析维基摘要失败: %w", err)
	}
	if strings.TrimSpace(s.Extract) == "" {
		return "", fmt.Errorf("条目无摘要")
	}
	pageURL := s.ContentURL.Desktop.Page
	if pageURL == "" {
		pageURL = fmt.Sprintf("https://%s.wikipedia.org/wiki/%s", lang, url.QueryEscape(title))
	}
	return fmt.Sprintf("%s\n\n%s\n\n来源: %s", s.Title, s.Extract, pageURL), nil
}

// ------------------------------------------------------------
// 6. currency_converter —— 汇率换算（免费免密钥）
// ------------------------------------------------------------

type CurrencyInput struct {
	Amount float64 `json:"amount" jsonschema_description:"要换算的金额"`
	From   string  `json:"from" jsonschema_description:"原始货币代码，如 USD、CNY、EUR、JPY、GBP（3 位大写）"`
	To     string  `json:"to" jsonschema_description:"目标货币代码，如 USD、CNY、EUR、JPY、GBP（3 位大写）"`
}

func GetCurrencyConverter() (tool.BaseTool, error) {
	return utils.InferTool(
		"currency_converter",
		"货币汇率换算工具（数据来自 open.er-api.com 免费接口，实时汇率）。支持主流货币如 CNY、USD、EUR、JPY、GBP、HKD 等之间的换算。当用户询问汇率、把某币种换成另一种币种、金额折算时调用。",
		func(ctx context.Context, input *CurrencyInput) (string, error) {
			if input == nil || input.From == "" || input.To == "" {
				return "", fmt.Errorf("from 与 to 必填")
			}
			from := strings.ToUpper(strings.TrimSpace(input.From))
			to := strings.ToUpper(strings.TrimSpace(input.To))
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_call",
				Name:    "currency_converter",
				Message: fmt.Sprintf("调用工具 currency_converter: %.4g %s -> %s", input.Amount, from, to),
			})
			out, err := convertCurrency(input.Amount, from, to)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:    "tool_result",
					Name:    "currency_converter",
					Result:  "error: " + err.Error(),
					Message: "currency_converter 失败",
				})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_result",
				Name:    "currency_converter",
				Result:  out,
				Message: "currency_converter 返回结果",
			})
			return out, nil
		},
	)
}

func convertCurrency(amount float64, from, to string) (string, error) {
	apiURL := fmt.Sprintf("https://open.er-api.com/v6/latest/%s", from)
	body, err := httpGet(apiURL)
	if err != nil {
		return "", err
	}
	var resp struct {
		Result  string             `json:"result"`
		Base    string             `json:"base_code"`
		Rates   map[string]float64 `json:"rates"`
		TimeLastUpdateUTC string   `json:"time_last_update_utc"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("解析汇率数据失败: %w", err)
	}
	if strings.ToLower(resp.Result) != "success" {
		return "", fmt.Errorf("汇率接口返回失败: %s", resp.Result)
	}
	rateTo, ok := resp.Rates[to]
	if !ok {
		return "", fmt.Errorf("不支持的目标货币: %s", to)
	}
	converted := amount * rateTo
	return fmt.Sprintf("%.4g %s = %.6g %s（汇率 1 %s = %.6g %s；更新时间: %s）",
		amount, from, converted, to, from, rateTo, to, resp.TimeLastUpdateUTC), nil
}
