package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// ============================================================
// RAGManager - RAG 管理器
// ============================================================
// 把 embedding + 向量存储 + 文档切片 组合在一起
// 支持本地文件持久化存储
// ============================================================

type RAGManager struct {
	embedder *Embedder
	store    *VectorStore
	config   SplitterConfig
	dataDir  string // 本地存储目录
	options  Options
	mu       sync.RWMutex // 保护写路径(Rebuild/Clear/loadAll)与读路径(Search/Count/...)之间的一致性，避免重建期间读到半量索引
}

type Options struct {
	MaxChunks            int
	MaxChunksPerDocument int
	ChunkSize            int
	ChunkOverlap         int
	MaxContextChars      int
	MaxDocumentChars     int
	MaxFileBytes         int64
	EmbeddingAPIMode     string
}

// NewRAGManager 创建 RAG 管理器
func NewRAGManager(apiKey, modelID, dataDir string, opts ...Options) (*RAGManager, error) {
	emb, err := NewEmbedder(apiKey, modelID)
	if err != nil {
		return nil, err
	}
	options := defaultOptions()
	if len(opts) > 0 {
		options = mergeOptions(options, opts[0])
	}
	if options.EmbeddingAPIMode != "" {
		emb.SetAPIMode(options.EmbeddingAPIMode)
	}

	// 确保目录存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	if err := os.MkdirAll(filepath.Join(dataDir, "originals"), 0755); err != nil {
		return nil, fmt.Errorf("create originals dir failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "indexes"), 0755); err != nil {
		return nil, fmt.Errorf("create indexes dir failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "failed"), 0755); err != nil {
		return nil, fmt.Errorf("create failed dir failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "tmp"), 0755); err != nil {
		return nil, fmt.Errorf("create tmp dir failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "exports"), 0755); err != nil {
		return nil, fmt.Errorf("create exports dir failed: %w", err)
	}

	splitterConfig := DefaultSplitterConfig()
	if options.ChunkSize > 0 {
		splitterConfig.ChunkSize = options.ChunkSize
	}
	if options.ChunkOverlap >= 0 && options.ChunkOverlap < splitterConfig.ChunkSize {
		splitterConfig.ChunkOverlap = options.ChunkOverlap
	}

	m := &RAGManager{
		embedder: emb,
		store:    NewVectorStore(options.MaxChunks),
		config:   splitterConfig,
		dataDir:  dataDir,
		options:  options,
	}

	// 启动时加载已有文档
	if err := m.loadAll(); err != nil {
		log.Printf("加载已有文档失败: %v", err)
	}

	return m, nil
}

func (m *RAGManager) OriginalsDir() string {
	return filepath.Join(m.dataDir, "originals")
}

func (m *RAGManager) IndexesDir() string {
	return filepath.Join(m.dataDir, "indexes")
}

func (m *RAGManager) FailedDir() string {
	return filepath.Join(m.dataDir, "failed")
}

func (m *RAGManager) TmpDir() string {
	return filepath.Join(m.dataDir, "tmp")
}

func (m *RAGManager) ExportsDir() string {
	return filepath.Join(m.dataDir, "exports")
}

func (m *RAGManager) EmbeddingStatus() EmbeddingStatus {
	if m == nil || m.embedder == nil {
		return EmbeddingStatus{LastMode: "not_initialized"}
	}
	return m.embedder.Status()
}

func (m *RAGManager) resolveOriginalsDir(dirPath string) string {
	if dirPath == "" {
		return m.OriginalsDir()
	}
	cleanDir := filepath.Clean(dirPath)
	if filepath.Base(cleanDir) == "originals" {
		return cleanDir
	}
	candidate := filepath.Join(cleanDir, "originals")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	return cleanDir
}

func defaultOptions() Options {
	return Options{
		MaxChunks:            2000,
		MaxChunksPerDocument: 400,
		ChunkSize:            500,
		ChunkOverlap:         50,
		MaxContextChars:      6000,
		MaxDocumentChars:     200000,
		MaxFileBytes:         20 * 1024 * 1024,
	}
}

func mergeOptions(base, override Options) Options {
	if override.MaxChunks > 0 {
		base.MaxChunks = override.MaxChunks
	}
	if override.MaxChunksPerDocument > 0 {
		base.MaxChunksPerDocument = override.MaxChunksPerDocument
	}
	if override.ChunkSize > 0 {
		base.ChunkSize = override.ChunkSize
	}
	if override.ChunkOverlap >= 0 {
		base.ChunkOverlap = override.ChunkOverlap
	}
	if override.MaxContextChars > 0 {
		base.MaxContextChars = override.MaxContextChars
	}
	if override.MaxDocumentChars > 0 {
		base.MaxDocumentChars = override.MaxDocumentChars
	}
	if override.MaxFileBytes > 0 {
		base.MaxFileBytes = override.MaxFileBytes
	}
	if override.EmbeddingAPIMode != "" {
		base.EmbeddingAPIMode = override.EmbeddingAPIMode
	}
	return base
}

// docRecord 文档持久化记录（包含 embedding 向量）
type docRecord struct {
	ID       string            `json:"id"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata"`
	Chunks   []string          `json:"chunks"`  // 切片后的内容
	Vectors  [][]float64       `json:"vectors"` // 对应的 embedding 向量
}

// AddDocument 添加单个文档到知识库
func (m *RAGManager) AddDocument(ctx context.Context, doc Document) error {
	if m.options.MaxDocumentChars > 0 && len([]rune(doc.Chunk)) > m.options.MaxDocumentChars {
		doc.Chunk = truncateRunes(doc.Chunk, m.options.MaxDocumentChars)
		doc.Content = doc.Chunk
	}
	// 1. 切片
	chunks := SplitDocument(doc.Chunk, m.config)
	if len(chunks) == 0 {
		return fmt.Errorf("文档切片后为空")
	}
	if m.options.MaxChunksPerDocument > 0 && len(chunks) > m.options.MaxChunksPerDocument {
		chunks = chunks[:m.options.MaxChunksPerDocument]
	}

	log.Printf("文档 %s 切成 %d 个片段", doc.ID, len(chunks))

	// 2. 并发 embedding（有界 worker 池），单块失败仅跳过该块而非整篇失败。
	const concurrency = 4
	sem := make(chan struct{}, concurrency)
	type embResult struct {
		idx    int
		chunk  string
		vector []float64
	}
	results := make([]embResult, len(chunks))
	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := 0
	for i, chunk := range chunks {
		wg.Add(1)
		go func(i int, chunk string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			vector, err := m.embedder.EmbedText(ctx, chunk)
			if err != nil {
				log.Printf("embedding 第 %d 个片段失败（跳过）: %v", i, err)
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
			results[i] = embResult{idx: i, chunk: chunk, vector: vector}
		}(i, chunk)
	}
	wg.Wait()

	succeeded := make([]string, 0, len(chunks))
	succeededVecs := make([][]float64, 0, len(chunks))
	for i := range chunks {
		if results[i].vector == nil {
			continue
		}
		succeeded = append(succeeded, results[i].chunk)
		succeededVecs = append(succeededVecs, results[i].vector)
		newDoc := Document{
			ID:       fmt.Sprintf("%s_%d", doc.ID, i),
			Content:  results[i].chunk,
			Chunk:    results[i].chunk,
			Metadata: doc.Metadata,
		}
		m.store.Add(newDoc, results[i].vector)
	}

	if len(succeeded) == 0 {
		return fmt.Errorf("文档 %s 所有片段 embedding 均失败", doc.ID)
	}
	if failed > 0 {
		log.Printf("文档 %s 有 %d/%d 个片段 embedding 失败已跳过", doc.ID, failed, len(chunks))
	}

	// 3. 保存到本地文件（仅成功片段）
	if err := m.saveDoc(doc, succeeded, succeededVecs); err != nil {
		log.Printf("保存文档到本地失败: %v", err)
	}

	log.Printf("文档 %s 已存入知识库，共 %d 个向量（跳过 %d）", doc.ID, len(succeeded), failed)
	return nil
}

// truncateRunes 按 rune（UTF-8 字符）数量安全截断字符串，避免切断多字节字符导致乱码。
func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 || len([]rune(value)) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}

// AddText 添加纯文本到知识库
func (m *RAGManager) AddText(ctx context.Context, id, text string, metadata map[string]string, owner string) error {
	if metadata == nil {
		metadata = map[string]string{}
	}
	if owner != "" {
		metadata["owner"] = owner
	}
	doc := Document{
		ID:       id,
		Content:  text,
		Chunk:    text,
		Metadata: metadata,
	}
	return m.AddDocument(ctx, doc)
}

// AddFile 读取文件并添加到知识库
func (m *RAGManager) AddFile(ctx context.Context, filePath string, owner string) error {
	if err := m.checkFileSize(filePath); err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(filePath))

	var content string
	var err error

	switch ext {
	case ".txt", ".md", ".json", ".csv", ".xml", ".html", ".log":
		// 直接读取文本
		data, e := os.ReadFile(filePath)
		if e != nil {
			return fmt.Errorf("读取文件失败: %w", e)
		}
		content = string(data)

	case ".docx":
		// 解析 Word 文档
		content, err = parseDocx(filePath)
		if err != nil {
			return fmt.Errorf("解析 Word 文档失败: %w", err)
		}

	case ".pdf":
		// 解析 PDF 文档（简单文本提取）
		content, err = parsePDF(filePath)
		if err != nil {
			return fmt.Errorf("解析 PDF 文档失败: %w", err)
		}

	case ".xlsx", ".xls":
		// 解析 Excel 文档
		content, err = parseExcel(filePath)
		if err != nil {
			return fmt.Errorf("解析 Excel 文档失败: %w", err)
		}

	case ".pptx", ".ppt":
		// 解析 PowerPoint 文档
		content, err = parsePowerPoint(filePath)
		if err != nil {
			return fmt.Errorf("解析 PowerPoint 文档失败: %w", err)
		}

	default:
		return fmt.Errorf("不支持的文件格式: %s (支持: txt, md, docx, pdf, xlsx, pptx, json, csv, xml, html, log)", ext)
	}

	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("文件内容为空")
	}

	fileName := filepath.Base(filePath)

	metadata := map[string]string{
		"source": filePath,
		"type":   "file",
		"ext":    ext,
	}
	if owner != "" {
		metadata["owner"] = owner
	}
	doc := Document{
		ID:      fileName,
		Content: content,
		Chunk:   content,
		Metadata: metadata,
	}

	return m.AddDocument(ctx, doc)
}

// AddDirectory 扫描目录，添加所有支持的文件
func (m *RAGManager) AddDirectory(ctx context.Context, dirPath string, owner string) error {
	supportedExts := map[string]bool{
		".txt": true, ".md": true, ".docx": true, ".pdf": true,
		".xlsx": true, ".xls": true, ".pptx": true, ".ppt": true,
		".csv": true, ".xml": true, ".html": true, ".log": true, ".json": true,
	}
	loaded := 0

	scanDir := m.resolveOriginalsDir(dirPath)
	err := filepath.Walk(scanDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !supportedExts[ext] {
			return nil
		}
		// 检查是否已索引（看有没有对应的 .json 记录）
		recordPath := filepath.Join(m.IndexesDir(), filepath.Base(path)+".json")
		if _, e := os.Stat(recordPath); e == nil {
			log.Printf("跳过已索引文件: %s", filepath.Base(path))
			return nil
		}
		if e := m.AddFile(ctx, path, owner); e != nil {
			log.Printf("导入文件 %s 失败: %v", filepath.Base(path), e)
		} else {
			loaded++
		}
		return nil
	})

	if loaded > 0 {
		log.Printf("从目录 %s 导入了 %d 个新文件", dirPath, loaded)
	}
	return err
}

