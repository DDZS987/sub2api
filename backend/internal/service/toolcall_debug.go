package service

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
)

const (
	toolCallDebugEnv         = "GATEWAY_TOOLCALL_DEBUG"
	toolCallDebugBodyEnv     = "GATEWAY_TOOLCALL_DEBUG_BODY"
	toolCallDebugMaxBytesEnv = "GATEWAY_TOOLCALL_DEBUG_MAX_BYTES"
)

// ToolCallDebugEnabled reports whether function/tool-call compatibility
// diagnostics are enabled. Keep it env-driven so production can toggle it
// without adding a persistent config migration.
func ToolCallDebugEnabled() bool {
	return envBool(toolCallDebugEnv)
}

func LogToolCallDebugOpenAIChat(component, stage string, body []byte, fields ...any) {
	if !ToolCallDebugEnabled() {
		return
	}
	parts := baseToolCallDebugParts(component, stage, "openai_chat", fields...)
	parts = append(parts,
		"model="+safeLogValue(gjson.GetBytes(body, "model").String()),
		"stream="+strconv.FormatBool(gjson.GetBytes(body, "stream").Bool()),
		"tools="+openAIToolsSummary(body),
		"tool_choice="+rawSummary(gjson.GetBytes(body, "tool_choice")),
		"assistant_tool_calls="+openAIAssistantToolCallsSummary(body),
		"response_tool_calls="+openAIResponseToolCallsSummary(body),
		"stream_delta_tool_calls="+openAIStreamDeltaToolCallsSummary(body),
		"finish_reason="+safeLogValue(gjson.GetBytes(body, "choices.0.finish_reason").String()),
		"message_content_empty="+strconv.FormatBool(openAIMessageContentEmpty(body)),
	)
	logToolCallDebug(parts, body)
}

func LogToolCallDebugAnthropic(component, stage string, body []byte, fields ...any) {
	if !ToolCallDebugEnabled() {
		return
	}
	parts := baseToolCallDebugParts(component, stage, "anthropic", fields...)
	parts = append(parts,
		"model="+safeLogValue(gjson.GetBytes(body, "model").String()),
		"stream="+strconv.FormatBool(gjson.GetBytes(body, "stream").Bool()),
		"tools="+anthropicToolsSummary(body),
		"tool_choice="+rawSummary(gjson.GetBytes(body, "tool_choice")),
		"content_tool_use="+anthropicContentToolUseSummary(body),
		"stop_reason="+safeLogValue(gjson.GetBytes(body, "stop_reason").String()),
	)
	logToolCallDebug(parts, body)
}

func LogToolCallDebugOpenAIResponses(component, stage string, body []byte, fields ...any) {
	if !ToolCallDebugEnabled() {
		return
	}
	parts := baseToolCallDebugParts(component, stage, "openai_responses", fields...)
	parts = append(parts,
		"model="+safeLogValue(gjson.GetBytes(body, "model").String()),
		"stream="+strconv.FormatBool(gjson.GetBytes(body, "stream").Bool()),
		"tools="+responsesToolsSummary(body),
		"tool_choice="+rawSummary(gjson.GetBytes(body, "tool_choice")),
		"output_function_calls="+responsesOutputFunctionCallsSummary(body),
		"event_type="+safeLogValue(gjson.GetBytes(body, "type").String()),
		"response_status="+safeLogValue(gjson.GetBytes(body, "response.status").String()),
	)
	logToolCallDebug(parts, body)
}

func LogToolCallDebugGemini(component, stage string, body []byte, fields ...any) {
	if !ToolCallDebugEnabled() {
		return
	}
	payload := unwrapGeminiDebugPayload(body)
	parts := baseToolCallDebugParts(component, stage, "gemini", fields...)
	parts = append(parts,
		"model="+safeLogValue(firstNonEmpty(gjson.GetBytes(body, "model").String(), gjson.GetBytes(payload, "model").String())),
		"project_set="+strconv.FormatBool(strings.TrimSpace(gjson.GetBytes(body, "project").String()) != ""),
		"tools="+geminiToolsSummary(payload),
		"tool_config="+rawSummary(gjson.GetBytes(payload, "toolConfig")),
		"function_calls="+geminiFunctionCallsSummary(payload),
		"text_parts="+geminiTextPartsSummary(payload),
		"finish_reason="+safeLogValue(gjson.GetBytes(payload, "candidates.0.finishReason").String()),
	)
	logToolCallDebug(parts, body)
}

