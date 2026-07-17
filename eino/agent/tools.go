package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	localtools "eino/tools"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type WeatherInput struct {
	City string `json:"city" jsonschema_description:"城市名称，支持中文与英文，例如 北京、上海、广州、London、Tokyo"`
}

type WeatherOutput struct {
	City        string  `json:"city"`
	Region      string  `json:"region"`
	Country     string  `json:"country"`
	Temperature float64 `json:"temperature"`
	Weather     string  `json:"weather"`
	Humidity    int     `json:"humidity"`
	WindSpeed   float64 `json:"wind_speed"`
	Timezone    string  `json:"timezone"`
}

func GetWeather() (tool.BaseTool, error) {
	return utils.InferTool(
		"get_weather",
		"查询指定城市的真实实时天气（数据来自 Open-Meteo，无需密钥、全球覆盖）。当用户询问天气、温度、是否下雨、风力、湿度等问题时调用。城市名支持中英文。",
		func(ctx context.Context, input *WeatherInput) (*WeatherOutput, error) {
			if input == nil || strings.TrimSpace(input.City) == "" {
				return nil, fmt.Errorf("city is required")
			}
			city := strings.TrimSpace(input.City)
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_call",
				Name:    "get_weather",
				Message: "调用工具 get_weather: " + city,
			})
			out, err := fetchWeather(city)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:    "tool_result",
					Name:    "get_weather",
					Result:  "error: " + err.Error(),
					Message: "get_weather 失败",
				})
				return nil, err
			}
			if b, e := json.Marshal(out); e == nil {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:    "tool_result",
					Name:    "get_weather",
					Result:  string(b),
					Message: "get_weather 返回结果",
				})
			}
			return out, nil
		},
	)
}

// weatherHTTPClient 带超时的共享 HTTP 客户端，供所有联网工具复用。
var weatherHTTPClient = &http.Client{Timeout: 15 * time.Second}

func httpGet(fullURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; EinoAgent/1.0)")
	req.Header.Set("Accept", "text/html,application/json,*/*;q=0.8")
	resp, err := weatherHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, fullURL)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
}

func weatherCodeCN(code int) string {
	m := map[int]string{
		0: "晴", 1: "大致晴朗", 2: "局部多云", 3: "阴",
		45: "雾", 48: "沉积雾凇",
		51: "小毛毛雨", 53: "毛毛雨", 55: "大毛毛雨",
		56: "冻毛毛雨", 57: "强冻毛毛雨",
		61: "小雨", 63: "中雨", 65: "大雨",
		66: "冻雨", 67: "强冻雨",
		71: "小雪", 73: "中雪", 75: "大雪", 77: "雪粒",
		80: "阵雨", 81: "强阵雨", 82: "暴雨",
		85: "阵雪", 86: "强阵雪",
		95: "雷阵雨", 96: "雷阵雨伴小冰雹", 99: "雷阵雨伴大冰雹",
	}
	if v, ok := m[code]; ok {
		return v
	}
	return "未知"
}

type geoResponse struct {
	Results []struct {
		Name     string  `json:"name"`
		Latitude float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Country  string  `json:"country"`
		Admin1   string  `json:"admin1"`
		Timezone string  `json:"timezone"`
	} `json:"results"`
}

type forecastResponse struct {
	Current struct {
		Temperature float64 `json:"temperature_2m"`
		Humidity    int     `json:"relative_humidity_2m"`
		WeatherCode int     `json:"weather_code"`
		WindSpeed   float64 `json:"wind_speed_10m"`
	} `json:"current"`
}

func fetchWeather(city string) (*WeatherOutput, error) {
	geoURL := "https://geocoding-api.open-meteo.com/v1/search?count=1&language=zh&format=json&name=" + url.QueryEscape(city)
	geoBody, err := httpGet(geoURL)
	if err != nil {
		return nil, err
	}
	var geo geoResponse
	if err := json.Unmarshal(geoBody, &geo); err != nil || len(geo.Results) == 0 {
		return nil, fmt.Errorf("未找到城市: %s", city)
	}
	g := geo.Results[0]
	fcURL := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&current=temperature_2m,relative_humidity_2m,weather_code,wind_speed_10m&timezone=auto", g.Latitude, g.Longitude)
	fcBody, err := httpGet(fcURL)
	if err != nil {
		return nil, err
	}
	var fc forecastResponse
	if err := json.Unmarshal(fcBody, &fc); err != nil {
		return nil, fmt.Errorf("解析天气数据失败: %w", err)
	}
	return &WeatherOutput{
		City:        g.Name,
		Region:      g.Admin1,
		Country:     g.Country,
		Temperature: fc.Current.Temperature,
		Weather:     weatherCodeCN(fc.Current.WeatherCode),
		Humidity:    fc.Current.Humidity,
		WindSpeed:   fc.Current.WindSpeed,
		Timezone:    g.Timezone,
	}, nil
}

