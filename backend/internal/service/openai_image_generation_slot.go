package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
)

const openAIImageGenerationSlotAcquirerContextKey = "openai_image_generation_slot_acquirer"

type openAIImageGenerationSlotAcquirer func() bool

// ShouldInjectCodexImageGenerationBridge 返回当前请求在指定账号上
// 是否会自动注入图片工具。HTTP handler 在账号并发名额申请前调用它，避免等待
// 图片名额期间占用账号名额；Forward 再用相同判定做最终兜底。
func (s *OpenAIGatewayService) ShouldInjectCodexImageGenerationBridge(ctx context.Context, c *gin.Context, account *Account, body []byte) bool {
	if s == nil || c == nil || account == nil {
		return false
	}
	requestView := newOpenAIRequestView(body)
	if isOpenAIResponsesCompactPath(c) || isOpenAIResponsesLiteHeader(c.GetHeader(responsesLiteHeader)) || isCodexSparkModel(requestView.Model) {
		return false
	}
	isCodexCLI := openai.IsCodexOfficialClientByHeaders(c.GetHeader("User-Agent"), c.GetHeader("originator")) || (s.cfg != nil && s.cfg.Gateway.ForceCodexCLI)
	if !isCodexCLI || account.CodexImageGenerationExplicitToolPolicy() == codexImageGenerationExplicitToolPolicyStrip {
		return false
	}
	// Codex's local image_gen extension is the only path that emits a native
	// image-generation item to the Desktop/App UI. Do not shadow it with the
	// server-side Responses image_generation tool.
	if codexClientUsesLocalImageGeneration(c) || openAIRequestBodyHasCodexImageGenNamespace(body) {
		return false
	}
	apiKey := getAPIKeyFromContext(c)
	if apiKey != nil && !GroupAllowsImageGeneration(apiKey.Group) {
		return false
	}
	return s.isCodexImageGenerationBridgeEnabled(ctx, account, apiKey)
}

// BindOpenAIImageGenerationSlotAcquirer 允许转发层在确认会注入图片工具后，
// 再向处理层申请图片并发名额。回调由处理层保证幂等，账号重试或切换不会重复占用。
func BindOpenAIImageGenerationSlotAcquirer(c *gin.Context, acquire func() bool) {
	if c == nil || acquire == nil {
		return
	}
	c.Set(openAIImageGenerationSlotAcquirerContextKey, openAIImageGenerationSlotAcquirer(acquire))
}

func acquireOpenAIImageGenerationSlotIfBound(c *gin.Context) bool {
	if c == nil {
		return true
	}
	raw, ok := c.Get(openAIImageGenerationSlotAcquirerContextKey)
	if !ok || raw == nil {
		return true
	}

	switch acquire := raw.(type) {
	case openAIImageGenerationSlotAcquirer:
		return acquire()
	case func() bool:
		return acquire()
	default:
		return true
	}
}
