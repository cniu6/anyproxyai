package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"openai-router-go/internal/adapters"
	"openai-router-go/internal/config"

	log "github.com/sirupsen/logrus"
)

type ProxyService struct {
	routeService *RouteService
	config       *config.Config
	httpClient   *http.Client
}

func NewProxyService(routeService *RouteService, cfg *config.Config) *ProxyService {
	return &ProxyService{
		routeService: routeService,
		config:       cfg,
		httpClient: &http.Client{
			Timeout: 0, // 不设置超时，因为大模型生成非常耗时
		},
	}
}

// ProxyRequest 代理请求
func (s *ProxyService) ProxyRequest(requestBody []byte, headers map[string]string) ([]byte, int, error) {
	// 解析请求
	var reqData map[string]interface{}
	if err := json.Unmarshal(requestBody, &reqData); err != nil {
		return nil, http.StatusBadRequest, fmt.Errorf("invalid JSON body: %v", err)
	}

	model, ok := reqData["model"].(string)
	if !ok || model == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("'model' field is required")
	}

	log.Infof("Received request for model: %s", model)

	// 提取真实的模型名（处理 Gemini streamGenerateContent 的情况）
	realModel := model
	if strings.Contains(model, ":streamGenerateContent") {
		realModel = strings.TrimSuffix(model, ":streamGenerateContent")
	}

	// 首先检查是否是重定向关键字（支持带后缀的模型名）
	if s.config.RedirectEnabled && (realModel == s.config.RedirectKeyword || strings.HasPrefix(realModel, s.config.RedirectKeyword+":")) {
		if s.config.RedirectTargetModel == "" {
			return nil, http.StatusNotFound, fmt.Errorf("redirect target model not configured")
		}
		log.Infof("Redirecting %s to model: %s", realModel, s.config.RedirectTargetModel)
		model = s.config.RedirectTargetModel
		reqData["model"] = model

		// 重新编码请求体
		requestBody, _ = json.Marshal(reqData)
	}

	// 查找路由
	route, err := s.routeService.GetRouteByModel(model)
	if err != nil {
		availableModels, _ := s.routeService.GetAvailableModels()
		return nil, http.StatusNotFound, fmt.Errorf("model '%s' not found in route list. Available models: %v", model, availableModels)
	}

	// 检查是否需要进行 API 转换
	var transformedBody []byte
	var targetURL string

	// 清理路由 API URL（移除末尾斜杠）
	cleanAPIUrl := strings.TrimSuffix(route.APIUrl, "/")

	// 检测是否需要使用适配器（判断是否需要API翻译）
	adapterName := s.detectAdapter(cleanAPIUrl, model)
	if adapterName != "" {
		// 使用适配器转换请求
		adapter := adapters.GetAdapter(adapterName)
		transformedReq, err := adapter.AdaptRequest(reqData, model)
		if err != nil {
			log.Errorf("Failed to adapt request: %v", err)
			return nil, http.StatusInternalServerError, err
		}
		transformedBody, _ = json.Marshal(transformedReq)
		targetURL = s.buildAdapterURL(cleanAPIUrl, adapterName, model)
	} else {
		// 不使用适配器，直接转发
		transformedBody = requestBody
		targetURL = cleanAPIUrl + "/v1/chat/completions"
	}

	log.Infof("Routing to: %s (route: %s)", targetURL, route.Name)

	// 创建代理请求
	proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(transformedBody))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	// 设置请求头
	proxyReq.Header.Set("Content-Type", "application/json")

	// 使用路由配置的 API Key（如果有），否则透传原始 Authorization
	if route.APIKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	} else if auth := headers["Authorization"]; auth != "" {
		proxyReq.Header.Set("Authorization", auth)
	}

	// 发送请求
	startTime := time.Now()
	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		s.routeService.LogRequest(model, route.ID, 0, 0, 0, false, err.Error())
		return nil, http.StatusServiceUnavailable, fmt.Errorf("backend service unavailable: %v", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		s.routeService.LogRequest(model, route.ID, 0, 0, 0, false, err.Error())
		return nil, http.StatusInternalServerError, err
	}

	log.Infof("Response received from %s in %v, status: %d", route.Name, time.Since(startTime), resp.StatusCode)

	// 记录使用情况（使用实际模型名而不是重定向关键字）
	if resp.StatusCode == http.StatusOK {
		var respData map[string]interface{}
		if err := json.Unmarshal(responseBody, &respData); err == nil {
			if usage, ok := respData["usage"].(map[string]interface{}); ok {
				totalTokens := int(usage["total_tokens"].(float64))
				promptTokens := int(usage["prompt_tokens"].(float64))
				completionTokens := int(usage["completion_tokens"].(float64))
				s.routeService.LogRequest(model, route.ID, promptTokens, completionTokens, totalTokens, true, "")
			}
		}
	} else {
		s.routeService.LogRequest(model, route.ID, 0, 0, 0, false, string(responseBody))
	}

	// 如果使用了适配器，转换响应
	if adapterName != "" {
		adapter := adapters.GetAdapter(adapterName)
		if adapter != nil {
			var respData map[string]interface{}
			if err := json.Unmarshal(responseBody, &respData); err == nil {
				adaptedResp, err := adapter.AdaptResponse(respData)
				if err != nil {
					log.Errorf("Failed to adapt response: %v", err)
				} else {
					responseBody, _ = json.Marshal(adaptedResp)
				}
			}
		}
	}

	return responseBody, resp.StatusCode, nil
}

