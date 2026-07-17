package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func TestVerifyRealTools(t *testing.T) {
	ctx := context.Background()

	// 1) 真实天气 (Open-Meteo)
	wt, err := GetWeather()
	if err != nil {
		t.Fatalf("GetWeather: %v", err)
	}
	wIn, _ := json.Marshal(WeatherInput{City: "Tokyo"})
	wit, ok := wt.(tool.InvokableTool)
	if !ok {
		t.Fatalf("weather not invokable")
	}
	wOut, wErr := wit.InvokableRun(ctx, string(wIn))
	t.Logf("WEATHER_OUT=%s ERR=%v", wOut, wErr)

	// 2) 真实联网搜索 (Bing)
	st, err := GetWebSearch()
	if err != nil {
		t.Fatalf("GetWebSearch: %v", err)
	}
	sIn, _ := json.Marshal(WebSearchInput{Query: "人工智能 最新进展 2026", Count: 3})
	sit, ok := st.(tool.InvokableTool)
	if !ok {
		t.Fatalf("search not invokable")
	}
	sOut, sErr := sit.InvokableRun(ctx, string(sIn))
	t.Logf("SEARCH_OUT=%s ERR=%v", sOut, sErr)

	// 3) 网页抓取 (fetch_url)
	ft, err := GetFetchURL()
	if err != nil {
		t.Fatalf("GetFetchURL: %v", err)
	}
	fIn, _ := json.Marshal(FetchURLInput{URL: "https://example.com", MaxChars: 2000})
	fit, ok := ft.(tool.InvokableTool)
	if !ok {
		t.Fatalf("fetch not invokable")
	}
	fOut, fErr := fit.InvokableRun(ctx, string(fIn))
	t.Logf("FETCH_OUT=%s ERR=%v", fOut, fErr)
}
