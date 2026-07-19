package agent

import (
	"container/list"
	"time"

	reactflow "github.com/cloudwego/eino/flow/agent/react"
)

const (
	// reactCacheCapacity 编译好的 ReAct 智能体缓存上限。
	// 模型种类有限（ark/openai 等），32 已留足余量；超出后按 LRU 淘汰最久未用者。
	reactCacheCapacity = 32
	// reactCacheTTL 单个缓存项的存活时间，到期后在下次访问时被惰性淘汰。
	reactCacheTTL = 30 * time.Minute
)

type reactCacheEntry struct {
	key    string
	agent  *reactflow.Agent
	expiry time.Time
}

// reactAgentCache 带容量上限与 TTL 的 LRU 缓存，替代原先无限增长的 map，
// 避免运行时切换模型导致内存只增不减的泄漏。
type reactAgentCache struct {
	capacity int
	ttl      time.Duration
	ll       *list.List // 队首为最近使用（MRU）
	items    map[string]*list.Element
}

func newReactAgentCache() *reactAgentCache {
	return &reactAgentCache{
		capacity: reactCacheCapacity,
		ttl:      reactCacheTTL,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
	}
}

func (c *reactAgentCache) get(key string) (*reactflow.Agent, bool) {
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*reactCacheEntry)
	if !entry.expiry.IsZero() && time.Now().After(entry.expiry) {
		c.removeElement(el)
		return nil, false
	}
	c.ll.MoveToFront(el)
	return entry.agent, true
}

func (c *reactAgentCache) add(key string, agent *reactflow.Agent) {
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		e := el.Value.(*reactCacheEntry)
		e.agent = agent
		e.expiry = time.Now().Add(c.ttl)
		return
	}
	if c.capacity > 0 && c.ll.Len() >= c.capacity {
		if back := c.ll.Back(); back != nil {
			c.removeElement(back)
		}
	}
	entry := &reactCacheEntry{key: key, agent: agent, expiry: time.Now().Add(c.ttl)}
	el := c.ll.PushFront(entry)
	c.items[key] = el
}

func (c *reactAgentCache) removeElement(el *list.Element) {
	c.ll.Remove(el)
	delete(c.items, el.Value.(*reactCacheEntry).key)
}
