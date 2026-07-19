package agent

import (
	"github.com/cloudwego/eino/components/tool"

	"eino/agent/tools"
)

// GetAllTools 根据配置构建工具列表。
// needTools=false 时返回空列表（纯聊天智能体）。
// computer_action 是否真正可用，还受 config.json 的 ComputerToolsEnabled 总开关约束。
//
// 无状态内置工具全部来自 eino/agent/tools 子包（不依赖 Agent 内部状态）；
// 需要 Agent 内部状态的 computer_action 保留在 agent 包内。
func GetAllTools(needTools bool) ([]tool.BaseTool, error) {
	list := make([]tool.BaseTool, 0, 4)
	if !needTools {
		return list, nil
	}

	// ---- 核心工具 ----
	coreFactories := []func() (tool.BaseTool, error){
		tools.GetWeather,
		tools.GetCurrentTime,
		tools.GetCalculator,
		tools.GetWebSearch,
		tools.GetFetchURL,
	}

	// ---- 扩展工具集（免密钥，离线/免费接口） ----
	extraFactories := []func() (tool.BaseTool, error){
		tools.GetUnitConverter,
		tools.GetDateCalculator,
		tools.GetTextTools,
		tools.GetRandomGenerator,
		tools.GetWikipediaSummary,
		tools.GetCurrencyConverter,
		// 扩展二：离线转换/计算 + 国内可稳定访问的在线工具
		tools.GetJSONFormatter,
		tools.GetBaseConverter,
		tools.GetTimestampConverter,
		tools.GetColorConverter,
		tools.GetRomanNumeral,
		tools.GetPercentageCalc,
		tools.GetBMICalculator,
		tools.GetRegexTester,
		tools.GetTranslator,
		tools.GetMyIP,
		tools.GetDNSLookup,
		tools.GetHTTPCheck,
		tools.GetCryptoPrice,
		tools.GetStockQuote,
		tools.GetHotTrends,
		// 扩展三：二维码/短链/RSS/图书/IP/节假日 + 离线计算
		tools.GetQRCode,
		tools.GetURLShorten,
		tools.GetURLExpand,
		tools.GetRSSReader,
		tools.GetBookSearch,
		tools.GetIPLookup,
		tools.GetHolidayCN,
		tools.GetGeoDistance,
		tools.GetMorseCode,
		tools.GetPasswordStrength,
	}

	for _, factory := range append(coreFactories, extraFactories...) {
		t, err := factory()
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}

	// computer_action 仅在策略启用（config.json 的 computerToolsEnabled=true
	// 且配置了 allowedRoots）时才暴露给模型。否则模型可能尝试调用一个被禁用的
	// 工具，导致 chat stream error。它依赖 Agent 内部权限状态，故保留在 agent 包内。
	if currentComputerPolicy().Enabled {
		computerTool, err := GetComputerAction()
		if err != nil {
			return nil, err
		}
		list = append(list, computerTool)
	}

	return list, nil
}