// Search 检索最相关的文档
func (m *RAGManager) Search(ctx context.Context, query string, topK int) ([]ScoredDocument, error) {
	return m.SearchWithOptions(ctx, query, topK, SearchOptions{})
}

func (m *RAGManager) SearchWithOptions(ctx context.Context, query string, topK int, opts SearchOptions) ([]ScoredDocument, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	searchQuery := NormalizeSearchQuery(query)
	queryVector, err := m.embedder.EmbedText(ctx, searchQuery)
	if err != nil {
		return nil, fmt.Errorf("查询 embedding 失败: %w", err)
	}
	results := m.store.SearchHybridWithOptions(queryVector, searchQuery, topK, opts)
	return results, nil
}

// GetContext 根据查询返回相关文档的上下文
func (m *RAGManager) GetContext(ctx context.Context, query string, topK int) (string, error) {
	return m.GetContextWithOptions(ctx, query, topK, SearchOptions{})
}

func (m *RAGManager) GetContextWithOptions(ctx context.Context, query string, topK int, opts SearchOptions) (string, error) {
	results, err := m.SearchWithOptions(ctx, query, topK, opts)
	if err != nil {
		return "", err
	}
	return m.ContextFromResultsForQuery(results, query), nil
}

func (m *RAGManager) ContextFromResults(results []ScoredDocument) string {
	return m.ContextFromResultsForQuery(results, "")
}

