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
	"openai-router-go/internal/database"

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

// getRedirectRoute 获取重定向目标路�?
// 如果配置�?RedirectTargetRouteID，优先使用该ID获取路由
// 否则根据 RedirectTargetModel 查找路由
func (s *ProxyService) getRedirectRoute() (*database.ModelRoute, error) {
	// 优先使用指定的路由ID
	if s.config.RedirectTargetRouteID > 0 {
		route, err := s.routeService.GetRouteByID(s.config.RedirectTargetRouteID)
		if err == nil {
			return route, nil
		}
		log.Warnf("Failed to get route by ID %d, falling back to model lookup: %v", s.config.RedirectTargetRouteID, err)
	}

	// 回退到按模型名查�?
	if s.config.RedirectTargetModel == "" {
		return nil, fmt.Errorf("redirect target model not configured")
	}
	return s.routeService.GetRouteByForwarding(s.config.RedirectTargetModel)
}

// extractModelFromRequest 从请求的模型名中提取原始模型名
// 输入: "LMStudio/qwen/qwen3-8b" 或 "qwen/qwen3-8b"
// 输出: "qwen/qwen3-8b", "LMStudio" (如果有的话)
func extractModelFromRequest(model string) (string, string) {
	if strings.Contains(model, "/") {
		parts := strings.SplitN(model, "/", 2)
		if len(parts) == 2 {
			routeName := parts[0]
			originalModel := parts[1]
			log.Infof("Extracted model from request: route=%s, model=%s", routeName, originalModel)
			return originalModel, routeName
		}
	}
	return model, ""
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

	// 详细日志：记录请求头和请求体
	log.Infof("=== PROXY REQUEST START ===")
	log.Infof("Request model: %s", model)
	log.Infof("Request headers:")
	for k, v := range headers {
		// 隐藏敏感信息
		if strings.Contains(strings.ToLower(k), "authorization") || strings.Contains(strings.ToLower(k), "key") {
			log.Infof("  %s: ***REDACTED***", k)
		} else {
			log.Infof("  %s: %s", k, v)
		}
	}
	log.Infof("Request body: %s", string(requestBody))
	log.Infof("=== PROXY REQUEST DETAILS ===")

	// 提取真实的模型名（处�?Gemini streamGenerateContent 的情况）
	realModel := model
	if strings.Contains(model, ":streamGenerateContent") {
		realModel = strings.TrimSuffix(model, ":streamGenerateContent")
	}

	// 首先检查是否是重定向关键字（支持带后缀的模型名�?
	var route *database.ModelRoute
	var err error
	isRedirect := s.config.RedirectEnabled && (realModel == s.config.RedirectKeyword || strings.HasPrefix(realModel, s.config.RedirectKeyword+":"))

	if isRedirect {
		// 使用重定向路�?
		route, err = s.getRedirectRoute()
		if err != nil {
			return nil, http.StatusNotFound, fmt.Errorf("redirect target not configured or not found: %v", err)
		}
		log.Infof("Redirecting %s to route: %s (model: %s, id: %d)", realModel, route.Name, route.Model, route.ID)
		model = route.Model
		reqData["model"] = model

		// 重新编码请求体
		requestBody, _ = json.Marshal(reqData)
	} else {
		// 查找路由
		route, err = s.routeService.GetRouteByForwarding(model)
		if err != nil {
			availableModels, _ := s.routeService.GetAvailableModels()
			return nil, http.StatusNotFound, fmt.Errorf("model '%s' not found in route list. Available models: %v", model, availableModels)
		}
		// 如果找到了带前缀的模型，更新请求体中的模型名为原始模型名（不带前缀）
		// route.Model 已经被 GetRouteByForwarding 设置为原始模型名
		if route.Model != model {
			reqData["model"] = route.Model
			requestBody, _ = json.Marshal(reqData)
			log.Infof("Updated model in request body from '%s' to '%s'", model, route.Model)
		}
	}

	// 检查是否需要进�?API 转换
	var transformedBody []byte
	var targetURL string

	// 不移除末尾斜杠，保留原始URL格式
	// 如果API URL末尾有斜杠，说明是固定路径，buildAdapterURL会正确处理
	cleanAPIUrl := route.APIUrl

	// 智能检测适配器 基于路由format和请求格式 (OpenAI格式请求)
	adapterName := s.detectAdapterForRoute(route, "openai")
	if adapterName != "" {
		// 使用适配器转换请求
		adapter := adapters.GetAdapter(adapterName)
		// 使用 route.Model（已去除前缀的原始模型名）来适配请求
		transformedReq, err := adapter.AdaptRequest(reqData, route.Model)
		if err != nil {
			log.Errorf("Failed to adapt request: %v", err)
			return nil, http.StatusInternalServerError, err
		}
		transformedBody, _ = json.Marshal(transformedReq)
		targetURL = s.buildAdapterURL(cleanAPIUrl, adapterName, route.Model)
	} else {
		// 不使用适配器，直接转发
		transformedBody = requestBody
		targetURL = buildOpenAIChatURL(route.APIUrl)
	}

	// 详细日志：记录目标路由信�?
	log.Infof("=== ROUTE TARGET ===")
	log.Infof("Target URL: %s", targetURL)
	log.Infof("Route name: %s", route.Name)
	log.Infof("Route API URL: %s", route.APIUrl)
	log.Infof("Route model: %s", route.Model)
	log.Infof("Route format: %s", route.Format)
	log.Infof("Route group: %s", route.Group)
	log.Infof("Route enabled: %v", route.Enabled)
	log.Infof("Adapter used: %s", adapterName)
	log.Infof("Transformed body: %s", string(transformedBody))
	log.Infof("=== ROUTE TARGET END ===")

	log.Infof("Routing to: %s (route: %s)", targetURL, route.Name)

	// 创建代理请求
	proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(transformedBody))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	// 设置请求�?
	proxyReq.Header.Set("Content-Type", "application/json")

	// 使用路由配置�?API Key（如果有），否则透传原始 Authorization
	if route.APIKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	} else if auth := headers["Authorization"]; auth != "" {
		proxyReq.Header.Set("Authorization", auth)
	}

	// 发送请�?
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

	// 详细日志：记录返回结�?
	log.Infof("=== RESPONSE RESULT ===")
	log.Infof("Response status code: %d", resp.StatusCode)
	log.Infof("Response time: %v", time.Since(startTime))
	log.Infof("Response headers:")
	for k, v := range resp.Header {
		log.Infof("  %s: %v", k, v)
	}
	log.Infof("Response body: %s", string(responseBody))
	log.Infof("=== RESPONSE RESULT END ===")

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
				log.Infof("=== ADAPTER TRANSFORMATION ===")
				log.Infof("Original response: %s", string(responseBody))
				adaptedResp, err := adapter.AdaptResponse(respData)
				if err != nil {
					log.Errorf("Failed to adapt response: %v", err)
				} else {
					responseBody, _ = json.Marshal(adaptedResp)
					log.Infof("Adapted response: %s", string(responseBody))
				}
				log.Infof("=== ADAPTER TRANSFORMATION END ===")
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

	// 详细日志：记录流式请求开�?
	log.Infof("=== STREAM PROXY REQUEST START ===")
	log.Infof("Stream request model: %s", originalModel)
	log.Infof("Stream request headers:")
	for k, v := range headers {
		// 隐藏敏感信息
		if strings.Contains(strings.ToLower(k), "authorization") || strings.Contains(strings.ToLower(k), "key") {
			log.Infof("  %s: ***REDACTED***", k)
		} else {
			log.Infof("  %s: %s", k, v)
		}
	}
	log.Infof("Stream request body: %s", string(requestBody))

	// 提取真实的模型名（处理 Gemini streamGenerateContent 的情况）
	realModel := model
	if strings.Contains(model, ":streamGenerateContent") {
		realModel = strings.TrimSuffix(model, ":streamGenerateContent")
	}

	// 首先检查是否是重定向关键字
	var route *database.ModelRoute
	var err error
	isRedirect := s.config.RedirectEnabled && (realModel == s.config.RedirectKeyword || strings.HasPrefix(realModel, s.config.RedirectKeyword+":"))

	if isRedirect {
		route, err = s.getRedirectRoute()
		if err != nil {
			return fmt.Errorf("redirect target not configured or not found: %v", err)
		}
		log.Infof("Redirecting %s to route: %s (model: %s, id: %d)", realModel, route.Name, route.Model, route.ID)
		model = route.Model
		reqData["model"] = model
		requestBody, _ = json.Marshal(reqData)
	} else {
		// 查找路由
		route, err = s.routeService.GetRouteByForwarding(model)
		if err != nil {
			// 检查是否是"模型未找到"错误
			if strings.Contains(err.Error(), "model not found") {
				availableModels, _ := s.routeService.GetAvailableModels()
				return fmt.Errorf("model '%s' not found in route list. Available models: %v", model, availableModels)
			}
			// 其他路由相关错误
			return fmt.Errorf("route lookup failed for model '%s': %v", model, err)
		}
		// 如果找到了带前缀的模型，更新请求体中的模型名为原始模型名（不带前缀）
		// route.Model 已经被 GetRouteByForwarding 设置为原始模型名
		if route.Model != model {
			reqData["model"] = route.Model
			requestBody, _ = json.Marshal(reqData)
			log.Infof("Updated model in request body from '%s' to '%s'", model, route.Model)
		}
	}

	// 不移除末尾斜杠，保留原始URL格式
	// 如果API URL末尾有斜杠，说明是固定路径，buildAdapterURL会正确处理
	cleanAPIUrl := route.APIUrl

	// 智能检测适配器: 基于路由format和请求格式 (OpenAI格式请求)
	adapterName := s.detectAdapterForRoute(route, "openai")
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
		// 使用 route.Model（已去除前缀的原始模型名）来适配请求
		transformedReq, err := adapter.AdaptRequest(reqData, route.Model)
		if err != nil {
			log.Errorf("Failed to adapt request: %v", err)
			return err
		}
		transformedBody, _ = json.Marshal(transformedReq)
		// 对流式请求使用专门的URL构建函数
		targetURL = s.buildAdapterStreamURL(cleanAPIUrl, adapterName, route.Model)
		log.Infof("Streaming to: %s (route: %s, adapter: %s)", targetURL, route.Name, adapterName)
	} else {
		// 不使用适配器，直接转发
		transformedBody = requestBody
		targetURL = buildOpenAIChatURL(route.APIUrl)
		log.Infof("Streaming to: %s (route: %s)", targetURL, route.Name)
	}

	// 详细日志：记录流式请求目标路由信�?
	log.Infof("=== STREAM ROUTE TARGET ===")
	log.Infof("Stream target URL: %s", targetURL)
	log.Infof("Stream route name: %s", route.Name)
	log.Infof("Stream route API URL: %s", route.APIUrl)
	log.Infof("Stream route model: %s", route.Model)
	log.Infof("Stream route format: %s", route.Format)
	log.Infof("Stream route group: %s", route.Group)
	log.Infof("Stream route enabled: %v", route.Enabled)
	log.Infof("Stream adapter used: %s", adapterName)
	log.Infof("Stream transformed body: %s", string(transformedBody))
	log.Infof("=== STREAM ROUTE TARGET END ===")

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

	// Claude需要特殊的版本�?
	if adapterName == "anthropic" {
		proxyReq.Header.Set("anthropic-version", "2023-06-01")
	}

	// 发送请�?
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
	// 使用实际路由到的模型名（model）用于统�?
	if adapterName != "" {
		// 需要转换SSE�?
		return s.streamWithAdapter(resp.Body, writer, flusher, adapterName, model, route.ID)
	} else {
		// 直接转发SSE�?
		return s.streamDirect(resp.Body, writer, flusher, model, route.ID)
	}
}

