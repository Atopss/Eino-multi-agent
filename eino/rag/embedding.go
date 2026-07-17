package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/cloudwego/eino/components/embedding"
)

// ============================================================
// Embedder - 实现 eino embedding.Embedder 接口
// 直接调 Ark HTTP API，绕过 eino-ext SDK 的内存泄漏
// ============================================================

var _ embedding.Embedder = (*Embedder)(nil)

// dimMismatchOnce 用于“维度不一致”告警仅打印一次，避免刷屏。
var dimMismatchOnce sync.Once

type Embedder struct {
	apiKey             string
	modelID            string
	endpoint           string
	multimodalEndpoint string
	apiMode            string
	multimodalVariant  int
	client             *http.Client
	allowLocalFallback bool
	remoteMu           sync.RWMutex
	remoteDisabledUntil time.Time // 冷却截止时间；零值表示远程启用
	dim                int        // 进程内统一向量维度，首次成功 embedding 后锁定
	lastMode           string
	lastError          string
	remoteCalls        int
	localFallbackCalls int
}

func NewEmbedder(apiKey, modelID string) (*Embedder, error) {
	log.Printf("创建 Embedder: modelID=%s (HTTP直连)", modelID)
	return &Embedder{
		apiKey:             apiKey,
		modelID:            modelID,
		endpoint:           "https://ark.cn-beijing.volces.com/api/v3/embeddings",
		multimodalEndpoint: "https://ark.cn-beijing.volces.com/api/v3/embeddings/multimodal",
		apiMode:            "auto",
		client:             &http.Client{Timeout: 60 * time.Second},
		allowLocalFallback: os.Getenv("RAG_LOCAL_EMBEDDING_FALLBACK") != "false",
		lastMode:           "not_used",
	}, nil
}

func (e *Embedder) SetAPIMode(mode string) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		mode = "auto"
	}
	e.remoteMu.Lock()
	defer e.remoteMu.Unlock()
	e.apiMode = mode
}

func (e *Embedder) SetLocalFallback(allow bool) {
	e.remoteMu.Lock()
	defer e.remoteMu.Unlock()
	e.allowLocalFallback = allow
}

type EmbeddingStatus struct {
	ModelID              string `json:"modelId"`
	APIBase              string `json:"apiBase"`
	APIMode              string `json:"apiMode"`
	LastMode             string `json:"lastMode"`
	LastError            string `json:"lastError"`
	RemoteDisabled       bool   `json:"remoteDisabled"`
	LocalFallbackEnabled bool   `json:"localFallbackEnabled"`
	RemoteCalls          int    `json:"remoteCalls"`
	LocalFallbackCalls    int    `json:"localFallbackCalls"`
}

func (e *Embedder) Status() EmbeddingStatus {
	e.remoteMu.RLock()
	defer e.remoteMu.RUnlock()
	return EmbeddingStatus{
		ModelID:              e.modelID,
		APIBase:              e.currentEndpointLocked(),
		APIMode:              e.apiMode,
		LastMode:             e.lastMode,
		LastError:            e.lastError,
		RemoteDisabled:       time.Now().Before(e.remoteDisabledUntil),
		LocalFallbackEnabled: e.allowLocalFallback,
		RemoteCalls:          e.remoteCalls,
		LocalFallbackCalls:    e.localFallbackCalls,
	}
}

// EmbedStrings 实现 eino embedding.Embedder 接口
func (e *Embedder) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	for i := range texts {
		if len(texts[i]) > 8000 {
			texts[i] = texts[i][:8000]
		}
	}
	results := make([][]float64, len(texts))
	for i, text := range texts {
		vector, usedRemote, err := e.embedOne(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embedding 第 %d 条失败: %w", i, err)
		}
		_ = usedRemote
		results[i] = vector
	}
	return results, nil
}

// embedOne 返回单条文本的向量，并保证其长度恒等于进程内锁定的 dim，
// 从而杜绝“本地兜底(256) 与远程(如1024) 维度不一致 → 相似度全 0 → 检索静默失效”。
func (e *Embedder) embedOne(ctx context.Context, text string) ([]float64, bool, error) {
	if e.shouldUseRemote() {
		v, err := e.callAPI(ctx, text)
		if err == nil {
			e.recordRemoteSuccess()
			return e.normalizeDim(v), true, nil
		}
		e.markRemoteFailure(err)
		if !e.allowLocalFallback {
			return nil, false, err
		}
		log.Printf("远端 embedding 失败，本次使用本地轻量 fallback: %v", err)
	}
	// 本地兜底：维度与进程内锁定 dim 保持一致（默认 256），保证与库内向量同维。
	d := e.dimOrDefault()
	v := localEmbedding(text, d)
	e.lockDim(d)
	e.recordLocalFallback()
	return v, false, nil
}

