package rag

import "strings"

// NormalizeSearchQuery 仅做轻量归一化，保持检索层与具体业务无关、可通用复用。
func NormalizeSearchQuery(query string) string {
	return strings.TrimSpace(query)
}