// ProxyStreamRequestWithAdapter 代理流式请求，使用指定的适配�?
func (s *ProxyService) ProxyStreamRequestWithAdapter(requestBody []byte, headers map[string]string, writer io.Writer, flusher http.Flusher, forceAdapter string) error {
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

	// 详细日志：记录流式请求开�?
	log.Infof("=== STREAM PROXY REQUEST START (FORCED ADAPTER: %s) ===", forceAdapter)
	log.Infof("Stream request model: %s", originalModel)
	log.Infof("Stream request headers:")
	for k, v := range headers {
		// 隐藏敏感信息
		if strings.Contains(strings.ToLower(k), "authorization") || strings.Contains(strings.ToLower(k), "key") {
			log.Infof("  %s: ***REDACTED***", k)
		} else {
			log.Infof("  %s: %s", k, v)
		}
	}
	log.Infof("Stream request body: %s", string(requestBody))

	// 提取真实的模型名（处理 Gemini streamGenerateContent 的情况）
	realModel := model
	if strings.Contains(model, ":streamGenerateContent") {
		realModel = strings.TrimSuffix(model, ":streamGenerateContent")
	}

	// 首先检查是否是重定向关键字
	var route *database.ModelRoute
	var err error
	isRedirect := s.config.RedirectEnabled && (realModel == s.config.RedirectKeyword || strings.HasPrefix(realModel, s.config.RedirectKeyword+":"))

	if isRedirect {
		route, err = s.getRedirectRoute()
		if err != nil {
			return fmt.Errorf("redirect target not configured or not found: %v", err)
		}
		log.Infof("Redirecting %s to route: %s (model: %s, id: %d)", realModel, route.Name, route.Model, route.ID)
		model = route.Model
		reqData["model"] = model
		requestBody, _ = json.Marshal(reqData)
	} else {
		// 查找路由
		route, err = s.routeService.GetRouteByForwarding(model)
		if err != nil {
			// 检查是否是"模型未找到"错误
			if strings.Contains(err.Error(), "model not found") {
				availableModels, _ := s.routeService.GetAvailableModels()
				return fmt.Errorf("model '%s' not found in route list. Available models: %v", model, availableModels)
			}
			// 其他路由相关错误
			return fmt.Errorf("route lookup failed for model '%s': %v", model, err)
		}
		// 如果找到了带前缀的模型，更新请求体中的模型名为原始模型名（不带前缀）
		// route.Model 已经被 GetRouteByForwarding 设置为原始模型名
		if route.Model != model {
			reqData["model"] = route.Model
			requestBody, _ = json.Marshal(reqData)
			log.Infof("Updated model in request body from '%s' to '%s'", model, route.Model)
		}
	}

	// 不移除末尾斜杠，保留原始URL格式
	// 如果API URL末尾有斜杠，说明是固定路径，buildAdapterURL会正确处理
	cleanAPIUrl := route.APIUrl

	// 强制使用指定的适配器（如果为空则不使用适配器转换请求）
	var transformedBody []byte
	var targetURL string
	var adapter adapters.Adapter

	if forceAdapter != "" {
		// 使用指定的适配器转换请求
		adapter = adapters.GetAdapter(forceAdapter)
		if adapter == nil {
			return fmt.Errorf("forced adapter not found: %s", forceAdapter)
		}

		// 确保开启stream
		reqData["stream"] = true
		// 使用 route.Model（已去除前缀的原始模型名）来适配请求
		transformedReq, err := adapter.AdaptRequest(reqData, route.Model)
		if err != nil {
			log.Errorf("Failed to adapt request: %v", err)
			return err
		}
		transformedBody, _ = json.Marshal(transformedReq)
		targetURL = s.buildAdapterStreamURL(cleanAPIUrl, forceAdapter, route.Model)
	} else {
		// 不使用适配器，直接转发原始请求
		transformedBody = requestBody
		adapter = nil
		targetURL = buildOpenAIChatURL(route.APIUrl)

		// 确保开启stream
		reqData["stream"] = true
	}
	log.Infof("Streaming to: %s (route: %s, adapter: %s)", targetURL, route.Name, forceAdapter)

	// 详细日志：记录流式请求目标路由信�?
	log.Infof("=== STREAM ROUTE TARGET ===")
	log.Infof("Stream target URL: %s", targetURL)
	log.Infof("Stream route name: %s", route.Name)
	log.Infof("Stream route API URL: %s", route.APIUrl)
	log.Infof("Stream route model: %s", route.Model)
	log.Infof("Stream route format: %s", route.Format)
	log.Infof("Stream route group: %s", route.Group)
	log.Infof("Stream route enabled: %v", route.Enabled)
	log.Infof("Stream adapter used: %s", forceAdapter)
	log.Infof("Stream transformed body: %s", string(transformedBody))
	log.Infof("=== STREAM ROUTE TARGET END ===")

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

	// Claude需要特殊的版本�?
	if forceAdapter == "anthropic" {
		proxyReq.Header.Set("anthropic-version", "2023-06-01")
	}

	// 发送请�?
	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("backend error: %d - %s", resp.StatusCode, string(body))
	}

	// 需要转换SSE流，使用实际路由到的模型�?
	return s.streamWithAdapter(resp.Body, writer, flusher, "openai-to-claude", model, route.ID)
}

// ProxyStreamRequestWithClaudeConversion 代理流式请求，保持原始请求格式但将响应转换为 Claude 格式
func (s *ProxyService) ProxyStreamRequestWithClaudeConversion(requestBody []byte, headers map[string]string, writer io.Writer, flusher http.Flusher) error {
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

	// 详细日志：记录流式请求开�?
	log.Infof("=== STREAM PROXY REQUEST START (CLAUDE CONVERSION) ===")
	log.Infof("Stream request model: %s", originalModel)
	log.Infof("Stream request headers:")
	for k, v := range headers {
		// 隐藏敏感信息
		if strings.Contains(strings.ToLower(k), "authorization") || strings.Contains(strings.ToLower(k), "key") {
			log.Infof("  %s: ***REDACTED***", k)
		} else {
			log.Infof("  %s: %s", k, v)
		}
	}
	log.Infof("Stream request body: %s", string(requestBody))

	// 提取真实的模型名（处理 Gemini streamGenerateContent 的情况）
	realModel := model
	if strings.Contains(model, ":streamGenerateContent") {
		realModel = strings.TrimSuffix(model, ":streamGenerateContent")
	}

	// 首先检查是否是重定向关键字
	var route *database.ModelRoute
	var err error
	isRedirect := s.config.RedirectEnabled && (realModel == s.config.RedirectKeyword || strings.HasPrefix(realModel, s.config.RedirectKeyword+":"))

	if isRedirect {
		route, err = s.getRedirectRoute()
		if err != nil {
			return fmt.Errorf("redirect target not configured or not found: %v", err)
		}
		log.Infof("Redirecting %s to route: %s (model: %s, id: %d)", realModel, route.Name, route.Model, route.ID)
		model = route.Model
		reqData["model"] = model
		requestBody, _ = json.Marshal(reqData)
	} else {
		// 查找路由
		route, err = s.routeService.GetRouteByForwarding(model)
		if err != nil {
			// 检查是否是"模型未找到"错误
			if strings.Contains(err.Error(), "model not found") {
				availableModels, _ := s.routeService.GetAvailableModels()
				return fmt.Errorf("model '%s' not found in route list. Available models: %v", model, availableModels)
			}
			// 其他路由相关错误
			return fmt.Errorf("route lookup failed for model '%s': %v", model, err)
		}
		// 如果找到了带前缀的模型，更新请求体中的模型名为原始模型名（不带前缀）
		// route.Model 已经被 GetRouteByForwarding 设置为原始模型名
		if route.Model != model {
			reqData["model"] = route.Model
			requestBody, _ = json.Marshal(reqData)
			log.Infof("Updated model in request body from '%s' to '%s'", model, route.Model)
		}
	}

	log.Infof("=== STREAM ROUTE TARGET ===")
	log.Infof("Stream target URL: %s", buildOpenAIChatURL(route.APIUrl))
	log.Infof("Stream route name: %s", route.Name)
	log.Infof("Stream route API URL: %s", route.APIUrl)
	log.Infof("Stream route model: %s", route.Model)
	log.Infof("Stream route format: %s", route.Format)
	log.Infof("Stream route group: %s", route.Group)
	log.Infof("Stream route enabled: %v", route.Enabled)
	log.Infof("Stream adapter used: openai-to-claude (response conversion only)")
	log.Infof("=== STREAM ROUTE TARGET END ===")

	// 创建代理请求
	proxyReq, err := http.NewRequest("POST", buildOpenAIChatURL(route.APIUrl), bytes.NewReader(requestBody))
	if err != nil {
		return err
	}

	proxyReq.Header.Set("Content-Type", "application/json")
	if route.APIKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	} else if auth := headers["Authorization"]; auth != "" {
		proxyReq.Header.Set("Authorization", auth)
	}

	// 发送请�?
	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("backend error: %d - %s", resp.StatusCode, string(body))
	}

	// 需要转换SSE流，使用实际路由到的模型�?
	return s.streamWithAdapter(resp.Body, writer, flusher, "openai-to-claude", model, route.ID)
}

// ProxyAnthropicRequest 代理 Anthropic 专用请求，不转换响应格式
func (s *ProxyService) ProxyAnthropicRequest(requestBody []byte, headers map[string]string) ([]byte, int, error) {
	// 解析请求
	var reqData map[string]interface{}
	if err := json.Unmarshal(requestBody, &reqData); err != nil {
		return nil, http.StatusBadRequest, fmt.Errorf("invalid JSON body: %v", err)
	}

	model, ok := reqData["model"].(string)
	if !ok || model == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("'model' field is required")
	}

	log.Infof("Received Anthropic request for model: %s", model)

	// 提取真实的模型名（处理可能的后缀）
	realModel := model
	if strings.Contains(model, ":streamGenerateContent") {
		realModel = strings.TrimSuffix(model, ":streamGenerateContent")
	}

	// 首先检查是否是重定向关键字（支持带后缀的模型名）
	var route *database.ModelRoute
	var err error
	isRedirect := s.config.RedirectEnabled && (realModel == s.config.RedirectKeyword || strings.HasPrefix(realModel, s.config.RedirectKeyword+":"))

	if isRedirect {
		route, err = s.getRedirectRoute()
		if err != nil {
			return nil, http.StatusNotFound, fmt.Errorf("redirect target not configured or not found: %v", err)
		}
		log.Infof("Redirecting %s to route: %s (model: %s, id: %d)", realModel, route.Name, route.Model, route.ID)
		model = route.Model
		reqData["model"] = model

		// 重新编码请求体
		requestBody, _ = json.Marshal(reqData)
	} else {
		// 查找路由
		route, err = s.routeService.GetRouteByForwarding(model)
		if err != nil {
			availableModels, _ := s.routeService.GetAvailableModels()
			return nil, http.StatusNotFound, fmt.Errorf("model '%s' not found in route list. Available models: %v", model, availableModels)
		}
	}

	// 检测是否需要进�?API 转换
	// 对于 Anthropic 接口，我们收到的�?Anthropic 格式的请�?
	var transformedBody []byte
	var targetURL string

	// 不移除末尾斜杠，保留原始URL格式
	// 如果API URL末尾有斜杠，说明是固定路径，buildAdapterURL会正确处理
	cleanAPIUrl := route.APIUrl

	// 智能检测适配�? 请求来自Claude格式,检测目标格�?
	adapterName := s.detectAdapterForRoute(route, "claude")

	if adapterName == "" {
		// 相同格式,直接转发 Anthropic 请求
		transformedBody = requestBody
		targetURL = buildClaudeMessagesURL(cleanAPIUrl)
		log.Infof("Forwarding Anthropic request directly (no conversion needed)")
	} else if adapterName == "claude-to-openai" {
		// 上游�?OpenAI 格式，需要将 Anthropic 格式转换�?OpenAI 格式
		adapter := adapters.GetAdapter("claude-to-openai")
		if adapter == nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("claude-to-openai adapter not found")
		}

		transformedReq, err := adapter.AdaptRequest(reqData, model)
		if err != nil {
			log.Errorf("Failed to adapt Anthropic request to OpenAI format: %v", err)
			return nil, http.StatusInternalServerError, err
		}
		transformedBody, _ = json.Marshal(transformedReq)
		targetURL = buildOpenAIChatURL(route.APIUrl)
		log.Infof("Converting Anthropic request to OpenAI format for upstream")
	} else {
		// 其他适配器暂不支�?
		log.Warnf("Unsupported adapter for Anthropic request: %s", adapterName)
		transformedBody = requestBody
		targetURL = buildClaudeMessagesURL(cleanAPIUrl)
	}

	log.Infof("Routing Anthropic request to: %s (route: %s)", targetURL, route.Name)

	// 创建代理请求
	proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(transformedBody))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	// 设置请求�?
	proxyReq.Header.Set("Content-Type", "application/json")

	// 使用路由配置�?API Key（如果有），否则透传原始 Authorization
	if route.APIKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	} else if auth := headers["Authorization"]; auth != "" {
		proxyReq.Header.Set("Authorization", auth)
	}

	// Claude需要特殊的版本�?
	if adapterName == "" && normalizeFormat(route.Format) == "claude" {
		proxyReq.Header.Set("anthropic-version", "2023-06-01")
	}

	// 发送请�?
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

	// 记录使用情况和响应转换（如果上游�?OpenAI 格式�?
	if resp.StatusCode == http.StatusOK {
		var respData map[string]interface{}
		if err := json.Unmarshal(responseBody, &respData); err == nil {
			// 记录使用情况
			if usage, ok := respData["usage"].(map[string]interface{}); ok {
				if totalTokens, ok := usage["total_tokens"].(float64); ok {
					promptTokens := 0
					completionTokens := 0
					if pt, ok := usage["prompt_tokens"].(float64); ok {
						promptTokens = int(pt)
					}
					if ct, ok := usage["completion_tokens"].(float64); ok {
						completionTokens = int(ct)
					}
					s.routeService.LogRequest(model, route.ID, promptTokens, completionTokens, int(totalTokens), true, "")
				}
			}

			// 如果使用了claude-to-openai适配�?说明上游是OpenAI格式,需要将响应转换�?Anthropic 格式
			if adapterName == "claude-to-openai" {
				log.Infof("Converting OpenAI response to Anthropic format for /api/anthropic endpoint")
				// �?OpenAI 格式响应转换�?Anthropic 格式
				anthropicResp := s.convertOpenAIToAnthropicResponse(respData)
				if convertedBody, err := json.Marshal(anthropicResp); err == nil {
					log.Infof("Successfully converted response to Anthropic format")
					return convertedBody, resp.StatusCode, nil
				} else {
					log.Errorf("Failed to marshal Anthropic response: %v", err)
				}
			}
		} else {
			log.Errorf("Failed to unmarshal response body: %v", err)
		}
	} else {
		s.routeService.LogRequest(model, route.ID, 0, 0, 0, false, string(responseBody))
	}

	// 对于 Anthropic 上游或转换失败的情况，返回原始响�?
	log.Infof("Returning original response (adapter=%s)", adapterName)
	return responseBody, resp.StatusCode, nil
}

