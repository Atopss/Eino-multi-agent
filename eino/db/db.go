package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Open 打开（或创建）SQLite 数据库并做基础调优。
// 选用 modernc.org/sqlite（纯 Go，免 CGO），保证任意环境 go build 零摩擦。
// SQLite 是单文件、写多读少场景：限制连接池为 1 避免写锁竞争。
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("创建数据库目录失败: %w", err)
		}
	}
	d, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	d.SetConnMaxLifetime(0)
	d.SetConnMaxIdleTime(5 * time.Minute)
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	return d, nil
}

// Migrate 创建数据表（幂等）。
func Migrate(d *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id            TEXT PRIMARY KEY,
			username      TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			is_admin      INTEGER NOT NULL DEFAULT 0,
			created_at    TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL,
			data       TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)`,
		// 每日配额用量：按用户 + 自然日累加请求数与 Token 估算数，
		// 支撑“上架给别人用”的成本控制（与全局 RPS 限流相互独立）。
		`CREATE TABLE IF NOT EXISTS quota_usage (
			user_id  TEXT NOT NULL,
			day      TEXT NOT NULL,
			requests INTEGER NOT NULL DEFAULT 0,
			tokens   INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, day)
		)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return fmt.Errorf("数据库迁移失败: %w", err)
		}
	}
	return nil
}

// Close 关闭数据库连接（衔接优雅关闭流程）。
func Close(d *sql.DB) {
	if d == nil {
		return
	}
	if err := d.Close(); err != nil {
		log.Printf("关闭数据库失败: %v", err)
	}
}