func baseToolCallDebugParts(component, stage, protocol string, fields ...any) []string {
	parts := []string{
		"[ToolCallDebug]",
		"component=" + safeLogValue(component),
		"stage=" + safeLogValue(stage),
		"protocol=" + safeLogValue(protocol),
	}
	for i := 0; i+1 < len(fields); i += 2 {
		parts = append(parts, fmt.Sprintf("%v=%s", fields[i], safeLogValue(fmt.Sprint(fields[i+1]))))
	}
	return parts
}

func logToolCallDebug(parts []string, body []byte) {
	if envBool(toolCallDebugBodyEnv) {
		parts = append(parts, "body_preview="+safeLogValue(truncateStringForToolCallDebug(string(body))))
	}
	logger.LegacyPrintf("service.toolcall_debug", "%s", strings.Join(parts, " "))
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func toolCallDebugMaxBytes() int {
	raw := strings.TrimSpace(os.Getenv(toolCallDebugMaxBytesEnv))
	if raw == "" {
		return 4096
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 4096
	}
	if n > 65536 {
		return 65536
	}
	return n
}

func truncateStringForToolCallDebug(s string) string {
	maxBytes := toolCallDebugMaxBytes()
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "...<truncated>"
}

func safeLogValue(s string) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	if s == "" {
		return "-"
	}
	return strconv.Quote(truncateStringForToolCallDebug(s))
}

func rawSummary(r gjson.Result) string {
	if !r.Exists() {
		return "-"
	}
	if r.Raw != "" {
		return safeLogValue(r.Raw)
	}
	return safeLogValue(r.String())
}

func jsonKeysSummary(r gjson.Result) string {
	if !r.Exists() {
		return "-"
	}
	switch {
	case r.IsObject():
		keys := make([]string, 0, len(r.Map()))
		for key := range r.Map() {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return strings.Join(keys, "|")
	case r.IsArray():
		items := r.Array()
		vals := make([]string, 0, len(items))
		for _, item := range items {
			vals = append(vals, item.String())
		}
		sort.Strings(vals)
		return strings.Join(vals, "|")
	default:
		return r.String()
	}
}

func openAIToolsSummary(body []byte) string {
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() {
		return "count=0"
	}
	items := tools.Array()
	summaries := make([]string, 0, len(items))
	for i, tool := range items {
		name := firstNonEmpty(tool.Get("function.name").String(), tool.Get("name").String())
		toolType := tool.Get("type").String()
		required := jsonKeysSummary(tool.Get("function.parameters.required"))
		props := jsonKeysSummary(tool.Get("function.parameters.properties"))
		summaries = append(summaries, fmt.Sprintf("%d:%s/%s|required=%s|props=%s", i, toolType, name, required, props))
	}
	return safeLogValue(fmt.Sprintf("count=%d [%s]", len(items), strings.Join(summaries, "; ")))
}

func openAIAssistantToolCallsSummary(body []byte) string {
	var summaries []string
	messages := gjson.GetBytes(body, "messages")
	for mi, msg := range messages.Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		for ti, tc := range msg.Get("tool_calls").Array() {
			summaries = append(summaries, fmt.Sprintf("m%d.t%d:id=%s|type=%s|name=%s|args_len=%d",
				mi,
				ti,
				tc.Get("id").String(),
				tc.Get("type").String(),
				tc.Get("function.name").String(),
				len(tc.Get("function.arguments").String()),
			))
		}
	}
	if len(summaries) == 0 {
		return "count=0"
	}
	return safeLogValue(fmt.Sprintf("count=%d [%s]", len(summaries), strings.Join(summaries, "; ")))
}

func openAIResponseToolCallsSummary(body []byte) string {
	var summaries []string
	for ci, choice := range gjson.GetBytes(body, "choices").Array() {
		for ti, tc := range choice.Get("message.tool_calls").Array() {
			summaries = append(summaries, fmt.Sprintf("c%d.t%d:id=%s|type=%s|name=%s|args_keys=%s|args_len=%d",
				ci,
				ti,
				tc.Get("id").String(),
				tc.Get("type").String(),
				tc.Get("function.name").String(),
				jsonKeysSummary(parseJSONStringResult(tc.Get("function.arguments"))),
				len(tc.Get("function.arguments").String()),
			))
		}
	}
	if len(summaries) == 0 {
		return "count=0"
	}
	return safeLogValue(fmt.Sprintf("count=%d [%s]", len(summaries), strings.Join(summaries, "; ")))
}

