package service

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"openai-router-go/internal/database"

	log "github.com/sirupsen/logrus"
)

type RouteService struct {
	db *sql.DB
}

func NewRouteService(db *sql.DB) *RouteService {
	return &RouteService{db: db}
}

// GetAllRoutes 获取所有路由
func (s *RouteService) GetAllRoutes() ([]database.ModelRoute, error) {
	query := `SELECT id, name, model, api_url, api_key, "group", COALESCE(format, 'openai'), enabled, created_at, updated_at
	          FROM model_routes ORDER BY created_at DESC`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var routes []database.ModelRoute
	for rows.Next() {
		var route database.ModelRoute
		err := rows.Scan(&route.ID, &route.Name, &route.Model, &route.APIUrl, &route.APIKey,
			&route.Group, &route.Format, &route.Enabled, &route.CreatedAt, &route.UpdatedAt)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}

	return routes, nil
}

// GetRouteByModel 根据模型名获取路由(支持负载均衡)
// 支持两种格式：
// 1. 原始模型名: "gemma-3-12b-it-qat"
// 2. 带路由名前缀: "LMStudio/gemma-3-12b-it-qat"
func (s *RouteService) GetRouteByModel(model string) (*database.ModelRoute, error) {
	var route database.ModelRoute
	var err error

	// 检查是否是带前缀的模型名 (RouteName/ModelName)
	if strings.Contains(model, "/") {
		parts := strings.SplitN(model, "/", 2)
		if len(parts) == 2 {
			routeName := parts[0]
			originalModel := parts[1]
			log.Infof("Looking up route by name: %s (original model: %s)", routeName, originalModel)

			// 通过路由名查询，并验证该路由是否包含请求的模型
			query := `SELECT id, name, model, api_url, api_key, "group", COALESCE(format, 'openai'), enabled, created_at, updated_at
			          FROM model_routes WHERE name = ? AND enabled = 1 ORDER BY RANDOM() LIMIT 1`
			err = s.db.QueryRow(query, routeName).Scan(&route.ID, &route.Name, &route.Model, &route.APIUrl,
				&route.APIKey, &route.Group, &route.Format, &route.Enabled, &route.CreatedAt, &route.UpdatedAt)

			if err == nil {
				// 找到路由后，临时替换Model字段为原始模型名（用于后续转发）
				route.Model = originalModel
				log.Infof("Found route by name: %s, using model: %s", routeName, originalModel)
				return &route, nil
			}
		}
	}

	// 如果不是带前缀的格式，或者通过路由名查找失败，则使用原始逻辑
	query := `SELECT id, name, model, api_url, api_key, "group", COALESCE(format, 'openai'), enabled, created_at, updated_at
	          FROM model_routes WHERE model = ? AND enabled = 1 ORDER BY RANDOM() LIMIT 1`

	err = s.db.QueryRow(query, model).Scan(&route.ID, &route.Name, &route.Model, &route.APIUrl,
		&route.APIKey, &route.Group, &route.Format, &route.Enabled, &route.CreatedAt, &route.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("model not found: %s", model)
	}
	if err != nil {
		return nil, err
	}

	return &route, nil
}

// GetRouteByID 根据路由ID获取路由
func (s *RouteService) GetRouteByID(id int64) (*database.ModelRoute, error) {
	query := `SELECT id, name, model, api_url, api_key, "group", COALESCE(format, 'openai'), enabled, created_at, updated_at
	          FROM model_routes WHERE id = ? AND enabled = 1`

	var route database.ModelRoute
	err := s.db.QueryRow(query, id).Scan(&route.ID, &route.Name, &route.Model, &route.APIUrl,
		&route.APIKey, &route.Group, &route.Format, &route.Enabled, &route.CreatedAt, &route.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("route not found: %d", id)
	}
	if err != nil {
		return nil, err
	}

	return &route, nil
}

// AddRoute 添加路由
func (s *RouteService) AddRoute(name, model, apiUrl, apiKey, group, format string) error {
	query := `INSERT INTO model_routes (name, model, api_url, api_key, "group", format, enabled, created_at, updated_at)
	          VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`

	now := time.Now()
	_, err := s.db.Exec(query, name, model, apiUrl, apiKey, group, format, now, now)
	if err != nil {
		log.Errorf("Failed to add route: %v", err)
		return err
	}

	log.Infof("Route added: %s -> %s (%s) [%s]", model, apiUrl, name, format)
	return nil
}

