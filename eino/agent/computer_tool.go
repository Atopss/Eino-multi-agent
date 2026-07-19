package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type ComputerPolicy struct {
	Enabled         bool
	AllowedRoots    []string
	AllowCommands   bool
	AllowedCommands []string
	CommandTimeout  time.Duration
	MaxReadBytes    int64
	MaxWriteBytes   int64
	MaxListEntries  int
	RequireApproval bool
	DesktopEnabled  bool // 桌面控制开关（截图、键鼠）
	DaemonPort      int  // Python 守护进程端口，默认 9876
}

var computerPolicy = struct {
	sync.RWMutex
	value ComputerPolicy
}{}

func SetComputerPolicy(policy ComputerPolicy) {
	if policy.CommandTimeout <= 0 {
		policy.CommandTimeout = 15 * time.Second
	}
	if policy.MaxReadBytes <= 0 {
		policy.MaxReadBytes = 256 * 1024
	}
	if policy.MaxWriteBytes <= 0 {
		policy.MaxWriteBytes = 256 * 1024
	}
	if policy.MaxListEntries <= 0 {
		policy.MaxListEntries = 200
	}
	policy.AllowedRoots = normalizeRoots(policy.AllowedRoots)

	computerPolicy.Lock()
	defer computerPolicy.Unlock()
	computerPolicy.value = policy
}

type ComputerActionInput struct {
	Action  string   `json:"action" jsonschema_description:"操作类型：list_dir/read_file/write_file/open_path/run_command（文件操作）；screenshot/click/move/drag/double_click/type_text/press_key/scroll/screen_size（桌面控制）"`
	Path    string   `json:"path" jsonschema_description:"要操作的文件或目录路径。必须位于允许目录内"`
	Content string   `json:"content" jsonschema_description:"write_file 时写入的文本内容；type_text 时输入的文字"`
	Command string   `json:"command" jsonschema_description:"run_command 时要执行的程序名。默认未开启"`
	Args    []string `json:"args" jsonschema_description:"run_command 时的参数数组。默认未开启"`
	WorkDir string   `json:"work_dir" jsonschema_description:"run_command 时的工作目录，必须位于允许目录内"`
	// 桌面控制字段
	X      int    `json:"x" jsonschema_description:"X坐标（click/move/double_click 的目标位置）"`
	Y      int    `json:"y" jsonschema_description:"Y坐标（click/move/double_click 的目标位置）"`
	X1     int    `json:"x1" jsonschema_description:"拖拽起始X坐标"`
	Y1     int    `json:"y1" jsonschema_description:"拖拽起始Y坐标"`
	X2     int    `json:"x2" jsonschema_description:"拖拽终点X坐标"`
	Y2     int    `json:"y2" jsonschema_description:"拖拽终点Y坐标"`
	Button string `json:"button" jsonschema_description:"鼠标按钮：left/right/middle，默认left"`
	Key    string `json:"key" jsonschema_description:"按键名称（press_key），如 enter/escape/tab/backspace"`
	Amount int    `json:"amount" jsonschema_description:"滚动量（scroll），正数向上、负数向下，如 -3 表示向下滚3格"`
	Text   string `json:"text" jsonschema_description:"type_text 时要输入的文本"`
	Interval float64 `json:"interval" jsonschema_description:"type_text 时每个字符之间的间隔（秒），默认0.05"`
}

// ScreenshotCache 全局截图缓存，通过 HTTP 端点提供给前端。
type ScreenshotEntry struct {
	Base64 string
	Width  int
	Height int
	Time   time.Time
}

var (
	screenshotCache   = map[string]ScreenshotEntry{}
	screenshotCacheMu sync.RWMutex
	screenshotSerial  int64
)

// StoreScreenshot 把截图 base64 数据存入缓存，返回 key（如 "shot-1"）。
func StoreScreenshot(b64 string, w, h int) string {
	screenshotCacheMu.Lock()
	defer screenshotCacheMu.Unlock()
	screenshotSerial++
	key := fmt.Sprintf("shot-%d", screenshotSerial)
	screenshotCache[key] = ScreenshotEntry{Base64: b64, Width: w, Height: h, Time: time.Now()}
	// 清理超过 10 分钟的截图
	for k, v := range screenshotCache {
		if time.Since(v.Time) > 10*time.Minute {
			delete(screenshotCache, k)
		}
	}
	return key
}

