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
	query := `SELECT id, name, model, api_url, api_key, "group", COALESCE(format, 'openai'), enabled,
	          COALESCE(target_route_id, 0), COALESCE(forwarding_enabled, 0), created_at, updated_at
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
			&route.Group, &route.Format, &route.Enabled, &route.TargetRouteID, &route.ForwardingEnabled, &route.CreatedAt, &route.UpdatedAt)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}

	return routes, nil
}

// GetRouteByModel 根据模型名获取路由(支持负载均衡)
// 支持两种格式：
// 1. 原始模型名: "gpt-4"
// 2. 带路由名前缀: "OpenAI/gpt-4"
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
			query := `SELECT id, name, model, api_url, api_key, "group", COALESCE(format, 'openai'), enabled,
			          COALESCE(target_route_id, 0), COALESCE(forwarding_enabled, 0), created_at, updated_at
			          FROM model_routes WHERE name = ? AND enabled = 1 ORDER BY RANDOM() LIMIT 1`
			err = s.db.QueryRow(query, routeName).Scan(&route.ID, &route.Name, &route.Model, &route.APIUrl,
				&route.APIKey, &route.Group, &route.Format, &route.Enabled, &route.TargetRouteID, &route.ForwardingEnabled, &route.CreatedAt, &route.UpdatedAt)

			if err == nil {
				// 找到路由后，临时替换Model字段为原始模型名（用于后续转发）
				route.Model = originalModel
				log.Infof("Found route by name: %s, using model: %s", routeName, originalModel)
				return &route, nil
			}
		}
	}

	// 如果不是带前缀的格式，或者通过路由名查找失败，则使用原始逻辑
	query := `SELECT id, name, model, api_url, api_key, "group", COALESCE(format, 'openai'), enabled,
	          COALESCE(target_route_id, 0), COALESCE(forwarding_enabled, 0), created_at, updated_at
	          FROM model_routes WHERE model = ? AND enabled = 1 ORDER BY RANDOM() LIMIT 1`

	err = s.db.QueryRow(query, model).Scan(&route.ID, &route.Name, &route.Model, &route.APIUrl,
		&route.APIKey, &route.Group, &route.Format, &route.Enabled, &route.TargetRouteID, &route.ForwardingEnabled, &route.CreatedAt, &route.UpdatedAt)

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
	query := `SELECT id, name, model, api_url, api_key, "group", COALESCE(format, 'openai'), enabled,
	          COALESCE(target_route_id, 0), COALESCE(forwarding_enabled, 0), created_at, updated_at
	          FROM model_routes WHERE id = ? AND enabled = 1`

	var route database.ModelRoute
	err := s.db.QueryRow(query, id).Scan(&route.ID, &route.Name, &route.Model, &route.APIUrl,
		&route.APIKey, &route.Group, &route.Format, &route.Enabled, &route.TargetRouteID, &route.ForwardingEnabled, &route.CreatedAt, &route.UpdatedAt)

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
	query := `INSERT INTO model_routes (name, model, api_url, api_key, "group", format, enabled, target_route_id, created_at, updated_at)
	          VALUES (?, ?, ?, ?, ?, ?, 1, 0, ?, ?)`

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
	// 检查 group 字段是否包含转发目标 ID 和转发启用状态
	// 新格式: "group|targetRouteId|forwardingEnabled" 或 "group|targetRouteId" 或 "group"
	targetRouteID := int64(0)
	forwardingEnabled := false

	if strings.Contains(group, "|") {
		parts := strings.Split(group, "|")
		group = parts[0]
		if len(parts) >= 2 {
			fmt.Sscanf(parts[1], "%d", &targetRouteID)
		}
		if len(parts) >= 3 {
			forwardingEnabled = parts[2] == "1"
		} else if targetRouteID > 0 {
			// 如果有 targetRouteId 但没有 forwardingEnabled，默认启用
			forwardingEnabled = true
		}
	}

	var query string
	var args []interface{}

	if targetRouteID > 0 {
		query = `UPDATE model_routes SET name = ?, model = ?, api_url = ?, api_key = ?, "group" = ?, format = ?, target_route_id = ?, forwarding_enabled = ?, updated_at = ?
		          WHERE id = ?`
		args = []interface{}{name, model, apiUrl, apiKey, group, format, targetRouteID, forwardingEnabled, time.Now(), id}
	} else {
		// 清除转发配置时，同时设置 target_route_id = 0 和 forwarding_enabled = 0
		query = `UPDATE model_routes SET name = ?, model = ?, api_url = ?, api_key = ?, "group" = ?, format = ?, target_route_id = 0, forwarding_enabled = 0, updated_at = ?
		          WHERE id = ?`
		args = []interface{}{name, model, apiUrl, apiKey, group, format, time.Now(), id}
	}

	result, err := s.db.Exec(query, args...)
	if err != nil {
		log.Errorf("Failed to update route: %v", err)
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("route not found: id=%d", id)
	}

	if targetRouteID > 0 {
		log.Infof("Route updated with forwarding: id=%d, target_route_id=%d, forwarding_enabled=%v", id, targetRouteID, forwardingEnabled)
	} else {
		log.Infof("Route updated: id=%d", id)
	}
	return nil
}

// UpdateRouteByKey 通过组合键 (oldName, oldModel) 更新路由
func (s *RouteService) UpdateRouteByKey(oldName, oldModel, name, model, apiUrl, apiKey, group, format string) error {
	query := `UPDATE model_routes SET name = ?, model = ?, api_url = ?, api_key = ?, "group" = ?, format = ?, updated_at = ?
	          WHERE name = ? AND model = ?`

	result, err := s.db.Exec(query, name, model, apiUrl, apiKey, group, format, time.Now(), oldName, oldModel)
	if err != nil {
		log.Errorf("Failed to update route by key: %v", err)
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("route not found: name=%s, model=%s", oldName, oldModel)
	}

	log.Infof("Route updated by key: %s/%s -> %s/%s", oldName, oldModel, name, model)
	return nil
}

// DeleteRoute 删除路由及其相关的请求日志
func (s *RouteService) DeleteRoute(id int64) error {
	// 先清除指向该路由的转发配置
	_, err := s.db.Exec(`UPDATE model_routes SET target_route_id = 0, forwarding_enabled = 0 WHERE target_route_id = ?`, id)
	if err != nil {
		log.Errorf("Failed to clear forwarding to deleted route: %v", err)
		return err
	}

	// 删除该路由相关的请求日志
	_, err = s.db.Exec(`DELETE FROM request_logs WHERE route_id = ?`, id)
	if err != nil {
		log.Errorf("Failed to delete route logs: %v", err)
		return err
	}

	// 删除路由
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

// DeleteRouteByKey 通过组合键 (name, model) 删除路由及其相关的请求日志
func (s *RouteService) DeleteRouteByKey(name, model string) error {
	// 先获取路由ID
	var id int64
	err := s.db.QueryRow(`SELECT id FROM model_routes WHERE name = ? AND model = ?`, name, model).Scan(&id)
	if err == sql.ErrNoRows {
		return fmt.Errorf("route not found: name=%s, model=%s", name, model)
	}
	if err != nil {
		log.Errorf("Failed to find route by key: %v", err)
		return err
	}

	// 删除该路由相关的请求日志
	_, err = s.db.Exec(`DELETE FROM request_logs WHERE route_id = ?`, id)
	if err != nil {
		log.Errorf("Failed to delete route logs: %v", err)
		return err
	}

	// 再删除路由
	query := `DELETE FROM model_routes WHERE name = ? AND model = ?`
	result, err := s.db.Exec(query, name, model)
	if err != nil {
		log.Errorf("Failed to delete route: %v", err)
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("route not found: name=%s, model=%s", name, model)
	}

	log.Infof("Route deleted by key: %s/%s (with related logs)", name, model)
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

// UpdateRouteForwarding 更新路由的转发目标
func (s *RouteService) UpdateRouteForwarding(routeID int64, targetRouteID int64) error {
	// 验证源路由存在
	sourceRoute, err := s.GetRouteByID(routeID)
	if err != nil {
		return fmt.Errorf("source route not found: %v", err)
	}

	// 如果设置了目标路由，验证目标路由存在
	if targetRouteID > 0 {
		targetRoute, err := s.GetRouteByID(targetRouteID)
		if err != nil {
			return fmt.Errorf("target route not found: %v", err)
		}
		log.Infof("Updating forwarding: route %s (id=%d) -> route %s (id=%d)",
			sourceRoute.Name, sourceRoute.ID, targetRoute.Name, targetRoute.ID)
	} else {
		log.Infof("Clearing forwarding: route %s (id=%d) -> no forwarding",
			sourceRoute.Name, sourceRoute.ID)
	}

	query := `UPDATE model_routes SET target_route_id = ?, forwarding_enabled = ?, updated_at = ? WHERE id = ?`
	// 当设置目标路由时，自动启用转发；清除目标时，禁用转发
	forwardingEnabled := targetRouteID > 0
	_, err = s.db.Exec(query, targetRouteID, forwardingEnabled, time.Now(), routeID)
	if err != nil {
		log.Errorf("Failed to update route forwarding: %v", err)
		return err
	}

	return nil
}

// ToggleRouteForwarding 切换路由的转发开关
func (s *RouteService) ToggleRouteForwarding(routeID int64, enabled bool) error {
	query := `UPDATE model_routes SET forwarding_enabled = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, enabled, time.Now(), routeID)
	if err != nil {
		log.Errorf("Failed to toggle route forwarding: %v", err)
		return err
	}

	log.Infof("Route forwarding toggled: id=%d, forwarding_enabled=%v", routeID, enabled)
	return nil
}

