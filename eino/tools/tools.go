package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/schema"
)

type ToolInfo struct {
	Name        string
	Description string
	Handler     func(args string) string
}

var allTools []ToolInfo

func init() {
	allTools = []ToolInfo{
		{
			Name:        "get_weather",
			Description: "查询指定城市的实时天气信息",
			Handler:     handleWeather,
		},
		{
			Name:        "calculator",
			Description: "计算数学表达式，支持加减乘除和括号",
			Handler:     handleCalculator,
		},
	}
}

// ToolDef 工具定义（返回给 Agent 用）
type ToolDef struct {
	Name        string
	Description string
	Params      *schema.ParamsOneOf
}

// GetAllToolDefs 返回所有工具的 schema 定义
func GetAllToolDefs() []ToolDef {
	return []ToolDef{
		{
			Name:        "get_weather",
			Description: "查询指定城市的实时天气信息。参数: city=城市名（如北京、上海）",
			Params: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"city": {
					Type:     schema.String,
					Desc:     "城市名称",
					Required: true,
				},
			}),
		},
		{
			Name:        "calculator",
			Description: "计算数学表达式，支持加减乘除和括号。参数: expression=数学表达式（如 (3+5)*2 ）",
			Params: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"expression": {
					Type:     schema.String,
					Desc:     "数学表达式",
					Required: true,
				},
			}),
		},
	}
}

// CallTool 根据名称调用工具
func CallTool(name string, args string) string {
	for _, t := range allTools {
		if t.Name == name {
			return t.Handler(args)
		}
	}
	return fmt.Sprintf("未知工具: %s", name)
}

// GetAllTools 保留旧接口兼容
func GetAllTools() ([]interface{}, error) {
	result := make([]interface{}, 0, len(allTools))
	for _, t := range allTools {
		tCopy := t
		result = append(result, map[string]interface{}{
			"name":        tCopy.Name,
			"description": tCopy.Description,
			"handler": func(args string) string {
				return tCopy.Handler(args)
			},
		})
	}
	return result, nil
}

func handleWeather(args string) string {
	var input struct {
		City string `json:"city"`
	}
	json.Unmarshal([]byte(args), &input)
	if input.City == "" {
		input.City = "北京"
	}
	weatherData := map[string]map[string]interface{}{
		"北京": {"temperature": 25, "weather": "晴天"},
		"上海": {"temperature": 28, "weather": "多云"},
		"广州": {"temperature": 32, "weather": "小雨"},
		"深圳": {"temperature": 30, "weather": "晴"},
	}
	if data, ok := weatherData[input.City]; ok {
		return fmt.Sprintf("城市: %s, 温度: %d°C, 天气: %s", input.City, data["temperature"], data["weather"])
	}
	return fmt.Sprintf("城市: %s, 温度: 22°C, 天气: 晴", input.City)
}

func handleCalculator(args string) string {
	var input struct {
		Expression string `json:"expression"`
	}
	json.Unmarshal([]byte(args), &input)
	expr := input.Expression
	if expr == "" {
		return "错误: 请提供数学表达式"
	}
	result, err := evalExpression(expr)
	if err != nil {
		return fmt.Sprintf("计算错误: %v", err)
	}
	return fmt.Sprintf("计算结果: %s = %v", expr, result)
}

func evalExpression(expr string) (float64, error) {
	expr = strings.ReplaceAll(expr, " ", "")
	tokens, err := tokenize(expr)
	if err != nil {
		return 0, err
	}
	result, _, err := parseExpr(tokens, 0)
	return result, err
}

func tokenize(expr string) ([]string, error) {
	var tokens []string
	i := 0
	for i < len(expr) {
		ch := expr[i]
		if ch == '+' || ch == '-' || ch == '*' || ch == '/' || ch == '(' || ch == ')' {
			tokens = append(tokens, string(ch))
			i++
		} else if ch >= '0' && ch <= '9' || ch == '.' {
			j := i
			for j < len(expr) && ((expr[j] >= '0' && expr[j] <= '9') || expr[j] == '.') {
				j++
			}
			tokens = append(tokens, expr[i:j])
			i = j
		} else {
			return nil, fmt.Errorf("unexpected character: %c", ch)
		}
	}
	return tokens, nil
}

func parseExpr(tokens []string, pos int) (float64, int, error) {
	left, pos, err := parseTerm(tokens, pos)
	if err != nil {
		return 0, pos, err
	}
	for pos < len(tokens) && (tokens[pos] == "+" || tokens[pos] == "-") {
		op := tokens[pos]
		right, newPos, err := parseTerm(tokens, pos+1)
		if err != nil {
			return 0, newPos, err
		}
		if op == "+" {
			left += right
		} else {
			left -= right
		}
		pos = newPos
	}
	return left, pos, nil
}

func parseTerm(tokens []string, pos int) (float64, int, error) {
	left, pos, err := parseFactor(tokens, pos)
	if err != nil {
		return 0, pos, err
	}
	for pos < len(tokens) && (tokens[pos] == "*" || tokens[pos] == "/") {
		op := tokens[pos]
		right, newPos, err := parseFactor(tokens, pos+1)
		if err != nil {
			return 0, newPos, err
		}
		if op == "*" {
			left *= right
		} else {
			if right == 0 {
				return 0, newPos, fmt.Errorf("division by zero")
			}
			left /= right
		}
		pos = newPos
	}
	return left, pos, nil
}

func parseFactor(tokens []string, pos int) (float64, int, error) {
	if pos >= len(tokens) {
		return 0, pos, fmt.Errorf("unexpected end of expression")
	}
	if tokens[pos] == "(" {
		val, newPos, err := parseExpr(tokens, pos+1)
		if err != nil {
			return 0, newPos, err
		}
		if newPos >= len(tokens) || tokens[newPos] != ")" {
			return 0, newPos, fmt.Errorf("missing closing parenthesis")
		}
		return val, newPos + 1, nil
	}
	if tokens[pos] == "-" {
		val, newPos, err := parseFactor(tokens, pos+1)
		if err != nil {
			return 0, newPos, err
		}
		return -val, newPos, nil
	}
	val, err := strconv.ParseFloat(tokens[pos], 64)
	if err != nil {
		return 0, pos, fmt.Errorf("invalid number: %s", tokens[pos])
	}
	return math.Abs(val), pos + 1, nil
}
