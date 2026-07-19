package toolutil

// TruncateRunes 按 rune 数量安全截断字符串，避免在多字节字符中间截断。
func TruncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 || len([]rune(value)) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}

// LimitString 是 TruncateRunes 的语义别名（限制字符串最大长度）。
func LimitString(value string, maxLen int) string {
	return TruncateRunes(value, maxLen)
}