// GetScreenshot 按 key 取出缓存的截图。
func GetScreenshot(key string) (ScreenshotEntry, bool) {
	screenshotCacheMu.RLock()
	defer screenshotCacheMu.RUnlock()
	e, ok := screenshotCache[key]
	return e, ok
}

func GetComputerAction() (tool.BaseTool, error) {
	return utils.InferTool(
		"computer_action",
		"电脑操作工具，可直接控制用户的Windows桌面。\n\n支持的操作：\n- 截图: screenshot\n- 键盘输入: type_text（文本，包括中文）、press_key（按键: enter/tab/escape/backspace/space/up/down/left/right）\n- 鼠标: click/double_click/move/drag（提供x,y坐标）\n- 滚动: scroll（正数向上，负数向下，-3=向下滚3格）\n- 屏幕: screen_size\n- 文件: open_path（打开文件/软件/快捷方式）、list_dir（浏览目录）、read_file、write_file\n\n截图后，工具返回中包含 \"screenshot_ready: shot-X\" 标记，你必须在回复中原样保留此标记，前端会自动展示截图给用户。\n\n系统命令默认关闭，只有白名单开启后才能运行。",
		func(ctx context.Context, input *ComputerActionInput) (string, error) {
			if input == nil {
				return "", fmt.Errorf("input is required")
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:      "tool_call",
				Name:      "computer_action",
				Arguments: `{"action":"` + input.Action + `","path":"` + input.Path + `"}`,
				Message:   "调用工具 computer_action",
			})
			policy := currentComputerPolicy()
			if !policy.Enabled {
				return "", fmt.Errorf("computer_action is disabled. Enable computerToolsEnabled and allowed roots in data/config.json first")
			}
			approved, requestID, err := requireComputerPermission(input, policy)
			if err != nil {
				return "", err
			}
			if !approved {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:    "tool_result",
					Name:    "computer_action",
					Result:  requestID,
					Message: "computer_action 等待用户授权",
				})
				return fmt.Sprintf("permission_required: %s. 已向前端发送电脑操作授权请求。用户批准后，请使用完全相同的参数再次调用 computer_action。", requestID), nil
			}
			action := strings.TrimSpace(input.Action)

			// 桌面控制动作分发
			if isDesktopAction(action) {
				if !policy.DesktopEnabled {
					return "", fmt.Errorf("desktop control is disabled. Enable desktopControl in computer settings first")
				}
				result, err := dispatchDesktopAction(ctx, action, input, policy)
				if err != nil {
					return "", err
				}
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:    "tool_result",
					Name:    "computer_action",
					Result:  limitString(result, 500),
					Message: "computer_action（桌面控制）返回结果",
				})
				return result, nil
			}

			// 文件操作分发
			var result string
			switch action {
			case "list_dir":
				result, err = computerListDir(input.Path, policy)
			case "read_file":
				result, err = computerReadFile(input.Path, policy)
			case "write_file":
				result, err = computerWriteFile(input.Path, input.Content, policy)
			case "open_path":
				result, err = computerOpenPath(ctx, input.Path, policy)
			case "run_command":
				result, err = computerRunCommand(ctx, input, policy)
			default:
				return "", fmt.Errorf("unsupported action %q", input.Action)
			}
			if err != nil {
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_result",
				Name:    "computer_action",
				Result:  limitString(result, 500),
				Message: "computer_action 返回结果",
			})
			return result, nil
		},
	)
}

func isDesktopAction(action string) bool {
	switch action {
	case "screenshot", "click", "move", "drag", "double_click", "type_text", "press_key", "scroll", "screen_size":
		return true
	}
	return false
}