func (m *RAGManager) ContextFromResultsForQuery(results []ScoredDocument, query string) string {
	if len(results) == 0 {
		return ""
	}

	var body strings.Builder
	for i, r := range results {
		if r.MatchType == "neighbor" && r.NeighborOffset < 0 && !TextHasQueryFocus(r.Document.Chunk, query) {
			continue
		}
		chunk := CleanTextForQuery(r.Document.Chunk, query)
		if chunk == "" {
			continue
		}
		if m.options.MaxContextChars > 0 && body.Len()+len(chunk) > m.options.MaxContextChars {
			remaining := m.options.MaxContextChars - body.Len()
			if remaining <= 0 {
				break
			}
			if remaining < len([]rune(chunk)) {
				chunk = truncateRunes(chunk, remaining)
			}
		}
		source := r.Document.Metadata["source"]
		if source == "" {
			source = baseDocumentID(r.Document.ID)
		}
		matchType := r.MatchType
		if matchType == "" {
			matchType = "hit"
		}
		body.WriteString(fmt.Sprintf("[%d] 文件：%s；切片：%d；类型：%s；分数：%.4f\n来源路径：%s\n内容：\n%s\n\n", i+1, SourceFileName(r.Document), ChunkIndex(r.Document), matchType, r.Score, source, chunk))
	}
	if body.Len() == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("以下是与问题相关的参考资料。hit 表示直接命中，neighbor 表示为了补全文补充的相邻切片。回答时请标注来源文件和切片号。\n\n")
	sb.WriteString(body.String())
	return sb.String()
}

