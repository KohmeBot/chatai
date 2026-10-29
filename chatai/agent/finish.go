package agent

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/kohmebot/chatai/chatai/model"
)

// The runner strips this optional parameter before invoking tool handlers.
const finishParameter = "_finish_after_success"

func withFinishParameter(definitions []model.Tool) []model.Tool {
	result := append([]model.Tool(nil), definitions...)
	for i := range result {
		// Copy shared schemas to keep concurrent runs independent.
		params := maps.Clone(result[i].Function.Parameters)
		if params == nil {
			continue
		}
		properties, ok := params["properties"].(map[string]any)
		if !ok {
			continue
		}
		properties = maps.Clone(properties)
		properties[finishParameter] = map[string]any{"type": "boolean", "description": "仅在本轮最后一个工具调用中设为 true，表示该调用成功后全部任务即完成，无需再读取结果或继续决策。所有调用成功且满足交付要求后直接结束；失败仍交回模型处理。需要读取新信息的工具不要设置。默认 false。"}
		params["properties"] = properties
		result[i].Function.Parameters = params
	}
	return result
}

func parseFinishParameter(call model.ToolCall) (model.ToolCall, bool, error) {
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return call, false, err
	}
	value, exists := args[finishParameter]
	if !exists {
		return call, false, nil
	}
	var finish bool
	if string(value) == "null" {
		return call, false, fmt.Errorf("%s must be a boolean", finishParameter)
	}
	if err := json.Unmarshal(value, &finish); err != nil {
		return call, false, err
	}
	delete(args, finishParameter)
	encoded, err := json.Marshal(args)
	call.Function.Arguments = string(encoded)
	return call, finish, err
}
