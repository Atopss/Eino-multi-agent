package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"eino/db"
)

const (
	backupTimeFormat  = "20060102-150405" // 用作备份目录名，字典序即时间序
	backupKeepEnv     = "BACKUP_KEEP"
	defaultBackupKeep = 30
)

// backupRoot 返回备份根目录：与 SQLite 同级的 backups/ 子目录。
func (s *Server) backupRoot() string {
	dataDir := filepath.Dir(s.runtime.SQLitePath)
	if dataDir == "" || dataDir == "." {
		return "backups"
	}
	return filepath.Join(dataDir, "backups")
}

// handleAdminBackup 管理员触发一次在线一致性备份：
//  1. 用 VACUUM INTO 生成一致的 eino.db 快照（运行时安全，不锁死、不损坏）；
//  2. 复制 data 目录下的其余运行时文件（config.json / agents.json / sessions 等，
//     排除 eino.db 与 backups 自身）；
//  3. 尽力复制 .env（位于 data 之外，含商家 API Key，备份便于整机恢复）；
//  4. 按 BACKUP_KEEP（默认 30）轮转，仅保留最新 N 份。
func (s *Server) handleAdminBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "仅支持 POST", http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil {
		jsonError(w, "数据库不可用", http.StatusServiceUnavailable)
		return
	}

	root := s.backupRoot()
	ts := time.Now().Format(backupTimeFormat)
	dir := filepath.Join(root, ts)
	if err := os.MkdirAll(dir, 0755); err != nil {
		jsonError(w, "创建备份目录失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 1) 数据库一致性快照（VACUUM INTO）
	if err := db.Backup(s.db, filepath.Join(dir, "eino.db")); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2) 复制 data 目录其余文件（排除 eino.db 与 backups）
	dataDir := filepath.Dir(s.runtime.SQLitePath)
	if dataDir == "" || dataDir == "." {
		dataDir = "."
	}
	if entries, err := os.ReadDir(dataDir); err == nil {
		for _, e := range entries {
			name := e.Name()
			if name == "eino.db" || name == "backups" {
				continue
			}
			src := filepath.Join(dataDir, name)
			dst := filepath.Join(dir, name)
			if e.IsDir() {
				if cerr := copyTree(src, dst); cerr != nil {
					log.Printf("备份复制目录失败 %s: %v", src, cerr)
				}
			} else if cerr := copyFile(src, dst); cerr != nil {
				log.Printf("备份复制文件失败 %s: %v", src, cerr)
			}
		}
	}

	// 3) 尽力复制 .env（可能位于 cwd 或 eino/ 子目录）
	for _, cand := range []string{".env", filepath.Join("eino", ".env")} {
		if _, serr := os.Stat(cand); serr == nil {
			_ = copyFile(cand, filepath.Join(dir, ".env"))
			break
		}
	}

	// 4) 轮转保留
	kept := s.pruneBackups(root)

	s.audit(r, "backup_create", dir, fmt.Sprintf("keep=%d", kept))
	jsonOK(w, map[string]interface{}{
		"ok":   true,
		"path": dir,
		"ts":   ts,
		"kept": kept,
	})
}

// handleAdminBackups 列出已有备份（供前端 / 运维查看）。
func (s *Server) handleAdminBackups(w http.ResponseWriter, r *http.Request) {
	root := s.backupRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		// 目录不存在视为暂无备份
		jsonOK(w, map[string]interface{}{"entries": []interface{}{}, "total": 0})
		return
	}
	type info struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Size int64  `json:"size"`
		Time string `json:"time"`
	}
	list := make([]info, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		var size int64
		if fi, serr := os.Stat(filepath.Join(dir, "eino.db")); serr == nil {
			size = fi.Size()
		}
		t, _ := time.Parse(backupTimeFormat, e.Name())
		list = append(list, info{
			Name: e.Name(),
			Path: dir,
			Size: size,
			Time: t.Format(time.RFC3339),
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name > list[j].Name })
	jsonOK(w, map[string]interface{}{"entries": list, "total": len(list)})
}

// pruneBackups 仅保留最新 keep 份备份目录，返回保留数量。
func (s *Server) pruneBackups(root string) int {
	keep := defaultBackupKeep
	if v := os.Getenv(backupKeepEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			keep = n
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	dirs := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs) // 目录名即时间戳，字典序 = 时间序
	if len(dirs) <= keep {
		return len(dirs)
	}
	for _, d := range dirs[:len(dirs)-keep] {
		if err := os.RemoveAll(filepath.Join(root, d)); err != nil {
			log.Printf("清理旧备份失败 %s: %v", d, err)
		} else {
			log.Printf("已清理旧备份: %s", d)
		}
	}
	return keep
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(p, target)
	})
}
