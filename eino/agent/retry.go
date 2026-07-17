package agent

import (
	"context"
	"log"
	"math/rand"
	"strings"
	"time"
)

// isRetriable 判断模型调用错误是否值得重试：
// 覆盖 Ark / OpenAI 常见的限流(429)、超时、网关 5xx、过载等可恢复错误。
func isRetriable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, hint := range []string{
		"429", "rate limit", "too many requests",
		"timeout", "timed out", "deadline exceeded",
		"500", "502", "503", "504",
		"overloaded", "internal server error", "bad gateway", "service unavailable",
		"connection reset", "connection refused", "temporarily",
	} {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}

// withRetry 以指数退避重试 op（最多 maxRetries 次），
// 仅在 isRetriable 为真时重试，并尊重 ctx 取消（避免无意义的后台重试）。
func withRetry(ctx context.Context, maxRetries int, op func() error) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return lastErr
			}
			return err
		}
		err := op()
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetriable(err) {
			return err
		}
		if attempt == maxRetries {
			return err
		}
		// 指数退避：500ms, 1s, 2s ... 叠加抖动避免惊群
		backoff := time.Duration(1<<uint(attempt)) * 500 * time.Millisecond
		jitter := time.Duration(rand.Intn(300)) * time.Millisecond
		log.Printf("模型调用第 %d 次失败，%v 后重试: %v", attempt+1, backoff, err)
		select {
		case <-ctx.Done():
			return err
		case <-time.After(backoff + jitter):
		}
	}
	return lastErr
}