// ProxyStreamRequest 代理流式请求
func (s *ProxyService) ProxyStreamRequest(requestBody []byte, headers map[string]string, writer io.Writer, flusher http.Flusher) error {
	// 解析请求
	var reqData map[string]interface{}
	if err := json.Unmarshal(requestBody, &reqData); err != nil {
		return fmt.Errorf("invalid JSON body: %v", err)
	}

	model, ok := reqData["model"].(string)
	if !ok || model == "" {
		return fmt.Errorf("'model' field is required")
	}

	originalModel := model

	// 提取真实的模型名（处理 Gemini streamGenerateContent 的情况）
	realModel := model
	if strings.Contains(model, ":streamGenerateContent") {
		realModel = strings.TrimSuffix(model, ":streamGenerateContent")
	}

	// 首先检查是否是重定向关键字
	if s.config.RedirectEnabled && (realModel == s.config.RedirectKeyword || strings.HasPrefix(realModel, s.config.RedirectKeyword+":")) {
		if s.config.RedirectTargetModel == "" {
			return fmt.Errorf("redirect target model not configured")
		}
		log.Infof("Redirecting %s to model: %s", realModel, s.config.RedirectTargetModel)
		model = s.config.RedirectTargetModel
		reqData["model"] = model
		requestBody, _ = json.Marshal(reqData)
	}

	// 查找路由
	route, err := s.routeService.GetRouteByModel(model)
	if err != nil {
		return err
	}

	// 清理路由 API URL（移除末尾斜杠）
	cleanAPIUrl := strings.TrimSuffix(route.APIUrl, "/")

	// 检测是否需要使用适配器（判断是否需要API翻译）
	adapterName := s.detectAdapter(cleanAPIUrl, model)
	var transformedBody []byte
	var targetURL string

	if adapterName != "" {
		// 使用适配器转换请求
		adapter := adapters.GetAdapter(adapterName)
		if adapter == nil {
			return fmt.Errorf("adapter not found: %s", adapterName)
		}

		// 确保开启stream
		reqData["stream"] = true
		transformedReq, err := adapter.AdaptRequest(reqData, model)
		if err != nil {
			log.Errorf("Failed to adapt request: %v", err)
			return err
		}
		transformedBody, _ = json.Marshal(transformedReq)
		// 对流式请求使用专门的URL构建函数
		targetURL = s.buildAdapterStreamURL(cleanAPIUrl, adapterName, model)
		log.Infof("Streaming to: %s (route: %s, adapter: %s)", targetURL, route.Name, adapterName)
	} else {
		// 不使用适配器，直接转发
		transformedBody = requestBody
		targetURL = cleanAPIUrl + "/v1/chat/completions"
		log.Infof("Streaming to: %s (route: %s)", targetURL, route.Name)
	}

	// 创建代理请求
	proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(transformedBody))
	if err != nil {
		return err
	}

	proxyReq.Header.Set("Content-Type", "application/json")
	if route.APIKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	} else if auth := headers["Authorization"]; auth != "" {
		proxyReq.Header.Set("Authorization", auth)
	}

	// Claude需要特殊的版本头
	if adapterName == "anthropic" {
		proxyReq.Header.Set("anthropic-version", "2023-06-01")
	}

	// 发送请求
	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("backend error: %d - %s", resp.StatusCode, string(body))
	}

	// 流式传输响应
	if adapterName != "" {
		// 需要转换SSE流
		return s.streamWithAdapter(resp.Body, writer, flusher, adapterName, originalModel, route.ID)
	} else {
		// 直接转发SSE流
		return s.streamDirect(resp.Body, writer, flusher, originalModel, route.ID)
	}
}

