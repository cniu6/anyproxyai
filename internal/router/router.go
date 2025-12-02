package router

import (
	"encoding/json"
	"io"
	"net/http"

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

	// API 路由组
	api := r.Group("/api")
	{
		// OpenAI 兼容接口
		v1 := api.Group("/v1")
		{
			// 列出可用模型
			v1.GET("/models", func(c *gin.Context) {
				models, err := routeService.GetAvailableModels()
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

			// 代理所有 OpenAI 接口
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

			v1.POST("/chat/completions", proxyHandler)
			v1.POST("/completions", proxyHandler)
			v1.POST("/embeddings", proxyHandler)
			v1.POST("/images/generations", proxyHandler)
			v1.POST("/audio/transcriptions", proxyHandler)
			v1.POST("/audio/speech", proxyHandler)

			// Claude 适配接口 - 直接访问 Anthropic API，不进行响应转换
			v1.POST("/anthropic/messages", func(c *gin.Context) {
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

						// 使用特殊的流式处理函数，不对 Anthropic 响应进行转换
						err := proxyService.ProxyAnthropicStreamRequest(body, headers, c.Writer, flusher)
						if err != nil {
							log.Errorf("Anthropic stream proxy error: %v", err)
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

			// Gemini 适配接口
			v1.POST("/gemini/completions", proxyHandler)
			v1.POST("/gemini/models/:model", func(c *gin.Context) {
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
			v1.POST("/gemini/:model", proxyHandler)
		}
	}

	// Gemini 流式生成接口 (支持 streamGenerateContent)
	// 这个接口已经通过适配器逻辑处理，不需要单独的路由

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	return r
}
