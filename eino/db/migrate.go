package db

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ImportLegacySessions 在首次启动时，把 data/sessions/*.json 历史会话
// 导入 SQLite（user_id 记为 "legacy"），防止存量数据丢失。
// 导入后不删除原文件，兼容必要时回退到文件存储。
func ImportLegacySessions(d *sql.DB, sessionsDir string) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".json")
		if strings.Contains(sessionID, "/") {
			// 已被命名空间化（user 前缀）的键跳过，避免重复导入
			continue
		}
		data, err := os.ReadFile(filepath.Join(sessionsDir, entry.Name()))
		if err != nil {
			continue
		}
		if _, err := d.ExecContext(context.Background(),
			`INSERT OR IGNORE INTO sessions(id, user_id, data, updated_at) VALUES(?,?,?,?)`,
			sessionID, "legacy", string(data), time.Now().Format(time.RFC3339)); err != nil {
			log.Printf("迁移会话 %s 失败: %v", sessionID, err)
			continue
		}
		count++
	}
	if count > 0 {
		log.Printf("已从本地 JSON 迁移 %d 个历史会话到数据库", count)
	}
}