// streamWithAdapter 使用适配器处理流式响应
func (s *ProxyService) streamWithAdapter(reader io.Reader, writer io.Writer, flusher http.Flusher, adapterName, model string, routeID int64) error {
	adapter := adapters.GetAdapter(adapterName)
	if adapter == nil {
		return fmt.Errorf("adapter not found: %s", adapterName)
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024) // 1MB max

	for scanner.Scan() {
		line := scanner.Text()

		// 跳过空行
		if line == "" {
			continue
		}

		// 处理SSE格式: "data: {...}"
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// 检查是否是结束标记
			if data == "[DONE]" {
				fmt.Fprintf(writer, "data: [DONE]\n\n")
				flusher.Flush()
				s.routeService.LogRequest(model, routeID, 0, 0, 0, true, "")
				return nil
			}

			// 解析JSON
			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				log.Warnf("Failed to parse chunk: %v, data: %s", err, data)
				continue
			}

			// 使用适配器转换chunk
			adaptedChunk, err := adapter.AdaptStreamChunk(chunk)
			if err != nil {
				log.Warnf("Failed to adapt chunk: %v", err)
				continue
			}

			// 发送转换后的chunk
			adaptedData, _ := json.Marshal(adaptedChunk)
			fmt.Fprintf(writer, "data: %s\n\n", string(adaptedData))
			flusher.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		s.routeService.LogRequest(model, routeID, 0, 0, 0, false, err.Error())
		return err
	}

	s.routeService.LogRequest(model, routeID, 0, 0, 0, true, "")
	return nil
}

// streamDirect 直接转发流式响应
func (s *ProxyService) streamDirect(reader io.Reader, writer io.Writer, flusher http.Flusher, model string, routeID int64) error {
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if _, writeErr := writer.Write(buf[:n]); writeErr != nil {
				s.routeService.LogRequest(model, routeID, 0, 0, 0, false, writeErr.Error())
				return writeErr
			}
			flusher.Flush()
		}
		if err != nil {
			if err == io.EOF {
				s.routeService.LogRequest(model, routeID, 0, 0, 0, true, "")
				return nil
			}
			s.routeService.LogRequest(model, routeID, 0, 0, 0, false, err.Error())
			return err
		}
	}
}

// FetchRemoteModels 获取远程模型列表
func (s *ProxyService) FetchRemoteModels(apiUrl, apiKey string) ([]string, error) {
	// 移除末尾的斜杠
	apiUrl = strings.TrimSuffix(apiUrl, "/")

	// 添加 http/https 前缀（如果没有）
	if !strings.HasPrefix(apiUrl, "http://") && !strings.HasPrefix(apiUrl, "https://") {
		apiUrl = "https://" + apiUrl
	}

	url := apiUrl + "/v1/models"
	log.Infof("Fetching models from: %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %v (body: %s)", err, string(body))
	}

	models := make([]string, len(result.Data))
	for i, m := range result.Data {
		models[i] = m.ID
	}

	log.Infof("Successfully fetched %d models", len(models))
	return models, nil
}

// detectAdapter 检测需要使用的适配器
func (s *ProxyService) detectAdapter(apiUrl, model string) string {
	lowerURL := strings.ToLower(apiUrl)
	lowerModel := strings.ToLower(model)

	// 先检查是否是标准的 OpenAI 格式 API 端点，如果是，则不应用任何适配器
	// 这样可以避免将 Gemini 适配器错误地应用到 OpenAI 兼容的 API 上
	if isStandardOpenAIEndpoint(lowerURL) {
		return "" // 对标准 OpenAI 端点不使用适配器
	}

	// 精确内容检测 - 基于API URL和模型名称中的关键词
	// 使用更精确的匹配避免误匹配，例如避免 "glm" 与 "gemini" 的混淆
	if containsExactWord(lowerURL, "anthropic") || containsExactWord(lowerModel, "claude") {
		return "anthropic"
	}
	
	// 使用更严格的匹配来检测 Gemini，避免 "glm" 与 "gemini" 等误匹配
	if containsExactWord(lowerURL, "gemini") || containsExactWord(lowerModel, "gemini") {
		return "gemini"
	}
	
	if containsExactWord(lowerURL, "deepseek") || containsExactWord(lowerModel, "deepseek") {
		return "deepseek"
	}

	return "" // 不需要适配器
}

// isStandardOpenAIEndpoint 检查 URL 是否为标准的 OpenAI API 端点
// 如果是，则不应该应用任何适配器转换
func isStandardOpenAIEndpoint(url string) bool {
	// 检查是否包含标准的 OpenAI API 路径
	if containsExactWord(url, "/v1/chat/completions") || 
	   containsExactWord(url, "/v1/completions") ||
	   containsExactWord(url, "/v1/embeddings") ||
	   containsExactWord(url, "/v1/images/generations") ||
	   containsExactWord(url, "/v1/audio/transcriptions") ||
	   containsExactWord(url, "/v1/audio/speech") {
		return true
	}
	
	// 检查常见的 OpenAI 兼容 API 基础路径
	if containsExactWord(url, "openai.com") || 
	   containsExactWord(url, "api.openai.com") ||
	   containsExactWord(url, "/openai/v1") ||
	   containsExactWord(url, "/v1") { // 这太宽泛了，需要更具体的检查
		// 对于 /v1 路径，需要更具体的检查，仅在确实包含 chat/completions 时才认为是 OpenAI API
		if strings.Contains(url, "/v1/chat/completions") ||
		   strings.Contains(url, "/v1/completions") {
			return true
		}
	}
	
	return false
}