// Count 返回知识库中的文档数量
func (m *RAGManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.Count()
}

func (m *RAGManager) SourceFileCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := make(map[string]bool)
	for _, doc := range m.store.GetAllDocuments() {
		source := doc.Metadata["source"]
		if source == "" || source == "upload" {
			source = baseDocumentID(doc.ID)
		}
		seen[source] = true
	}
	return len(seen)
}

func baseDocumentID(id string) string {
	idx := strings.LastIndex(id, "_")
	if idx <= 0 || idx == len(id)-1 {
		return id
	}
	if _, err := strconv.Atoi(id[idx+1:]); err != nil {
		return id
	}
	return id[:idx]
}

// GetDocuments 获取所有文档列表
func (m *RAGManager) GetDocuments() []Document {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.GetAllDocuments()
}

// SearchWithFilter 带过滤条件的搜索
func (m *RAGManager) SearchWithFilter(ctx context.Context, query string, topK int, filter map[string]string) ([]ScoredDocument, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	queryVector, err := m.embedder.EmbedText(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询 embedding 失败: %w", err)
	}
	results := m.store.SearchWithFilter(queryVector, topK, filter)
	return results, nil
}

// Clear 清空知识库
func (m *RAGManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store.Clear()
	os.RemoveAll(m.IndexesDir())
	os.MkdirAll(m.IndexesDir(), 0755)
	runtime.GC()
}

