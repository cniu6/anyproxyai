package adapters

type GeminiAdapter struct{}

func (a *GeminiAdapter) AdaptRequest(request map[string]interface{}, targetModel string) (map[string]interface{}, error) {
	adapted := make(map[string]interface{})

	// 设置目标模型（支持重定向）
	if targetModel != "" {
		adapted["model"] = targetModel
	} else if model, ok := request["model"].(string); ok {
		adapted["model"] = model
	} else {
		adapted["model"] = getOrDefault(request, "model", "gemini-pro")
	}

	// Gemini 使用 contents 而不是 messages
	if messages, ok := request["messages"].([]interface{}); ok {
		adapted["contents"] = a.convertMessages(messages)
	} else {
		// 如果没有 messages，但其他适配器需要这个字段，提供一个默认值
		adapted["contents"] = []map[string]interface{}{
			{"role": "user", "parts": []map[string]interface{}{{"text": ""}}},
		}
	}

	// 处理生成配置
	generationConfig := make(map[string]interface{})

	if temp, ok := request["temperature"]; ok {
		generationConfig["temperature"] = temp
	}
	if topP, ok := request["top_p"]; ok {
		generationConfig["topP"] = topP
	}
	if maxTokens, ok := request["max_tokens"]; ok {
		generationConfig["maxOutputTokens"] = maxTokens
	}

	// 只有当有配置时才添加 generationConfig
	if len(generationConfig) > 0 {
		adapted["generationConfig"] = generationConfig
	}

	// Gemini 的流式参数在 URL 中处理，这里不需要设置 stream
	// 因为调用时会使用 buildAdapterStreamURL 构建正确的 URL

	return adapted, nil
}

func (a *GeminiAdapter) AdaptResponse(response map[string]interface{}) (map[string]interface{}, error) {
	// 将 Gemini 响应转换为 OpenAI 格式
	adapted := map[string]interface{}{
		"id":      "chatcmpl-gemini",
		"object":  "chat.completion",
		"created": 0,
		"model":   "gemini-pro",
	}

	candidates, _ := response["candidates"].([]interface{})

	if len(candidates) == 0 {
		adapted["choices"] = []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "",
				},
				"finish_reason": "stop",
			},
		}
		adapted["usage"] = map[string]interface{}{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		}
		return adapted, nil
	}

	candidate := candidates[0].(map[string]interface{})
	content := candidate["content"].(map[string]interface{})
	parts := content["parts"].([]interface{})

	var contentText string
	for _, part := range parts {
		if partMap, ok := part.(map[string]interface{}); ok {
			if text, ok := partMap["text"].(string); ok {
				contentText += text
			}
		}
	}

	finishReason := a.convertFinishReason("")
	if fr, ok := candidate["finishReason"].(string); ok {
		finishReason = a.convertFinishReason(fr)
	}

	adapted["choices"] = []map[string]interface{}{
		{
			"index": 0,
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": contentText,
			},
			"finish_reason": finishReason,
		},
	}

	// 处理使用量信息
	if usageMetadata, ok := response["usageMetadata"].(map[string]interface{}); ok {
		promptTokens := int(getOrDefault(usageMetadata, "promptTokenCount", float64(0)).(float64))
		candidatesTokens := int(getOrDefault(usageMetadata, "candidatesTokenCount", float64(0)).(float64))
		totalTokens := int(getOrDefault(usageMetadata, "totalTokenCount", float64(0)).(float64))

		adapted["usage"] = map[string]interface{}{
			"prompt_tokens":     promptTokens,
			"completion_tokens": candidatesTokens,
			"total_tokens":      totalTokens,
		}
	} else {
		adapted["usage"] = map[string]interface{}{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		}
	}

	return adapted, nil
}

func (a *GeminiAdapter) AdaptStreamChunk(chunk map[string]interface{}) (map[string]interface{}, error) {
	// 将 Gemini 流式响应转换为 OpenAI 格式
	adaptedChunk := map[string]interface{}{
		"id":      "chatcmpl-gemini",
		"object":  "chat.completion.chunk",
		"created": 0,
		"model":   "gemini-pro",
	}

	candidates, exists := chunk["candidates"].([]interface{})
	if !exists || len(candidates) == 0 {
		// 检查是否是结束块（可能包含 usageMetadata）
		if usageMetadata, ok := chunk["usageMetadata"].(map[string]interface{}); ok {
			adaptedChunk["choices"] = []map[string]interface{}{
				{
					"index": 0,
					"delta": map[string]interface{}{},
					"finish_reason": "stop",
				},
			}
			
			// 添加使用量信息
			promptTokens := int(getOrDefault(usageMetadata, "promptTokenCount", float64(0)).(float64))
			candidatesTokens := int(getOrDefault(usageMetadata, "candidatesTokenCount", float64(0)).(float64))
			totalTokens := int(getOrDefault(usageMetadata, "totalTokenCount", float64(0)).(float64))

			adaptedChunk["usage"] = map[string]interface{}{
				"prompt_tokens":     promptTokens,
				"completion_tokens": candidatesTokens,
				"total_tokens":      totalTokens,
			}
		} else {
			adaptedChunk["choices"] = []map[string]interface{}{
				{
					"index":         0,
					"delta":         map[string]interface{}{},
					"finish_reason": nil,
				},
			}
		}
		return adaptedChunk, nil
	}

	// 处理候选响应
	candidate := candidates[0].(map[string]interface{})
	content := candidate["content"].(map[string]interface{})
	parts := content["parts"].([]interface{})

	var deltaContent string
	for _, part := range parts {
		if partMap, ok := part.(map[string]interface{}); ok {
			if text, ok := partMap["text"].(string); ok {
				deltaContent += text
			}
		}
	}

	finishReason := a.convertFinishReason("")
	if fr, ok := candidate["finishReason"].(string); ok {
		finishReason = a.convertFinishReason(fr)
	}

	var delta map[string]interface{}
	if deltaContent != "" {
		delta = map[string]interface{}{
			"role": "assistant",
			"content": deltaContent,
		}
	}

	adaptedChunk["choices"] = []map[string]interface{}{
		{
			"index": 0,
			"delta": delta,
			"finish_reason": finishReason,
		},
	}

	return adaptedChunk, nil
}

func (a *GeminiAdapter) convertMessages(messages []interface{}) []map[string]interface{} {
	contents := make([]map[string]interface{}, 0)

	for _, msg := range messages {
		if msgMap, ok := msg.(map[string]interface{}); ok {
			role := msgMap["role"].(string)
			content := msgMap["content"]

			// Gemini 使用 "user" 和 "model" 作为角色
			geminiRole := "user"
			if role == "assistant" {
				geminiRole = "model"
			}

			geminiMsg := map[string]interface{}{
				"role": geminiRole,
				"parts": []map[string]interface{}{
					{"text": content},
				},
			}
			contents = append(contents, geminiMsg)
		}
	}

	return contents
}

func (a *GeminiAdapter) convertFinishReason(finishReason string) string {
	if finishReason == "" {
		return ""
	}

	// 将 Gemini 的停止原因转换为 OpenAI 格式
	switch finishReason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	case "RECITATION":
		return "content_filter"
	case "OTHER":
		return "stop"
	default:
		return "stop"
	}
}
