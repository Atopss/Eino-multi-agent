package tools

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// ===================== QR 码生成 =====================

type qrInput struct {
	Text string `json:"text" jsonschema:"文本或链接内容，将生成二维码"`
	Size int    `json:"size,omitempty" jsonschema:"二维码边长像素，默认 300，范围 100-1000"`
}

func GetQRCode() (tool.BaseTool, error) {
	return utils.InferTool("qr_code", "生成二维码。把文本/链接转换为可扫描的二维码图片（使用免费 QR 服务，无需密钥）。",
		func(ctx context.Context, input qrInput) (string, error) {
			text := strings.TrimSpace(input.Text)
			if text == "" {
				return "", fmt.Errorf("text 不能为空")
			}
			size := input.Size
			if size <= 0 {
				size = 300
			}
			if size < 100 {
				size = 100
			}
			if size > 1000 {
				size = 1000
			}
			api := fmt.Sprintf("https://api.qrserver.com/v1/create-qr-code/?size=%dx%d&data=%s&margin=10",
				size, size, url.QueryEscape(text))
			_, err := httpGet(api)
			if err != nil {
				return "", err
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:     "tool",
				Name:     "qr_code",
				Arguments:    fmt.Sprintf("text=%q size=%d", text, size),
				Result:   "二维码已生成",
	
			})
			return fmt.Sprintf("二维码已生成（图片地址）：%s\n\n可直接在浏览器打开，或将该图片下载后使用。", api), nil
		})
}

// ===================== 短链生成 =====================

type urlShortenInput struct {
	URL string `json:"url" jsonschema:"要缩短的长链接（需以 http:// 或 https:// 开头）"`
}

func GetURLShorten() (tool.BaseTool, error) {
	return utils.InferTool("url_shorten", "将长链接缩短为短链接（使用免费 is.gd 服务，无需密钥）。",
		func(ctx context.Context, input urlShortenInput) (string, error) {
			u := strings.TrimSpace(input.URL)
			if u == "" {
				return "", fmt.Errorf("url 不能为空")
			}
			api := "https://is.gd/create.php?format=json&url=" + url.QueryEscape(u)
			body, err := httpGet(api)
			if err != nil {
				return "", err
			}
			var r struct {
				ShortURL string `json:"shorturl"`
				ErrMsg   string `json:"errormessage"`
			}
			if err := json.Unmarshal(body, &r); err != nil {
				return "", fmt.Errorf("解析短链接口返回失败: %w", err)
			}
			if r.ShortURL == "" {
				msg := r.ErrMsg
				if msg == "" {
					msg = "未知错误"
				}
				return "", fmt.Errorf("短链生成失败: %s", msg)
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:     "tool",
				Name:     "url_shorten",
				Arguments:    u,
				Result:   r.ShortURL,
	
			})
			return fmt.Sprintf("原链接: %s\n短链接: %s", u, r.ShortURL), nil
		})
}

// ===================== 短链还原 =====================

type urlExpandInput struct {
	URL string `json:"url" jsonschema:"要还原的短链接（如 https://is.gd/xxx）"`
}

func GetURLExpand() (tool.BaseTool, error) {
	return utils.InferTool("url_expand", "还原短链接，获取其最终指向的真实长链接（通过跟随重定向实现，无需密钥）。",
		func(ctx context.Context, input urlExpandInput) (string, error) {
			u := strings.TrimSpace(input.URL)
			if u == "" {
				return "", fmt.Errorf("url 不能为空")
			}
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				u = "https://" + u
			}
			client := &http.Client{
				Timeout: 15 * time.Second,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return nil
				},
			}
			req, err := http.NewRequest(http.MethodGet, u, nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; EinoAgent/1.0)")
			resp, err := client.Do(req)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()
			final := resp.Request.URL.String()
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:     "tool",
				Name:     "url_expand",
				Arguments:    input.URL,
				Result:   final,
	
			})
			if final == u {
				return fmt.Sprintf("该链接未重定向，最终地址与输入一致：\n%s", final), nil
			}
			return fmt.Sprintf("短链接: %s\n真实地址: %s\n（HTTP 状态码: %d）", input.URL, final, resp.StatusCode), nil
		})
}

// ===================== RSS / Atom 阅读 =====================

