package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAcquireOpenAIImageGenerationSlotIfBound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("no binding remains backward compatible", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		require.True(t, acquireOpenAIImageGenerationSlotIfBound(c))
	})

	t.Run("bound rejection is returned", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		calls := 0
		BindOpenAIImageGenerationSlotAcquirer(c, func() bool {
			calls++
			return false
		})

		require.False(t, acquireOpenAIImageGenerationSlotIfBound(c))
		require.Equal(t, 1, calls)
	})
}

func TestOpenAIGatewayServiceShouldInjectCodexImageGenerationBridge(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func(path string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, path, nil)
		c.Request.Header.Set("User-Agent", "codex_cli_rs/0.1.0")
		c.Set("api_key", &APIKey{Group: &Group{AllowImageGeneration: true}})
		SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
		return c
	}
	svc := &OpenAIGatewayService{}
	account := &Account{Platform: PlatformOpenAI, Extra: map[string]any{"codex_image_generation_bridge": true}}

	require.True(t, svc.ShouldInjectCodexImageGenerationBridge(
		context.Background(),
		newContext("/v1/responses"),
		account,
		[]byte(`{"model":"gpt-5.4","input":"draw a cat"}`),
	))
	require.False(t, svc.ShouldInjectCodexImageGenerationBridge(
		context.Background(),
		newContext("/v1/responses"),
		account,
		[]byte(`{"model":"gpt-5.4","input":"draw a cat","tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}]}`),
	))
	require.False(t, svc.ShouldInjectCodexImageGenerationBridge(
		context.Background(),
		newContext("/v1/responses"),
		account,
		[]byte(`{"model":"gpt-5.4","input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"image_gen"}]}]}`),
	))
	localToolContext := newContext("/v1/responses")
	localToolContext.Request.Header.Set(codexLocalImageGenerationHeader, codexLocalImageGenerationMarker)
	require.False(t, svc.ShouldInjectCodexImageGenerationBridge(
		context.Background(),
		localToolContext,
		account,
		[]byte(`{"model":"gpt-5.4","input":"draw a cat"}`),
	))
	require.False(t, svc.ShouldInjectCodexImageGenerationBridge(
		context.Background(),
		newContext("/v1/responses/compact"),
		account,
		[]byte(`{"model":"gpt-5.4","input":"compact"}`),
	))
	require.False(t, svc.ShouldInjectCodexImageGenerationBridge(
		context.Background(),
		newContext("/v1/responses"),
		account,
		[]byte(`{"model":"gpt-5.3-codex-spark","input":"draw a cat"}`),
	))
}