// UpdateRoute 更新路由
func (s *RouteService) UpdateRoute(id int64, name, model, apiUrl, apiKey, group, format string) error {
	query := `UPDATE model_routes SET name = ?, model = ?, api_url = ?, api_key = ?, "group" = ?, format = ?, updated_at = ?
	          WHERE id = ?`

	result, err := s.db.Exec(query, name, model, apiUrl, apiKey, group, format, time.Now(), id)
	if err != nil {
		log.Errorf("Failed to update route: %v", err)
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("route not found: id=%d", id)
	}

	log.Infof("Route updated: id=%d", id)
	return nil
}

// DeleteRoute 删除路由及其相关的请求日志
func (s *RouteService) DeleteRoute(id int64) error {
	// 先删除该路由相关的请求日志
	_, err := s.db.Exec(`DELETE FROM request_logs WHERE route_id = ?`, id)
	if err != nil {
		log.Errorf("Failed to delete route logs: %v", err)
		return err
	}

	// 再删除路由
	query := `DELETE FROM model_routes WHERE id = ?`
	result, err := s.db.Exec(query, id)
	if err != nil {
		log.Errorf("Failed to delete route: %v", err)
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("route not found: id=%d", id)
	}

	log.Infof("Route deleted: id=%d (with related logs)", id)
	return nil
}

// ToggleRoute 启用/禁用路由
func (s *RouteService) ToggleRoute(id int64, enabled bool) error {
	query := `UPDATE model_routes SET enabled = ?, updated_at = ? WHERE id = ?`

	_, err := s.db.Exec(query, enabled, time.Now(), id)
	if err != nil {
		log.Errorf("Failed to toggle route: %v", err)
		return err
	}

	log.Infof("Route toggled: id=%d, enabled=%v", id, enabled)
	return nil
}

// GetStats 获取统计信息
func (s *RouteService) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// 路由总数
	var routeCount int
	err := s.db.QueryRow("SELECT COUNT(*) FROM model_routes WHERE enabled = 1").Scan(&routeCount)
	if err != nil {
		return nil, err
	}
	stats["route_count"] = routeCount

	// 模型总数（去重）
	var modelCount int
	err = s.db.QueryRow("SELECT COUNT(DISTINCT model) FROM model_routes WHERE enabled = 1").Scan(&modelCount)
	if err != nil {
		return nil, err
	}
	stats["model_count"] = modelCount

	// 总请求数
	var totalRequests int
	err = s.db.QueryRow("SELECT COUNT(*) FROM request_logs").Scan(&totalRequests)
	if err != nil {
		return nil, err
	}
	stats["total_requests"] = totalRequests

	// 总Token使用量
	var totalTokens int
	err = s.db.QueryRow("SELECT COALESCE(SUM(total_tokens), 0) FROM request_logs").Scan(&totalTokens)
	if err != nil {
		return nil, err
	}
	stats["total_tokens"] = totalTokens

	// 今日请求数 - 直接比较日期字符串
	var todayRequests int
	err = s.db.QueryRow(`
		SELECT COUNT(*) FROM request_logs 
		WHERE substr(created_at, 1, 10) = date('now', 'localtime')
	`).Scan(&todayRequests)
	if err != nil {
		return nil, err
	}
	stats["today_requests"] = todayRequests

	// 今日Token消耗 - 直接比较日期字符串
	var todayTokens int
	err = s.db.QueryRow(`
		SELECT COALESCE(SUM(total_tokens), 0) FROM request_logs 
		WHERE substr(created_at, 1, 10) = date('now', 'localtime')
	`).Scan(&todayTokens)
	if err != nil {
		return nil, err
	}
	stats["today_tokens"] = todayTokens

	// 成功率
	var successCount int
	err = s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE success = 1").Scan(&successCount)
	if err != nil {
		return nil, err
	}

	successRate := 0.0
	if totalRequests > 0 {
		successRate = float64(successCount) / float64(totalRequests) * 100
	}
	stats["success_rate"] = successRate

	log.Infof("Stats loaded: today_requests=%d, today_tokens=%d, total_requests=%d, total_tokens=%d",
		todayRequests, todayTokens, totalRequests, totalTokens)

	return stats, nil
}