// ProxyAnthropicStreamRequest 代理 Anthropic 专用流式请求
// 请求来自 /api/anthropic/v1/messages，格式为 Claude 格式
// 根据路由配置的 format 决定是否需要转换
func (s *ProxyService) ProxyAnthropicStreamRequest(requestBody []byte, headers map[string]string, writer io.Writer, flusher http.Flusher) error {
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
	var route *database.ModelRoute
	var err error
	isRedirect := s.config.RedirectEnabled && (realModel == s.config.RedirectKeyword || strings.HasPrefix(realModel, s.config.RedirectKeyword+":"))

	if isRedirect {
		route, err = s.getRedirectRoute()
		if err != nil {
			return fmt.Errorf("redirect target not configured or not found: %v", err)
		}
		log.Infof("Redirecting %s to route: %s (model: %s, id: %d)", realModel, route.Name, route.Model, route.ID)
		model = route.Model
		reqData["model"] = model
		requestBody, _ = json.Marshal(reqData)
	} else {
		// 查找路由
		route, err = s.routeService.GetRouteByForwarding(model)
		if err != nil {
			// 检查是否是"模型未找到"错误
			if strings.Contains(err.Error(), "model not found") {
				availableModels, _ := s.routeService.GetAvailableModels()
				return fmt.Errorf("model '%s' not found in route list. Available models: %v", model, availableModels)
			}
			// 其他路由相关错误
			return fmt.Errorf("route lookup failed for model '%s': %v", model, err)
		}
		// 如果找到了带前缀的模型，更新请求体中的模型名为原始模型名（不带前缀）
		// route.Model 已经被 GetRouteByForwarding 设置为原始模型名
		if route.Model != model {
			reqData["model"] = route.Model
			requestBody, _ = json.Marshal(reqData)
			log.Infof("Updated model in request body from '%s' to '%s'", model, route.Model)
		}
	}

	// 不移除末尾斜杠，保留原始URL格式
	// 如果API URL末尾有斜杠，说明是固定路径，buildAdapterURL会正确处理
	cleanAPIUrl := route.APIUrl

	// 智能检测适配�? 请求来自 Claude 格式 (因为�?/api/anthropic 路径)
	adapterName := s.detectAdapterForRoute(route, "claude")
	var transformedBody []byte
	var targetURL string

	log.Infof("[Anthropic Stream] Request format: claude, Route format: %s, Adapter: %s", route.Format, adapterName)

	if adapterName == "claude-to-openai" {
		// 目标是OpenAI 格式，需要将 Claude 请求转换为 OpenAI 格式
		adapter := adapters.GetAdapter("claude-to-openai")
		if adapter == nil {
			return fmt.Errorf("adapter not found: claude-to-openai")
		}

		// 确保开启stream
		reqData["stream"] = true
		// 使用 route.Model（已去除前缀的原始模型名）来适配请求
		transformedReq, err := adapter.AdaptRequest(reqData, route.Model)
		if err != nil {
			log.Errorf("Failed to adapt request: %v", err)
			return err
		}
		transformedBody, _ = json.Marshal(transformedReq)
		targetURL = buildOpenAIChatURL(route.APIUrl)
		log.Infof("Streaming to: %s (route: %s, adapter: claude-to-openai)", targetURL, route.Name)
	} else {
		// 目标也是 Claude 格式，直接透传�?/v1/messages
		transformedBody = requestBody
		targetURL = buildClaudeMessagesURL(cleanAPIUrl)
		log.Infof("Streaming to: %s (route: %s, passthrough)", targetURL, route.Name)
	}

	// 详细日志：记录流式请求目标路由信�?
	log.Infof("=== STREAM ROUTE TARGET ===")
	log.Infof("Stream target URL: %s", targetURL)
	log.Infof("Stream route name: %s", route.Name)
	log.Infof("Stream route API URL: %s", route.APIUrl)
	log.Infof("Stream route model: %s", route.Model)
	log.Infof("Stream route format: %s", route.Format)
	log.Infof("Stream route group: %s", route.Group)
	log.Infof("Stream route enabled: %v", route.Enabled)
	log.Infof("Stream adapter used: %s", adapterName)
	log.Infof("Stream transformed body: %s", string(transformedBody))
	log.Infof("=== STREAM ROUTE TARGET END ===")

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

	// Claude需要特殊的版本�?
	if adapterName == "anthropic" {
		proxyReq.Header.Set("anthropic-version", "2023-06-01")
	}

	// 发送请�?
	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("backend error: %d - %s", resp.StatusCode, string(body))
	}

	// 根据适配器决定如何处理响应流
	// 使用实际路由到的模型名（model）而不是原始请求的模型名（originalModel）用于统�?
	_ = originalModel // 保留原始模型名用于响�?
	if adapterName == "claude-to-openai" {
		// 需要将 OpenAI 流式响应转换�?Claude 流式响应
		log.Infof("[Anthropic Stream] Converting OpenAI stream response to Claude format")
		return s.streamOpenAIToClaude(resp.Body, writer, flusher, model, route.ID)
	}

	// 直接转发SSE流（目标�?Claude 格式，无需转换�?
	return s.streamDirect(resp.Body, writer, flusher, model, route.ID)
}

// streamWithAdapter 使用适配器处理流式响�?
func (s *ProxyService) streamWithAdapter(reader io.Reader, writer io.Writer, flusher http.Flusher, adapterName, model string, routeID int64) error {
	// 获取反向适配器（用于响应转换�?
	// 例如：请求用 openai-to-claude，响应应该用 claude-to-openai
	reverseAdapterName := getReverseAdapterName(adapterName)
	if reverseAdapterName == "" {
		return fmt.Errorf("no reverse adapter for: %s", adapterName)
	}

	log.Infof("[Stream Adapter] Request adapter: %s, Response adapter: %s", adapterName, reverseAdapterName)

	adapter := adapters.GetAdapter(reverseAdapterName)
	if adapter == nil {
		return fmt.Errorf("adapter not found: %s", reverseAdapterName)
	}

	// 发送开始事�?
	startEvents := adapter.AdaptStreamStart(model)
	log.Infof("[Stream Adapter] Sending %d start events", len(startEvents))
	for _, event := range startEvents {
		eventData, _ := json.Marshal(event)
		log.Infof("[STREAM TO CLIENT] Start event: %s", string(eventData))
		fmt.Fprintf(writer, "data: %s\n\n", string(eventData))
	}
	flusher.Flush()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024) // 1MB max

	// 用于累积token统计信息
	var totalPromptTokens int
	var totalCompletionTokens int
	var chunkCount int

	log.Infof("[Stream Adapter] Starting to read chunks from backend...")

	for scanner.Scan() {
		line := scanner.Text()

		log.Infof("[Stream Adapter] Raw line from backend: %s", line)

		// 跳过空行和事件行
		if line == "" || strings.HasPrefix(line, "event:") {
			log.Infof("[Stream Adapter] Skipping line (empty or event)")
			continue
		}

		log.Infof("[Stream Adapter] Processing data line: %s", line)

		// 处理SSE格式: "data: {...}" �?"data:{...}"
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data) // 去掉可能的空�?

			// 检查是否是结束标记
			if data == "[DONE]" {
				fmt.Fprintf(writer, "data: [DONE]\n\n")
				flusher.Flush()
				totalTokens := totalPromptTokens + totalCompletionTokens
				s.routeService.LogRequest(model, routeID, totalPromptTokens, totalCompletionTokens, totalTokens, true, "")
				return nil
			}

			// 解析JSON
			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				log.Warnf("Failed to parse chunk: %v, data: %s", err, data)
				continue
			}

			// 从原始chunk中提取token使用信息（适配器转换前）
			// 根据反向适配器判断远端格式
			if strings.HasPrefix(reverseAdapterName, "claude-") || reverseAdapterName == "anthropic" {
				// 远端是 Claude 格式
				if chunkType, ok := chunk["type"].(string); ok {
					switch chunkType {
					case "message_start":
						if message, ok := chunk["message"].(map[string]interface{}); ok {
							if usage, ok := message["usage"].(map[string]interface{}); ok {
								if inputTokens, ok := usage["input_tokens"].(float64); ok {
									totalPromptTokens = int(inputTokens)
								}
							}
						}
					case "message_delta":
						// Claude 的 message_delta 格式：
						// { "type": "message_delta", "delta": {...}, "usage": {...} }
						// usage 字段在根级别，不在 delta 里面
						if usage, ok := chunk["usage"].(map[string]interface{}); ok {
							if inputTokens, ok := usage["input_tokens"].(float64); ok {
								totalPromptTokens = int(inputTokens)
							}
							if outputTokens, ok := usage["output_tokens"].(float64); ok {
								totalCompletionTokens = int(outputTokens)
							}
						}
					}
				}
			} else if strings.HasPrefix(reverseAdapterName, "gemini-") || reverseAdapterName == "gemini" {
				// 远端是 Gemini 格式
				if usageMetadata, ok := chunk["usageMetadata"].(map[string]interface{}); ok {
					if promptTokens, ok := usageMetadata["promptTokenCount"].(float64); ok {
						totalPromptTokens = int(promptTokens)
					}
					if candidatesTokens, ok := usageMetadata["candidatesTokenCount"].(float64); ok {
						totalCompletionTokens = int(candidatesTokens)
					}
				}
			} else {
				// 默认处理 OpenAI 格式（适用于大多数情况）
				// OpenAI 格式的 token 信息在 choices[x].finish_reason 和 usage 字段中
				if usage, ok := chunk["usage"].(map[string]interface{}); ok {
					if promptTokens, ok := usage["prompt_tokens"].(float64); ok {
						totalPromptTokens = int(promptTokens)
					}
					if completionTokens, ok := usage["completion_tokens"].(float64); ok {
						totalCompletionTokens = int(completionTokens)
					}
				}
			}

			// 使用适配器转换chunk
			log.Infof("[Stream Adapter] Calling adapter.AdaptStreamChunk() for chunk type: %v", chunk["type"])
			adaptedChunk, err := adapter.AdaptStreamChunk(chunk)
			if err != nil {
				log.Warnf("[Stream Adapter] Failed to adapt chunk: %v", err)
				continue
			}

			log.Infof("[Stream Adapter] Adapter returned: adaptedChunk=%v (is nil: %v)", adaptedChunk, adaptedChunk == nil)

			// 只有�?adaptedChunk 不为 nil 时才发�?
			if adaptedChunk != nil {
				chunkCount++
				// 发送转换后的chunk
				adaptedData, _ := json.Marshal(adaptedChunk)
				log.Infof("[STREAM TO CLIENT] Chunk #%d: %s", chunkCount, string(adaptedData))
				fmt.Fprintf(writer, "data: %s\n\n", string(adaptedData))
				flusher.Flush()
			} else {
				log.Infof("[Stream Adapter] Adapted chunk is nil - skipping")
			}
		}
	}

	log.Infof("[Stream Adapter] Finished reading stream. Total chunks sent: %d", chunkCount)

	if err := scanner.Err(); err != nil {
		s.routeService.LogRequest(model, routeID, totalPromptTokens, totalCompletionTokens, totalPromptTokens+totalCompletionTokens, false, err.Error())
		return err
	}

	// 发送结束事�?
	endEvents := adapter.AdaptStreamEnd()
	for _, event := range endEvents {
		eventData, _ := json.Marshal(event)
		log.Infof("[STREAM TO CLIENT] %s", string(eventData))
		fmt.Fprintf(writer, "data: %s\n\n", string(eventData))
	}
	flusher.Flush()

	totalTokens := totalPromptTokens + totalCompletionTokens
	s.routeService.LogRequest(model, routeID, totalPromptTokens, totalCompletionTokens, totalTokens, true, "")
	return nil
}