// ToggleAllForwarding 切换所有路由的转发开关（总开关）
func (s *RouteService) ToggleAllForwarding(enabled bool) error {
	query := `UPDATE model_routes SET forwarding_enabled = ?, updated_at = ?`
	result, err := s.db.Exec(query, enabled, time.Now())
	if err != nil {
		log.Errorf("Failed to toggle all forwarding: %v", err)
		return err
	}

	rows, _ := result.RowsAffected()
	log.Infof("All forwarding toggled: affected %d routes, forwarding_enabled=%v", rows, enabled)
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
	err = s.db.QueryRow("SELECT COALESCE(SUM(request_count), 0) FROM request_logs").Scan(&totalRequests)
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
		SELECT COALESCE(SUM(request_count), 0) FROM request_logs
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

	// 成功率 - 计算成功请求数占总请求数的比例
	var successRequests int
	err = s.db.QueryRow("SELECT COALESCE(SUM(CASE WHEN success = 1 THEN request_count ELSE 0 END), 0) FROM request_logs").Scan(&successRequests)
	if err != nil {
		return nil, err
	}

	successRate := 0.0
	if totalRequests > 0 {
		successRate = float64(successRequests) / float64(totalRequests) * 100
	}
	stats["success_rate"] = successRate

	log.Infof("Stats loaded: today_requests=%d, today_tokens=%d, total_requests=%d, total_tokens=%d",
		todayRequests, todayTokens, totalRequests, totalTokens)

	return stats, nil
}

