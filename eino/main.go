package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"eino/server"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("=== 崩溃原因 ===\n%v\n\n%s", r, debug.Stack())
			fmt.Println("\n程序崩溃，错误日志已保存到 crash.log")
			os.WriteFile("crash.log", []byte(fmt.Sprintf("Panic: %v\n\n%s", r, debug.Stack())), 0644)
		}
	}()

	loadEnvFiles()

	addr := ":8899"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	s := server.New()

	fmt.Println("========================================")
	fmt.Println("  硕硕 智能体 API 服务已启动")
	fmt.Println("  后端地址: http://localhost" + addr)
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("前端: 进入 web/ 执行 `npm run dev`（默认 http://localhost:5173）")
	fmt.Println("或在项目根目录双击 start.bat 一键启动前后端")
	fmt.Println("本地模式：打开即用，无需登录（如需对外暴露，请在反向代理层加 basic auth）")
	fmt.Println()

	// 在独立 goroutine 中启动 HTTP 服务；主 goroutine 监听退出信号并优雅关闭。
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start(addr)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("收到信号 %v，正在优雅关闭...", sig)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Shutdown(shutdownCtx); err != nil {
		log.Printf("优雅关闭出错: %v", err)
	}
	if err := <-errCh; err != nil && err != http.ErrServerClosed {
		log.Printf("服务退出: %v", err)
	}
	log.Println("已停止")
}

func loadEnvFiles() {
	paths := make([]string, 0, 6)
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths,
			filepath.Join(cwd, ".env"),
			filepath.Join(cwd, "eino", ".env"),
			filepath.Join(cwd, "..", ".env"),
		)
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		paths = append(paths,
			filepath.Join(exeDir, ".env"),
			filepath.Join(exeDir, "..", ".env"),
		)
	}
	seen := make(map[string]bool)
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil || seen[abs] {
			continue
		}
		seen[abs] = true
		if _, err := os.Stat(abs); err == nil {
			if err := godotenv.Load(abs); err != nil {
				log.Printf("Failed to load .env from %s: %v", abs, err)
			} else {
				log.Printf("Loaded .env from %s", abs)
			}
		}
	}
}