// streamDirect 直接转发流式响应
func (s *ProxyService) streamDirect(reader io.Reader, writer io.Writer, flusher http.Flusher, model string, routeID int64) error {
	buf := make([]byte, 4096)
	var responseBuffer bytes.Buffer

	for {
		n, err := reader.Read(buf)
		if n > 0 {
			// 将数据写入缓冲区以便后续解析token使用信息
			responseBuffer.Write(buf[:n])

			if _, writeErr := writer.Write(buf[:n]); writeErr != nil {
				s.routeService.LogRequest(model, routeID, 0, 0, 0, false, writeErr.Error())
				return writeErr
			}
			flusher.Flush()
		}
		if err != nil {
			if err == io.EOF {
				// 尝试从响应中提取token使用信息
				promptTokens, completionTokens := s.extractTokensFromStreamResponse(responseBuffer.String())
				totalTokens := promptTokens + completionTokens
				s.routeService.LogRequest(model, routeID, promptTokens, completionTokens, totalTokens, true, "")
				return nil
			}
			s.routeService.LogRequest(model, routeID, 0, 0, 0, false, err.Error())
			return err
		}
	}
}

// streamOpenAIToClaude �?OpenAI 流式响应转换�?Claude 流式响应
// 用于 /api/anthropic 路径，当目标�?OpenAI 格式 API �?
func (s *ProxyService) streamOpenAIToClaude(reader io.Reader, writer io.Writer, flusher http.Flusher, model string, routeID int64) error {
	// 发�?Claude 流式响应的开始事�?
	messageID := fmt.Sprintf("msg_%d", time.Now().UnixNano())

	// message_start 事件
	messageStart := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            messageID,
			"type":          "message",
			"role":          "assistant",
			"content":       []interface{}{},
			"model":         model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]interface{}{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	}
	startData, _ := json.Marshal(messageStart)
	fmt.Fprintf(writer, "event: message_start\ndata: %s\n\n", string(startData))
	flusher.Flush()

	// content_block_start 事件
	contentBlockStart := map[string]interface{}{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	}
	blockStartData, _ := json.Marshal(contentBlockStart)
	fmt.Fprintf(writer, "event: content_block_start\ndata: %s\n\n", string(blockStartData))
	flusher.Flush()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	var totalPromptTokens int
	var totalCompletionTokens int

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				break
			}

			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			// 提取 token 使用信息
			if usage, ok := chunk["usage"].(map[string]interface{}); ok {
				if pt, ok := usage["prompt_tokens"].(float64); ok {
					totalPromptTokens = int(pt)
				}
				if ct, ok := usage["completion_tokens"].(float64); ok {
					totalCompletionTokens = int(ct)
				}
			}

			// 提取内容
			if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						if content, ok := delta["content"].(string); ok && content != "" {
							// 发�?content_block_delta 事件
							deltaEvent := map[string]interface{}{
								"type":  "content_block_delta",
								"index": 0,
								"delta": map[string]interface{}{
									"type": "text_delta",
									"text": content,
								},
							}
							deltaData, _ := json.Marshal(deltaEvent)
							fmt.Fprintf(writer, "event: content_block_delta\ndata: %s\n\n", string(deltaData))
							flusher.Flush()
						}
					}

					// 检查是否结�?
					if finishReason, ok := choice["finish_reason"].(string); ok && finishReason != "" {
						// 不在这里处理结束，等循环结束后统一处理
					}
				}
			}
		}
	}

	// content_block_stop 事件
	contentBlockStop := map[string]interface{}{
		"type":  "content_block_stop",
		"index": 0,
	}
	blockStopData, _ := json.Marshal(contentBlockStop)
	fmt.Fprintf(writer, "event: content_block_stop\ndata: %s\n\n", string(blockStopData))
	flusher.Flush()

	// message_delta 事件
	messageDelta := map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
		},
		"usage": map[string]interface{}{
			"output_tokens": totalCompletionTokens,
		},
	}
	deltaData, _ := json.Marshal(messageDelta)
	fmt.Fprintf(writer, "event: message_delta\ndata: %s\n\n", string(deltaData))
	flusher.Flush()

	// message_stop 事件
	messageStop := map[string]interface{}{
		"type": "message_stop",
	}
	stopData, _ := json.Marshal(messageStop)
	fmt.Fprintf(writer, "event: message_stop\ndata: %s\n\n", string(stopData))
	flusher.Flush()

	// 记录请求
	totalTokens := totalPromptTokens + totalCompletionTokens
	s.routeService.LogRequest(model, routeID, totalPromptTokens, totalCompletionTokens, totalTokens, true, "")

	return nil
}

// FetchRemoteModels 获取远程模型列表
func (s *ProxyService) FetchRemoteModels(apiUrl, apiKey string) ([]string, error) {
	// 记录原始 URL 是否�?"/" 结尾
	hasTrailingSlash := strings.HasSuffix(apiUrl, "/")

	// 移除末尾的斜�?
	apiUrl = strings.TrimSuffix(apiUrl, "/")

	// 添加 http/https 前缀（如果没有）
	if !strings.HasPrefix(apiUrl, "http://") && !strings.HasPrefix(apiUrl, "https://") {
		apiUrl = "https://" + apiUrl
	}

	// 构建 URL：如果原�?URL 末尾�?"/"，说明用户已指定完整路径，只�?/models
	// 否则加上 /v1/models
	var url string
	if hasTrailingSlash {
		url = apiUrl + "/models"
	} else {
		url = apiUrl + "/v1/models"
	}
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

	// 读取响应�?
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

// detectAdapter 智能检测需要使用的适配�?
// 参数: route - 路由配置, requestFormat - 请求格式 (openai/claude/gemini)
// 基于路由的format字段和requestFormat进行智能判断
// 相同格式直接透传,不同格式才使用适配�?
func (s *ProxyService) detectAdapter(apiUrl, model string) string {
	// 保持向后兼容:如果是旧的URL检测逻辑,使用启发式检�?
	return s.detectAdapterByURL(apiUrl, model)
}

// detectAdapterForRoute 根据路由配置和请求格式智能检测适配�?
// requestFormat: "openai", "claude", "gemini"
// route.Format: 目标API的格�?
// 返回: 适配器名�?空字符串表示直接透传
func (s *ProxyService) detectAdapterForRoute(route *database.ModelRoute, requestFormat string) string {
	if route == nil {
		return ""
	}

	// 标准化请求格�?
	requestFormat = normalizeFormat(requestFormat)

	// 获取目标格式(路由配置的format)
	targetFormat := normalizeFormat(route.Format)
	if targetFormat == "" {
		// 如果路由没有明确指定format,根据URL和模型名推断
		targetFormat = inferFormatFromRoute(route.APIUrl, route.Model)
	}

	log.Infof("[Format Detection] Request=%s, Target=%s, Route=%s", requestFormat, targetFormat, route.Name)

	// 相同格式直接透传
	if requestFormat == targetFormat {
		log.Infof("[Format Match] Same format detected, using passthrough")
		return ""
	}

	// 不同格式需要转�?
	return getAdapterName(requestFormat, targetFormat)
}

// normalizeFormat 标准化格式名�?
func normalizeFormat(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "claude", "anthropic":
		return "claude"
	case "gemini", "google":
		return "gemini"
	case "openai", "gpt", "":
		return "openai"
	default:
		return "openai"
	}
}

// inferFormatFromRoute 从路由信息推断格�?
func inferFormatFromRoute(apiUrl, model string) string {
	lowerURL := strings.ToLower(apiUrl)
	lowerModel := strings.ToLower(model)

	// 基于URL判断
	if strings.Contains(lowerURL, "anthropic") || strings.Contains(lowerURL, "claude") {
		return "claude"
	}
	if strings.Contains(lowerURL, "gemini") || strings.Contains(lowerURL, "googleapis.com") {
		return "gemini"
	}

	// 基于模型名判�?
	if strings.HasPrefix(lowerModel, "claude-") {
		return "claude"
	}
	if strings.HasPrefix(lowerModel, "gemini-") {
		return "gemini"
	}

	// 默认�?OpenAI 格式
	return "openai"
}

// getAdapterName 根据源格式和目标格式返回适配器名�?
func getAdapterName(requestFormat, targetFormat string) string {
	key := requestFormat + "->" + targetFormat
	switch key {
	case "openai->claude":
		return "openai-to-claude"
	case "openai->gemini":
		return "openai-to-gemini"
	case "claude->openai":
		return "claude-to-openai"
	case "gemini->openai":
		return "gemini-to-openai"
	case "claude->gemini":
		return "claude-to-gemini"
	case "gemini->claude":
		return "gemini-to-claude"
	default:
		log.Warnf("[Adapter] Unsupported conversion: %s", key)
		return ""
	}
}

// getReverseAdapterName 获取反向适配器名称（用于响应转换�?
// 例如：请求用 openai-to-claude，响应应该用 claude-to-openai
func getReverseAdapterName(adapterName string) string {
	switch adapterName {
	case "openai-to-claude":
		return "claude-to-openai"
	case "claude-to-openai":
		return "openai-to-claude"
	case "openai-to-gemini":
		return "gemini-to-openai"
	case "gemini-to-openai":
		return "openai-to-gemini"
	case "claude-to-gemini":
		return "gemini-to-claude"
	case "gemini-to-claude":
		return "claude-to-gemini"
	case "anthropic":
		// 旧的 anthropic 适配器名称，映射�?claude-to-openai
		return "claude-to-openai"
	case "gemini":
		// 旧的 gemini 适配器名称，映射�?gemini-to-openai
		return "gemini-to-openai"
	default:
		log.Warnf("[Adapter] No reverse adapter for: %s", adapterName)
		return ""
	}
}

// detectAdapterByURL 旧的基于URL的启发式检测逻辑(向后兼容)
func (s *ProxyService) detectAdapterByURL(apiUrl, model string) string {
	lowerURL := strings.ToLower(apiUrl)
	lowerModel := strings.ToLower(model)

	// 先检查是否是标准�?OpenAI 格式 API 端点，如果是，则不应用任何适配�?
	// 这样可以避免�?Gemini 适配器错误地应用�?OpenAI 兼容�?API �?
	if isStandardOpenAIEndpoint(lowerURL) {
		return "" // 对标�?OpenAI 端点不使用适配�?
	}

	// 精确内容检�?- 基于API URL和模型名称中的关键词
	// 使用更精确的匹配避免误匹配，例如避免 "glm" �?"gemini" 的混�?
	if containsExactWord(lowerURL, "anthropic") || containsExactWord(lowerModel, "claude") {
		return "anthropic"
	}

	// 使用更严格的匹配来检�?Gemini，避�?"glm" �?"gemini" 等误匹配
	if containsExactWord(lowerURL, "gemini") || containsExactWord(lowerModel, "gemini") {
		return "gemini"
	}

	if containsExactWord(lowerURL, "deepseek") || containsExactWord(lowerModel, "deepseek") {
		return "deepseek"
	}

	return "" // 不需要适配�?
}

// buildAdapterURL 构建适配�?URL
func (s *ProxyService) buildAdapterURL(apiURL, adapterName, model string) string {
	switch adapterName {
	case "anthropic", "openai-to-claude":
		return buildClaudeMessagesURL(apiURL)
	case "gemini", "openai-to-gemini":
		// Gemini 使用不同�?URL 格式
		if strings.HasSuffix(apiURL, "/") {
			// 末尾有斜杠，去掉/v1（如果存在）然后添加models路径
			if strings.Contains(apiURL, "/v1/") {
				// 去掉/v1/部分
				baseUrl := strings.Replace(apiURL, "/v1/", "/", 1)
				return fmt.Sprintf("%smodels/%s:generateContent", baseUrl, model)
			} else if strings.HasSuffix(apiURL, "/v1") {
				// 去掉末尾�?v1
				baseUrl := strings.TrimSuffix(apiURL, "/v1")
				return fmt.Sprintf("%s/models/%s:generateContent", baseUrl, model)
			}
			// 末尾有斜杠但没有/v1，直接使用当前路�?
			return fmt.Sprintf("%smodels/%s:generateContent", apiURL, model)
		}
		return fmt.Sprintf("%s/v1/models/%s:generateContent", apiURL, model)
	case "deepseek":
		return buildOpenAIChatURL(apiURL)
	default:
		return buildOpenAIChatURL(apiURL)
	}
}

