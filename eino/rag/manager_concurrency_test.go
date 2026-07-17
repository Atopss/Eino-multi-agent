package rag

import (
	"os"
	"sync"
	"testing"
	"time"
)

// TestRAGManagerLockConcurrency 验证 manager 新增的全局 RWMutex 在
// 并发读（Count / GetDocuments / SourceFileCount）与写（Clear / Rebuild）
// 之间无数据竞争。请以 `go test -race ./rag/` 运行本用例。
//
// 对应修复 B-3：此前 Rebuild/Clear 与 Search 之间缺乏全局串行化，
// 重建期间读到半量索引。现写入路径持写锁、读取路径持读锁，
// 并发读写被串行化，杜绝竞争与半量读。
func TestRAGManagerLockConcurrency(t *testing.T) {
	dir, err := os.MkdirTemp("", "rag-conc")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	m, err := NewRAGManager("", "", dir)
	if err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// 并发写路径：Clear / Rebuild 均持写锁，彼此串行。
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					m.Clear()
					_, _ = m.Rebuild(nil, dir, "")
				}
			}
		}()
	}

	// 并发读路径：Count / GetDocuments / SourceFileCount 均持读锁。
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = m.Count()
					_ = m.GetDocuments()
					_ = m.SourceFileCount()
				}
			}
		}()
	}

	<-time.After(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}