// LogRequest 记录请求日志（按小时聚合）
func (s *RouteService) LogRequest(model string, routeID int64, requestTokens, responseTokens, totalTokens int, success bool, errorMsg string) error {
	// 使用 SQLite 的 datetime('now', 'localtime') 确保时区一致，并按小时聚合
	// 首先尝试更新现有记录（同一模型同一小时）
	currentHour := time.Now().Format("2006-01-02 15:04:05")[:13] // "YYYY-MM-DD HH"

	updateQuery := `
		UPDATE request_logs
		SET request_tokens = request_tokens + ?,
		    response_tokens = response_tokens + ?,
		    total_tokens = total_tokens + ?,
		    request_count = request_count + 1,
		    success = success & ?,
		    error_message = ?
		WHERE model = ? AND substr(created_at, 1, 13) = ?
	`

	result, err := s.db.Exec(updateQuery, requestTokens, responseTokens, totalTokens, boolToInt(success), errorMsg, model, currentHour)
	if err != nil {
		log.Errorf("LogRequest update error: %v", err)
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// 没有现有记录，插入新记录
		insertQuery := `INSERT INTO request_logs (model, route_id, request_tokens, response_tokens, total_tokens, request_count, success, error_message, created_at)
		                VALUES (?, ?, ?, ?, ?, 1, ?, ?, datetime('now', 'localtime'))`
		_, err = s.db.Exec(insertQuery, model, routeID, requestTokens, responseTokens, totalTokens, success, errorMsg)
		if err != nil {
			log.Errorf("LogRequest insert error: %v", err)
			return err
		}
	}

	log.Infof("LogRequest: model=%s, tokens=%d, success=%v", model, totalTokens, success)
	return nil
}