// buildAdapterStreamURL 构建适配器流�?URL
func (s *ProxyService) buildAdapterStreamURL(apiURL, adapterName, model string) string {
	switch adapterName {
	case "anthropic", "openai-to-claude":
		return buildClaudeMessagesURL(apiURL)
	case "gemini", "openai-to-gemini":
		// Gemini 使用不同�?URL 格式
		if strings.HasSuffix(apiURL, "/") {
			// 末尾有斜杠，去掉/v1（如果存在）然后添加models路径
			if strings.Contains(apiURL, "/v1/") {
				// 去掉/v1/部分
				baseUrl := strings.Replace(apiURL, "/v1/", "/", 1)
				return fmt.Sprintf("%smodels/%s:streamGenerateContent", baseUrl, model)
			} else if strings.HasSuffix(apiURL, "/v1") {
				// 去掉末尾�?v1
				baseUrl := strings.TrimSuffix(apiURL, "/v1")
				return fmt.Sprintf("%s/models/%s:streamGenerateContent", baseUrl, model)
			}
			// 末尾有斜杠但没有/v1，直接使用当前路�?
			return fmt.Sprintf("%smodels/%s:streamGenerateContent", apiURL, model)
		}
		return fmt.Sprintf("%s/v1/models/%s:streamGenerateContent", apiURL, model)
	case "deepseek":
		return buildOpenAIChatURL(apiURL)
	default:
		return buildOpenAIChatURL(apiURL)
	}
}

// isStandardOpenAIEndpoint 检�?URL 是否为标准的 OpenAI API 端点
// 如果是，则不应该应用任何适配器转�?
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
		containsExactWord(url, "/v1") { // 这太宽泛了，需要更具体的检�?
		// 对于 /v1 路径，需要更具体的检查，仅在确实包含 chat/completions 时才认为�?OpenAI API
		if strings.Contains(url, "/v1/chat/completions") ||
			strings.Contains(url, "/v1/completions") {
			return true
		}
	}

	return false
}

// containsExactWord 检�?needle 是否作为一个独立的单词存在�?haystack �?
// 通过检查边界字符来确保精确匹配，避免子串误匹配
func containsExactWord(haystack, needle string) bool {
	if haystack == needle {
		return true
	}

	// 使用更精确的查找方法，确�?needle 前后是边界或分隔�?
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

// isAlphanumeric 检查字符是否为字母或数�?
func isAlphanumeric(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// buildOpenAIChatURL 智能构建 OpenAI chat completions URL
// 如果 apiUrl 末尾�?/，则不添�?/v1 前缀（用于兼容如智谱等非标准路径�?API�?
// 例如�?
//   - https://api.openai.com/v1 -> https://api.openai.com/v1/chat/completions
//   - https://open.bigmodel.cn/api/coding/paas/v4/ -> https://open.bigmodel.cn/api/coding/paas/v4/chat/completions
//   - https://api.kkyyxx.xyz -> https://api.kkyyxx.xyz/v1/chat/completions
func buildOpenAIChatURL(apiUrl string) string {
	if strings.HasSuffix(apiUrl, "/") {
		// 末尾有斜杠，说明是固定API路径，直接添�?chat/completions
		return apiUrl + "chat/completions"
	}
	// 末尾没有斜杠，是标准API，需要添�?/v1/chat/completions
	return apiUrl + "/v1/chat/completions"
}

// buildClaudeMessagesURL 智能构建 Claude messages URL
func buildClaudeMessagesURL(apiUrl string) string {
	if strings.HasSuffix(apiUrl, "/") {
		// 末尾有斜杠，去掉/v1（如果存在）然后添加messages
		if strings.Contains(apiUrl, "/v1/") {
			// 去掉/v1/部分
			baseUrl := strings.Replace(apiUrl, "/v1/", "/", 1)
			return baseUrl + "messages"
		} else if strings.HasSuffix(apiUrl, "/v1") {
			// 去掉末尾�?v1
			baseUrl := strings.TrimSuffix(apiUrl, "/v1")
			return baseUrl + "/messages"
		}
		// 末尾有斜杠但没有/v1，直接使用当前路�?+ messages
		return apiUrl + "messages"
	}
	return apiUrl + "/v1/messages"
}

// convertOpenAIToAnthropicResponse �?OpenAI 格式响应转换�?Anthropic 格式
func (s *ProxyService) convertOpenAIToAnthropicResponse(openaiResp map[string]interface{}) map[string]interface{} {
	// OpenAI 响应格式示例:
	// {
	//   "id": "chatcmpl-...",
	//   "object": "chat.completion",
	//   "created": 1234567890,
	//   "model": "gpt-3.5-turbo",
	//   "choices": [
	//     {
	//       "index": 0,
	//       "message": {
	//         "role": "assistant",
	//         "content": "Hello! How can I help you?"
	//       },
	//       "finish_reason": "stop"
	//     }
	//   ],
	//   "usage": {
	//     "prompt_tokens": 10,
	//     "completion_tokens": 20,
	//     "total_tokens": 30
	//   }
	// }
	//
	// Anthropic 响应格式示例:
	// {
	//   "id": "msg_...",
	//   "type": "message",
	//   "role": "assistant",
	//   "content": [
	//     {
	//       "type": "text",
	//       "text": "Hello! How can I help you?"
	//     }
	//   ],
	//   "model": "claude-3-haiku-20240307",
	//   "stop_reason": "end_turn",
	//   "usage": {
	//     "input_tokens": 10,
	//     "output_tokens": 20
	//   }
	// }

	anthropicResp := make(map[string]interface{})

	// 复制 ID
	if id, ok := openaiResp["id"].(string); ok {
		anthropicResp["id"] = id
	} else {
		anthropicResp["id"] = "msg_" + fmt.Sprintf("%d", time.Now().UnixNano())
	}

	// 设置类型
	anthropicResp["type"] = "message"

	// 设置角色
	anthropicResp["role"] = "assistant"

	// 复制模型�?
	if model, ok := openaiResp["model"].(string); ok {
		anthropicResp["model"] = model
	}

	// 转换 content - �?choices[0].message.content �?content[{type, text}]
	if choices, ok := openaiResp["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := message["content"].(string); ok {
					anthropicResp["content"] = []map[string]interface{}{
						{
							"type": "text",
							"text": content,
						},
					}
				}
			}

			// 转换 finish_reason �?stop_reason
			if finishReason, ok := choice["finish_reason"].(string); ok {
				switch finishReason {
				case "stop":
					anthropicResp["stop_reason"] = "end_turn"
				case "length":
					anthropicResp["stop_reason"] = "max_tokens"
				default:
					anthropicResp["stop_reason"] = finishReason
				}
			}
		}
	}

	// 转换 usage - �?{prompt_tokens, completion_tokens, total_tokens} �?{input_tokens, output_tokens}
	if usage, ok := openaiResp["usage"].(map[string]interface{}); ok {
		anthropicUsage := make(map[string]interface{})
		if promptTokens, ok := usage["prompt_tokens"].(float64); ok {
			anthropicUsage["input_tokens"] = int(promptTokens)
		}
		if completionTokens, ok := usage["completion_tokens"].(float64); ok {
			anthropicUsage["output_tokens"] = int(completionTokens)
		}
		anthropicResp["usage"] = anthropicUsage
	}

	log.Infof("Converted OpenAI response to Anthropic format: %+v", anthropicResp)
	return anthropicResp
}

// convertClaudeToOpenAIResponse �?Claude 格式响应转换�?OpenAI 格式
func (s *ProxyService) convertClaudeToOpenAIResponse(claudeResp map[string]interface{}) map[string]interface{} {
	openaiResp := make(map[string]interface{})

	// 基本字段
	if id, ok := claudeResp["id"].(string); ok {
		openaiResp["id"] = id
	} else {
		openaiResp["id"] = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	}
	openaiResp["object"] = "chat.completion"
	openaiResp["created"] = time.Now().Unix()

	if model, ok := claudeResp["model"].(string); ok {
		openaiResp["model"] = model
	}

	// 转换 content
	var content string
	if contentArray, ok := claudeResp["content"].([]interface{}); ok {
		for _, item := range contentArray {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if itemType, ok := itemMap["type"].(string); ok && itemType == "text" {
					if text, ok := itemMap["text"].(string); ok {
						content += text
					}
				}
			}
		}
	}

	// 转换 stop_reason
	finishReason := "stop"
	if stopReason, ok := claudeResp["stop_reason"].(string); ok {
		switch stopReason {
		case "end_turn":
			finishReason = "stop"
		case "max_tokens":
			finishReason = "length"
		case "tool_use":
			finishReason = "tool_calls"
		default:
			finishReason = stopReason
		}
	}

	openaiResp["choices"] = []interface{}{
		map[string]interface{}{
			"index": 0,
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": content,
			},
			"finish_reason": finishReason,
		},
	}

	// 转换 usage
	if usage, ok := claudeResp["usage"].(map[string]interface{}); ok {
		openaiUsage := make(map[string]interface{})
		if inputTokens, ok := usage["input_tokens"].(float64); ok {
			openaiUsage["prompt_tokens"] = int(inputTokens)
		}
		if outputTokens, ok := usage["output_tokens"].(float64); ok {
			openaiUsage["completion_tokens"] = int(outputTokens)
		}
		if promptTokens, ok := openaiUsage["prompt_tokens"].(int); ok {
			if completionTokens, ok := openaiUsage["completion_tokens"].(int); ok {
				openaiUsage["total_tokens"] = promptTokens + completionTokens
			}
		}
		openaiResp["usage"] = openaiUsage
	}

	return openaiResp
}

// convertOpenAIToGeminiResponse �?OpenAI 格式响应转换�?Gemini 格式
func (s *ProxyService) convertOpenAIToGeminiResponse(openaiResp map[string]interface{}) map[string]interface{} {
	geminiResp := make(map[string]interface{})

	// 转换 choices �?candidates
	var text string
	var finishReason string

	if choices, ok := openaiResp["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := message["content"].(string); ok {
					text = content
				}
			}
			if fr, ok := choice["finish_reason"].(string); ok {
				switch fr {
				case "stop":
					finishReason = "STOP"
				case "length":
					finishReason = "MAX_TOKENS"
				default:
					finishReason = "STOP"
				}
			}
		}
	}

	geminiResp["candidates"] = []interface{}{
		map[string]interface{}{
			"content": map[string]interface{}{
				"role": "model",
				"parts": []interface{}{
					map[string]interface{}{
						"text": text,
					},
				},
			},
			"finishReason": finishReason,
		},
	}

	// 转换 usage
	if usage, ok := openaiResp["usage"].(map[string]interface{}); ok {
		usageMetadata := make(map[string]interface{})
		if promptTokens, ok := usage["prompt_tokens"].(float64); ok {
			usageMetadata["promptTokenCount"] = int(promptTokens)
		} else if promptTokens, ok := usage["prompt_tokens"].(int); ok {
			usageMetadata["promptTokenCount"] = promptTokens
		}
		if completionTokens, ok := usage["completion_tokens"].(float64); ok {
			usageMetadata["candidatesTokenCount"] = int(completionTokens)
		} else if completionTokens, ok := usage["completion_tokens"].(int); ok {
			usageMetadata["candidatesTokenCount"] = completionTokens
		}
		if totalTokens, ok := usage["total_tokens"].(float64); ok {
			usageMetadata["totalTokenCount"] = int(totalTokens)
		} else if totalTokens, ok := usage["total_tokens"].(int); ok {
			usageMetadata["totalTokenCount"] = totalTokens
		}
		geminiResp["usageMetadata"] = usageMetadata
	}

	return geminiResp
}