// LogRequest 记录请求日志
func (s *RouteService) LogRequest(model string, routeID int64, requestTokens, responseTokens, totalTokens int, success bool, errorMsg string) error {
	// 使用 SQLite 的 datetime('now', 'localtime') 确保时区一致
	query := `INSERT INTO request_logs (model, route_id, request_tokens, response_tokens, total_tokens, success, error_message, created_at)
	          VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now', 'localtime'))`

	_, err := s.db.Exec(query, model, routeID, requestTokens, responseTokens, totalTokens, success, errorMsg)
	if err != nil {
		log.Errorf("LogRequest error: %v", err)
	} else {
		log.Infof("LogRequest: model=%s, tokens=%d, success=%v", model, totalTokens, success)
	}
	return err
}

// GetAvailableModels 获取所有可用的模型列表，返回带路由名前缀的模型
// 例如：["LMStudio/qwen/qwen3-8b", "OpenAI/gpt-4"]
func (s *RouteService) GetAvailableModels() ([]string, error) {
	query := `SELECT name, model FROM model_routes WHERE enabled = 1 ORDER BY name`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	modelMap := make(map[string]bool)
	for rows.Next() {
		var routeName, model string
		if err := rows.Scan(&routeName, &model); err != nil {
			return nil, err
		}
		// 分割逗号分隔的模型列表，添加路由名前缀
		models := strings.Split(model, ",")
		for _, m := range models {
			trimmed := strings.TrimSpace(m)
			if trimmed != "" {
				// 格式：路由名/原始模型名
				renamedModel := fmt.Sprintf("%s/%s", routeName, trimmed)
				modelMap[renamedModel] = true
			}
		}
	}

	// 转换为切片并排序
	var models []string
	for model := range modelMap {
		models = append(models, model)
	}
	sort.Strings(models)

	return models, nil
}

// GetAvailableModelsWithRedirect 获取所有可用的模型列表（包含重定向关键字），返回带路由名前缀的模型
func (s *RouteService) GetAvailableModelsWithRedirect(redirectKeyword string) ([]string, error) {
	query := `SELECT name, model FROM model_routes WHERE enabled = 1 ORDER BY name`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	modelMap := make(map[string]bool)

	// 首先添加重定向关键字（如果配置了）
	if redirectKeyword != "" {
		modelMap[redirectKeyword] = true
	}

	for rows.Next() {
		var routeName, model string
		if err := rows.Scan(&routeName, &model); err != nil {
			return nil, err
		}
		// 分割逗号分隔的模型列表，添加路由名前缀
		models := strings.Split(model, ",")
		for _, m := range models {
			trimmed := strings.TrimSpace(m)
			if trimmed != "" {
				// 格式：路由名/原始模型名
				renamedModel := fmt.Sprintf("%s/%s", routeName, trimmed)
				modelMap[renamedModel] = true
			}
		}
	}

	// 转换为切片并排序
	var models []string
	for model := range modelMap {
		models = append(models, model)
	}
	sort.Strings(models)

	return models, nil
}

