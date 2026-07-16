package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexImageGenerationStreamingCompatibilityWorksWithServerBridge(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	markCodexImageGenerationBridgeApplied(c)

	done := `{"type":"response.output_item.done","output_index":0,"item":{"id":"ig_native","type":"image_generation_call","status":"generating","result":"final-image","output_format":"png"}}`
	completed := `{"type":"response.completed","response":{"id":"resp_native","output":[{"id":"ig_native","type":"image_generation_call","status":"generating","result":"final-image","output_format":"png"}],"usage":{"input_tokens":1,"output_tokens":2}}}`
	upstream := strings.Join([]string{
		"event: response.output_item.done",
		"data: " + done,
		"",
		"event: response.completed",
		"data: " + completed,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstream)),
	}

	gateway := &OpenAIGatewayService{}
	_, err := gateway.handleStreamingResponsePassthrough(context.Background(), resp, c, &Account{ID: 42, Platform: PlatformOpenAI}, time.Now(), "", "")
	require.NoError(t, err)

	events := collectSSEDataPayloads(t, recorder.Body.String())
	doneEvent := findSSEEvent(t, events, "response.output_item.done", "")
	completedEvent := findSSEEvent(t, events, "response.completed", "")
	require.Equal(t, "completed", gjson.Get(doneEvent, "item.status").String())
	require.Equal(t, "completed", gjson.Get(completedEvent, "response.output.0.status").String())
	require.NotContains(t, recorder.Body.String(), "codex-images")
	require.NotContains(t, recorder.Body.String(), "response.output_item.added")
}

func TestCodexImageGenerationStreamingCompatibilityRestoresMissingTerminalImage(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	markCodexImageGenerationBridgeApplied(c)

	done := `{"type":"response.output_item.done","output_index":0,"item":{"id":"ig_missing_terminal","type":"image_generation_call","status":"generating","result":"final-image-base64","output_format":"png"}}`
	completed := `{"type":"response.completed","response":{"id":"resp_native","output":[],"usage":{"input_tokens":1,"output_tokens":2}}}`
	upstream := strings.Join([]string{
		"event: response.output_item.done",
		"data: " + done,
		"",
		"event: response.completed",
		"data: " + completed,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstream)),
	}

	gateway := &OpenAIGatewayService{}
	_, err := gateway.handleStreamingResponsePassthrough(context.Background(), resp, c, &Account{ID: 42, Platform: PlatformOpenAI}, time.Now(), "", "")
	require.NoError(t, err)

	events := collectSSEDataPayloads(t, recorder.Body.String())
	completedEvent := findSSEEvent(t, events, "response.completed", "")
	require.Equal(t, "image_generation_call", gjson.Get(completedEvent, "response.output.0.type").String())
	require.Equal(t, "completed", gjson.Get(completedEvent, "response.output.0.status").String())
	require.Equal(t, "final-image-base64", gjson.Get(completedEvent, "response.output.0.result").String())
	require.NotContains(t, recorder.Body.String(), "codex-images")
}

func TestCodexImageGenerationStreamingStatusNormalizationDoesNotRequireAppliedBridge(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)

	done := `{"type":"response.output_item.done","item":{"id":"ig_passthrough","type":"image_generation_call","status":"generating","result":"final-image"}}`
	upstream := "event: response.output_item.done\ndata: " + done + "\n\ndata: [DONE]\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(upstream))}

	gateway := &OpenAIGatewayService{}
	_, err := gateway.handleStreamingResponsePassthrough(context.Background(), resp, c, &Account{ID: 42, Platform: PlatformOpenAI}, time.Now(), "", "")
	require.NoError(t, err)
	events := collectSSEDataPayloads(t, recorder.Body.String())
	require.Equal(t, "completed", gjson.Get(findSSEEvent(t, events, "response.output_item.done", ""), "item.status").String())
}

func TestCodexImageGenerationNonStreamingCompatibilityWorksWithServerBridge(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	markCodexImageGenerationBridgeApplied(c)

	body := `{"id":"resp_native","output":[{"id":"ig_native","type":"image_generation_call","status":"in_progress","result":"final-image","output_format":"png"}],"usage":{"input_tokens":1,"output_tokens":2}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	gateway := &OpenAIGatewayService{}
	_, err := gateway.handleNonStreamingResponsePassthrough(context.Background(), resp, c, "", "")
	require.NoError(t, err)
	require.Equal(t, "completed", gjson.Get(recorder.Body.String(), "output.0.status").String())
	require.Len(t, gjson.Get(recorder.Body.String(), "output").Array(), 1)
	require.NotContains(t, recorder.Body.String(), "codex-images")
}

func TestCodexImageGenerationCompatibilityDoesNotForgeFailedImage(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	markCodexImageGenerationBridgeApplied(c)

	data := []byte(`{"type":"response.output_item.done","item":{"type":"image_generation_call","status":"failed","result":"partial-image"}}`)
	normalized, changed := normalizeCodexImageGenerationSSEDataForClient(c, data)
	require.False(t, changed)
	require.Equal(t, "failed", gjson.GetBytes(normalized, "item.status").String())
}

func TestCodexImageGenerationStreamingCompatibilityDoesNotRestoreFailedImage(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	markCodexImageGenerationBridgeApplied(c)

	failed := `{"type":"response.output_item.done","output_index":0,"item":{"id":"ig_failed","type":"image_generation_call","status":"failed","result":"partial-image"}}`
	completed := `{"type":"response.completed","response":{"id":"resp_native","output":[]}}`
	upstream := strings.Join([]string{
		"event: response.output_item.done",
		"data: " + failed,
		"",
		"event: response.completed",
		"data: " + completed,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(upstream))}

	gateway := &OpenAIGatewayService{}
	_, err := gateway.handleStreamingResponsePassthrough(context.Background(), resp, c, &Account{ID: 42, Platform: PlatformOpenAI}, time.Now(), "", "")
	require.NoError(t, err)
	events := collectSSEDataPayloads(t, recorder.Body.String())
	require.Empty(t, gjson.Get(findSSEEvent(t, events, "response.completed", ""), "response.output").Array())
}
