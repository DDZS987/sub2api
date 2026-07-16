package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIGatewayService_OAuthPassthrough_ForceInjectsCodexImageBridge(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.1.0")
	c.Set("api_key", &APIKey{Group: &Group{AllowImageGeneration: true}})
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	imageSlotAcquireCalls := 0
	BindOpenAIImageGenerationSlotAcquirer(c, func() bool {
		imageSlotAcquireCalls++
		return true
	})

	originalBody := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"existing instructions","input":"draw a cat"}`)
	upstreamSSE := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"id":"ig_1","type":"image_generation_call","result":"final-image","size":"1024x1024"}}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_image_1","output":[{"id":"ig_1","type":"image_generation_call","result":"final-image","size":"1024x1024"}],"usage":{"input_tokens":1,"output_tokens":2}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          123,
		Name:        "acc",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
		Extra: map[string]any{
			"openai_passthrough":            true,
			"codex_image_generation_bridge": true,
		},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, originalBody)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, imageSlotAcquireCalls)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.ImageCount)
	require.True(t, gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`).Exists())
	require.Equal(t, "png", gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation").output_format`).String())
	require.Equal(t, "image_generation", gjson.GetBytes(upstream.lastBody, "tool_choice.type").String())
	require.Contains(t, gjson.GetBytes(upstream.lastBody, "instructions").String(), codexImageGenerationBridgeMarker)
	require.Contains(t, rec.Body.String(), `"type":"image_generation_call"`)
	require.Contains(t, rec.Body.String(), `"result":"final-image"`)
}

func TestOpenAIGatewayService_OAuthPassthrough_ImageNamespaceUsesCodexLocalTool(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.1.0")
	c.Set("api_key", &APIKey{Group: &Group{AllowImageGeneration: true}})
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	imageSlotAcquireCalls := 0
	BindOpenAIImageGenerationSlotAcquirer(c, func() bool {
		imageSlotAcquireCalls++
		return true
	})

	originalBody := []byte(`{
		"model":"gpt-5.4",
		"stream":true,
		"tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}],
		"tool_choice":"auto",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"请生成一张北极极光雪景图片"}]}]
	}`)
	upstreamSSE := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"id":"fc_namespace","type":"function_call","call_id":"call_namespace","name":"imagegen","namespace":"image_gen","arguments":"{\"prompt\":\"北极极光雪景\"}"}}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_namespace","output":[{"id":"fc_namespace","type":"function_call","call_id":"call_namespace","name":"imagegen","namespace":"image_gen","arguments":"{\"prompt\":\"北极极光雪景\"}"}],"usage":{"input_tokens":1,"output_tokens":2}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID: 123, Name: "acc", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-acc"},
		Extra:       map[string]any{"openai_passthrough": true, "codex_image_generation_bridge": true},
		Status:      StatusActive, Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, originalBody)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 0, imageSlotAcquireCalls)
	require.Equal(t, 0, result.ImageCount)
	require.False(t, gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`).Exists())
	require.True(t, gjson.GetBytes(upstream.lastBody, `tools.#(type=="namespace")`).Exists())
	require.Equal(t, "auto", gjson.GetBytes(upstream.lastBody, "tool_choice").String())
	require.NotContains(t, gjson.GetBytes(upstream.lastBody, "instructions").String(), codexImageGenerationBridgeMarker)

	events := collectSSEDataPayloads(t, rec.Body.String())
	require.Equal(t, "imagegen", gjson.Get(findSSEEvent(t, events, "response.output_item.done", ""), "item.name").String())
	require.Equal(t, "image_gen", gjson.Get(findSSEEvent(t, events, "response.completed", ""), "response.output.0.namespace").String())
}
