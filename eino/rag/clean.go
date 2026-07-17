package rag

import (
	"regexp"
	"strings"
)

var spaceRun = regexp.MustCompile(`[ \t]+`)

func CleanText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "�", "")
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "`", "")

	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || isMarkdownSeparator(line) {
			continue
		}
		line = strings.Trim(line, "|")
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, ">")
		line = strings.TrimSpace(line)
		line = strings.ReplaceAll(line, "|", " ")
		line = spaceRun.ReplaceAllString(line, " ")
		if line != "" && !isMarkdownSeparator(line) {
			cleaned = append(cleaned, line)
		}
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func CleanTextForQuery(text, query string) string {
	cleaned := CleanText(text)
	if cleaned == "" || query == "" {
		return cleaned
	}
	lines := strings.Split(cleaned, "\n")
	terms := queryFocusTerms(query)
	if len(terms) == 0 {
		return cleaned
	}
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, term := range terms {
			if strings.Contains(lower, term) {
				start := i
				end := i + 9
				if end > len(lines) {
					end = len(lines)
				}
				return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
			}
		}
	}
	return cleaned
}

func TextHasQueryFocus(text, query string) bool {
	cleaned := strings.ToLower(CleanText(text))
	if cleaned == "" || query == "" {
		return false
	}
	for _, term := range queryFocusTerms(query) {
		if strings.Contains(cleaned, term) {
			return true
		}
	}
	return false
}

// queryFocusTerms 从 query 中提取聚焦词（去掉弱词与重复），
// 用于邻居扩展与文本聚焦裁剪。不绑定任何具体业务领域。
func queryFocusTerms(query string) []string {
	query = strings.ToLower(query)
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\t' || r == ' ' || r == ',' || r == '，' || r == '?' || r == '？'
	})
	terms := make([]string, 0, 4)
	seen := make(map[string]bool)
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len([]rune(field)) < 2 || seen[field] || isWeakTerm(field) {
			continue
		}
		seen[field] = true
		terms = append(terms, field)
	}
	return terms
}

func isMarkdownSeparator(line string) bool {
	if line == "" {
		return true
	}
	hasDash := false
	for _, r := range line {
		switch r {
		case '-', '|', ':', ' ', '\t':
			if r == '-' {
				hasDash = true
			}
		default:
			return false
		}
	}
	return hasDash
}
