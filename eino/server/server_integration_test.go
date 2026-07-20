package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer 构造一个不加载磁盘配置 / RAG 的最小 Server，
// 仅用于 HTTP 路由与中间件（CORS / 方法限制 / 错误格式）的集成测试。
// 权限相关的 handler 走 agent 包内的内存 store，无需外部依赖。
func newTestServer() *httptest.Server {
	s := &Server{}
	// 补齐 New() 中设置的必要初始化项：空白名单退化为 ["*"]，
	// 否则 corsAllowOrigin 不会回显 Origin。
	s.allowedOrigins = parseCORSOrigins("")
	return httptest.NewServer(s.buildMux())
}

func decodeError(t *testing.T, body io.Reader) string {
	t.Helper()
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	return payload.Error
}

// 验证 CORS 预检（OPTIONS）返回 200 且带必要的跨域头。
func TestCORSPreflight(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/permissions/pending", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("期望 OPTIONS 预检返回 200，实际 %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("期望回显 Origin，实际 %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); got == "" {
		t.Errorf("缺少 Access-Control-Allow-Methods 头")
	}
}

// 验证带 Origin 的 GET 请求会回显 Access-Control-Allow-Origin。
func TestCORSGetEcho(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/permissions/pending", nil)
	req.Header.Set("Origin", "https://example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("期望回显 Origin，实际 %q", got)
	}
}

// 验证 GET /api/permissions/pending 返回 200 与合法的空权限列表。
func TestPermissionsPendingEmpty(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/permissions/pending")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("期望 200，实际 %d", resp.StatusCode)
	}
	var payload struct {
		Permissions []json.RawMessage `json:"permissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if payload.Permissions == nil {
		t.Errorf("permissions 字段不应为 null")
	}
}

// 验证 POST 缺少字段时返回 400 与 {"error":...} 格式。
func TestPermissionsResolveMissingFields(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/permissions/resolve", "application/json",
		strings.NewReader(`{"id":"x"}`))
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", resp.StatusCode)
	}
	if msg := decodeError(t, resp.Body); msg == "" {
		t.Errorf("期望返回 error 文案")
	}
}

// 验证对不存在的权限 ID 进行 resolve 时返回 400 与错误文案。
func TestPermissionsResolveUnknownID(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	body := strings.NewReader(`{"id":"does-not-exist","decision":"approve"}`)
	resp, err := http.Post(ts.URL+"/api/permissions/resolve", "application/json", body)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", resp.StatusCode)
	}
	if msg := decodeError(t, resp.Body); !strings.Contains(msg, "not found") {
		t.Errorf("期望 not found 错误，实际 %q", msg)
	}
}

// 验证方法限制：POST 打到仅允许 GET 的接口应返回 405。
func TestMethodNotAllowed(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/permissions/pending", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("期望 405，实际 %d", resp.StatusCode)
	}
}

// 验证非法 JSON 请求体返回 400。
func TestPermissionsResolveInvalidJSON(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/permissions/resolve", "application/json",
		strings.NewReader(`{not-json`))
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", resp.StatusCode)
	}
}
