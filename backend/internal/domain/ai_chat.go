package domain

// AIScene AI 对话场景。
type AIScene string

const (
	AISceneChat     AIScene = "chat"      // 自由对话
	AISceneBuySell  AIScene = "buy_sell"  // 买卖建议
	AISceneAnalysis AIScene = "analysis"  // 盈亏分析
	AISceneAdvisor  AIScene = "advisor"   // 持仓建议
	AISceneReport   AIScene = "report"    // 报表生成（二阶段）
)

// IsValid 校验场景。
func (s AIScene) IsValid() bool {
	switch s {
	case AISceneChat, AISceneBuySell, AISceneAnalysis, AISceneAdvisor, AISceneReport:
		return true
	}
	return false
}

// === AI 消息角色（与 OpenAI 协议保持一致）===

const (
	AIRoleSystem    = "system"
	AIRoleUser      = "user"
	AIRoleAssistant = "assistant"
	AIRoleTool      = "tool"
)