// normalizeDim 将远程向量对齐到进程内锁定维度：首次成功时锁定，之后若模型维度变化则截断/补零，
// 避免与已入库向量长度不一致导致 CosineSimilarity 因长度不等静默返回 0。
func (e *Embedder) normalizeDim(v []float64) []float64 {
	e.remoteMu.Lock()
	defer e.remoteMu.Unlock()
	if e.dim == 0 {
		e.dim = len(v)
		return v
	}
	if len(v) == e.dim {
		return v
	}
	dimMismatchOnce.Do(func() {
		log.Printf("WARN: embedding 维度与已锁定维度 %d 不一致（本次 %d），已对齐以避免检索失效", e.dim, len(v))
	})
	out := make([]float64, e.dim)
	copy(out, v)
	return out
}

// dimOrDefault 返回当前锁定维度，未锁定时返回默认本地维度 256。
func (e *Embedder) dimOrDefault() int {
	e.remoteMu.RLock()
	defer e.remoteMu.RUnlock()
	if e.dim == 0 {
		return 256
	}
	return e.dim
}

// lockDim 在本地兜底首次使用时锁定维度。
func (e *Embedder) lockDim(d int) {
	e.remoteMu.Lock()
	defer e.remoteMu.Unlock()
	if e.dim == 0 {
		e.dim = d
	}
}

// Dim 返回进程内锁定的向量维度（用于状态展示与测试）。
func (e *Embedder) Dim() int {
	e.remoteMu.RLock()
	defer e.remoteMu.RUnlock()
	return e.dim
}

func (e *Embedder) shouldUseRemote() bool {
	e.remoteMu.RLock()
	defer e.remoteMu.RUnlock()
	return !time.Now().Before(e.remoteDisabledUntil) && e.apiKey != "" && e.modelID != ""
}

func (e *Embedder) currentEndpointLocked() string {
	if e.apiMode == "multimodal" {
		return e.multimodalEndpoint
	}
	return e.endpoint
}

// markRemoteFailure 记录一次远程失败并进入冷却（默认 30s），冷却结束后自动重试远程，
// 避免单次抖动导致整会话永久退化。
func (e *Embedder) markRemoteFailure(err error) {
	e.remoteMu.Lock()
	defer e.remoteMu.Unlock()
	e.remoteDisabledUntil = time.Now().Add(30 * time.Second)
	e.lastMode = "local_fallback"
	if err != nil {
		e.lastError = err.Error()
	}
}

func (e *Embedder) recordRemoteSuccess() {
	e.remoteMu.Lock()
	defer e.remoteMu.Unlock()
	e.remoteCalls++
	e.lastMode = "remote"
	e.lastError = ""
	e.remoteDisabledUntil = time.Time{}
}

func (e *Embedder) recordLocalFallback() {
	e.remoteMu.Lock()
	defer e.remoteMu.Unlock()
	e.localFallbackCalls++
	e.lastMode = "local_fallback"
}

func localEmbedding(text string, dim int) []float64 {
	if dim <= 0 {
		dim = 256
	}
	vector := make([]float64, dim)
	var previous rune
	for _, r := range text {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			continue
		}
		addFeature(vector, string(r), 1)
		if previous != 0 {
			addFeature(vector, string([]rune{previous, r}), 0.5)
		}
		previous = r
	}
	normalize(vector)
	return vector
}

func addFeature(vector []float64, feature string, weight float64) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(feature))
	idx := int(h.Sum64() % uint64(len(vector)))
	vector[idx] += weight
}

func normalize(vector []float64) {
	var norm float64
	for _, v := range vector {
		norm += v * v
	}
	if norm == 0 {
		return
	}
	norm = math.Sqrt(norm)
	for i := range vector {
		vector[i] /= norm
	}
}