func openAIStreamDeltaToolCallsSummary(body []byte) string {
	var summaries []string
	for ci, choice := range gjson.GetBytes(body, "choices").Array() {
		for ti, tc := range choice.Get("delta.tool_calls").Array() {
			summaries = append(summaries, fmt.Sprintf("c%d.t%d:index=%d|id=%s|type=%s|name=%s|args_delta_len=%d",
				ci,
				ti,
				tc.Get("index").Int(),
				tc.Get("id").String(),
				tc.Get("type").String(),
				tc.Get("function.name").String(),
				len(tc.Get("function.arguments").String()),
			))
		}
	}
	if len(summaries) == 0 {
		return "count=0"
	}
	return safeLogValue(fmt.Sprintf("count=%d [%s]", len(summaries), strings.Join(summaries, "; ")))
}

func openAIMessageContentEmpty(body []byte) bool {
	content := gjson.GetBytes(body, "choices.0.message.content")
	if !content.Exists() {
		return true
	}
	return strings.TrimSpace(content.String()) == ""
}

func parseJSONStringResult(r gjson.Result) gjson.Result {
	raw := strings.TrimSpace(r.String())
	if raw == "" || !gjson.Valid(raw) {
		return gjson.Result{}
	}
	return gjson.Parse(raw)
}

func anthropicToolsSummary(body []byte) string {
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() {
		return "count=0"
	}
	items := tools.Array()
	summaries := make([]string, 0, len(items))
	for i, tool := range items {
		summaries = append(summaries, fmt.Sprintf("%d:%s|required=%s|props=%s",
			i,
			tool.Get("name").String(),
			jsonKeysSummary(tool.Get("input_schema.required")),
			jsonKeysSummary(tool.Get("input_schema.properties")),
		))
	}
	return safeLogValue(fmt.Sprintf("count=%d [%s]", len(items), strings.Join(summaries, "; ")))
}

func anthropicContentToolUseSummary(body []byte) string {
	var summaries []string
	content := gjson.GetBytes(body, "content")
	for i, block := range content.Array() {
		if block.Get("type").String() != "tool_use" {
			continue
		}
		summaries = append(summaries, fmt.Sprintf("%d:id=%s|name=%s|input_keys=%s",
			i,
			block.Get("id").String(),
			block.Get("name").String(),
			jsonKeysSummary(block.Get("input")),
		))
	}
	if len(summaries) == 0 {
		return "count=0"
	}
	return safeLogValue(fmt.Sprintf("count=%d [%s]", len(summaries), strings.Join(summaries, "; ")))
}

func responsesToolsSummary(body []byte) string {
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() {
		return "count=0"
	}
	items := tools.Array()
	summaries := make([]string, 0, len(items))
	for i, tool := range items {
		toolType := tool.Get("type").String()
		name := firstNonEmpty(tool.Get("name").String(), tool.Get("function.name").String())
		required := jsonKeysSummary(firstExisting(tool.Get("parameters.required"), tool.Get("function.parameters.required")))
		props := jsonKeysSummary(firstExisting(tool.Get("parameters.properties"), tool.Get("function.parameters.properties")))
		summaries = append(summaries, fmt.Sprintf("%d:%s/%s|required=%s|props=%s", i, toolType, name, required, props))
	}
	return safeLogValue(fmt.Sprintf("count=%d [%s]", len(items), strings.Join(summaries, "; ")))
}