type CalculatorInput struct {
	Expression string `json:"expression" jsonschema_description:"要计算的数学表达式，例如 2+3*4"`
}

func GetCalculator() (tool.BaseTool, error) {
	return utils.InferTool(
		"calculator",
		"计算数学表达式，支持加减乘除",
		func(ctx context.Context, input *CalculatorInput) (string, error) {
			args, _ := json.Marshal(input)
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:      "tool_call",
				Name:      "calculator",
				Arguments: string(args),
				Message:   "调用工具 calculator",
			})
			result := localtools.CallTool("calculator", string(args))
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_result",
				Name:    "calculator",
				Result:  result,
				Message: "calculator 返回结果",
			})
			return result, nil
		},
	)
}

func GetCurrentTime() (tool.BaseTool, error) {
	return utils.InferTool(
		"get_current_time",
		"获取当前的日期、时间和星期几（服务器所在时区，通常为东八区/北京时间）。当用户询问现在几点、今天几号、星期几、当前日期、现在是什么时候等任何与时间相关的问题时，务必先调用本工具获取准确时间，不要凭记忆或训练知识回答。",
		func(ctx context.Context, _ *struct{}) (string, error) {
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_call",
				Name:    "get_current_time",
				Message: "调用工具 get_current_time",
			})
			now := time.Now()
			cst := time.FixedZone("CST", 8*3600)
			nowCST := now.In(cst)
			weekdayCN := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}[int(nowCST.Weekday())]
			out := fmt.Sprintf("%s %s（东八区/北京时间）", nowCST.Format("2006-01-02 15:04:05"), weekdayCN)
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_result",
				Name:    "get_current_time",
				Result:  out,
				Message: "get_current_time 返回结果",
			})
			return out, nil
		},
	)
}

// ============================================================
// 联网工具：真实搜索与网页抓取
// ============================================================

type WebSearchInput struct {
	Query string `json:"query" jsonschema_description:"搜索关键词"`
	Count int    `json:"count" jsonschema_description:"返回结果条数，默认 5，最多 10"`
}

func GetWebSearch() (tool.BaseTool, error) {
	return utils.InferTool(
		"web_search",
		"通过 Bing 进行真实联网搜索，返回相关网页的标题、链接与摘要。当用户需要查找实时资讯、新闻、最新数据、外部资料等互联网信息时调用。",
		func(ctx context.Context, input *WebSearchInput) (string, error) {
			if input == nil || strings.TrimSpace(input.Query) == "" {
				return "", fmt.Errorf("query is required")
			}
			count := input.Count
			if count <= 0 {
				count = 5
			}
			if count > 10 {
				count = 10
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_call",
				Name:    "web_search",
				Message: "调用工具 web_search: " + input.Query,
			})
			out, err := duckDuckGoSearch(input.Query, count)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:    "tool_result",
					Name:    "web_search",
					Result:  "error: " + err.Error(),
					Message: "web_search 失败",
				})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_result",
				Name:    "web_search",
				Result:  truncateRunes(out, 500),
				Message: "web_search 返回结果",
			})
			return out, nil
		},
	)
}

func duckDuckGoSearch(query string, count int) (string, error) {
	return bingSearch(query, count)
}