// Rebuild 重建索引：扫描目录，为缺少 vectors 的记录重新 embedding
func (m *RAGManager) Rebuild(ctx context.Context, dirPath string, owner string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 1. 先清空内存向量库
	m.store.Clear()

	// 2. 重新扫描目录下所有文件
	supportedExts := map[string]bool{
		".txt": true, ".md": true, ".docx": true, ".pdf": true,
		".xlsx": true, ".xls": true, ".pptx": true, ".ppt": true,
		".csv": true, ".xml": true, ".html": true, ".log": true, ".json": true,
	}
	os.RemoveAll(m.IndexesDir())
	os.MkdirAll(m.IndexesDir(), 0755)
	loaded := 0

	scanDir := m.resolveOriginalsDir(dirPath)
	err := filepath.Walk(scanDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !supportedExts[ext] {
			return nil
		}
		if e := m.addFileWithEmbedding(ctx, path, owner); e != nil {
			recordPath := filepath.Join(m.IndexesDir(), filepath.Base(path)+".json")
			if loadedFromRecord := m.loadRecord(recordPath); loadedFromRecord {
				log.Printf("重建索引 - %s 源文件解析失败，已使用现有索引记录恢复: %v", filepath.Base(path), e)
				loaded++
				return nil
			}
			log.Printf("重建索引 - 导入 %s 失败: %v", filepath.Base(path), e)
		} else {
			loaded++
		}
		return nil
	})

	log.Printf("重建索引完成，共 %d 个文件，%d 个向量", loaded, m.Count())
	runtime.GC()
	return loaded, err
}

// addFileWithEmbedding 读取文件并 embedding（用于重建索引）
func (m *RAGManager) addFileWithEmbedding(ctx context.Context, filePath string, owner string) error {
	if err := m.checkFileSize(filePath); err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(filePath))

	var content string
	var err error

	switch ext {
	case ".txt", ".md", ".csv", ".xml", ".html", ".log", ".json":
		data, e := os.ReadFile(filePath)
		if e != nil {
			return fmt.Errorf("读取文件失败: %w", e)
		}
		content = string(data)
	case ".docx":
		content, err = parseDocx(filePath)
		if err != nil {
			return fmt.Errorf("解析 Word 文档失败: %w", err)
		}
	case ".pdf":
		content, err = parsePDF(filePath)
		if err != nil {
			return fmt.Errorf("解析 PDF 文档失败: %w", err)
		}
	case ".xlsx", ".xls":
		content, err = parseExcel(filePath)
		if err != nil {
			return fmt.Errorf("解析 Excel 文档失败: %w", err)
		}
	case ".pptx", ".ppt":
		content, err = parsePowerPoint(filePath)
		if err != nil {
			return fmt.Errorf("解析 PowerPoint 文档失败: %w", err)
		}
	default:
		return fmt.Errorf("不支持的文件格式: %s", ext)
	}

	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("文件内容为空")
	}

	fileName := filepath.Base(filePath)
	metadata := map[string]string{
		"source": filePath,
		"type":   "file",
		"ext":    ext,
	}
	if owner != "" {
		metadata["owner"] = owner
	}
	doc := Document{
		ID:      fileName,
		Content: content,
		Chunk:   content,
		Metadata: metadata,
	}

	return m.AddDocument(ctx, doc)
}