// GetTodayStats 获取今日统计
func (s *RouteService) GetTodayStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// 今日请求数
	var todayRequests int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM request_logs 
		WHERE substr(created_at, 1, 10) = date('now', 'localtime')
	`).Scan(&todayRequests)
	if err != nil {
		return nil, err
	}
	stats["today_requests"] = todayRequests

	// 今日Token消耗
	var todayTokens int
	err = s.db.QueryRow(`
		SELECT COALESCE(SUM(total_tokens), 0) FROM request_logs 
		WHERE substr(created_at, 1, 10) = date('now', 'localtime')
	`).Scan(&todayTokens)
	if err != nil {
		return nil, err
	}
	stats["today_tokens"] = todayTokens

	return stats, nil
}

// GetDailyStats 获取每日统计（用于热力图）
func (s *RouteService) GetDailyStats(days int) ([]map[string]interface{}, error) {
	query := `
		SELECT
			substr(created_at, 1, 10) as date,
			COUNT(*) as requests,
			COALESCE(SUM(request_tokens), 0) as request_tokens,
			COALESCE(SUM(response_tokens), 0) as response_tokens,
			COALESCE(SUM(total_tokens), 0) as total_tokens
		FROM request_logs
		WHERE substr(created_at, 1, 10) >= date('now', 'localtime', ?)
		GROUP BY substr(created_at, 1, 10)
		ORDER BY date
	`

	rows, err := s.db.Query(query, fmt.Sprintf("-%d days", days))
	if err != nil {
		log.Errorf("GetDailyStats query error: %v", err)
		return nil, err
	}
	defer rows.Close()

	var stats []map[string]interface{}
	for rows.Next() {
		var date string
		var requests, requestTokens, responseTokens, totalTokens int
		err := rows.Scan(&date, &requests, &requestTokens, &responseTokens, &totalTokens)
		if err != nil {
			log.Errorf("GetDailyStats scan error: %v", err)
			return nil, err
		}

		stats = append(stats, map[string]interface{}{
			"date":            date,
			"requests":        requests,
			"request_tokens":  requestTokens,
			"response_tokens": responseTokens,
			"total_tokens":    totalTokens,
		})
	}

	log.Infof("GetDailyStats: loaded %d days of data", len(stats))
	return stats, nil
}

// GetHourlyStats 获取今日按小时统计
func (s *RouteService) GetHourlyStats() ([]map[string]interface{}, error) {
	query := `
		SELECT
			CAST(substr(created_at, 12, 2) AS INTEGER) as hour,
			COUNT(*) as requests,
			COALESCE(SUM(total_tokens), 0) as total_tokens,
			COALESCE(SUM(request_tokens), 0) as request_tokens,
			COALESCE(SUM(response_tokens), 0) as response_tokens
		FROM request_logs
		WHERE substr(created_at, 1, 10) = date('now', 'localtime')
		GROUP BY hour
		ORDER BY hour
	`

	rows, err := s.db.Query(query)
	if err != nil {
		log.Errorf("GetHourlyStats query error: %v", err)
		return nil, err
	}
	defer rows.Close()

	var stats []map[string]interface{}
	for rows.Next() {
		var hour, requests, totalTokens, requestTokens, responseTokens int
		err := rows.Scan(&hour, &requests, &totalTokens, &requestTokens, &responseTokens)
		if err != nil {
			log.Errorf("GetHourlyStats scan error: %v", err)
			return nil, err
		}

		stats = append(stats, map[string]interface{}{
			"hour":            hour,
			"requests":        requests,
			"total_tokens":    totalTokens,
			"request_tokens":  requestTokens,
			"response_tokens": responseTokens,
		})
	}

	log.Infof("GetHourlyStats: loaded %d hours of data", len(stats))
	return stats, nil
}

// ImportRouteFromFormat 从不同格式导入路由
func (s *RouteService) ImportRouteFromFormat(name, model, apiUrl, apiKey, group, targetFormat string) (string, error) {
	// 根据目标格式自动转换 API URL 和模型名
	convertedUrl, convertedModel, err := s.convertRouteFormat(apiUrl, model, targetFormat)
	if err != nil {
		return "", fmt.Errorf("格式转换失败: %v", err)
	}

	// 添加转换后的路由
	err = s.AddRoute(name+" ("+targetFormat+")", convertedModel, convertedUrl, apiKey, group, targetFormat)
	if err != nil {
		return "", fmt.Errorf("添加路由失败: %v", err)
	}

	log.Infof("Route imported and converted: %s [%s] -> %s:%s", name, model, convertedUrl, convertedModel)
	return targetFormat, nil
}

// convertRouteFormat 转换路由格式
func (s *RouteService) convertRouteFormat(apiUrl, model, targetFormat string) (string, string, error) {
	switch targetFormat {
	case "openai":
		return s.convertToOpenAI(apiUrl, model)
	case "claude":
		return s.convertToClaude(apiUrl, model)
	case "gemini":
		return s.convertToGemini(apiUrl, model)
	default:
		return apiUrl, model, nil
	}
}

// convertToOpenAI 转换为 OpenAI 格式
func (s *RouteService) convertToOpenAI(apiUrl, model string) (string, string, error) {
	// 如果已经是 OpenAI 格式，直接返回
	if isOpenAIFormat(apiUrl, model) {
		return apiUrl, model, nil
	}

	// Claude 到 OpenAI
	if isClaudeFormat(apiUrl, model) {
		return "https://api.openai.com/v1", convertClaudeModelToOpenAI(model), nil
	}

	// Gemini 到 OpenAI
	if isGeminiFormat(apiUrl, model) {
		return "https://api.openai.com/v1", convertGeminiModelToOpenAI(model), nil
	}

	// 默认转换为 OpenAI 兼容格式
	return "https://api.openai.com/v1", "gpt-3.5-turbo", nil
}

// convertToClaude 转换为 Claude 格式
func (s *RouteService) convertToClaude(apiUrl, model string) (string, string, error) {
	// 如果已经是 Claude 格式，直接返回
	if isClaudeFormat(apiUrl, model) {
		return apiUrl, model, nil
	}

	// OpenAI 到 Claude
	if isOpenAIFormat(apiUrl, model) {
		return "https://api.anthropic.com/v1", convertOpenAIModelToClaude(model), nil
	}

	// Gemini 到 Claude
	if isGeminiFormat(apiUrl, model) {
		return "https://api.anthropic.com/v1", convertGeminiModelToClaude(model), nil
	}

	// 默认转换为 Claude 兼容格式
	return "https://api.anthropic.com/v1", "claude-3-sonnet-20240229", nil
}

// convertToGemini 转换为 Gemini 格式
func (s *RouteService) convertToGemini(apiUrl, model string) (string, string, error) {
	// 如果已经是 Gemini 格式，直接返回
	if isGeminiFormat(apiUrl, model) {
		return apiUrl, model, nil
	}

	// OpenAI 到 Gemini
	if isOpenAIFormat(apiUrl, model) {
		return "https://generativelanguage.googleapis.com/v1", convertOpenAIModelToGemini(model), nil
	}

	// Claude 到 Gemini
	if isClaudeFormat(apiUrl, model) {
		return "https://generativelanguage.googleapis.com/v1", convertClaudeModelToGemini(model), nil
	}

	// 默认转换为 Gemini 兼容格式
	return "https://generativelanguage.googleapis.com/v1", "gemini-pro", nil
}

// 格式检测函数
func isOpenAIFormat(apiUrl, model string) bool {
	return strings.Contains(apiUrl, "api.openai.com") ||
		strings.HasPrefix(model, "gpt-") ||
		strings.HasPrefix(model, "o1-")
}

func isClaudeFormat(apiUrl, model string) bool {
	return strings.Contains(apiUrl, "api.anthropic.com") ||
		strings.HasPrefix(model, "claude-")
}

func isGeminiFormat(apiUrl, model string) bool {
	return strings.Contains(apiUrl, "generativelanguage.googleapis.com") ||
		strings.HasPrefix(model, "gemini-")
}

// 模型名转换函数
func convertClaudeModelToOpenAI(model string) string {
	modelMap := map[string]string{
		"claude-3-opus-20240229":     "gpt-4-turbo",
		"claude-3-sonnet-20240229":   "gpt-4",
		"claude-3-haiku-20240307":    "gpt-3.5-turbo",
		"claude-3-5-sonnet-20241022": "gpt-4-turbo",
	}
	if mapped, exists := modelMap[model]; exists {
		return mapped
	}
	return "gpt-4" // 默认映射
}

func convertOpenAIModelToClaude(model string) string {
	modelMap := map[string]string{
		"gpt-4-turbo":   "claude-3-5-sonnet-20241022",
		"gpt-4":         "claude-3-sonnet-20240229",
		"gpt-3.5-turbo": "claude-3-haiku-20240307",
		"o1-preview":    "claude-3-opus-20240229",
		"o1-mini":       "claude-3-sonnet-20240229",
	}
	if mapped, exists := modelMap[model]; exists {
		return mapped
	}
	return "claude-3-sonnet-20240229" // 默认映射
}

func convertGeminiModelToOpenAI(model string) string {
	modelMap := map[string]string{
		"gemini-1.5-pro":    "gpt-4-turbo",
		"gemini-1.5-flash":  "gpt-3.5-turbo",
		"gemini-1.0-pro":    "gpt-4",
		"gemini-pro-vision": "gpt-4-vision-preview",
	}
	if mapped, exists := modelMap[model]; exists {
		return mapped
	}
	return "gpt-4" // 默认映射
}

func convertOpenAIModelToGemini(model string) string {
	modelMap := map[string]string{
		"gpt-4-turbo":          "gemini-1.5-pro",
		"gpt-4":                "gemini-1.0-pro",
		"gpt-3.5-turbo":        "gemini-1.5-flash",
		"gpt-4-vision-preview": "gemini-pro-vision",
	}
	if mapped, exists := modelMap[model]; exists {
		return mapped
	}
	return "gemini-1.5-pro" // 默认映射
}

func convertGeminiModelToClaude(model string) string {
	modelMap := map[string]string{
		"gemini-1.5-pro":   "claude-3-5-sonnet-20241022",
		"gemini-1.5-flash": "claude-3-haiku-20240307",
		"gemini-1.0-pro":   "claude-3-sonnet-20240229",
	}
	if mapped, exists := modelMap[model]; exists {
		return mapped
	}
	return "claude-3-sonnet-20240229" // 默认映射
}

func convertClaudeModelToGemini(model string) string {
	modelMap := map[string]string{
		"claude-3-opus-20240229":     "gemini-1.5-pro",
		"claude-3-sonnet-20240229":   "gemini-1.0-pro",
		"claude-3-haiku-20240307":    "gemini-1.5-flash",
		"claude-3-5-sonnet-20241022": "gemini-1.5-pro",
	}
	if mapped, exists := modelMap[model]; exists {
		return mapped
	}
	return "gemini-1.5-pro" // 默认映射
}

// ClearStats 清除统计数据
func (s *RouteService) ClearStats() error {
	query := "DELETE FROM request_logs"
	_, err := s.db.Exec(query)
	if err != nil {
		log.Errorf("Failed to clear stats: %v", err)
		return err
	}
	log.Info("All statistics data cleared")
	return nil
}

// IsRedirectModel 判断是否为重定向模型（排除在排行榜之外）
func (s *RouteService) IsRedirectModel(model string) bool {
	// 常见的重定向/代理模型标识
	redirectPatterns := []string{
		"proxy_auto",
		"proxy_",
		"redirect_",
		"forward_",
	}

	for _, pattern := range redirectPatterns {
		if strings.Contains(strings.ToLower(model), pattern) {
			return true
		}
	}
	return false
}

// GetModelRanking 获取模型使用排行（排除重定向模型）
func (s *RouteService) GetModelRanking(limit int) ([]map[string]interface{}, error) {
	query := `
		SELECT
			model,
			COUNT(*) as requests,
			COALESCE(SUM(request_tokens), 0) as request_tokens,
			COALESCE(SUM(response_tokens), 0) as response_tokens,
			COALESCE(SUM(total_tokens), 0) as total_tokens,
			ROUND(AVG(CASE WHEN success = 1 THEN 100.0 ELSE 0.0 END), 2) as success_rate
		FROM request_logs
		GROUP BY model
		ORDER BY total_tokens DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ranking []map[string]interface{}
	rank := 1
	for rows.Next() {
		var model string
		var requests, requestTokens, responseTokens, totalTokens int
		var successRate float64
		err := rows.Scan(&model, &requests, &requestTokens, &responseTokens, &totalTokens, &successRate)
		if err != nil {
			return nil, err
		}

		// 跳过重定向模型
		if strings.Contains(strings.ToLower(model), "proxy_auto") ||
			strings.Contains(strings.ToLower(model), "proxy_") ||
			strings.Contains(strings.ToLower(model), "redirect_") ||
			strings.Contains(strings.ToLower(model), "forward_") {
			continue
		}

		ranking = append(ranking, map[string]interface{}{
			"rank":            rank,
			"model":           model,
			"requests":        requests,
			"request_tokens":  requestTokens,
			"response_tokens": responseTokens,
			"total_tokens":    totalTokens,
			"success_rate":    successRate,
		})
		rank++

		// 如果达到限制数量，停止添加
		if rank > limit {
			break
		}
	}

	return ranking, nil
}
