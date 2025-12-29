package router

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"openai-router-go/internal/config"
	"openai-router-go/internal/service"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func SetupAPIRouter(cfg *config.Config, routeService *service.RouteService, proxyService *service.ProxyService) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	// 自定义日志中间件
	r.Use(func(c *gin.Context) {
		c.Next()
		log.Infof("%s %s %d", c.Request.Method, c.Request.URL.Path, c.Writer.Status())
	})

	// 移除请求体大小限制
	r.MaxMultipartMemory = 512 << 20 // 512MB

	// 创建对话聚合服务
	conversationService := service.NewConversationService(routeService, proxyService, cfg)

	// API 密钥验证中间件
	apiKeyAuth := func(c *gin.Context) {
		// 如果没有配置本地 API Key，则跳过验证
		if cfg.LocalAPIKey == "" {
			c.Next()
			return
		}

		// 从 Authorization header 获取 API Key
		authHeader := c.GetHeader("Authorization")
		apiKey := ""

		if strings.HasPrefix(authHeader, "Bearer ") {
			apiKey = strings.TrimPrefix(authHeader, "Bearer ")
		} else if strings.HasPrefix(authHeader, "bearer ") {
			apiKey = strings.TrimPrefix(authHeader, "bearer ")
		}

		// 也支持从 x-api-key header 获取
		if apiKey == "" {
			apiKey = c.GetHeader("x-api-key")
		}

		// 支持 Gemini 风格的 x-goog-api-key header
		if apiKey == "" {
			apiKey = c.GetHeader("x-goog-api-key")
		}

		// 支持 Gemini 风格的 URL 参数 key=xxx
		if apiKey == "" {
			apiKey = c.Query("key")
		}

		// 调试日志：打印收到的认证信息
		log.Debugf("API Key Auth - Authorization: %s, x-api-key: %s, x-goog-api-key: %s, query key: %s",
			authHeader, c.GetHeader("x-api-key"), c.GetHeader("x-goog-api-key"), c.Query("key"))

		// 跳过 OPTIONS 请求（CORS 预检请求）
		if c.Request.Method == "OPTIONS" {
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, x-goog-api-key")
			c.Status(http.StatusOK)
			c.Abort()
			return
		}

		// 验证 API Key
		if apiKey != cfg.LocalAPIKey {
			log.Warnf("Invalid API key from %s, path: %s, received key: '%s', expected: '%s'",
				c.ClientIP(), c.Request.URL.Path, apiKey, cfg.LocalAPIKey)
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "Invalid API key. Please check your API key and try again.",
					"type":    "invalid_api_key",
					"code":    "invalid_api_key",
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}

	// API 路由组
	api := r.Group("/api")
	api.Use(apiKeyAuth) // 应用 API 密钥验证中间件
	{
		// 列出可用模型 - OpenAI 标准接口 /api/models（包含重定向关键字）
		api.GET("/models", func(c *gin.Context) {
			// 获取包含重定向关键字的模型列表
			var models []string
			var err error

			if cfg.RedirectEnabled && cfg.RedirectKeyword != "" {
				models, err = routeService.GetAvailableModelsWithRedirect(cfg.RedirectKeyword)
			} else {
				models, err = routeService.GetAvailableModels()
			}

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"message": err.Error(),
						"type":    "internal_error",
					},
				})
				return
			}

			modelsData := make([]gin.H, len(models))
			for i, model := range models {
				modelsData[i] = gin.H{
					"id":       model,
					"object":   "model",
					"created":  1677610602,
					"owned_by": "openai-router",
				}
			}

			c.JSON(http.StatusOK, gin.H{
				"object": "list",
				"data":   modelsData,
			})
		})

		// Claude 专用接口 - 使用 /api/anthropic 路径
		// 需要创建子路由组来处理 /api/anthropic/* 的所有路径
		anthropic := api.Group("/anthropic")
		{
			anthropic.POST("/v1/messages", func(c *gin.Context) {
				// 读取请求体
				body, err := io.ReadAll(c.Request.Body)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": gin.H{
							"message": "Failed to read request body",
							"type":    "invalid_request_error",
						},
					})
					return
				}

				// 提取请求头
				headers := make(map[string]string)
				for key, values := range c.Request.Header {
					if len(values) > 0 {
						headers[key] = values[0]
					}
				}

				// 检查是否是流式请求
				var reqData map[string]interface{}
				if err := json.Unmarshal(body, &reqData); err == nil {
					if stream, ok := reqData["stream"].(bool); ok && stream {
						// 流式请求 - 对 Anthropic 路径，不转换响应
						c.Header("Content-Type", "text/event-stream")
						c.Header("Cache-Control", "no-cache")
						c.Header("Connection", "keep-alive")
						c.Header("X-Accel-Buffering", "no") // 禁用nginx缓冲

						flusher, ok := c.Writer.(http.Flusher)
						if !ok {
							log.Errorf("Streaming not supported")
							c.JSON(http.StatusInternalServerError, gin.H{
								"error": gin.H{
									"message": "Streaming not supported",
									"type":    "internal_error",
								},
							})
							return
						}

						// 使用 Anthropic 专用流式处理（智能检测目标格式）
						// 请求来自 Claude 格式，根据路由配置的 format 决定是否转换
						err := proxyService.ProxyAnthropicStreamRequest(body, headers, c.Writer, flusher)
						if err != nil {
							log.Errorf("Stream proxy error: %v", err)
						}
						return
					}
				}

				// 非流式请求 - 对 Anthropic 路径，不转换响应
				respBody, statusCode, err := proxyService.ProxyAnthropicRequest(body, headers)
				if err != nil {
					c.JSON(statusCode, gin.H{
						"error": gin.H{
							"message": err.Error(),
							"type":    "proxy_error",
						},
					})
					return
				}

				c.Data(statusCode, "application/json", respBody)
			})
		}

		// Claude Code 专用接口 - 使用 /api/claudecode 路径
		// 专门处理 Claude Code 的特殊格式，包括工具链、系统提示词等
		// 始终将请求转换为 OpenAI 格式，响应转换回 Claude 格式
		claudecode := api.Group("/claudecode")
		{
			claudecode.POST("/v1/messages", func(c *gin.Context) {
				// 读取请求体
				body, err := io.ReadAll(c.Request.Body)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": gin.H{
							"message": "Failed to read request body",
							"type":    "invalid_request_error",
						},
					})
					return
				}

				// 提取请求头
				headers := make(map[string]string)
				for key, values := range c.Request.Header {
					if len(values) > 0 {
						headers[key] = values[0]
					}
				}

				// 检查是否是流式请求
				var reqData map[string]interface{}
				if err := json.Unmarshal(body, &reqData); err == nil {
					if stream, ok := reqData["stream"].(bool); ok && stream {
						// 流式请求
						c.Header("Content-Type", "text/event-stream")
						c.Header("Cache-Control", "no-cache")
						c.Header("Connection", "keep-alive")
						c.Header("X-Accel-Buffering", "no") // 禁用nginx缓冲

						flusher, ok := c.Writer.(http.Flusher)
						if !ok {
							log.Errorf("Streaming not supported")
							c.JSON(http.StatusInternalServerError, gin.H{
								"error": gin.H{
									"message": "Streaming not supported",
									"type":    "internal_error",
								},
							})
							return
						}

						// 使用 Claude Code 专用流式处理
						// 将 Claude Code 格式转换为 OpenAI 格式，响应转换回 Claude 格式
						err := proxyService.ProxyClaudeCodeStreamRequest(body, headers, c.Writer, flusher)
						if err != nil {
							log.Errorf("Claude Code stream proxy error: %v", err)
						}
						return
					}
				}

				// 非流式请求
				respBody, statusCode, err := proxyService.ProxyClaudeCodeRequest(body, headers)
				if err != nil {
					c.JSON(statusCode, gin.H{
						"error": gin.H{
							"message": err.Error(),
							"type":    "proxy_error",
						},
					})
					return
				}

				c.Data(statusCode, "application/json", respBody)
			})
		}

		// Gemini 专用接口 - 支持 Google 官方 API 格式
		// 路径格式: /api/gemini/v1beta/models/{model}:generateContent
		// 或: /api/gemini/v1beta/models/{model}:streamGenerateContent
		gemini := api.Group("/gemini")
		{
			// Gemini 流式生成接口
			gemini.POST("/completions", func(c *gin.Context) {
				// 读取请求体
				body, err := io.ReadAll(c.Request.Body)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": gin.H{
							"message": "Failed to read request body",
							"type":    "invalid_request_error",
						},
					})
					return
				}

				// 提取请求头
				headers := make(map[string]string)
				for key, values := range c.Request.Header {
					if len(values) > 0 {
						headers[key] = values[0]
					}
				}

				// 检查是否是流式请求
				var reqData map[string]interface{}
				if err := json.Unmarshal(body, &reqData); err == nil {
					if stream, ok := reqData["stream"].(bool); ok && stream {
						c.Header("Content-Type", "text/event-stream")
						c.Header("Cache-Control", "no-cache")
						c.Header("Connection", "keep-alive")
						c.Header("X-Accel-Buffering", "no")

						flusher, ok := c.Writer.(http.Flusher)
						if !ok {
							log.Errorf("Streaming not supported")
							c.JSON(http.StatusInternalServerError, gin.H{
								"error": gin.H{
									"message": "Streaming not supported",
									"type":    "internal_error",
								},
							})
							return
						}

						err := proxyService.ProxyStreamRequest(body, headers, c.Writer, flusher)
						if err != nil {
							log.Errorf("Stream proxy error: %v", err)
						}
						return
					}
				}

				// 非流式请求
				respBody, statusCode, err := proxyService.ProxyRequest(body, headers)
				if err != nil {
					c.JSON(statusCode, gin.H{
						"error": gin.H{
							"message": err.Error(),
							"type":    "proxy_error",
						},
					})
					return
				}

				c.Data(statusCode, "application/json", respBody)
			})

			// Gemini 模型指定接口
			gemini.POST("/models/:model", func(c *gin.Context) {
				// 从URL路径提取模型名
				modelFromPath := c.Param("model")

				// 读取请求体
				body, err := io.ReadAll(c.Request.Body)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": gin.H{
							"message": "Failed to read request body",
							"type":    "invalid_request_error",
						},
					})
					return
				}

				// 解析并注入模型名
				var reqData map[string]interface{}
				if err := json.Unmarshal(body, &reqData); err == nil {
					reqData["model"] = modelFromPath
					body, _ = json.Marshal(reqData)
				}

				// 提取请求头
				headers := make(map[string]string)
				for key, values := range c.Request.Header {
					if len(values) > 0 {
						headers[key] = values[0]
					}
				}

				// 检查是否是流式请求
				if stream, ok := reqData["stream"].(bool); ok && stream {
					c.Header("Content-Type", "text/event-stream")
					c.Header("Cache-Control", "no-cache")
					c.Header("Connection", "keep-alive")
					c.Header("X-Accel-Buffering", "no")

					flusher, ok := c.Writer.(http.Flusher)
					if !ok {
						log.Errorf("Streaming not supported")
						c.JSON(http.StatusInternalServerError, gin.H{
							"error": gin.H{
								"message": "Streaming not supported",
								"type":    "internal_error",
							},
						})
						return
					}

					err := proxyService.ProxyStreamRequest(body, headers, c.Writer, flusher)
					if err != nil {
						log.Errorf("Stream proxy error: %v", err)
					}
					return
				}

				// 非流式请求
				respBody, statusCode, err := proxyService.ProxyRequest(body, headers)
				if err != nil {
					c.JSON(statusCode, gin.H{
						"error": gin.H{
							"message": err.Error(),
							"type":    "proxy_error",
						},
					})
					return
				}

				c.Data(statusCode, "application/json", respBody)
			})

			gemini.POST("/:model", func(c *gin.Context) {
				// 从URL路径提取模型名
				modelFromPath := c.Param("model")

				// 读取请求体
				body, err := io.ReadAll(c.Request.Body)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": gin.H{
							"message": "Failed to read request body",
							"type":    "invalid_request_error",
						},
					})
					return
				}

				// 解析并注入模型名
				var reqData map[string]interface{}
				if err := json.Unmarshal(body, &reqData); err == nil {
					reqData["model"] = modelFromPath
					body, _ = json.Marshal(reqData)
				}

				// 提取请求头
				headers := make(map[string]string)
				for key, values := range c.Request.Header {
					if len(values) > 0 {
						headers[key] = values[0]
					}
				}

				// 检查是否是流式请求
				if stream, ok := reqData["stream"].(bool); ok && stream {
					c.Header("Content-Type", "text/event-stream")
					c.Header("Cache-Control", "no-cache")
					c.Header("Connection", "keep-alive")
					c.Header("X-Accel-Buffering", "no")

					flusher, ok := c.Writer.(http.Flusher)
					if !ok {
						log.Errorf("Streaming not supported")
						c.JSON(http.StatusInternalServerError, gin.H{
							"error": gin.H{
								"message": "Streaming not supported",
								"type":    "internal_error",
							},
						})
						return
					}

					err := proxyService.ProxyStreamRequest(body, headers, c.Writer, flusher)
					if err != nil {
						log.Errorf("Stream proxy error: %v", err)
					}
					return
				}

				// 非流式请求
				respBody, statusCode, err := proxyService.ProxyRequest(body, headers)
				if err != nil {
					c.JSON(statusCode, gin.H{
						"error": gin.H{
							"message": err.Error(),
							"type":    "proxy_error",
						},
					})
					return
				}

				c.Data(statusCode, "application/json", respBody)
			})
		}

		// OpenAI 兼容接口 (默认)
		v1 := api.Group("/v1")
		{
			// 列出可用模型（包含重定向关键字）
			v1.GET("/models", func(c *gin.Context) {
				// 获取包含重定向关键字的模型列表
				var models []string
				var err error

				if cfg.RedirectEnabled && cfg.RedirectKeyword != "" {
					models, err = routeService.GetAvailableModelsWithRedirect(cfg.RedirectKeyword)
				} else {
					models, err = routeService.GetAvailableModels()
				}

				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"error": gin.H{
							"message": err.Error(),
							"type":    "internal_error",
						},
					})
					return
				}

				modelsData := make([]gin.H, len(models))
				for i, model := range models {
					modelsData[i] = gin.H{
						"id":       model,
						"object":   "model",
						"created":  1677610602,
						"owned_by": "openai-router",
					}
				}

				c.JSON(http.StatusOK, gin.H{
					"object": "list",
					"data":   modelsData,
				})
			})

			// 代理所有 OpenAI 接口 (默认 v1 路径)
			proxyHandler := func(c *gin.Context) {
				// 读取请求体
				body, err := io.ReadAll(c.Request.Body)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": gin.H{
							"message": "Failed to read request body",
							"type":    "invalid_request_error",
						},
					})
					return
				}

				// 提取请求头
				headers := make(map[string]string)
				for key, values := range c.Request.Header {
					if len(values) > 0 {
						headers[key] = values[0]
					}
				}

				// 检查是否是流式请求
				var reqData map[string]interface{}
				if err := json.Unmarshal(body, &reqData); err == nil {
					if stream, ok := reqData["stream"].(bool); ok && stream {
						// 流式请求
						c.Header("Content-Type", "text/event-stream")
						c.Header("Cache-Control", "no-cache")
						c.Header("Connection", "keep-alive")
						c.Header("X-Accel-Buffering", "no") // 禁用nginx缓冲

						flusher, ok := c.Writer.(http.Flusher)
						if !ok {
							log.Errorf("Streaming not supported")
							c.JSON(http.StatusInternalServerError, gin.H{
								"error": gin.H{
									"message": "Streaming not supported",
									"type":    "internal_error",
								},
							})
							return
						}

						err := proxyService.ProxyStreamRequest(body, headers, c.Writer, flusher)
						if err != nil {
							log.Errorf("Stream proxy error: %v", err)
						}
						return
					}
				}

				// 非流式请求
				respBody, statusCode, err := proxyService.ProxyRequest(body, headers)
				if err != nil {
					c.JSON(statusCode, gin.H{
						"error": gin.H{
							"message": err.Error(),
							"type":    "proxy_error",
						},
					})
					return
				}

				c.Data(statusCode, "application/json", respBody)
			}

			// OpenAI 兼容接口
			v1.POST("/chat/completions", proxyHandler)
			v1.POST("/completions", proxyHandler)
			v1.POST("/embeddings", proxyHandler)
			v1.POST("/images/generations", proxyHandler)
			v1.POST("/audio/transcriptions", proxyHandler)
			v1.POST("/audio/speech", proxyHandler)

			// Gemini 官方 API 格式兼容
			// 路径: /api/v1/gemini/models/{model}:generateContent
			// 路径: /api/v1/gemini/models/{model}:streamGenerateContent
			geminiV1 := v1.Group("/gemini")
			{
				// 使用通配符捕获整个路径
				geminiV1.POST("/models/:modelAction", func(c *gin.Context) {
					modelAction := c.Param("modelAction")
					log.Infof("[Gemini API] Received request, modelAction: %s", modelAction)

					// 解析模型名和操作
					// modelAction 格式: proxy_auto:streamGenerateContent 或 proxy_auto:generateContent
					parts := strings.Split(modelAction, ":")
					if len(parts) < 2 {
						c.JSON(http.StatusBadRequest, gin.H{
							"error": gin.H{
								"message": "Invalid Gemini API path format. Expected: /models/{model}:{action}",
								"type":    "invalid_request_error",
							},
						})
						return
					}

					modelName := parts[0]
					actionType := parts[1]
					isStream := actionType == "streamGenerateContent"

					log.Infof("[Gemini API] Model: %s, Action: %s, Stream: %v", modelName, actionType, isStream)

					// 读取请求体
					body, err := io.ReadAll(c.Request.Body)
					if err != nil {
						c.JSON(http.StatusBadRequest, gin.H{
							"error": gin.H{
								"message": "Failed to read request body",
								"type":    "invalid_request_error",
							},
						})
						return
					}

					// 解析 Gemini 格式请求并转换为内部格式
					var geminiReq map[string]interface{}
					if err := json.Unmarshal(body, &geminiReq); err != nil {
						c.JSON(http.StatusBadRequest, gin.H{
							"error": gin.H{
								"message": "Invalid JSON body: " + err.Error(),
								"type":    "invalid_request_error",
							},
						})
						return
					}

					// 注入模型名
					geminiReq["model"] = modelName
					geminiReq["stream"] = isStream

					// 重新编码
					body, _ = json.Marshal(geminiReq)

					// 提取请求头
					headers := make(map[string]string)
					for key, values := range c.Request.Header {
						if len(values) > 0 {
							headers[key] = values[0]
						}
					}

					if isStream {
						// 流式请求
						c.Header("Content-Type", "text/event-stream")
						c.Header("Cache-Control", "no-cache")
						c.Header("Connection", "keep-alive")
						c.Header("X-Accel-Buffering", "no")

						flusher, ok := c.Writer.(http.Flusher)
						if !ok {
							c.JSON(http.StatusInternalServerError, gin.H{
								"error": gin.H{
									"message": "Streaming not supported",
									"type":    "internal_error",
								},
							})
							return
						}

						// 使用 Gemini 专用流式处理
						err := proxyService.ProxyGeminiStreamRequest(body, headers, c.Writer, flusher)
						if err != nil {
							log.Errorf("Gemini stream proxy error: %v", err)
						}
						return
					}

					// 非流式请求
					respBody, statusCode, err := proxyService.ProxyGeminiRequest(body, headers)
					if err != nil {
						c.JSON(statusCode, gin.H{
							"error": gin.H{
								"message": err.Error(),
								"type":    "proxy_error",
							},
						})
						return
					}

					c.Data(statusCode, "application/json", respBody)
				})
			}
		}
	}

	// Conversation Aggregation Interface
	api.POST("/conversation", func(c *gin.Context) {
		var req service.ConversationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"message": "Invalid request format: " + err.Error(),
					"type":    "invalid_request_error",
				},
			})
			return
		}

		// Validate provider
		if !strings.Contains(strings.ToLower(req.Provider), "openai") &&
			!strings.Contains(strings.ToLower(req.Provider), "claude") &&
			!strings.Contains(strings.ToLower(req.Provider), "gemini") {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"message": "Provider must be one of: openai, claude, gemini",
					"type":    "invalid_request_error",
				},
			})
			return
		}

		// Handle streaming request
		if req.Stream {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("Connection", "keep-alive")
			c.Header("X-Accel-Buffering", "no")

			flusher, ok := c.Writer.(http.Flusher)
			if !ok {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"message": "Streaming not supported",
						"type":    "internal_error",
					},
				})
				return
			}

			// For streaming, we need to handle this differently based on provider
			// This is a simplified implementation - in practice, you'd want to
			// stream the actual response from the provider
			go func() {
				response, err := conversationService.SendConversation(req)
				if err != nil {
					c.Writer.Write([]byte("data: " + `{"error": "` + err.Error() + `"}` + "\n\n"))
				} else {
					c.Writer.Write([]byte("data: " + `{"provider": "` + response.Provider + `", "content": "` + response.Content + `"}` + "\n\n"))
				}
				c.Writer.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
			}()

			return
		}

		// Handle non-streaming request
		response, err := conversationService.SendConversation(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{
					"message": err.Error(),
					"type":    "conversation_error",
				},
			})
			return
		}

		if response.Error != "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"message": response.Error,
					"type":    "provider_error",
				},
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"provider":     response.Provider,
			"model":        response.Model,
			"content":      response.Content,
			"tokens_used":  response.TokensUsed,
			"raw_response": response.RawResponse,
		})
	})

	// SDK Examples endpoint
	api.GET("/sdk-examples", func(c *gin.Context) {
		examples := conversationService.GetSDKExamples()
		c.JSON(http.StatusOK, gin.H{
			"examples": examples,
		})
	})

	// Available models by provider
	api.GET("/models-by-provider", func(c *gin.Context) {
		models, err := conversationService.GetAvailableModels()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{
					"message": "Failed to get models: " + err.Error(),
					"type":    "internal_error",
				},
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"models": models,
		})
	})

	// Gemini 流式生成接口 (支持 streamGenerateContent)
	// 这个接口已经通过适配器逻辑处理，不需要单独的路由

	// ========== 管理端点（用于路由转发管理） ==========

	// CORS 预检请求处理
	api.OPTIONS("/admin/routes/*path", func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, x-goog-api-key")
		c.Status(http.StatusOK)
	})

	// 更新路由的转发目标（使用 target_route_id 字段）
	api.PUT("/admin/routes/:routeId/forwarding", func(c *gin.Context) {
		// 解析路由ID
		var routeID int64
		if _, err := fmt.Sscanf(c.Param("routeId"), "%d", &routeID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("invalid route ID: %v", err),
			})
			return
		}

		var req struct {
			TargetRouteID int64 `json:"target_route_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("invalid request: %v", err),
			})
			return
		}

		err := routeService.UpdateRouteForwarding(routeID, req.TargetRouteID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to update route forwarding: %v", err),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Route forwarding updated successfully",
		})
	})

	// 获取所有路由（包含转发信息）
	api.GET("/admin/routes", func(c *gin.Context) {
		routes, err := routeService.GetAllRoutes()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to get routes: %v", err),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"routes": routes,
		})
	})

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	return r
}
