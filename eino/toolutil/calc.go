// Package toolutil 是 agent 与工具子包共享的「低层工具包」，
// 不反向依赖 agent，作为清晰的依赖底边：agent 与未来的 agent/tools/* 子包都只依赖它。
// 目前承载纯函数式的共享能力（表达式求值、字符串安全截断等），
// 避免工具逻辑与 agent 内部状态耦合，也为后续工具分包消除循环依赖。
package toolutil

import (
	"fmt"
	"strconv"
	"strings"
)

// EvalExpression 计算数学表达式，支持加减乘除与括号。
// 原实现位于已删除的 eino/tools 包（自写求值器），迁入本低层包统一维护。
func EvalExpression(expr string) (float64, error) {
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
		} else if (ch >= '0' && ch <= '9') || ch == '.' {
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
	return val, pos + 1, nil
}