func (e *Embedder) EmbedText(ctx context.Context, text string) ([]float64, error) {
	vectors, err := e.EmbedStrings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

func (e *Embedder) EmbedTexts(ctx context.Context, texts []string) ([][]float64, error) {
	return e.EmbedStrings(ctx, texts)
}

// ============================================================
// HTTP 直连 Ark Embedding API
// ============================================================

type arkRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type multimodalTextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (e *Embedder) callAPI(ctx context.Context, text string) ([]float64, error) {
	mode := e.getAPIMode()
	if mode == "multimodal" {
		return e.callMultimodalAPI(ctx, text)
	}

	reqBody := arkRequest{Model: e.modelID, Input: []string{text}}
	vector, err := e.postEmbedding(ctx, e.endpoint, reqBody)
	if err == nil {
		return vector, nil
	}
	if mode == "auto" && shouldTryMultimodal(err) {
		vector, multiErr := e.callMultimodalAPI(ctx, text)
		if multiErr == nil {
			return vector, nil
		}
		return nil, fmt.Errorf("文本接口失败: %v；多模态接口失败: %w", err, multiErr)
	}
	return nil, err
}

func (e *Embedder) getAPIMode() string {
	e.remoteMu.RLock()
	defer e.remoteMu.RUnlock()
	return e.apiMode
}

func (e *Embedder) setAPIMode(mode string, variant int) {
	e.remoteMu.Lock()
	defer e.remoteMu.Unlock()
	e.apiMode = mode
	if variant > 0 {
		e.multimodalVariant = variant
	}
}

func shouldTryMultimodal(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "does not support this api") ||
		strings.Contains(message, "not support this api") ||
		strings.Contains(message, "unsupported") ||
		strings.Contains(message, "invalidparameter")
}

func (e *Embedder) callMultimodalAPI(ctx context.Context, text string) ([]float64, error) {
	if variant := e.getMultimodalVariant(); variant > 0 {
		vector, err := e.postEmbedding(ctx, e.multimodalEndpoint, e.multimodalBody(variant, text))
		if err == nil {
			e.setAPIMode("multimodal", variant)
			return vector, nil
		}
	}

	var lastErr error
	for variant := 1; variant <= 3; variant++ {
		vector, err := e.postEmbedding(ctx, e.multimodalEndpoint, e.multimodalBody(variant, text))
		if err == nil {
			e.setAPIMode("multimodal", variant)
			return vector, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (e *Embedder) getMultimodalVariant() int {
	e.remoteMu.RLock()
	defer e.remoteMu.RUnlock()
	return e.multimodalVariant
}

func (e *Embedder) multimodalBody(variant int, text string) interface{} {
	part := multimodalTextPart{Type: "text", Text: text}
	switch variant {
	case 1:
		return map[string]interface{}{"model": e.modelID, "input": []multimodalTextPart{part}}
	case 2:
		return map[string]interface{}{"model": e.modelID, "input": map[string]interface{}{"type": "text", "text": text}}
	default:
		return map[string]interface{}{"model": e.modelID, "input": []map[string]interface{}{{"content": []multimodalTextPart{part}}}}
	}
}

func (e *Embedder) postEmbedding(ctx context.Context, endpoint string, reqBody interface{}) ([]float64, error) {
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API返回 %d: %s", resp.StatusCode, string(body))
	}

	vector, err := parseEmbeddingResponse(body)
	if err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return vector, nil
}

func parseEmbeddingResponse(body []byte) ([]float64, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if data, ok := raw["data"].([]interface{}); ok && len(data) > 0 {
		if item, ok := data[0].(map[string]interface{}); ok {
			if vector := floatsFromAny(item["embedding"]); len(vector) > 0 {
				return vector, nil
			}
		}
	}
	if data, ok := raw["data"].(map[string]interface{}); ok {
		if vector := floatsFromAny(data["embedding"]); len(vector) > 0 {
			return vector, nil
		}
	}
	if vector := floatsFromAny(raw["embedding"]); len(vector) > 0 {
		return vector, nil
	}
	return nil, fmt.Errorf("API未返回向量")
}

func floatsFromAny(value interface{}) []float64 {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	vector := make([]float64, 0, len(items))
	for _, item := range items {
		n, ok := item.(float64)
		if !ok {
			return nil
		}
		vector = append(vector, n)
	}
	return vector
}

// ============================================================
// 向量工具函数
// ============================================================

func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