// boolToInt 将布尔值转换为整数（用于 SQL 操作）
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GetAvailableModels 获取所有可用的模型列表，返回带路由名前缀的模型
// 例如：["OpenAI/gpt-4", "Claude/claude-3-sonnet-20240229"]
func (s *RouteService) GetAvailableModels() ([]string, error) {
	query := `SELECT name, model FROM model_routes WHERE enabled = 1 ORDER BY name`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []string
	for rows.Next() {
		var routeName, model string
		if err := rows.Scan(&routeName, &model); err != nil {
			return nil, err
		}
		// 格式：路由名/模型名
		renamedModel := fmt.Sprintf("%s/%s", routeName, model)
		models = append(models, renamedModel)
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

	var models []string

	// 首先添加重定向关键字（如果配置了）
	if redirectKeyword != "" {
		models = append(models, redirectKeyword)
	}

	for rows.Next() {
		var routeName, model string
		if err := rows.Scan(&routeName, &model); err != nil {
			return nil, err
		}
		// 格式：路由名/模型名
		renamedModel := fmt.Sprintf("%s/%s", routeName, model)
		models = append(models, renamedModel)
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
		SELECT COALESCE(SUM(request_count), 0) FROM request_logs
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
			COALESCE(SUM(request_count), 0) as requests,
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
			COALESCE(SUM(request_count), 0) as requests,
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
			COALESCE(SUM(request_count), 0) as requests,
			COALESCE(SUM(request_tokens), 0) as request_tokens,
			COALESCE(SUM(response_tokens), 0) as response_tokens,
			COALESCE(SUM(total_tokens), 0) as total_tokens,
			ROUND(CAST(SUM(CASE WHEN success = 1 THEN request_count ELSE 0 END) AS FLOAT) * 100.0 / NULLIF(SUM(request_count), 0), 2) as success_rate
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

// AddRoutes 批量添加路由（一个模型一条记录）
func (s *RouteService) AddRoutes(baseName string, models []string, apiUrl, apiKey, group, format string) error {
	now := time.Now()

	for _, model := range models {
		// 路由名直接使用基础名称（不再拼接模型名）
		routeName := baseName

		query := `INSERT INTO model_routes (name, model, api_url, api_key, "group", format, enabled, target_route_id, created_at, updated_at)
		          VALUES (?, ?, ?, ?, ?, ?, 1, 0, ?, ?)`

		_, err := s.db.Exec(query, routeName, model, apiUrl, apiKey, group, format, now, now)
		if err != nil {
			log.Errorf("Failed to add route: %v", err)
			return err
		}

		log.Infof("Route added: %s -> %s (%s) [%s]", model, apiUrl, routeName, format)
	}

	return nil
}

// ClearAllRoutes 清空所有路由数据
func (s *RouteService) ClearAllRoutes() error {
	return database.ClearAllRoutes(s.db)
}

// HasMultiModelRoutes 检测是否存在包含逗号分隔多模型的旧数据
func (s *RouteService) HasMultiModelRoutes() (bool, error) {
	return database.HasMultiModelRoutes(s.db)
}

// AutoCompressOldLogs 自动压缩7天前的日志数据到天级别
// 在应用启动时调用，将7天前的小时级别数据合并为天级别
func (s *RouteService) AutoCompressOldLogs() error {
	// 1. 查找7天前的数据（按小时记录的）
	// 首先检查是否有需要压缩的数据
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(DISTINCT substr(created_at, 1, 10))
		FROM request_logs
		WHERE substr(created_at, 1, 10) < date('now', 'localtime', '-7 days')
	`).Scan(&count)
	if err != nil {
		log.Errorf("Failed to check old logs: %v", err)
		return err
	}

	if count == 0 {
		log.Info("No old logs to compress")
		return nil
	}

	log.Infof("Found %d days of old logs to compress", count)

	// 2. 创建临时表存储7天前按天聚合后的数据
	createTempTable := `
		CREATE TEMPORARY TABLE daily_compressed_logs AS
		SELECT
			min(id) as id,
			model,
			min(route_id) as route_id,
			SUM(request_tokens) as request_tokens,
			SUM(response_tokens) as response_tokens,
			SUM(total_tokens) as total_tokens,
			SUM(request_count) as request_count,
			MIN(CASE WHEN success = 0 THEN 0 ELSE 1 END) as success,
			MAX(error_message) as error_message,
			substr(created_at, 1, 10) || ' 00:00:00' as created_at
		FROM request_logs
		WHERE substr(created_at, 1, 10) < date('now', 'localtime', '-7 days')
		GROUP BY model, substr(created_at, 1, 10)
	`

	_, err = s.db.Exec(createTempTable)
	if err != nil {
		log.Errorf("Failed to create temp table for compression: %v", err)
		return err
	}
	defer s.db.Exec("DROP TABLE IF EXISTS daily_compressed_logs")

	// 3. 获取压缩前的记录数
	var beforeCount int
	err = s.db.QueryRow(`
		SELECT COUNT(*) FROM request_logs
		WHERE substr(created_at, 1, 10) < date('now', 'localtime', '-7 days')
	`).Scan(&beforeCount)
	if err != nil {
		return err
	}

	// 4. 删除7天前的原数据
	_, err = s.db.Exec(`
		DELETE FROM request_logs
		WHERE substr(created_at, 1, 10) < date('now', 'localtime', '-7 days')
	`)
	if err != nil {
		log.Errorf("Failed to delete old logs: %v", err)
		return err
	}

	// 5. 从临时表插入压缩后的数据
	insertCompressed := `
		INSERT INTO request_logs (id, model, route_id, request_tokens, response_tokens, total_tokens, request_count, success, error_message, created_at)
		SELECT id, model, route_id, request_tokens, response_tokens, total_tokens, request_count, success, error_message, created_at
		FROM daily_compressed_logs
	`

	_, err = s.db.Exec(insertCompressed)
	if err != nil {
		log.Errorf("Failed to insert compressed logs: %v", err)
		return err
	}

	// 6. 获取压缩后的记录数
	var afterCount int
	err = s.db.QueryRow(`
		SELECT COUNT(*) FROM request_logs
		WHERE substr(created_at, 1, 10) < date('now', 'localtime', '-7 days')
	`).Scan(&afterCount)
	if err != nil {
		log.Warnf("Failed to count compressed logs: %v", err)
		afterCount = 0
	}

	// 7. 执行 VACUUM 释放数据库空间
	// 注意：VACUUM 会重建数据库文件，可能需要一些时间
	_, err = s.db.Exec("VACUUM")
	if err != nil {
		log.Warnf("Failed to VACUUM database after compression: %v", err)
		// VACUUM 失败不影响压缩结果，仅记录警告
	}

	deletedCount := beforeCount - afterCount
	log.Infof("Old logs auto-compressed: %d -> %d days (reduced %d hourly records to daily)", beforeCount, afterCount, deletedCount)
	return nil
}

// GetRouteByForwarding 通过路由的 target_route_id 字段获取目标路由
// 首先检查路由的 target_route_id 字段，如果设置了且非零，并且转发已启用，则转发到目标路由
func (s *RouteService) GetRouteByForwarding(model string) (*database.ModelRoute, error) {
	// 首先直接查询该模型对应的路由
	var route database.ModelRoute
	var err error

	// 检查是否是带前缀的模型名 (RouteName/ModelName)
	if strings.Contains(model, "/") {
		parts := strings.SplitN(model, "/", 2)
		if len(parts) == 2 {
			routeName := parts[0]
			originalModel := parts[1]
			log.Infof("Looking up route by name: %s (original model: %s)", routeName, originalModel)

			// 通过路由名和模型名查询（精确匹配）
			query := `SELECT id, name, model, api_url, api_key, "group", COALESCE(format, 'openai'), enabled,
			          COALESCE(target_route_id, 0), COALESCE(forwarding_enabled, 0), created_at, updated_at
			          FROM model_routes WHERE name = ? AND model = ? AND enabled = 1 LIMIT 1`
			err = s.db.QueryRow(query, routeName, originalModel).Scan(&route.ID, &route.Name, &route.Model, &route.APIUrl,
				&route.APIKey, &route.Group, &route.Format, &route.Enabled, &route.TargetRouteID, &route.ForwardingEnabled, &route.CreatedAt, &route.UpdatedAt)

			if err == nil {
				// 检查是否设置了转发目标且转发已启用
				if route.ForwardingEnabled && route.TargetRouteID > 0 && route.TargetRouteID != route.ID {
					// 获取目标路由
					targetRoute, err := s.GetRouteByID(route.TargetRouteID)
					if err != nil {
						log.Errorf("Failed to get target route %d for model '%s': %v", route.TargetRouteID, model, err)
						return nil, fmt.Errorf("forwarding target route not found: %d", route.TargetRouteID)
					}
					// 使用原始模型名记录日志
					log.Infof("Model '%s' forwarded from route %s (id=%d) to route %s (id=%d)",
						originalModel, routeName, route.ID, targetRoute.Name, targetRoute.ID)
					return targetRoute, nil
				}
				// 没有转发设置或转发未启用，使用当前路由
				if route.ForwardingEnabled && route.TargetRouteID > 0 {
					log.Infof("Forwarding configured but disabled for route %s (id=%d), target_route_id=%d",
						routeName, route.ID, route.TargetRouteID)
				}
				route.Model = originalModel
				log.Infof("Found route by name: %s, using model: %s", routeName, originalModel)
				return &route, nil
			}
		}
	}

	// 如果不是带前缀的格式，或者通过路由名查找失败，则按模型名查找
	query := `SELECT id, name, model, api_url, api_key, "group", COALESCE(format, 'openai'), enabled,
	          COALESCE(target_route_id, 0), COALESCE(forwarding_enabled, 0), created_at, updated_at
	          FROM model_routes WHERE model = ? AND enabled = 1 ORDER BY RANDOM() LIMIT 1`

	err = s.db.QueryRow(query, model).Scan(&route.ID, &route.Name, &route.Model, &route.APIUrl,
		&route.APIKey, &route.Group, &route.Format, &route.Enabled, &route.TargetRouteID, &route.ForwardingEnabled, &route.CreatedAt, &route.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("model not found: %s", model)
	}
	if err != nil {
		return nil, err
	}

	// 检查是否设置了转发目标且转发已启用
	if route.ForwardingEnabled && route.TargetRouteID > 0 && route.TargetRouteID != route.ID {
		// 获取目标路由
		targetRoute, err := s.GetRouteByID(route.TargetRouteID)
		if err != nil {
			log.Errorf("Failed to get target route %d for model '%s': %v", route.TargetRouteID, model, err)
			return nil, fmt.Errorf("forwarding target route not found: %d", route.TargetRouteID)
		}
		log.Infof("Model '%s' forwarded from route id=%d to route %s (id=%d)",
			model, route.ID, targetRoute.Name, targetRoute.ID)
		return targetRoute, nil
	}

	// 没有转发设置或转发未启用，返回当前路由
	if route.ForwardingEnabled && route.TargetRouteID > 0 {
		log.Infof("Forwarding configured but disabled for route id=%d, target_route_id=%d",
			route.ID, route.TargetRouteID)
	}
	return &route, nil
}