// extractTokensFromStreamResponse 从流式响应中提取token使用信息（支�?OpenAI �?Claude 格式�?
func (s *ProxyService) extractTokensFromStreamResponse(response string) (promptTokens, completionTokens int) {
	// 将响应按行分�?
	lines := strings.Split(response, "\n")

	for _, line := range lines {
		// 处理SSE格式: "data: {...}" �?"data:{...}"
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data)

			// 跳过结束标记
			if data == "[DONE]" {
				continue
			}

			// 解析JSON
			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			// OpenAI 格式: usage.prompt_tokens, usage.completion_tokens
			if usage, ok := chunk["usage"].(map[string]interface{}); ok {
				if pt, ok := usage["prompt_tokens"].(float64); ok && promptTokens == 0 {
					promptTokens = int(pt)
				}
				if ct, ok := usage["completion_tokens"].(float64); ok {
					completionTokens = int(ct) // 使用最后一个�?
				}
			}

			// Claude 格式: message_start.message.usage.input_tokens
			if chunkType, ok := chunk["type"].(string); ok {
				switch chunkType {
				case "message_start":
					if message, ok := chunk["message"].(map[string]interface{}); ok {
						if usage, ok := message["usage"].(map[string]interface{}); ok {
							if inputTokens, ok := usage["input_tokens"].(float64); ok {
								promptTokens = int(inputTokens)
							}
						}
					}
				case "message_delta":
					if usage, ok := chunk["usage"].(map[string]interface{}); ok {
						if outputTokens, ok := usage["output_tokens"].(float64); ok {
							completionTokens = int(outputTokens)
						}
					}
				}
			}
		}
	}

	return promptTokens, completionTokens
}

// ProxyGeminiRequest 代理 Gemini 格式的非流式请求
// 请求来自 /api/v1/gemini/models/{model}:generateContent
func (s *ProxyService) ProxyGeminiRequest(requestBody []byte, headers map[string]string) ([]byte, int, error) {
	// 解析请求
	var reqData map[string]interface{}
	if err := json.Unmarshal(requestBody, &reqData); err != nil {
		return nil, http.StatusBadRequest, fmt.Errorf("invalid JSON body: %v", err)
	}

	model, ok := reqData["model"].(string)
	if !ok || model == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("'model' field is required")
	}

	// 首先检查是否是重定向关键字
	var route *database.ModelRoute
	var err error
	isRedirect := s.config.RedirectEnabled && (model == s.config.RedirectKeyword || strings.HasPrefix(model, s.config.RedirectKeyword+":"))

	if isRedirect {
		route, err = s.getRedirectRoute()
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("redirect target not configured or not found: %v", err)
		}
		log.Infof("Redirecting %s to route: %s (model: %s, id: %d)", model, route.Name, route.Model, route.ID)
		model = route.Model
		reqData["model"] = model
	} else {
		// 查找路由
		route, err = s.routeService.GetRouteByForwarding(model)
		if err != nil {
			availableModels, _ := s.routeService.GetAvailableModels()
			return nil, http.StatusNotFound, fmt.Errorf("model '%s' not found in route list. Available models: %v", model, availableModels)
		}
	}

	// 清理路由 API URL
	cleanAPIUrl := strings.TrimSuffix(route.APIUrl, "/")

	var transformedBody []byte
	var targetURL string

	// 获取目标格式
	targetFormat := normalizeFormat(route.Format)
	if targetFormat == "" {
		targetFormat = inferFormatFromRoute(route.APIUrl, route.Model)
	}

	log.Infof("[Gemini Request] Request format: gemini, Target format: %s, Route: %s", targetFormat, route.Name)

	// 用于标记响应转换类型
	var needConvertResponse string // "none", "openai", "claude"

	if targetFormat == "gemini" {
		// 目标也是 Gemini 格式，直接透传
		transformedBody = requestBody
		targetURL = fmt.Sprintf("%s/v1beta/models/%s:generateContent", cleanAPIUrl, model)
		needConvertResponse = "none"
		log.Infof("Forwarding Gemini request directly to: %s", targetURL)
	} else if targetFormat == "openai" {
		// 目标�?OpenAI 格式，需要将 Gemini 请求转换�?OpenAI 格式
		adapter := adapters.GetAdapter("gemini-to-openai")
		if adapter == nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("gemini-to-openai adapter not found")
		}

		transformedReq, err := adapter.AdaptRequest(reqData, model)
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}
		transformedBody, _ = json.Marshal(transformedReq)
		targetURL = buildOpenAIChatURL(route.APIUrl)
		needConvertResponse = "openai"
		log.Infof("Converting Gemini -> OpenAI, target: %s", targetURL)
	} else if targetFormat == "claude" {
		// 目标�?Claude 格式，需�?Gemini -> OpenAI -> Claude 两步转换
		// 第一步：Gemini -> OpenAI
		geminiToOpenAI := adapters.GetAdapter("gemini-to-openai")
		if geminiToOpenAI == nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("gemini-to-openai adapter not found")
		}

		openaiReq, err := geminiToOpenAI.AdaptRequest(reqData, model)
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("gemini-to-openai conversion failed: %v", err)
		}

		// 第二步：OpenAI -> Claude
		openaiToClaude := adapters.GetAdapter("openai-to-claude")
		if openaiToClaude == nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("openai-to-claude adapter not found")
		}

		claudeReq, err := openaiToClaude.AdaptRequest(openaiReq, model)
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("openai-to-claude conversion failed: %v", err)
		}

		transformedBody, _ = json.Marshal(claudeReq)
		targetURL = buildClaudeMessagesURL(cleanAPIUrl)
		needConvertResponse = "claude"
		log.Infof("Converting Gemini -> OpenAI -> Claude, target: %s", targetURL)
	} else {
		return nil, http.StatusInternalServerError, fmt.Errorf("unsupported target format: %s", targetFormat)
	}

	// 创建代理请求
	proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(transformedBody))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	// 根据目标格式设置请求�?
	proxyReq.Header.Set("Content-Type", "application/json")

	if route.APIKey != "" {
		switch targetFormat {
		case "claude":
			// Claude 格式使用 x-api-key
			proxyReq.Header.Set("x-api-key", route.APIKey)
			proxyReq.Header.Set("anthropic-version", "2023-06-01")
		case "gemini":
			// Gemini 使用 x-goog-api-key
			proxyReq.Header.Set("x-goog-api-key", route.APIKey)
		default:
			// OpenAI 格式使用 Bearer token
			proxyReq.Header.Set("Authorization", "Bearer "+route.APIKey)
		}
	}

	// 发送请�?
	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		return nil, http.StatusServiceUnavailable, fmt.Errorf("backend service unavailable: %v", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	// 根据需要转换响�?
	if resp.StatusCode == http.StatusOK && needConvertResponse != "none" {
		var respData map[string]interface{}
		if err := json.Unmarshal(responseBody, &respData); err == nil {
			switch needConvertResponse {
			case "openai":
				// OpenAI �?Gemini
				log.Infof("[Gemini Request] Converting OpenAI response to Gemini format")
				geminiResp := s.convertOpenAIToGeminiResponse(respData)
				if convertedBody, err := json.Marshal(geminiResp); err == nil {
					return convertedBody, resp.StatusCode, nil
				}
			case "claude":
				// Claude �?OpenAI �?Gemini
				log.Infof("[Gemini Request] Converting Claude response to Gemini format")
				// 先将 Claude 转换�?OpenAI
				openaiResp := s.convertClaudeToOpenAIResponse(respData)
				// 再将 OpenAI 转换�?Gemini
				geminiResp := s.convertOpenAIToGeminiResponse(openaiResp)
				if convertedBody, err := json.Marshal(geminiResp); err == nil {
					return convertedBody, resp.StatusCode, nil
				}
			}
		}
	}

	return responseBody, resp.StatusCode, nil
}

// ProxyGeminiStreamRequest 代理 Gemini 格式的流式请求
// 请求来自 /api/v1/gemini/models/{model}:streamGenerateContent
func (s *ProxyService) ProxyGeminiStreamRequest(requestBody []byte, headers map[string]string, writer io.Writer, flusher http.Flusher) error {
	// 解析请求
	var reqData map[string]interface{}
	if err := json.Unmarshal(requestBody, &reqData); err != nil {
		return fmt.Errorf("invalid JSON body: %v", err)
	}

	model, ok := reqData["model"].(string)
	if !ok || model == "" {
		return fmt.Errorf("'model' field is required")
	}

	// 首先检查是否是重定向关键字
	var route *database.ModelRoute
	var err error
	isRedirect := s.config.RedirectEnabled && (model == s.config.RedirectKeyword || strings.HasPrefix(model, s.config.RedirectKeyword+":"))

	if isRedirect {
		route, err = s.getRedirectRoute()
		if err != nil {
			return fmt.Errorf("redirect target not configured or not found: %v", err)
		}
		log.Infof("Redirecting %s to route: %s (model: %s, id: %d)", model, route.Name, route.Model, route.ID)
		model = route.Model
		reqData["model"] = model
		requestBody, _ = json.Marshal(reqData)
	} else {
		// 查找路由
		route, err = s.routeService.GetRouteByForwarding(model)
		if err != nil {
			availableModels, _ := s.routeService.GetAvailableModels()
			return fmt.Errorf("model '%s' not found in route list. Available models: %v", model, availableModels)
		}
	}

	// 清理路由 API URL
	cleanAPIUrl := strings.TrimSuffix(route.APIUrl, "/")

	var transformedBody []byte
	var targetURL string

	// 获取目标格式
	targetFormat := normalizeFormat(route.Format)
	if targetFormat == "" {
		targetFormat = inferFormatFromRoute(route.APIUrl, route.Model)
	}

	log.Infof("[Gemini Stream] Request format: gemini, Target format: %s, Route: %s", targetFormat, route.Name)

	// 用于标记响应转换类型
	var responseConversionType string // "none", "openai-to-gemini", "claude-to-gemini"

	if targetFormat == "gemini" {
		// 目标也是 Gemini 格式，直接透传
		transformedBody = requestBody
		targetURL = fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", cleanAPIUrl, model)
		responseConversionType = "none"
		log.Infof("Streaming Gemini request directly to: %s", targetURL)
	} else if targetFormat == "openai" {
		// 目标�?OpenAI 格式，需要将 Gemini 请求转换�?OpenAI 格式
		adapter := adapters.GetAdapter("gemini-to-openai")
		if adapter == nil {
			return fmt.Errorf("gemini-to-openai adapter not found")
		}

		reqData["stream"] = true
		transformedReq, err := adapter.AdaptRequest(reqData, model)
		if err != nil {
			return err
		}
		transformedBody, _ = json.Marshal(transformedReq)
		targetURL = buildOpenAIChatURL(route.APIUrl)
		responseConversionType = "openai-to-gemini"
		log.Infof("Converting Gemini -> OpenAI, target: %s", targetURL)
	} else if targetFormat == "claude" {
		// 目标�?Claude 格式，需�?Gemini -> OpenAI -> Claude 两步转换
		// 第一步：Gemini -> OpenAI
		geminiToOpenAI := adapters.GetAdapter("gemini-to-openai")
		if geminiToOpenAI == nil {
			return fmt.Errorf("gemini-to-openai adapter not found")
		}

		reqData["stream"] = true
		openaiReq, err := geminiToOpenAI.AdaptRequest(reqData, model)
		if err != nil {
			return fmt.Errorf("gemini-to-openai conversion failed: %v", err)
		}

		// 第二步：OpenAI -> Claude
		openaiToClaude := adapters.GetAdapter("openai-to-claude")
		if openaiToClaude == nil {
			return fmt.Errorf("openai-to-claude adapter not found")
		}

		claudeReq, err := openaiToClaude.AdaptRequest(openaiReq, model)
		if err != nil {
			return fmt.Errorf("openai-to-claude conversion failed: %v", err)
		}

		transformedBody, _ = json.Marshal(claudeReq)
		targetURL = buildClaudeMessagesURL(cleanAPIUrl)
		responseConversionType = "claude-to-gemini"
		log.Infof("Converting Gemini -> OpenAI -> Claude, target: %s", targetURL)
	} else {
		return fmt.Errorf("unsupported target format: %s", targetFormat)
	}

	// 创建代理请求
	proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(transformedBody))
	if err != nil {
		return err
	}

	// 根据目标格式设置请求�?
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("Accept", "text/event-stream")

	if route.APIKey != "" {
		switch targetFormat {
		case "claude":
			// Claude 格式使用 x-api-key
			proxyReq.Header.Set("x-api-key", route.APIKey)
			proxyReq.Header.Set("anthropic-version", "2023-06-01")
		case "gemini":
			// Gemini 使用 x-goog-api-key
			proxyReq.Header.Set("x-goog-api-key", route.APIKey)
		default:
			// OpenAI 格式使用 Bearer token
			proxyReq.Header.Set("Authorization", "Bearer "+route.APIKey)
		}
	}

	// 发送请�?
	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		return fmt.Errorf("backend service unavailable: %v", err)
	}
	defer resp.Body.Close()

	// 检查响应状�?
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("backend error: %d - %s", resp.StatusCode, string(body))
	}

	// 根据响应转换类型来处理流
	switch responseConversionType {
	case "openai-to-gemini":
		// �?OpenAI 流式响应转换�?Gemini 流式响应
		log.Infof("[Gemini Stream] Converting OpenAI stream response to Gemini format")
		return s.streamOpenAIToGemini(resp.Body, writer, flusher, model, route.ID)
	case "claude-to-gemini":
		// �?Claude 流式响应转换�?Gemini 流式响应
		log.Infof("[Gemini Stream] Converting Claude stream response to Gemini format")
		return s.streamClaudeToGemini(resp.Body, writer, flusher, model, route.ID)
	default:
		// 直接转发流式响应
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
			writer.Write(line)
			flusher.Flush()
		}
		return nil
	}
}