// dispatchDesktopAction 通过 HTTP 调用 Python 守护进程执行桌面操作。
func dispatchDesktopAction(ctx context.Context, action string, input *ComputerActionInput, policy ComputerPolicy) (string, error) {
	port := policy.DaemonPort
	if port <= 0 {
		port = 9876
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	bodyMap := map[string]interface{}{}
	switch action {
	case "screenshot":
		// GET 不需要 body
	case "click":
		bodyMap["x"] = input.X
		bodyMap["y"] = input.Y
		bodyMap["button"] = orDefault(input.Button, "left")
	case "double_click":
		bodyMap["x"] = input.X
		bodyMap["y"] = input.Y
	case "move":
		bodyMap["x"] = input.X
		bodyMap["y"] = input.Y
	case "drag":
		bodyMap["x1"] = input.X1
		bodyMap["y1"] = input.Y1
		bodyMap["x2"] = input.X2
		bodyMap["y2"] = input.Y2
		bodyMap["button"] = orDefault(input.Button, "left")
	case "type_text":
		bodyMap["text"] = orDefault(input.Text, input.Content)
		interval := input.Interval
		if interval <= 0 {
			interval = 0.05
		}
		bodyMap["interval"] = interval
	case "press_key":
		bodyMap["key"] = input.Key
	case "scroll":
		bodyMap["amount"] = input.Amount
		if bodyMap["amount"] == 0 {
			bodyMap["amount"] = -3
		}
	case "screen_size":
		// GET 不需要 body
	default:
		return "", fmt.Errorf("unknown desktop action: %s", action)
	}

	var url string
	var req *http.Request
	var err error

	if action == "screen_size" || action == "screenshot" {
		url = fmt.Sprintf("%s/%s", baseURL, action)
		req, err = http.NewRequestWithContext(ctx, "GET", url, nil)
	} else {
		url = fmt.Sprintf("%s/%s", baseURL, action)
		jsonBody, _ := json.Marshal(bodyMap)
		req, err = http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if err != nil {
		return "", fmt.Errorf("daemon request failed: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("无法连接到桌面控制守护进程（127.0.0.1:%d）：%w。请确保已运行 eino/computer/run.bat", port, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var daemonResp struct {
		Ok     bool   `json:"ok"`
		Error  string `json:"error"`
		Base64 string `json:"base64"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Action string `json:"action"`
		X      int    `json:"x"`
		Y      int    `json:"y"`
		Key    string `json:"key"`
		Amount int    `json:"amount"`
	}
	if err := json.Unmarshal(respBody, &daemonResp); err != nil {
		return "", fmt.Errorf("daemon response parse error: %w (raw: %s)", err, string(respBody)[:min(len(respBody), 200)])
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("daemon error (%d): %s", resp.StatusCode, daemonResp.Error)
	}
	if !daemonResp.Ok && daemonResp.Error != "" {
		return "", fmt.Errorf("%s", daemonResp.Error)
	}

	// 截图特殊处理：存入缓存，返回引用链接
	if action == "screenshot" {
		key := StoreScreenshot(daemonResp.Base64, daemonResp.Width, daemonResp.Height)
		return fmt.Sprintf("screenshot_ready: %s | 尺寸: %dx%d | 截图已完成，前端将自动展示此画面。", key, daemonResp.Width, daemonResp.Height), nil
	}

	// 其他桌面操作
	switch action {
	case "click", "double_click":
		return fmt.Sprintf("%s at (%d, %d)", action, daemonResp.X, daemonResp.Y), nil
	case "move":
		return fmt.Sprintf("mouse moved to (%d, %d)", daemonResp.X, daemonResp.Y), nil
	case "drag":
		return fmt.Sprintf("dragged from (%d,%d) to (%d,%d)", input.X1, input.Y1, input.X2, input.Y2), nil
	case "type_text":
		return fmt.Sprintf("typed %d characters", len(orDefault(input.Text, input.Content))), nil
	case "press_key":
		return fmt.Sprintf("pressed key: %s", daemonResp.Key), nil
	case "scroll":
		return fmt.Sprintf("scrolled by %d", daemonResp.Amount), nil
	case "screen_size":
		return fmt.Sprintf("screen size: %dx%d", daemonResp.Width, daemonResp.Height), nil
	}
	return string(respBody), nil
}

func orDefault(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

func currentComputerPolicy() ComputerPolicy {
	computerPolicy.RLock()
	defer computerPolicy.RUnlock()
	return computerPolicy.value
}

func computerListDir(path string, policy ComputerPolicy) (string, error) {
	target, err := resolveAllowedPath(path, policy)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return "", err
	}
	limit := policy.MaxListEntries
	lines := make([]string, 0, min(len(entries), limit))
	for i, entry := range entries {
		if i >= limit {
			lines = append(lines, fmt.Sprintf("... truncated, total entries: %d", len(entries)))
			break
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		lines = append(lines, fmt.Sprintf("%s\t%s", kind, entry.Name()))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n"), nil
}

func computerReadFile(path string, policy ComputerPolicy) (string, error) {
	target, err := resolveAllowedPath(path, policy)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory")
	}
	if info.Size() > policy.MaxReadBytes {
		return "", fmt.Errorf("file too large: %d bytes, limit %d bytes", info.Size(), policy.MaxReadBytes)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	if strings.ContainsRune(string(data), '\x00') {
		return "", fmt.Errorf("binary file is not supported")
	}
	return string(data), nil
}

func computerWriteFile(path, content string, policy ComputerPolicy) (string, error) {
	if int64(len(content)) > policy.MaxWriteBytes {
		return "", fmt.Errorf("content too large: %d bytes, limit %d bytes", len(content), policy.MaxWriteBytes)
	}
	target, err := resolveAllowedPath(path, policy)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, []byte(content), 0644); err != nil {
		return "", err
	}
	return "written: " + target, nil
}

func computerOpenPath(ctx context.Context, path string, policy ComputerPolicy) (string, error) {
	target, err := resolveAllowedPath(path, policy)
	if err != nil {
		return "", err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32.exe", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", target)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	return "opened: " + target, nil
}

func computerRunCommand(ctx context.Context, input *ComputerActionInput, policy ComputerPolicy) (string, error) {
	if !policy.AllowCommands {
		return "", fmt.Errorf("run_command is disabled")
	}
	command := strings.TrimSpace(input.Command)
	if command == "" {
		return "", fmt.Errorf("command is required")
	}
	if !commandAllowed(command, policy.AllowedCommands) {
		return "", fmt.Errorf("command %q is not allowed", command)
	}
	workDir := input.WorkDir
	if workDir == "" {
		workDir = firstRoot(policy)
	}
	resolvedWorkDir, err := resolveAllowedPath(workDir, policy)
	if err != nil {
		return "", err
	}
	cmdCtx, cancel := context.WithTimeout(ctx, policy.CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, command, input.Args...)
	cmd.Dir = resolvedWorkDir
	output, err := cmd.CombinedOutput()
	if cmdCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("command timed out after %s", policy.CommandTimeout)
	}
	text := string(output)
	if len([]rune(text)) > int(policy.MaxReadBytes) {
		text = truncateRunes(text, int(policy.MaxReadBytes)) + "\n... truncated"
	}
	if err != nil {
		return text, fmt.Errorf("command failed: %w", err)
	}
	return text, nil
}

func resolveAllowedPath(path string, policy ComputerPolicy) (string, error) {
	if len(policy.AllowedRoots) == 0 {
		return "", fmt.Errorf("no allowed roots configured")
	}
	if strings.TrimSpace(path) == "" {
		path = firstRoot(policy)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	for _, root := range policy.AllowedRoots {
		if pathWithin(abs, root) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("path is outside allowed roots: %s", abs)
}

func normalizeRoots(roots []string) []string {
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if abs, err := filepath.Abs(root); err == nil {
			result = append(result, filepath.Clean(abs))
		}
	}
	return result
}

func pathWithin(path, root string) bool {
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
		root = strings.ToLower(root)
	}
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func firstRoot(policy ComputerPolicy) string {
	if len(policy.AllowedRoots) == 0 {
		return "."
	}
	return policy.AllowedRoots[0]
}

func commandAllowed(command string, allowed []string) bool {
	base := strings.ToLower(filepath.Base(command))
	for _, item := range allowed {
		allowedBase := strings.ToLower(filepath.Base(strings.TrimSpace(item)))
		if allowedBase == base && allowedBase != "" {
			return true
		}
	}
	return false
}
