package adapters

type AnthropicAdapter struct{}

func (a *AnthropicAdapter) AdaptRequest(request map[string]interface{}, targetModel string) (map[string]interface{}, error) {
	adapted := make(map[string]interface{})

	// 设置模型 - 优先使用目标模型
	if targetModel != "" {
		adapted["model"] = targetModel
	} else if model, ok := request["model"].(string); ok {
		adapted["model"] = model
	} else {
		adapted["model"] = "claude-3-sonnet-20240229"
	}

	// 设置最大tokens
	if maxTokens, ok := request["max_tokens"]; ok {
		adapted["max_tokens"] = maxTokens
	} else {
		adapted["max_tokens"] = 4096
	}

	// 转换消息格式
	if messages, ok := request["messages"].([]interface{}); ok {
		claudeMessages := make([]map[string]interface{}, 0)
		var systemPrompt string

		for _, msg := range messages {
			if msgMap, ok := msg.(map[string]interface{}); ok {
				role := msgMap["role"].(string)
				content := msgMap["content"]

				if role == "system" {
					// Claude 使用单独的 system 参数
					systemPrompt = content.(string)
					continue
				}

				claudeMsg := map[string]interface{}{
					"role":    role,
					"content": a.convertContent(content),
				}
				claudeMessages = append(claudeMessages, claudeMsg)
			}
		}

		adapted["messages"] = claudeMessages
		if systemPrompt != "" {
			adapted["system"] = systemPrompt
		}
	}

	// 其他参数
	if temp, ok := request["temperature"]; ok {
		adapted["temperature"] = temp
	}
	if topP, ok := request["top_p"]; ok {
		adapted["top_p"] = topP
	}
	if stream, ok := request["stream"]; ok {
		adapted["stream"] = stream
	}

	return adapted, nil
}

func (a *AnthropicAdapter) AdaptResponse(response map[string]interface{}) (map[string]interface{}, error) {
	adapted := map[string]interface{}{
		"id":      getOrDefault(response, "id", "chatcmpl-anthropic"),
		"object":  "chat.completion",
		"created": getOrDefault(response, "created", 0),
		"model":   getOrDefault(response, "model", "claude-3-sonnet-20240229"),
	}

	// 转换 content
	var contentText string
	if content, ok := response["content"].([]interface{}); ok && len(content) > 0 {
		if firstContent, ok := content[0].(map[string]interface{}); ok {
			contentText = getOrDefault(firstContent, "text", "").(string)
		}
	}

	adapted["choices"] = []map[string]interface{}{
		{
			"index": 0,
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": contentText,
			},
			"finish_reason": a.convertStopReason(getOrDefault(response, "stop_reason", "").(string)),
		},
	}

	// 转换 usage
	if usage, ok := response["usage"].(map[string]interface{}); ok {
		inputTokens := int(getOrDefault(usage, "input_tokens", 0).(float64))
		outputTokens := int(getOrDefault(usage, "output_tokens", 0).(float64))
		adapted["usage"] = map[string]interface{}{
			"prompt_tokens":     inputTokens,
			"completion_tokens": outputTokens,
			"total_tokens":      inputTokens + outputTokens,
		}
	}

	return adapted, nil
}

func (a *AnthropicAdapter) AdaptStreamChunk(chunk map[string]interface{}) (map[string]interface{}, error) {
	chunkType := getOrDefault(chunk, "type", "").(string)

	base := map[string]interface{}{
		"id":      "chatcmpl-anthropic",
		"object":  "chat.completion.chunk",
		"created": 0,
		"model":   "claude-3-sonnet-20240229",
	}

	// 根据 Claude API 的不同事件类型进行处理
	switch chunkType {
	case "message_start":
		// Claude API 的 message_start 事件
		message, ok := chunk["message"].(map[string]interface{})
		if !ok {
			break
		}
		
		// 提取使用量信息
		var promptTokens int
		if usage, ok := message["usage"].(map[string]interface{}); ok {
			if inputTokens, ok := usage["input_tokens"].(float64); ok {
				promptTokens = int(inputTokens)
			}
		}
		
		base["usage"] = map[string]interface{}{
			"prompt_tokens": promptTokens,
		}
		
		base["choices"] = []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": nil,
			},
		}
	case "content_block_start":
		// Claude API 的 content_block_start 事件
		base["choices"] = []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": nil,
			},
		}
	case "content_block_delta":
		// Claude API 的 content_block_delta 事件
		var contentText string
		if delta, ok := chunk["delta"].(map[string]interface{}); ok {
			contentText = getOrDefault(delta, "text", "").(string)
		}

		base["choices"] = []map[string]interface{}{
			{
				"index": 0,
				"delta": map[string]interface{}{
					"role":    "assistant",
					"content": contentText,
				},
				"finish_reason": nil,
			},
		}
	case "content_block_stop":
		// Claude API 的 content_block_stop 事件
		base["choices"] = []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": nil,
			},
		}
	case "message_delta":
		// Claude API 的 message_delta 事件，包含停止原因
		var finishReason string
		if delta, ok := chunk["delta"].(map[string]interface{}); ok {
			if stopReason, ok := delta["stop_reason"].(string); ok {
				finishReason = a.convertStopReason(stopReason)
			}
		}
		
		base["choices"] = []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": finishReason,
			},
		}
	case "message_stop":
		// Claude API 的 message_stop 事件
		base["choices"] = []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": "stop",
			},
		}
	default:
		// 未知类型，返回空的 delta
		base["choices"] = []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": nil,
			},
		}
	}

	return base, nil
}

func (a *AnthropicAdapter) convertContent(content interface{}) interface{} {
	if str, ok := content.(string); ok {
		return []map[string]interface{}{
			{
				"type": "text",
				"text": str,
			},
		}
	}
	return content
}

func (a *AnthropicAdapter) convertStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	default:
		return "stop"
	}
}

func getOrDefault(m map[string]interface{}, key string, defaultValue interface{}) interface{} {
	if val, ok := m[key]; ok {
		return val
	}
	return defaultValue
}