// streamOpenAIToGemini �?OpenAI 流式响应转换�?Gemini 流式响应
func (s *ProxyService) streamOpenAIToGemini(reader io.Reader, writer io.Writer, flusher http.Flusher, model string, routeID int64) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	var totalPromptTokens int
	var totalCompletionTokens int

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				// 发送最终的 Gemini 格式结束
				break
			}

			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			// 提取 token 使用信息
			if usage, ok := chunk["usage"].(map[string]interface{}); ok {
				if pt, ok := usage["prompt_tokens"].(float64); ok {
					totalPromptTokens = int(pt)
				}
				if ct, ok := usage["completion_tokens"].(float64); ok {
					totalCompletionTokens = int(ct)
				}
			}

			// 提取内容并转换为 Gemini 格式
			if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						if content, ok := delta["content"].(string); ok && content != "" {
							// 构建 Gemini 格式的流式响�?
							geminiChunk := map[string]interface{}{
								"candidates": []interface{}{
									map[string]interface{}{
										"content": map[string]interface{}{
											"role": "model",
											"parts": []interface{}{
												map[string]interface{}{
													"text": content,
												},
											},
										},
									},
								},
							}

							chunkData, _ := json.Marshal(geminiChunk)
							fmt.Fprintf(writer, "data: %s\n\n", string(chunkData))
							flusher.Flush()
						}
					}

					// 检查是否结�?
					if finishReason, ok := choice["finish_reason"].(string); ok && finishReason != "" {
						// 发送带�?finishReason 的最终块
						geminiChunk := map[string]interface{}{
							"candidates": []interface{}{
								map[string]interface{}{
									"finishReason": "STOP",
									"content": map[string]interface{}{
										"role":  "model",
										"parts": []interface{}{},
									},
								},
							},
							"usageMetadata": map[string]interface{}{
								"promptTokenCount":     totalPromptTokens,
								"candidatesTokenCount": totalCompletionTokens,
								"totalTokenCount":      totalPromptTokens + totalCompletionTokens,
							},
						}

						chunkData, _ := json.Marshal(geminiChunk)
						fmt.Fprintf(writer, "data: %s\n\n", string(chunkData))
						flusher.Flush()
					}
				}
			}
		}
	}

	// 记录请求
	totalTokens := totalPromptTokens + totalCompletionTokens
	s.routeService.LogRequest(model, routeID, totalPromptTokens, totalCompletionTokens, totalTokens, true, "")

	return nil
}

// streamClaudeToGemini �?Claude 流式响应转换�?Gemini 流式响应
func (s *ProxyService) streamClaudeToGemini(reader io.Reader, writer io.Writer, flusher http.Flusher, model string, routeID int64) error {
	log.Infof("[Claude->Gemini Stream] Starting conversion for model: %s", model)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	var totalInputTokens int
	var totalOutputTokens int
	var chunkCount int

	for scanner.Scan() {
		line := scanner.Text()

		// 跳过空行和事件行
		if line == "" || strings.HasPrefix(line, "event:") {
			continue
		}

		log.Infof("[Claude->Gemini Stream] Processing line: %s", line)

		// Claude SSE 格式: "data: {...}" �?"data:{...}"
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data) // 去掉可能的空�?

			var event map[string]interface{}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				log.Warnf("[Claude->Gemini Stream] Failed to parse JSON: %v, data: %s", err, data)
				continue
			}

			eventType, _ := event["type"].(string)
			log.Infof("[Claude->Gemini Stream] Event type: %s", eventType)

			switch eventType {
			case "message_start":
				// 提取 input_tokens
				if message, ok := event["message"].(map[string]interface{}); ok {
					if usage, ok := message["usage"].(map[string]interface{}); ok {
						if inputTokens, ok := usage["input_tokens"].(float64); ok {
							totalInputTokens = int(inputTokens)
						}
					}
				}

			case "content_block_delta":
				// 提取文本内容
				if delta, ok := event["delta"].(map[string]interface{}); ok {
					if deltaType, ok := delta["type"].(string); ok && deltaType == "text_delta" {
						if text, ok := delta["text"].(string); ok && text != "" {
							chunkCount++
							log.Infof("[Claude->Gemini Stream] Converting text chunk #%d: %s", chunkCount, text)

							// 构建 Gemini 格式的流式响�?
							geminiChunk := map[string]interface{}{
								"candidates": []interface{}{
									map[string]interface{}{
										"content": map[string]interface{}{
											"role": "model",
											"parts": []interface{}{
												map[string]interface{}{
													"text": text,
												},
											},
										},
									},
								},
							}

							chunkData, _ := json.Marshal(geminiChunk)
							log.Infof("[Claude->Gemini Stream] Sending to client: %s", string(chunkData))
							fmt.Fprintf(writer, "data: %s\n\n", string(chunkData))
							flusher.Flush()
						}
					}
				}

			case "message_delta":
				// 提取 output_tokens
				if usage, ok := event["usage"].(map[string]interface{}); ok {
					if outputTokens, ok := usage["output_tokens"].(float64); ok {
						totalOutputTokens = int(outputTokens)
					}
				}

			case "message_stop":
				// 发送带�?finishReason 的最终块
				geminiChunk := map[string]interface{}{
					"candidates": []interface{}{
						map[string]interface{}{
							"finishReason": "STOP",
							"content": map[string]interface{}{
								"role":  "model",
								"parts": []interface{}{},
							},
						},
					},
					"usageMetadata": map[string]interface{}{
						"promptTokenCount":     totalInputTokens,
						"candidatesTokenCount": totalOutputTokens,
						"totalTokenCount":      totalInputTokens + totalOutputTokens,
					},
				}

				chunkData, _ := json.Marshal(geminiChunk)
				fmt.Fprintf(writer, "data: %s\n\n", string(chunkData))
				flusher.Flush()
			}
		}
	}

	log.Infof("[Claude->Gemini Stream] Stream completed. Total chunks: %d, Input tokens: %d, Output tokens: %d",
		chunkCount, totalInputTokens, totalOutputTokens)

	// 记录请求
	totalTokens := totalInputTokens + totalOutputTokens
	s.routeService.LogRequest(model, routeID, totalInputTokens, totalOutputTokens, totalTokens, true, "")

	return nil
}

// ProxyClaudeCodeRequest 代理 Claude Code 专用请求
// 请求来自 /api/claudecode/v1/messages，格式为 Claude Code 格式（包含工具链、系统提示词等）
// 智能检测目标路由格式：如果目标是 Claude 格式则直接透传，如果是 OpenAI 格式则转换
func (s *ProxyService) ProxyClaudeCodeRequest(requestBody []byte, headers map[string]string) ([]byte, int, error) {
	// 解析请求
	var reqData map[string]interface{}
	if err := json.Unmarshal(requestBody, &reqData); err != nil {
		return nil, http.StatusBadRequest, fmt.Errorf("invalid JSON body: %v", err)
	}

	model, ok := reqData["model"].(string)
	if !ok || model == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("'model' field is required")
	}

	log.Infof("[Claude Code] Received request for model: %s", model)

	// 提取真实的模型名（处理可能的后缀）
	realModel := model
	if strings.Contains(model, ":streamGenerateContent") {
		realModel = strings.TrimSuffix(model, ":streamGenerateContent")
	}

	// 首先检查是否是重定向关键字
	var route *database.ModelRoute
	var err error
	isRedirect := s.config.RedirectEnabled && (realModel == s.config.RedirectKeyword || strings.HasPrefix(realModel, s.config.RedirectKeyword+":"))

	if isRedirect {
		route, err = s.getRedirectRoute()
		if err != nil {
			return nil, http.StatusNotFound, fmt.Errorf("redirect target not configured or not found: %v", err)
		}
		log.Infof("Redirecting %s to route: %s (model: %s, id: %d)", realModel, route.Name, route.Model, route.ID)
		model = route.Model
		reqData["model"] = model
		requestBody, _ = json.Marshal(reqData)
	} else {
		// 查找路由
		route, err = s.routeService.GetRouteByForwarding(model)
		if err != nil {
			availableModels, _ := s.routeService.GetAvailableModels()
			return nil, http.StatusNotFound, fmt.Errorf("model '%s' not found in route list. Available models: %v", model, availableModels)
		}
	}

	// 清理路由 API URL
	cleanAPIUrl := strings.TrimSuffix(route.APIUrl, "/")

	// 智能检测目标路由格�?
	targetFormat := normalizeFormat(route.Format)
	if targetFormat == "" {
		targetFormat = inferFormatFromRoute(route.APIUrl, route.Model)
	}

	log.Infof("[Claude Code] Target route format: %s", targetFormat)

	var transformedBody []byte
	var targetURL string
	var needConvertResponse bool

	if targetFormat == "claude" || targetFormat == "anthropic" {
		// 目标�?Claude 格式，直接透传请求
		log.Infof("[Claude Code] Target is Claude format, passing through directly")
		transformedBody = requestBody
		targetURL = buildClaudeMessagesURL(cleanAPIUrl)
		needConvertResponse = false
	} else {
		// 目标�?OpenAI 格式，需要转�?
		log.Infof("[Claude Code] Target is OpenAI format, converting request")
		adapter := adapters.GetAdapter("claudecode-to-openai")
		if adapter == nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("claudecode-to-openai adapter not found")
		}

		transformedReq, err := adapter.AdaptRequest(reqData, model)
		if err != nil {
			log.Errorf("[Claude Code] Failed to adapt request: %v", err)
			return nil, http.StatusInternalServerError, err
		}
		transformedBody, _ = json.Marshal(transformedReq)
		targetURL = buildOpenAIChatURL(route.APIUrl)
		needConvertResponse = true
	}

	log.Infof("[Claude Code] Routing to: %s (route: %s)", targetURL, route.Name)

	// 创建代理请求
	proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(transformedBody))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	// 设置请求�?
	proxyReq.Header.Set("Content-Type", "application/json")
	if targetFormat == "claude" || targetFormat == "anthropic" {
		// Claude 格式使用 x-api-key
		if route.APIKey != "" {
			proxyReq.Header.Set("x-api-key", route.APIKey)
		}
		proxyReq.Header.Set("anthropic-version", "2023-06-01")
	} else {
		// OpenAI 格式使用 Bearer token
		if route.APIKey != "" {
			proxyReq.Header.Set("Authorization", "Bearer "+route.APIKey)
		} else if auth := headers["Authorization"]; auth != "" {
			proxyReq.Header.Set("Authorization", auth)
		}
	}

	// 发送请�?
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

	log.Infof("[Claude Code] Response received from %s in %v, status: %d", route.Name, time.Since(startTime), resp.StatusCode)

	// 记录使用情况并处理响�?
	if resp.StatusCode == http.StatusOK {
		var respData map[string]interface{}
		if err := json.Unmarshal(responseBody, &respData); err == nil {
			// 记录 token 使用
			if needConvertResponse {
				// OpenAI 格式响应
				if usage, ok := respData["usage"].(map[string]interface{}); ok {
					promptTokens := 0
					completionTokens := 0
					totalTokens := 0
					if pt, ok := usage["prompt_tokens"].(float64); ok {
						promptTokens = int(pt)
					}
					if ct, ok := usage["completion_tokens"].(float64); ok {
						completionTokens = int(ct)
					}
					if tt, ok := usage["total_tokens"].(float64); ok {
						totalTokens = int(tt)
					}
					s.routeService.LogRequest(model, route.ID, promptTokens, completionTokens, totalTokens, true, "")
				}

				// �?OpenAI 响应转换�?Claude 格式
				log.Infof("[Claude Code] Converting OpenAI response to Claude format")
				adapter := adapters.GetAdapter("claudecode-to-openai")
				if adapter != nil {
					claudeResp, err := adapter.AdaptResponse(respData)
					if err != nil {
						log.Errorf("[Claude Code] Failed to adapt response: %v", err)
					} else {
						if convertedBody, err := json.Marshal(claudeResp); err == nil {
							log.Infof("[Claude Code] Successfully converted response to Claude format")
							return convertedBody, resp.StatusCode, nil
						}
					}
				}
			} else {
				// Claude 格式响应，直接透传，但记录 token
				if usage, ok := respData["usage"].(map[string]interface{}); ok {
					inputTokens := 0
					outputTokens := 0
					if it, ok := usage["input_tokens"].(float64); ok {
						inputTokens = int(it)
					}
					if ot, ok := usage["output_tokens"].(float64); ok {
						outputTokens = int(ot)
					}
					s.routeService.LogRequest(model, route.ID, inputTokens, outputTokens, inputTokens+outputTokens, true, "")
				}
				// 直接返回 Claude 格式响应
				return responseBody, resp.StatusCode, nil
			}
		}
	} else {
		s.routeService.LogRequest(model, route.ID, 0, 0, 0, false, string(responseBody))
	}

	return responseBody, resp.StatusCode, nil
}

