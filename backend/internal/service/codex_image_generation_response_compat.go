package service

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type codexImageGenerationCompletedItem struct {
	id          string
	outputIndex int
	raw         json.RawMessage
}

// codexImageGenerationResponseCompatStreamState remembers completed image
// items because some ChatGPT OAuth streams omit them from the terminal
// response.completed.response.output. Codex Desktop uses that terminal output
// to build image cards, so the earlier output_item.done event alone is not
// sufficient even though it already contains the final base64 image.
type codexImageGenerationResponseCompatStreamState struct {
	itemsByID map[string]codexImageGenerationCompletedItem
}

func newCodexImageGenerationResponseCompatStreamState(c *gin.Context) *codexImageGenerationResponseCompatStreamState {
	if !codexImageGenerationResponseCompatibilityEnabled(c) {
		return nil
	}
	return &codexImageGenerationResponseCompatStreamState{itemsByID: make(map[string]codexImageGenerationCompletedItem)}
}

func (s *codexImageGenerationResponseCompatStreamState) ObserveAndPatch(data []byte) ([]byte, bool) {
	if s == nil || len(data) == 0 {
		return data, false
	}
	switch gjson.GetBytes(data, "type").String() {
	case "response.output_item.done":
		s.observeCompletedItem(data)
		return data, false
	case "response.completed":
		return s.patchTerminalOutput(data)
	default:
		return data, false
	}
}

func (s *codexImageGenerationResponseCompatStreamState) observeCompletedItem(data []byte) {
	item := gjson.GetBytes(data, "item")
	if !item.IsObject() || item.Get("type").String() != "image_generation_call" ||
		strings.ToLower(strings.TrimSpace(item.Get("status").String())) != "completed" ||
		strings.TrimSpace(item.Get("result").String()) == "" {
		return
	}
	id := strings.TrimSpace(item.Get("id").String())
	if id == "" || item.Raw == "" {
		return
	}
	outputIndex := int(gjson.GetBytes(data, "output_index").Int())
	s.itemsByID[id] = codexImageGenerationCompletedItem{
		id:          id,
		outputIndex: outputIndex,
		raw:         append(json.RawMessage(nil), item.Raw...),
	}
}

func (s *codexImageGenerationResponseCompatStreamState) patchTerminalOutput(data []byte) ([]byte, bool) {
	if len(s.itemsByID) == 0 {
		return data, false
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return data, false
	}
	response, ok := root["response"].(map[string]any)
	if !ok {
		return data, false
	}
	output, _ := response["output"].([]any)
	existing := make(map[string]int, len(output))
	for index, rawItem := range output {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		if id := strings.TrimSpace(firstNonEmptyString(item["id"])); id != "" {
			existing[id] = index
		}
	}

	completed := make([]codexImageGenerationCompletedItem, 0, len(s.itemsByID))
	for _, item := range s.itemsByID {
		completed = append(completed, item)
	}
	sort.SliceStable(completed, func(i, j int) bool { return completed[i].outputIndex < completed[j].outputIndex })

	changed := false
	for _, cached := range completed {
		var item map[string]any
		if err := json.Unmarshal(cached.raw, &item); err != nil {
			continue
		}
		item["status"] = "completed"
		if index, found := existing[cached.id]; found {
			current, _ := output[index].(map[string]any)
			if strings.TrimSpace(firstNonEmptyString(current["result"])) == "" {
				output[index] = item
				changed = true
			}
			continue
		}
		insertAt := cached.outputIndex
		if insertAt < 0 || insertAt > len(output) {
			insertAt = len(output)
		}
		output = append(output, nil)
		copy(output[insertAt+1:], output[insertAt:])
		output[insertAt] = item
		for id, index := range existing {
			if index >= insertAt {
				existing[id] = index + 1
			}
		}
		existing[cached.id] = insertAt
		changed = true
	}
	if !changed {
		return data, false
	}
	response["output"] = output
	patched, err := json.Marshal(root)
	if err != nil {
		return data, false
	}
	return patched, true
}

// codexImageGenerationResponseCompatibilityEnabled 将返回协议修正绑定到
// image_generation 桥接本身。只有本次请求确实由网关注入了图片工具时才修正，
// 避免影响客户端自行携带工具的普通透传请求。
func codexImageGenerationResponseCompatibilityEnabled(c *gin.Context) bool {
	return codexImageGenerationBridgeWasApplied(c) && !isOpenAIResponsesCompactPath(c)
}

// normalizeCodexImageGenerationSSEDataForClient 修正 Responses 流中已经携带
// 最终图片结果、但状态仍停留在 generating/in_progress 的图片项。Codex 桌面端
// 只有看到 completed 才会把它转换成原生图片卡片。
func normalizeCodexImageGenerationSSEDataForClient(c *gin.Context, data []byte) ([]byte, bool) {
	if !codexImageGenerationResponseCompatibilityEnabled(c) || len(data) == 0 {
		return data, false
	}

	switch gjson.GetBytes(data, "type").String() {
	case "response.output_item.done":
		return normalizeCodexImageGenerationItemStatus(data, "item")
	case "response.completed":
		return normalizeCodexImageGenerationOutputStatuses(data, "response.output")
	default:
		return data, false
	}
}

// normalizeCodexImageGenerationResponseBodyForClient 对非流式 Responses 最终
// JSON 做同样的兼容修正，保证 SSE 被转换成 JSON 或上游直接返回 JSON 时行为一致。
func normalizeCodexImageGenerationResponseBodyForClient(c *gin.Context, body []byte) ([]byte, bool) {
	if !codexImageGenerationResponseCompatibilityEnabled(c) || len(body) == 0 {
		return body, false
	}
	return normalizeCodexImageGenerationOutputStatuses(body, "output")
}

func normalizeCodexImageGenerationOutputStatuses(data []byte, outputPath string) ([]byte, bool) {
	output := gjson.GetBytes(data, outputPath)
	if !output.Exists() || !output.IsArray() {
		return data, false
	}

	normalized := data
	changed := false
	for index, item := range output.Array() {
		if !codexImageGenerationItemNeedsCompletedStatus(item) {
			continue
		}
		path := outputPath + "." + strconv.Itoa(index) + ".status"
		next, err := sjson.SetBytes(normalized, path, "completed")
		if err != nil {
			continue
		}
		normalized = next
		changed = true
	}
	return normalized, changed
}

func normalizeCodexImageGenerationItemStatus(data []byte, itemPath string) ([]byte, bool) {
	if !codexImageGenerationItemNeedsCompletedStatus(gjson.GetBytes(data, itemPath)) {
		return data, false
	}
	normalized, err := sjson.SetBytes(data, itemPath+".status", "completed")
	if err != nil {
		return data, false
	}
	return normalized, true
}

func codexImageGenerationItemNeedsCompletedStatus(item gjson.Result) bool {
	if !item.IsObject() || item.Get("type").String() != "image_generation_call" {
		return false
	}
	// 没有最终图片内容时不能伪造成功；failed/cancelled 等明确终态也必须原样保留。
	if strings.TrimSpace(item.Get("result").String()) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.Get("status").String())) {
	case "", "generating", "in_progress":
		return true
	default:
		return false
	}
}