func (m *RAGManager) loadRecord(filePath string) bool {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}
	var record docRecord
	if err := json.Unmarshal(data, &record); err != nil {
		log.Printf("解析索引记录 %s 失败: %v", filepath.Base(filePath), err)
		return false
	}
	if len(record.Chunks) == 0 || len(record.Vectors) == 0 || len(record.Chunks) != len(record.Vectors) {
		return false
	}
	if recordLooksBinary(record) {
		log.Printf("跳过二进制或损坏的索引记录: %s", filepath.Base(filePath))
		return false
	}
	for i := range record.Chunks {
		newDoc := Document{
			ID:       fmt.Sprintf("%s_%d", record.ID, i),
			Content:  record.Chunks[i],
			Chunk:    record.Chunks[i],
			Metadata: record.Metadata,
		}
		m.store.Add(newDoc, record.Vectors[i])
	}
	return true
}

func recordLooksBinary(record docRecord) bool {
	for i, chunk := range record.Chunks {
		if i >= 3 {
			break
		}
		if strings.HasPrefix(chunk, "PK\x03\x04") || strings.ContainsRune(chunk, '\x00') {
			return true
		}
	}
	return false
}

// ============================================================
// 本地文件持久化
// ============================================================

// saveDoc 保存文档到本地文件（包含 chunks 和 vectors）
func (m *RAGManager) saveDoc(doc Document, chunks []string, vectors [][]float64) error {
	record := docRecord{
		ID:       doc.ID,
		Content:  "",
		Metadata: doc.Metadata,
		Chunks:   chunks,
		Vectors:  vectors,
	}

	data, err := json.Marshal(record)
	if err != nil {
		return err
	}

	filePath := filepath.Join(m.IndexesDir(), filepath.Base(doc.ID)+".json")
	return os.WriteFile(filePath, data, 0644)
}

func (m *RAGManager) checkFileSize(filePath string) error {
	if m.options.MaxFileBytes <= 0 {
		return nil
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("读取文件信息失败: %w", err)
	}
	if info.Size() > m.options.MaxFileBytes {
		return fmt.Errorf("文件过大: %.2f MB，当前上限 %.2f MB", float64(info.Size())/1024/1024, float64(m.options.MaxFileBytes)/1024/1024)
	}
	return nil
}

// loadAll 启动时加载所有已保存的文档（直接使用已保存的 embedding 向量）
func (m *RAGManager) loadAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := os.ReadDir(m.IndexesDir())
	if err != nil {
		return err
	}

	loaded := 0
	skipped := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if m.options.MaxChunks > 0 && m.store.Count() >= m.options.MaxChunks {
			log.Printf("RAG 已达到内存切片上限 %d，剩余索引文件将在检索时跳过", m.options.MaxChunks)
			break
		}

		filePath := filepath.Join(m.IndexesDir(), entry.Name())
		_, err := os.Stat(filePath)
		if err != nil {
			log.Printf("读取文件 %s 失败: %v", entry.Name(), err)
			continue
		}

		if m.loadRecord(filePath) {
			loaded++
			continue
		}

		// 旧格式没有 vectors：跳过，不重新 embedding（避免启动时内存暴涨）
		log.Printf("跳过旧记录 %s（无 vectors，需要手动 rebuild）", entry.Name())
		skipped++
	}

	// 注意：不能在此处调用 m.Count()，因为 loadAll 已持有 m.mu 写锁，
	// 而 Count() 需要获取 m.mu 读锁，会导致同一 goroutine 死锁。
	totalVectors := m.store.Count()
	if loaded > 0 {
		log.Printf("从本地加载了 %d 个文档，共 %d 个向量", loaded, totalVectors)
	}
	if skipped > 0 {
		log.Printf("跳过 %d 个旧记录（无 vectors），请在前端点击「重建索引」", skipped)
	}
	return nil
}

// ============================================================
// Word 文档解析（.docx = zip 包 + XML）
// ============================================================

// docxBody XML 结构