// ProxyClaudeCodeStreamRequest 代理 Claude Code 专用流式请求
// 请求来自 /api/claudecode/v1/messages，格式为 Claude Code 格式
// 智能检测目标路由格式：如果目标是 Claude 格式则直接透传，如果是 OpenAI 格式则转换
func (s *ProxyService) ProxyClaudeCodeStreamRequest(requestBody []byte, headers map[string]string, writer io.Writer, flusher http.Flusher) error {
	// 解析请求
	var reqData map[string]interface{}
	if err := json.Unmarshal(requestBody, &reqData); err != nil {
		return fmt.Errorf("invalid JSON body: %v", err)
	}

	model, ok := reqData["model"].(string)
	if !ok || model == "" {
		return fmt.Errorf("'model' field is required")
	}

	log.Infof("[Claude Code Stream] Received request for model: %s", model)

	// 提取真实的模型名
	realModel := model
	if strings.Contains(model, ":streamGenerateContent") {
		realModel = strings.TrimSuffix(model, ":streamGenerateContent")
	}

	// 首先检查是否是重定向关键字
	var route *database.ModelRoute
	var err error
	isRedirect := s.config.RedirectEnabled && (realModel == s.config.RedirectKeyword || strings.HasPrefix(realModel, s.config.RedirectKeyword+":"))

	if isRedirect {
		route, err = s.getRedirectRoute()
		if err != nil {
			return fmt.Errorf("redirect target not configured or not found: %v", err)
		}
		log.Infof("Redirecting %s to route: %s (model: %s, id: %d)", realModel, route.Name, route.Model, route.ID)
		model = route.Model
		reqData["model"] = model
		requestBody, _ = json.Marshal(reqData)
	} else {
		// 查找路由
		route, err = s.routeService.GetRouteByForwarding(model)
		if err != nil {
			if strings.Contains(err.Error(), "model not found") {
				availableModels, _ := s.routeService.GetAvailableModels()
				return fmt.Errorf("model '%s' not found in route list. Available models: %v", model, availableModels)
			}
			return fmt.Errorf("route lookup failed for model '%s': %v", model, err)
		}
	}

	// 清理路由 API URL
	cleanAPIUrl := strings.TrimSuffix(route.APIUrl, "/")

	// 智能检测目标路由格�?
	targetFormat := normalizeFormat(route.Format)
	if targetFormat == "" {
		targetFormat = inferFormatFromRoute(route.APIUrl, route.Model)
	}

	log.Infof("[Claude Code Stream] Target route format: %s", targetFormat)

	var transformedBody []byte
	var targetURL string
	var needConvertResponse bool

	// 确保开�?stream
	reqData["stream"] = true

	if targetFormat == "claude" || targetFormat == "anthropic" {
		// 目标�?Claude 格式，直接透传请求
		log.Infof("[Claude Code Stream] Target is Claude format, passing through directly")
		requestBody, _ = json.Marshal(reqData)
		transformedBody = requestBody
		targetURL = buildClaudeMessagesURL(cleanAPIUrl)
		needConvertResponse = false
	} else {
		// 目标�?OpenAI 格式，需要转�?
		log.Infof("[Claude Code Stream] Target is OpenAI format, converting request")
		adapter := adapters.GetAdapter("claudecode-to-openai")
		if adapter == nil {
			return fmt.Errorf("claudecode-to-openai adapter not found")
		}

		transformedReq, err := adapter.AdaptRequest(reqData, model)
		if err != nil {
			log.Errorf("[Claude Code Stream] Failed to adapt request: %v", err)
			return err
		}
		transformedBody, _ = json.Marshal(transformedReq)
		targetURL = buildOpenAIChatURL(route.APIUrl)
		needConvertResponse = true
	}

	log.Infof("[Claude Code Stream] Streaming to: %s (route: %s)", targetURL, route.Name)

	// 创建代理请求
	proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(transformedBody))
	if err != nil {
		return err
	}

	proxyReq.Header.Set("Content-Type", "application/json")
	if targetFormat == "claude" || targetFormat == "anthropic" {
		// Claude 格式使用 x-api-key
		if route.APIKey != "" {
			proxyReq.Header.Set("x-api-key", route.APIKey)
		}
		proxyReq.Header.Set("anthropic-version", "2023-06-01")
	} else {
		// OpenAI 格式使用 Bearer token
		if route.APIKey != "" {
			proxyReq.Header.Set("Authorization", "Bearer "+route.APIKey)
		} else if auth := headers["Authorization"]; auth != "" {
			proxyReq.Header.Set("Authorization", auth)
		}
	}

	// 发送请�?
	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("backend error: %d - %s", resp.StatusCode, string(body))
	}

	// 使用实际路由到的模型名用于统�?
	if needConvertResponse {
		// �?OpenAI 流式响应转换�?Claude 流式响应
		log.Infof("[Claude Code Stream] Converting OpenAI stream response to Claude format")
		return s.streamOpenAIToClaudeCode(resp.Body, writer, flusher, model, route.ID)
	} else {
		// Claude 格式响应，直接透传
		log.Infof("[Claude Code Stream] Passing through Claude stream response directly")
		return s.streamDirect(resp.Body, writer, flusher, model, route.ID)
	}
}

// streamOpenAIToClaudeCode �?OpenAI 流式响应转换�?Claude Code 流式响应
// 专门用于 /api/claudecode 路径，支持工具调用等高级功能
func (s *ProxyService) streamOpenAIToClaudeCode(reader io.Reader, writer io.Writer, flusher http.Flusher, model string, routeID int64) error {
	// 发�?Claude 流式响应的开始事�?
	messageID := fmt.Sprintf("msg_%d", time.Now().UnixNano())

	// message_start 事件
	messageStart := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            messageID,
			"type":          "message",
			"role":          "assistant",
			"content":       []interface{}{},
			"model":         model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]interface{}{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	}
	startData, _ := json.Marshal(messageStart)
	fmt.Fprintf(writer, "event: message_start\ndata: %s\n\n", string(startData))
	flusher.Flush()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	var totalPromptTokens int
	var totalCompletionTokens int
	var contentBlockStarted bool
	var toolCallsStarted bool
	var currentToolCalls []map[string]interface{}
	var contentIndex int

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				break
			}

			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			// 提取 token 使用信息
			if usage, ok := chunk["usage"].(map[string]interface{}); ok {
				if pt, ok := usage["prompt_tokens"].(float64); ok {
					totalPromptTokens = int(pt)
				}
				if ct, ok := usage["completion_tokens"].(float64); ok {
					totalCompletionTokens = int(ct)
				}
			}

			// 提取内容
			if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						// 处理文本内容
						if content, ok := delta["content"].(string); ok && content != "" {
							if !contentBlockStarted {
								// content_block_start 事件
								contentBlockStart := map[string]interface{}{
									"type":  "content_block_start",
									"index": contentIndex,
									"content_block": map[string]interface{}{
										"type": "text",
										"text": "",
									},
								}
								blockStartData, _ := json.Marshal(contentBlockStart)
								fmt.Fprintf(writer, "event: content_block_start\ndata: %s\n\n", string(blockStartData))
								flusher.Flush()
								contentBlockStarted = true
							}

							// 发�?content_block_delta 事件
							deltaEvent := map[string]interface{}{
								"type":  "content_block_delta",
								"index": contentIndex,
								"delta": map[string]interface{}{
									"type": "text_delta",
									"text": content,
								},
							}
							deltaData, _ := json.Marshal(deltaEvent)
							fmt.Fprintf(writer, "event: content_block_delta\ndata: %s\n\n", string(deltaData))
							flusher.Flush()
						}

						// 处理工具调用
						if toolCalls, ok := delta["tool_calls"].([]interface{}); ok {
							for _, tc := range toolCalls {
								if tcMap, ok := tc.(map[string]interface{}); ok {
									// 关闭之前的文本块
									if contentBlockStarted && !toolCallsStarted {
										contentBlockStop := map[string]interface{}{
											"type":  "content_block_stop",
											"index": contentIndex,
										}
										blockStopData, _ := json.Marshal(contentBlockStop)
										fmt.Fprintf(writer, "event: content_block_stop\ndata: %s\n\n", string(blockStopData))
										flusher.Flush()
										contentIndex++
									}
									toolCallsStarted = true

									// 获取工具调用索引
									tcIndex := 0
									if idx, ok := tcMap["index"].(float64); ok {
										tcIndex = int(idx)
									}

									// 确保 currentToolCalls 有足够的空间
									for len(currentToolCalls) <= tcIndex {
										currentToolCalls = append(currentToolCalls, map[string]interface{}{
											"id":        "",
											"name":      "",
											"arguments": "",
										})
									}

									// 更新工具调用信息
									if id, ok := tcMap["id"].(string); ok && id != "" {
										currentToolCalls[tcIndex]["id"] = id
									}
									if function, ok := tcMap["function"].(map[string]interface{}); ok {
										if name, ok := function["name"].(string); ok && name != "" {
											currentToolCalls[tcIndex]["name"] = name

											// 发�?tool_use content_block_start
											toolBlockStart := map[string]interface{}{
												"type":  "content_block_start",
												"index": contentIndex + tcIndex,
												"content_block": map[string]interface{}{
													"type":  "tool_use",
													"id":    currentToolCalls[tcIndex]["id"],
													"name":  name,
													"input": map[string]interface{}{},
												},
											}
											toolBlockStartData, _ := json.Marshal(toolBlockStart)
											fmt.Fprintf(writer, "event: content_block_start\ndata: %s\n\n", string(toolBlockStartData))
											flusher.Flush()
										}
										if args, ok := function["arguments"].(string); ok && args != "" {
											currentToolCalls[tcIndex]["arguments"] = currentToolCalls[tcIndex]["arguments"].(string) + args

											// 发�?input_json_delta
											inputDelta := map[string]interface{}{
												"type":  "content_block_delta",
												"index": contentIndex + tcIndex,
												"delta": map[string]interface{}{
													"type":         "input_json_delta",
													"partial_json": args,
												},
											}
											inputDeltaData, _ := json.Marshal(inputDelta)
											fmt.Fprintf(writer, "event: content_block_delta\ndata: %s\n\n", string(inputDeltaData))
											flusher.Flush()
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// 关闭所有打开的内容块
	if contentBlockStarted && !toolCallsStarted {
		contentBlockStop := map[string]interface{}{
			"type":  "content_block_stop",
			"index": contentIndex,
		}
		blockStopData, _ := json.Marshal(contentBlockStop)
		fmt.Fprintf(writer, "event: content_block_stop\ndata: %s\n\n", string(blockStopData))
		flusher.Flush()
	}

	// 关闭工具调用�?
	for i := range currentToolCalls {
		toolBlockStop := map[string]interface{}{
			"type":  "content_block_stop",
			"index": contentIndex + i,
		}
		toolBlockStopData, _ := json.Marshal(toolBlockStop)
		fmt.Fprintf(writer, "event: content_block_stop\ndata: %s\n\n", string(toolBlockStopData))
		flusher.Flush()
	}

	// 确定 stop_reason
	stopReason := "end_turn"
	if len(currentToolCalls) > 0 {
		stopReason = "tool_use"
	}

	// message_delta 事件
	messageDelta := map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]interface{}{
			"output_tokens": totalCompletionTokens,
		},
	}
	deltaData, _ := json.Marshal(messageDelta)
	fmt.Fprintf(writer, "event: message_delta\ndata: %s\n\n", string(deltaData))
	flusher.Flush()

	// message_stop 事件
	messageStop := map[string]interface{}{
		"type": "message_stop",
	}
	stopData, _ := json.Marshal(messageStop)
	fmt.Fprintf(writer, "event: message_stop\ndata: %s\n\n", string(stopData))
	flusher.Flush()

	// 记录请求
	totalTokens := totalPromptTokens + totalCompletionTokens
	s.routeService.LogRequest(model, routeID, totalPromptTokens, totalCompletionTokens, totalTokens, true, "")

	return nil
}