func responsesOutputFunctionCallsSummary(body []byte) string {
	var summaries []string
	for i, item := range gjson.GetBytes(body, "output").Array() {
		if item.Get("type").String() != "function_call" {
			continue
		}
		args := item.Get("arguments")
		summaries = append(summaries, fmt.Sprintf("%d:id=%s|call_id=%s|name=%s|args_keys=%s|args_len=%d",
			i,
			item.Get("id").String(),
			item.Get("call_id").String(),
			item.Get("name").String(),
			jsonKeysSummary(parseJSONStringResult(args)),
			len(args.String()),
		))
	}
	responseOutput := gjson.GetBytes(body, "response.output")
	for i, item := range responseOutput.Array() {
		if item.Get("type").String() != "function_call" {
			continue
		}
		args := item.Get("arguments")
		summaries = append(summaries, fmt.Sprintf("response.%d:id=%s|call_id=%s|name=%s|args_keys=%s|args_len=%d",
			i,
			item.Get("id").String(),
			item.Get("call_id").String(),
			item.Get("name").String(),
			jsonKeysSummary(parseJSONStringResult(args)),
			len(args.String()),
		))
	}
	if len(summaries) == 0 {
		return "count=0"
	}
	return safeLogValue(fmt.Sprintf("count=%d [%s]", len(summaries), strings.Join(summaries, "; ")))
}

func firstExisting(results ...gjson.Result) gjson.Result {
	for _, r := range results {
		if r.Exists() {
			return r
		}
	}
	return gjson.Result{}
}

func unwrapGeminiDebugPayload(body []byte) []byte {
	if request := gjson.GetBytes(body, "request"); request.Exists() && request.IsObject() {
		return []byte(request.Raw)
	}
	if response := gjson.GetBytes(body, "response"); response.Exists() && response.IsObject() {
		return []byte(response.Raw)
	}
	return body
}

func geminiToolsSummary(body []byte) string {
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() {
		return "count=0"
	}
	var summaries []string
	for ti, tool := range tools.Array() {
		for fi, decl := range tool.Get("functionDeclarations").Array() {
			summaries = append(summaries, fmt.Sprintf("t%d.f%d:%s|required=%s|props=%s",
				ti,
				fi,
				decl.Get("name").String(),
				jsonKeysSummary(decl.Get("parameters.required")),
				jsonKeysSummary(decl.Get("parameters.properties")),
			))
		}
		if tool.Get("googleSearch").Exists() || tool.Get("google_search").Exists() {
			summaries = append(summaries, fmt.Sprintf("t%d.google_search", ti))
		}
	}
	if len(summaries) == 0 {
		return "count=0"
	}
	return safeLogValue(fmt.Sprintf("count=%d [%s]", len(summaries), strings.Join(summaries, "; ")))
}

func geminiFunctionCallsSummary(body []byte) string {
	var summaries []string
	candidates := gjson.GetBytes(body, "candidates")
	for ci, cand := range candidates.Array() {
		for pi, part := range cand.Get("content.parts").Array() {
			fc := part.Get("functionCall")
			if !fc.Exists() {
				continue
			}
			summaries = append(summaries, fmt.Sprintf("c%d.p%d:id=%s|name=%s|args_keys=%s|args_len=%d",
				ci,
				pi,
				fc.Get("id").String(),
				fc.Get("name").String(),
				jsonKeysSummary(fc.Get("args")),
				len(fc.Get("args").Raw),
			))
		}
	}
	contents := gjson.GetBytes(body, "contents")
	for ci, content := range contents.Array() {
		for pi, part := range content.Get("parts").Array() {
			fc := part.Get("functionCall")
			if !fc.Exists() {
				continue
			}
			summaries = append(summaries, fmt.Sprintf("content%d.p%d:id=%s|name=%s|args_keys=%s|args_len=%d",
				ci,
				pi,
				fc.Get("id").String(),
				fc.Get("name").String(),
				jsonKeysSummary(fc.Get("args")),
				len(fc.Get("args").Raw),
			))
		}
	}
	if len(summaries) == 0 {
		return "count=0"
	}
	return safeLogValue(fmt.Sprintf("count=%d [%s]", len(summaries), strings.Join(summaries, "; ")))
}

func geminiTextPartsSummary(body []byte) string {
	count := 0
	var totalLen int
	for _, cand := range gjson.GetBytes(body, "candidates").Array() {
		for _, part := range cand.Get("content.parts").Array() {
			text := part.Get("text")
			if text.Exists() {
				count++
				totalLen += len(text.String())
			}
		}
	}
	if count == 0 {
		return "count=0"
	}
	return safeLogValue(fmt.Sprintf("count=%d total_len=%d", count, totalLen))
}
