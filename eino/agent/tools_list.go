package agent

import "github.com/cloudwego/eino/components/tool"

// GetAllTools 根据配置构建工具列表。
// needTools=false 时返回空列表（纯聊天智能体）。
// computer_action 是否真正可用，还受 config.json 的 ComputerToolsEnabled 总开关约束。
func GetAllTools(needTools bool) ([]tool.BaseTool, error) {
	tools := make([]tool.BaseTool, 0, 4)
	if !needTools {
		return tools, nil
	}

	weatherTool, err := GetWeather()
	if err != nil {
		return nil, err
	}
	tools = append(tools, weatherTool)

	timeTool, err := GetCurrentTime()
	if err != nil {
		return nil, err
	}
	tools = append(tools, timeTool)

	calcTool, err := GetCalculator()
	if err != nil {
		return nil, err
	}
	tools = append(tools, calcTool)

	searchTool, err := GetWebSearch()
	if err != nil {
		return nil, err
	}
	tools = append(tools, searchTool)

	fetchTool, err := GetFetchURL()
	if err != nil {
		return nil, err
	}
	tools = append(tools, fetchTool)

	// ---- 扩展工具集（免密钥，离线/免费接口） ----
	extraFactories := []func() (tool.BaseTool, error){
		GetUnitConverter,
		GetDateCalculator,
		GetTextTools,
		GetRandomGenerator,
		GetWikipediaSummary,
		GetCurrencyConverter,
		// 扩展二：离线转换/计算 + 国内可稳定访问的在线工具
		GetJSONFormatter,
		GetBaseConverter,
		GetTimestampConverter,
		GetColorConverter,
		GetRomanNumeral,
		GetPercentageCalc,
		GetBMICalculator,
		GetRegexTester,
		GetTranslator,
		GetMyIP,
		GetDNSLookup,
		GetHTTPCheck,
		GetCryptoPrice,
		GetStockQuote,
		GetHotTrends,
		// 扩展三：二维码/短链/RSS/图书/IP/节假日 + 离线计算
		GetQRCode,
		GetURLShorten,
		GetURLExpand,
		GetRSSReader,
		GetBookSearch,
		GetIPLookup,
		GetHolidayCN,
		GetGeoDistance,
		GetMorseCode,
		GetPasswordStrength,
	}
	for _, factory := range extraFactories {
		t, err := factory()
		if err != nil {
			return nil, err
		}
		tools = append(tools, t)
	}

	// computer_action 仅在策略启用（config.json 的 computerToolsEnabled=true
	// 且配置了 allowedRoots）时才暴露给模型。否则模型可能尝试调用一个被禁用的
	// 工具，导致 chat stream error。
	if currentComputerPolicy().Enabled {
		computerTool, err := GetComputerAction()
		if err != nil {
			return nil, err
		}
		tools = append(tools, computerTool)
	}

	return tools, nil
}