type rssInput struct {
	FeedURL string `json:"feed_url" jsonschema:"RSS 或 Atom 订阅源地址"`
	Limit   int    `json:"limit,omitempty" jsonschema:"返回条目数量，默认 10，最大 30"`
}

// RSS 2.0 结构
type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
}

type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Title string    `xml:"title"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

// Atom 结构（根元素为 <feed>，条目直接在 feed 下）
type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

type atomFeed struct {
	XMLName xml.Name `xml:"feed"`
	Title   string   `xml:"title"`
	Entries []struct {
		Title     string     `xml:"title"`
		Links     []atomLink `xml:"link"`
		Updated   string     `xml:"updated"`
		Published string     `xml:"published"`
		Summary   string     `xml:"summary"`
		Content   string     `xml:"content"`
	} `xml:"entry"`
}

func pickAtomLink(links []atomLink) string {
	for _, l := range links {
		if l.Rel == "" || l.Rel == "alternate" {
			return l.Href
		}
	}
	if len(links) > 0 {
		return links[0].Href
	}
	return ""
}

func GetRSSReader() (tool.BaseTool, error) {
	return utils.InferTool("rss_reader", "读取 RSS / Atom 订阅源，返回最新文章标题、链接与发布时间（无需密钥）。",
		func(ctx context.Context, input rssInput) (string, error) {
			feedURL := strings.TrimSpace(input.FeedURL)
			if feedURL == "" {
				return "", fmt.Errorf("feed_url 不能为空")
			}
			limit := input.Limit
			if limit <= 0 {
				limit = 10
			}
			if limit > 30 {
				limit = 30
			}
			body, err := httpGet(feedURL)
			if err != nil {
				return "", err
			}
			var lines []string
			count := 0

			// 先按 RSS 2.0 解析
			var feed rssFeed
			if err := xml.Unmarshal(body, &feed); err == nil && len(feed.Channel.Items) > 0 {
				if feed.Channel.Title != "" {
					lines = append(lines, fmt.Sprintf("订阅源：%s\n", feed.Channel.Title))
				}
				for _, it := range feed.Channel.Items {
					if count >= limit {
						break
					}
					lines = append(lines, fmt.Sprintf("- %s\n  链接: %s\n  时间: %s", it.Title, it.Link, it.PubDate))
					count++
				}
			} else {
				// 回退到 Atom（根元素 <feed>）
				var af atomFeed
				if err := xml.Unmarshal(body, &af); err != nil {
					return "", fmt.Errorf("解析订阅源失败（可能不是标准 RSS/Atom）: %w", err)
				}
				if af.Title != "" {
					lines = append(lines, fmt.Sprintf("订阅源：%s\n", af.Title))
				}
				for _, e := range af.Entries {
					if count >= limit {
						break
					}
					date := e.Published
					if date == "" {
						date = e.Updated
					}
					desc := e.Summary
					if desc == "" {
						desc = e.Content
					}
					lines = append(lines, fmt.Sprintf("- %s\n  链接: %s\n  时间: %s\n  %s",
						e.Title, pickAtomLink(e.Links), date, truncateRunes(desc, 120)))
					count++
				}
			}
			if count == 0 {
				return "订阅源为空或未解析到条目。", nil
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:      "tool",
				Name:      "rss_reader",
				Arguments: feedURL,
				Result:    fmt.Sprintf("%d 条", count),
			})
			return strings.Join(lines, "\n"), nil
		})
}

// ===================== 图书搜索（Open Library） =====================

type bookInput struct {
	Query string `json:"query" jsonschema:"书名或作者关键词"`
	Limit int    `json:"limit,omitempty" jsonschema:"返回数量，默认 5，最大 10"`
}

func GetBookSearch() (tool.BaseTool, error) {
	return utils.InferTool("book_search", "通过 Open Library 免费接口按书名/作者搜索图书（无需密钥）。",
		func(ctx context.Context, input bookInput) (string, error) {
			q := strings.TrimSpace(input.Query)
			if q == "" {
				return "", fmt.Errorf("query 不能为空")
			}
			limit := input.Limit
			if limit <= 0 {
				limit = 5
			}
			if limit > 10 {
				limit = 10
			}
			api := fmt.Sprintf("https://openlibrary.org/search.json?q=%s&limit=%d", url.QueryEscape(q), limit)
			body, err := httpGet(api)
			if err != nil {
				return "", err
			}
			var r struct {
				NumFound int `json:"num_found"`
				Docs     []struct {
					Title        string   `json:"title"`
					AuthorName   []string `json:"author_name"`
					FirstPublish int      `json:"first_publish_year"`
					Key          string   `json:"key"`
					Subject      []string `json:"subject"`
				} `json:"docs"`
			}
			if err := json.Unmarshal(body, &r); err != nil {
				return "", fmt.Errorf("解析图书搜索结果失败: %w", err)
			}
			if len(r.Docs) == 0 {
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:   "tool",
					Name:   "book_search",
					Arguments:  q,
					Result: "无结果",
				})
				return fmt.Sprintf("未找到与「%s」相关的图书。", q), nil
			}
			var lines []string
			for i, d := range r.Docs {
				authors := strings.Join(d.AuthorName, "、")
				if authors == "" {
					authors = "未知"
				}
				year := ""
				if d.FirstPublish > 0 {
					year = fmt.Sprintf("（%d）", d.FirstPublish)
				}
				lines = append(lines, fmt.Sprintf("%d. 《%s》%s\n   作者: %s\n   详情: https://openlibrary.org%s", i+1, d.Title, year, authors, d.Key))
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:   "tool",
				Name:   "book_search",
				Arguments:  q,
				Result: fmt.Sprintf("%d 条", len(r.Docs)),
			})
			return strings.Join(lines, "\n"), nil
		})
}

// ===================== IP 归属地查询 =====================

type ipLookupInput struct {
	IP string `json:"ip,omitempty" jsonschema:"要查询的 IP，留空则查询当前出口 IP"`
}

func GetIPLookup() (tool.BaseTool, error) {
	return utils.InferTool("ip_lookup", "查询 IP 地址归属地（国家/省/市/运营商/经纬度），使用免费 ipwho.is 接口（无需密钥）。",
		func(ctx context.Context, input ipLookupInput) (string, error) {
			u := "https://ipwho.is/"
			if strings.TrimSpace(input.IP) != "" {
				u += strings.TrimSpace(input.IP)
			}
			body, err := httpGet(u)
			if err != nil {
				return "", err
			}
			// ipwho.is 的 connection 是嵌套对象，必须用结构体正确映射，
			// 否则把对象解析成 string 会导致整个 Unmarshal 失败。
			var r struct {
				IP         string  `json:"ip"`
				City       string  `json:"city"`
				Region     string  `json:"region"`
				Country    string  `json:"country"`
				Continent  string  `json:"continent"`
				Lat        float64 `json:"latitude"`
				Lon        float64 `json:"longitude"`
				Connection struct {
					ISP string `json:"isp"`
					Org string `json:"org"`
				} `json:"connection"`
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(body, &r); err != nil {
				return "", fmt.Errorf("解析 IP 查询结果失败: %w", err)
			}
			if !r.Success {
				msg := r.Message
				if msg == "" {
					msg = "查询失败（可能是无效 IP 或接口限流）"
				}
				return "", fmt.Errorf("IP 查询失败: %s", msg)
			}
			isp := r.Connection.ISP
			if isp == "" {
				isp = r.Connection.Org
			}
			if isp == "" {
				isp = "未知"
			}
			region := strings.TrimSpace(r.Region)
			loc := strings.TrimSpace(r.City)
			addr := r.Country
			if region != "" {
				addr = region + "，" + addr
			}
			if loc != "" {
				addr = loc + "，" + addr
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:   "tool",
				Name:   "ip_lookup",
				Arguments:  input.IP,
				Result: addr,
			})
			return fmt.Sprintf("IP: %s\n归属地: %s\n运营商: %s\n经纬度: %.4f, %.4f", r.IP, addr, isp, r.Lat, r.Lon), nil
		})
}

// ===================== 中国节假日查询 =====================

type holidayInput struct {
	Date string `json:"date,omitempty" jsonschema:"查询日期 YYYY-MM-DD 或年份 YYYY，默认今天/今年"`
	Year string `json:"year,omitempty" jsonschema:"查询年份 YYYY（与 date 二选一）"`
}

func GetHolidayCN() (tool.BaseTool, error) {
	return utils.InferTool("holiday_cn", "查询中国法定节假日/调休补班（免费 timor.tech 接口）。返回指定日期是工作日、周末、节假日还是补班日。",
		func(ctx context.Context, input holidayInput) (string, error) {
			date := strings.TrimSpace(input.Date)
			year := strings.TrimSpace(input.Year)
			var api string
			if date != "" {
				if matched, _ := regexp.MatchString(`^\d{4}-\d{2}-\d{2}$`, date); !matched {
					return "", fmt.Errorf("date 需为 YYYY-MM-DD 格式")
				}
				api = "https://timor.tech/api/holiday/info/" + date
			} else if year != "" {
				api = "https://timor.tech/api/holiday/year/" + year
			} else {
				api = "https://timor.tech/api/holiday/info/" + time.Now().Format("2006-01-02")
			}
			body, err := httpGet(api)
			if err != nil {
				return "", err
			}

			// 年度查询：holiday 字段是 map[MM-DD]对象，与单日结构不同，需单独解析。
			if year != "" && date == "" {
				var yr struct {
					Code    int `json:"code"`
					Holiday map[string]struct {
						Holiday bool   `json:"holiday"`
						Name    string `json:"name"`
						Wage    int    `json:"wage"`
						Date    string `json:"date"`
						Target  string `json:"target"`
					} `json:"holiday"`
				}
				if err := json.Unmarshal(body, &yr); err != nil {
					return "", fmt.Errorf("解析年度节假日失败: %w", err)
				}
				if yr.Code != 0 {
					return "", fmt.Errorf("节假日接口返回错误码 %d", yr.Code)
				}
				if len(yr.Holiday) == 0 {
					return fmt.Sprintf("未获取到 %s 年的节假日数据。", year), nil
				}
				type hItem struct {
					date, name string
					rest       bool
					wage       int
				}
				items := make([]hItem, 0, len(yr.Holiday))
				for _, v := range yr.Holiday {
					items = append(items, hItem{date: v.Date, name: v.Name, rest: v.Holiday, wage: v.Wage})
				}
				sort.Slice(items, func(i, j int) bool { return items[i].date < items[j].date })
				var b strings.Builder
				fmt.Fprintf(&b, "【%s 年节假日与调休安排】\n", year)
				for _, it := range items {
					if it.rest {
						tag := "放假"
						if it.wage >= 3 {
							tag = "法定节假日(3 倍工资)"
						} else if it.wage == 2 {
							tag = "假期(2 倍工资)"
						}
						fmt.Fprintf(&b, "- %s %s：%s\n", it.date, it.name, tag)
					} else {
						fmt.Fprintf(&b, "- %s %s：调休补班（需上班）\n", it.date, it.name)
					}
				}
				out := strings.TrimSpace(b.String())
				appendTraceItem(ctx, ExecutionTraceItem{
					Type: "tool", Name: "holiday_cn",
					Arguments: year, Result: fmt.Sprintf("%d 条", len(items)),
				})
				return out, nil
			}

			// 单日查询（指定 date 或默认今天）
			var r struct {
				Code int `json:"code"`
				Type struct {
					Type int    `json:"type"`
					Name string `json:"name"`
					Week int    `json:"week"`
				} `json:"type"`
			}
			if err := json.Unmarshal(body, &r); err != nil {
				return "", fmt.Errorf("解析节假日结果失败: %w", err)
			}
			if r.Code != 0 {
				return "", fmt.Errorf("节假日接口返回错误码 %d", r.Code)
			}
			var kind string
			switch r.Type.Type {
			case 0:
				kind = "工作日"
			case 1:
				kind = "周末"
			case 2:
				kind = "法定节假日"
			case 3:
				kind = "节假日调休补班（需上班）"
			default:
				kind = "未知"
			}
			day := date
			if day == "" {
				day = time.Now().Format("2006-01-02")
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:      "tool",
				Name:      "holiday_cn",
				Arguments: day,
				Result:    kind,
			})
			return fmt.Sprintf("%s：%s\n名称: %s\n星期: 周%d", day, kind, r.Type.Name, r.Type.Week), nil
		})
}

// ===================== 地理距离（Haversine） =====================

type geoInput struct {
	Lat1  float64 `json:"lat1" jsonschema:"起点纬度（-90~90）"`
	Lon1  float64 `json:"lon1" jsonschema:"起点经度（-180~180）"`
	Lat2  float64 `json:"lat2" jsonschema:"终点纬度（-90~90）"`
	Lon2  float64 `json:"lon2" jsonschema:"终点经度（-180~180）"`
	Unit  string  `json:"unit,omitempty" jsonschema:"距离单位：km（默认）或 mi"`
}

func GetGeoDistance() (tool.BaseTool, error) {
	return utils.InferTool("geo_distance", "计算两个经纬度坐标之间的地表距离（Haversine 公式，离线可用）。",
		func(ctx context.Context, input geoInput) (string, error) {
			if input.Lat1 < -90 || input.Lat1 > 90 || input.Lat2 < -90 || input.Lat2 > 90 {
				return "", fmt.Errorf("纬度必须在 -90~90 之间")
			}
			if input.Lon1 < -180 || input.Lon1 > 180 || input.Lon2 < -180 || input.Lon2 > 180 {
				return "", fmt.Errorf("经度必须在 -180~180 之间")
			}
			const Rkm = 6371.0
			const Rmi = 3958.8
			toRad := func(d float64) float64 { return d * math.Pi / 180.0 }
			dLat := toRad(input.Lat2 - input.Lat1)
			dLon := toRad(input.Lon2 - input.Lon1)
			a := math.Sin(dLat/2)*math.Sin(dLat/2) +
				math.Cos(toRad(input.Lat1))*math.Cos(toRad(input.Lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
			c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
			unit := strings.ToLower(strings.TrimSpace(input.Unit))
			if unit == "" {
				unit = "km"
			}
			var dist, u string
			if unit == "mi" {
				dist = fmt.Sprintf("%.3f", Rmi*c)
				u = "英里"
			} else {
				dist = fmt.Sprintf("%.3f", Rkm*c)
				u = "公里"
			}
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:   "tool",
				Name:   "geo_distance",
				Arguments:  fmt.Sprintf("(%f,%f)->(%f,%f)", input.Lat1, input.Lon1, input.Lat2, input.Lon2),
				Result: dist + u,
			})
			return fmt.Sprintf("两点间距离: %s %s\n起点: (%.6f, %.6f)\n终点: (%.6f, %.6f)", dist, u, input.Lat1, input.Lon1, input.Lat2, input.Lon2), nil
		})
}

// ===================== 摩斯密码 =====================

type morseInput struct {
	Text string `json:"text" jsonschema:"要编码/解码的文本"`
	Mode string `json:"mode" jsonschema:"encode（文本→摩斯）或 decode（摩斯→文本），默认 encode"`
}

var morseMap = map[rune]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".", 'F': "..-.",
	'G': "--.", 'H': "....", 'I': "..", 'J': ".---", 'K': "-.-", 'L': ".-..",
	'M': "--", 'N': "-.", 'O': "---", 'P': ".--.", 'Q': "--.-", 'R': ".-.",
	'S': "...", 'T': "-", 'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-",
	'Y': "-.--", 'Z': "--..",
	'0': "-----", '1': ".----", '2': "..---", '3': "...--", '4': "....-",
	'5': ".....", '6': "-....", '7': "--...", '8': "---..", '9': "----.",
	'.': ".-.-.-", ',': "--..--", '?': "..--..", '\'': ".----.", '!': "-.-.--",
	'/': "-..-.", '(': "-.--.", ')': "-.--.-", '&': ".-...", ':': "---...",
	';': "-.-.-.", '=': "-...-", '+': ".-.-.", '-': "-....-", '_': "..--.-",
	'"': ".-..-.", '$': "...-..-", '@': ".--.-.",
}

func GetMorseCode() (tool.BaseTool, error) {
	return utils.InferTool("morse_code", "文本与摩斯密码互转（离线可用）。支持字母、数字及常见符号。",
		func(ctx context.Context, input morseInput) (string, error) {
			text := strings.TrimSpace(input.Text)
			if text == "" {
				return "", fmt.Errorf("text 不能为空")
			}
			mode := strings.ToLower(strings.TrimSpace(input.Mode))
			if mode == "" {
				mode = "encode"
			}
			if mode == "encode" {
				words := strings.Split(text, " ")
				var out []string
				for _, w := range words {
					var letters []string
					for _, ch := range strings.ToUpper(w) {
						if code, ok := morseMap[ch]; ok {
							letters = append(letters, code)
						} else if ch == ' ' {
							// 忽略
						} else {
							letters = append(letters, "?")
						}
					}
					out = append(out, strings.Join(letters, " "))
				}
				result := strings.Join(out, "   ")
				appendTraceItem(ctx, ExecutionTraceItem{Type: "tool", Name: "morse_code", Arguments: text, Result: result})
				return fmt.Sprintf("摩斯密码（词间用 3 空格分隔）：\n%s", result), nil
			}
			if mode == "decode" {
				// 构建反向 map
				rev := map[string]rune{}
				for k, v := range morseMap {
					rev[v] = k
				}
				words := strings.Split(text, "   ")
				var out []string
				for _, w := range words {
					letters := strings.Split(strings.TrimSpace(w), " ")
					var sb strings.Builder
					for _, l := range letters {
						if r, ok := rev[l]; ok {
							sb.WriteRune(r)
						} else {
							sb.WriteString("?")
						}
					}
					out = append(out, sb.String())
				}
				result := strings.Join(out, " ")
				appendTraceItem(ctx, ExecutionTraceItem{Type: "tool", Name: "morse_code", Arguments: text, Result: result})
				return fmt.Sprintf("解码结果：\n%s", result), nil
			}
			return "", fmt.Errorf("mode 仅支持 encode / decode")
		})
}

// ===================== 密码强度评估 =====================

type pwdInput struct {
	Password string `json:"password" jsonschema:"要评估强度的密码"`
}

func GetPasswordStrength() (tool.BaseTool, error) {
	return utils.InferTool("password_strength", "离线评估密码强度，给出 0-4 分（弱/中/强/很强）及改进建议。",
		func(ctx context.Context, input pwdInput) (string, error) {
			pw := input.Password
			if len(pw) == 0 {
				return "", fmt.Errorf("password 不能为空")
			}
			hasLower := regexp.MustCompile(`[a-z]`).MatchString(pw)
			hasUpper := regexp.MustCompile(`[A-Z]`).MatchString(pw)
			hasDigit := regexp.MustCompile(`[0-9]`).MatchString(pw)
			hasSym := regexp.MustCompile(`[^\w]`).MatchString(pw)
			categories := 0
			for _, b := range []bool{hasLower, hasUpper, hasDigit, hasSym} {
				if b {
					categories++
				}
			}
			uniq := map[rune]bool{}
			for _, r := range pw {
				uniq[r] = true
			}
			score := 0
			if len(pw) >= 8 {
				score++
			}
			if len(pw) >= 12 {
				score++
			}
			if categories >= 3 {
				score++
			}
			if categories == 4 && len(uniq) >= 8 {
				score++
			}
			// 降级：长度太短或全是重复字符
			if len(pw) < 6 || len(uniq) <= 1 {
				score = 0
			}
			labels := []string{"非常弱", "弱", "中等", "强", "很强"}
			label := labels[score]
			var tips []string
			if len(pw) < 8 {
				tips = append(tips, "长度建议至少 8 位（12 位以上更佳）")
			}
			if !hasLower || !hasUpper {
				tips = append(tips, "建议同时包含大小写字母")
			}
			if !hasDigit {
				tips = append(tips, "建议包含数字")
			}
			if !hasSym {
				tips = append(tips, "建议包含符号（如 !@#$%^&*）")
			}
			if len(uniq) < 8 {
				tips = append(tips, "字符种类偏少，建议使用更多不重复字符")
			}
			result := fmt.Sprintf("密码长度: %d\n字符多样性: %d/4（小写=%v 大写=%v 数字=%v 符号=%v）\n强度评分: %d/4（%s）",
				len(pw), categories, hasLower, hasUpper, hasDigit, hasSym, score, label)
			if len(tips) > 0 {
				result += "\n改进建议:\n- " + strings.Join(tips, "\n- ")
			}
			appendTraceItem(ctx, ExecutionTraceItem{Type: "tool", Name: "password_strength", Arguments: "***", Result: label})
			return result, nil
		})
}

// ===================== 文件结束 =====================