// bingSearch 通过 cn.bing.com 进行真实联网搜索（DuckDuckGo 在本网络被屏蔽）。
func bingSearch(query string, count int) (string, error) {
	searchURL := "https://cn.bing.com/search?q=" + url.QueryEscape(query) + "&setlang=zh-CN&cc=CN"
	body, err := httpGet(searchURL)
	if err != nil {
		// 失败时回退到国际版 Bing
		searchURL = "https://www.bing.com/search?q=" + url.QueryEscape(query)
		body, err = httpGet(searchURL)
		if err != nil {
			return "", err
		}
	}
	html := string(body)
	blocks := regexp.MustCompile(`(?is)<li class="b_algo"[^>]*>(.*?)</li>`).FindAllStringSubmatch(html, -1)
	if len(blocks) == 0 {
		return "未找到与「" + query + "」相关的搜索结果。", nil
	}
	n := count
	if n > len(blocks) {
		n = len(blocks)
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		block := blocks[i][1]
		// 优先从 <h2> 内取标题与链接（避免误取站点域名 cite 链接）
		link := ""
		title := ""
		if m := regexp.MustCompile(`(?is)<h2>\s*<a[^>]+href="(https?://[^"]+)"[^>]*>(.*?)</a>`).FindStringSubmatch(block); len(m) > 2 {
			link = m[1]
			title = strings.TrimSpace(decodeEntities(stripTags(m[2])))
		}
		if link == "" {
			if m := regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]+)"[^>]*>(.*?)</a>`).FindStringSubmatch(block); len(m) > 2 {
				link = m[1]
				title = strings.TrimSpace(decodeEntities(stripTags(m[2])))
			}
		}
		if link == "" {
			continue
		}
		// 摘要：块内第一个 <p> 的文本
		snippet := ""
		if m := regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`).FindStringSubmatch(block); len(m) > 1 {
			snippet = strings.TrimSpace(decodeEntities(stripTags(m[1])))
		}
		fmt.Fprintf(&b, "%d. %s\n   URL: %s\n   %s\n\n", i+1, title, link, snippet)
	}
	res := strings.TrimSpace(b.String())
	if res == "" {
		return "未找到与「" + query + "」相关的搜索结果。", nil
	}
	return res, nil
}

type FetchURLInput struct {
	URL      string `json:"url" jsonschema_description:"要抓取的网页地址，需以 http:// 或 https:// 开头"`
	MaxChars int    `json:"max_chars" jsonschema_description:"返回正文最大字符数，默认 4000，最多 20000"`
}

func GetFetchURL() (tool.BaseTool, error) {
	return utils.InferTool(
		"fetch_url",
		"抓取指定网址的网页内容并抽取可读正文（自动去除脚本/样式/标签），返回纯文本供阅读与分析。当用户需要阅读某个网页、博客、文档或获取页面具体内容时调用。",
		func(ctx context.Context, input *FetchURLInput) (string, error) {
			if input == nil || !strings.HasPrefix(strings.ToLower(input.URL), "http") {
				return "", fmt.Errorf("a valid http(s) URL is required")
			}
			maxChars := input.MaxChars
			if maxChars <= 0 {
				maxChars = 4000
			}
			if maxChars > 20000 {
				maxChars = 20000
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_call",
				Name:    "fetch_url",
				Message: "调用工具 fetch_url: " + input.URL,
			})
			out, err := fetchURLText(input.URL, maxChars)
			if err != nil {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:    "tool_result",
					Name:    "fetch_url",
					Result:  "error: " + err.Error(),
					Message: "fetch_url 失败",
				})
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "tool_result",
				Name:    "fetch_url",
				Result:  truncateRunes(out, 500),
				Message: "fetch_url 返回结果",
			})
			return out, nil
		},
	)
}

func fetchURLText(rawURL string, maxChars int) (string, error) {
	body, err := httpGet(rawURL)
	if err != nil {
		return "", err
	}
	contentType := http.DetectContentType(body)
	text := string(body)
	if strings.Contains(contentType, "html") || strings.Contains(strings.ToLower(text), "<html") {
		text = htmlToText(text)
	} else {
		text = decodeEntities(text)
	}
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) > maxChars {
		text = string(runes[:maxChars]) + "\n... (已截断)"
	}
	if text == "" {
		return "（页面无可读取的正文内容）", nil
	}
	return text, nil
}

var (
	reScript   = regexp.MustCompile(`(?is)<script.*?</script>`)
	reStyle    = regexp.MustCompile(`(?is)<style.*?</style>`)
	reComment  = regexp.MustCompile(`(?is)<!--.*?-->`)
	reTag      = regexp.MustCompile(`(?is)<[^>]+>`)
	reSpace    = regexp.MustCompile(`[ \t]+`)
	reNewlines = regexp.MustCompile(`\n{3,}`)
)

func htmlToText(html string) string {
	html = reScript.ReplaceAllString(html, " ")
	html = reStyle.ReplaceAllString(html, " ")
	html = reComment.ReplaceAllString(html, " ")
	html = reTag.ReplaceAllString(html, " ")
	html = decodeEntities(html)
	html = reSpace.ReplaceAllString(html, " ")
	html = reNewlines.ReplaceAllString(html, "\n\n")
	return strings.TrimSpace(html)
}

func stripTags(s string) string {
	return strings.TrimSpace(reTag.ReplaceAllString(s, ""))
}

func decodeEntities(s string) string {
	repl := map[string]string{
		"&amp;":   "&",
		"&lt;":    "<",
		"&gt;":    ">",
		"&quot;":  "\"",
		"&apos;":  "'",
		"&nbsp;":  " ",
		"&ensp;":  " ",
		"&emsp;":  " ",
		"&thinsp;": " ",
		"&hellip;": "…",
		"&middot;": "·",
		"&mdash;":  "—",
		"&ndash;":  "–",
		"&#39;":    "'",
		"&#x27;":   "'",
	}
	for k, v := range repl {
		s = strings.ReplaceAll(s, k, v)
	}
	// 处理数字实体 &#ddd; 与十六进制 &#xhh;
	s = regexp.MustCompile(`&#(\d+);`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`\d+`).FindString(m)
		if c, err := strconv.Atoi(sub); err == nil && c > 0 && c < 0x110000 {
			return string(rune(c))
		}
		return m
	})
	s = regexp.MustCompile(`(?i)&#x([0-9a-f]+);`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`(?i)[0-9a-f]+`).FindString(m)
		if c, err := strconv.ParseInt(sub, 16, 32); err == nil && c > 0 && c < 0x110000 {
			return string(rune(c))
		}
		return m
	})
	return s
}