// containsExactWord 检查 needle 是否作为一个独立的单词存在于 haystack 中
// 通过检查边界字符来确保精确匹配，避免子串误匹配
func containsExactWord(haystack, needle string) bool {
	if haystack == needle {
		return true
	}
	
	// 使用更精确的查找方法，确保 needle 前后是边界或分隔符
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			// 检查前面的字符（如果存在）是否为非字母数字字符
			prevIsBoundary := i == 0 || !isAlphanumeric(rune(haystack[i-1]))
			// 检查后面的字符（如果存在）是否为非字母数字字符
			nextIsBoundary := i+len(needle) == len(haystack) || !isAlphanumeric(rune(haystack[i+len(needle)]))
			
			if prevIsBoundary && nextIsBoundary {
				return true
			}
		}
	}
	
	return false
}

// isAlphanumeric 检查字符是否为字母或数字
func isAlphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// isStandaloneMatch 检查 needle 是否作为一个独立的词在 haystack 中
// 避免 "glm" 匹配到 "gemini" 这样的误匹配
func isStandaloneMatch(haystack, needle string) bool {
	// 检查 needle 作为独立词出现的情况
	// 在 URL 中，独立词可能被 /、.、-、_ 等分隔符包围
	// 在模型名中，可能被 -、_ 等分隔符包围
	
	// 如果完全匹配，返回 true
	if haystack == needle {
		return true
	}
	
	// 检查 needle 是否被分隔符包围
	// 使用简单的检查方式：检查 needle 前后是否是分隔符或字符串边界
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			// 检查前面是否是边界或分隔符
			prevIsDelimiter := i == 0 || isDelimiter(rune(haystack[i-1]))
			// 检查后面是否是边界或分隔符
			nextIsDelimiter := i+len(needle) == len(haystack) || isDelimiter(rune(haystack[i+len(needle)]))
			
			if prevIsDelimiter && nextIsDelimiter {
				return true
			}
		}
	}
	
	// 检查 needle 是否是另一个词的前缀但后跟分隔符（如 "gemini" 在 "gemini-pro" 中）
	// 或后缀但前跟分隔符（如 "gemini" 在 "my-gemini" 中）
	if len(needle) < len(haystack) {
		if strings.HasPrefix(haystack, needle+"_") || strings.HasPrefix(haystack, needle+"-") ||
			strings.HasSuffix(haystack, "_"+needle) || strings.HasSuffix(haystack, "-"+needle) {
			return true
		}
	}
	
	return false
}

// isDelimiter 检查字符是否为分隔符
func isDelimiter(r rune) bool {
	return r == '/' || r == '.' || r == '-' || r == '_' || r == ':' || r == '?' || r == '&' || r == '#'
}

// buildAdapterURL 构建适配器URL
func (s *ProxyService) buildAdapterURL(baseURL, adapterName, model string) string {
	switch adapterName {
	case "anthropic":
		return baseURL + "/v1/messages"
	case "gemini":
		// 清理模型名，移除 :streamGenerateContent 后缀
		cleanModel := model
		if strings.Contains(model, ":streamGenerateContent") {
			cleanModel = strings.TrimSuffix(model, ":streamGenerateContent")
		}
		return baseURL + "/v1beta/models/" + cleanModel + ":generateContent"
	case "deepseek":
		return baseURL + "/v1/chat/completions"
	default:
		return baseURL + "/v1/chat/completions"
	}
}

// buildAdapterStreamURL 构建适配器流式URL
func (s *ProxyService) buildAdapterStreamURL(baseURL, adapterName, model string) string {
	switch adapterName {
	case "anthropic":
		return baseURL + "/v1/messages"
	case "gemini":
		// 处理 Gemini 的 streamGenerateContent 格式
		cleanModel := model
		if strings.Contains(model, ":streamGenerateContent") {
			cleanModel = strings.TrimSuffix(model, ":streamGenerateContent")
		}
		return baseURL + "/v1beta/models/" + cleanModel + ":streamGenerateContent"
	case "deepseek":
		return baseURL + "/v1/chat/completions"
	default:
		return baseURL + "/v1/chat/completions"
	}
}
