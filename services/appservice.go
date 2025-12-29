package services

import (
	"fmt"
	"os"
	"os/exec"

	"openai-router-go/internal/config"
	"openai-router-go/internal/service"
	"openai-router-go/internal/system"

	log "github.com/sirupsen/logrus"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// RouteInfo 路由信息结构体（用于前端）
type RouteInfo struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Model   string `json:"model"`
	APIUrl  string `json:"api_url"`
	APIKey  string `json:"api_key"`
	Group   string `json:"group"`
	Format  string `json:"format"`
	Enabled bool   `json:"enabled"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

// StatsInfo 统计信息结构体
type StatsInfo struct {
	RouteCount    int     `json:"route_count"`
	ModelCount    int     `json:"model_count"`
	TotalRequests int64   `json:"total_requests"`
	TotalTokens   int64   `json:"total_tokens"`
	TodayRequests int64   `json:"today_requests"`
	TodayTokens   int64   `json:"today_tokens"`
	SuccessRate   float64 `json:"success_rate"`
}

// ConfigInfo 配置信息结构体
type ConfigInfo struct {
	LocalApiKey           string `json:"localApiKey"`
	OpenaiEndpoint        string `json:"openaiEndpoint"`
	RedirectEnabled       bool   `json:"redirectEnabled"`
	RedirectKeyword       string `json:"redirectKeyword"`
	RedirectTargetModel   string `json:"redirectTargetModel"`
	RedirectTargetName    string `json:"redirectTargetName"`
	RedirectTargetRouteID int64  `json:"redirectTargetRouteId"`
	MinimizeToTray        bool   `json:"minimizeToTray"`
	AutoStart             bool   `json:"autoStart"`
	EnableFileLog         bool   `json:"enableFileLog"`
	Port                  int    `json:"port"`
}

// AppSettingsInfo 应用设置结构体
type AppSettingsInfo struct {
	MinimizeToTray   bool `json:"minimizeToTray"`
	AutoStart        bool `json:"autoStart"`
	AutoStartEnabled bool `json:"autoStartEnabled"`
}

// DailyStatsInfo 每日统计结构体
type DailyStatsInfo struct {
	Date           string `json:"date"`
	Requests       int64  `json:"requests"`
	RequestTokens  int64  `json:"request_tokens"`
	ResponseTokens int64  `json:"response_tokens"`
	TotalTokens    int64  `json:"total_tokens"`
}

// HourlyStatsInfo 小时统计结构体
type HourlyStatsInfo struct {
	Hour        int   `json:"hour"`
	Requests    int64 `json:"requests"`
	TotalTokens int64 `json:"total_tokens"`
}

// ModelRankingInfo 模型排行结构体
type ModelRankingInfo struct {
	Rank           int     `json:"rank"`
	Model          string  `json:"model"`
	Requests       int64   `json:"requests"`
	RequestTokens  int64   `json:"request_tokens"`
	ResponseTokens int64   `json:"response_tokens"`
	TotalTokens    int64   `json:"total_tokens"`
	SuccessRate    float64 `json:"success_rate"`
}

// AppService 结构体用于 Wails v3 服务绑定
type AppService struct {
	App          *application.App
	RouteService *service.RouteService
	ProxyService *service.ProxyService
	Config       *config.Config
	AutoStart    *system.AutoStart
}

// NewAppService 创建新的 AppService 实例
func NewAppService(routeService *service.RouteService, proxyService *service.ProxyService, cfg *config.Config, autoStart *system.AutoStart) *AppService {
	return &AppService{
		RouteService: routeService,
		ProxyService: proxyService,
		Config:       cfg,
		AutoStart:    autoStart,
	}
}

// SetApp 设置应用引用
func (a *AppService) SetApp(app *application.App) {
	a.App = app
}

// GetLanguage 获取当前语言设置
func (a *AppService) GetLanguage() string {
	return a.Config.Language
}

// SetLanguage 设置语言
func (a *AppService) SetLanguage(lang string) error {
	a.Config.Language = lang
	return a.Config.Save()
}

// GetRoutes 获取所有路由
func (a *AppService) GetRoutes() ([]RouteInfo, error) {
	routes, err := a.RouteService.GetAllRoutes()
	if err != nil {
		return nil, err
	}

	result := make([]RouteInfo, len(routes))
	for i, route := range routes {
		result[i] = RouteInfo{
			ID:      route.ID,
			Name:    route.Name,
			Model:   route.Model,
			APIUrl:  route.APIUrl,
			APIKey:  route.APIKey,
			Group:   route.Group,
			Format:  route.Format,
			Enabled: route.Enabled,
			Created: route.CreatedAt.Format("2006-01-02 15:04:05"),
			Updated: route.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
	}
	return result, nil
}

// AddRoute 添加路由
func (a *AppService) AddRoute(name, model, apiUrl, apiKey, group, format string) error {
	return a.RouteService.AddRoute(name, model, apiUrl, apiKey, group, format)
}

// AddRoutes 批量添加路由（一个模型一条记录）
func (a *AppService) AddRoutes(baseName string, models []string, apiUrl, apiKey, group, format string) error {
	return a.RouteService.AddRoutes(baseName, models, apiUrl, apiKey, group, format)
}

// UpdateRoute 更新路由
func (a *AppService) UpdateRoute(id int64, name, model, apiUrl, apiKey, group, format string) error {
	return a.RouteService.UpdateRoute(id, name, model, apiUrl, apiKey, group, format)
}

// UpdateRouteByKey 通过组合键 (oldName, oldModel) 更新路由
func (a *AppService) UpdateRouteByKey(oldName, oldModel, name, model, apiUrl, apiKey, group, format string) error {
	return a.RouteService.UpdateRouteByKey(oldName, oldModel, name, model, apiUrl, apiKey, group, format)
}

// DeleteRoute 删除路由
func (a *AppService) DeleteRoute(id int64) error {
	return a.RouteService.DeleteRoute(id)
}

// DeleteRouteByKey 通过组合键 (name, model) 删除路由
func (a *AppService) DeleteRouteByKey(name, model string) error {
	return a.RouteService.DeleteRouteByKey(name, model)
}

// GetStats 获取统计信息
func (a *AppService) GetStats() (StatsInfo, error) {
	stats, err := a.RouteService.GetStats()
	if err != nil {
		return StatsInfo{}, err
	}
	// 从 map 转换为结构体
	result := StatsInfo{}
	if v, ok := stats["route_count"].(int); ok {
		result.RouteCount = v
	}
	if v, ok := stats["model_count"].(int); ok {
		result.ModelCount = v
	}
	// RouteService.GetStats 返回的是 int 类型，不是 int64
	if v, ok := stats["total_requests"].(int); ok {
		result.TotalRequests = int64(v)
	}
	if v, ok := stats["total_tokens"].(int); ok {
		result.TotalTokens = int64(v)
	}
	if v, ok := stats["today_requests"].(int); ok {
		result.TodayRequests = int64(v)
	}
	if v, ok := stats["today_tokens"].(int); ok {
		result.TodayTokens = int64(v)
	}
	if v, ok := stats["success_rate"].(float64); ok {
		result.SuccessRate = v
	}
	return result, nil
}

// GetDailyStats 获取每日统计（用于热力图）
func (a *AppService) GetDailyStats(days int) ([]map[string]interface{}, error) {
	return a.RouteService.GetDailyStats(days)
}

// GetHourlyStats 获取今日按小时统计（用于折线图）
func (a *AppService) GetHourlyStats() ([]map[string]interface{}, error) {
	return a.RouteService.GetHourlyStats()
}

// GetModelRanking 获取模型使用排行
func (a *AppService) GetModelRanking(limit int) ([]map[string]interface{}, error) {
	return a.RouteService.GetModelRanking(limit)
}

// GetConfig 获取配置
func (a *AppService) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"localApiKey":           a.Config.LocalAPIKey,
		"openaiEndpoint":        fmt.Sprintf("http://%s:%d", a.Config.Host, a.Config.Port),
		"redirectEnabled":       a.Config.RedirectEnabled,
		"redirectKeyword":       a.Config.RedirectKeyword,
		"redirectTargetModel":   a.Config.RedirectTargetModel,
		"redirectTargetName":    a.Config.RedirectTargetName,
		"redirectTargetRouteId": a.Config.RedirectTargetRouteID,
		"minimizeToTray":        a.Config.MinimizeToTray,
		"autoStart":             a.Config.AutoStart,
		"enableFileLog":         a.Config.EnableFileLog,
		"port":                  a.Config.Port,
	}
}

// UpdateConfig 更新配置
func (a *AppService) UpdateConfig(redirectEnabled bool, redirectKeyword, redirectTargetModel string, redirectTargetRouteId int64) error {
	a.Config.RedirectEnabled = redirectEnabled
	a.Config.RedirectKeyword = redirectKeyword
	a.Config.RedirectTargetModel = redirectTargetModel
	a.Config.RedirectTargetRouteID = redirectTargetRouteId
	return a.Config.Save()
}

// UpdatePort 更新端口配置
func (a *AppService) UpdatePort(port int) error {
	a.Config.Port = port
	return a.Config.Save()
}

// UpdateLocalApiKey 更新本地 API Key
func (a *AppService) UpdateLocalApiKey(newApiKey string) error {
	a.Config.LocalAPIKey = newApiKey
	return a.Config.Save()
}

// FetchRemoteModels 获取远程模型列表
func (a *AppService) FetchRemoteModels(apiUrl, apiKey string) ([]string, error) {
	return a.ProxyService.FetchRemoteModels(apiUrl, apiKey)
}

// ImportRouteFromFormat 从不同格式导入路由
func (a *AppService) ImportRouteFromFormat(name, model, apiUrl, apiKey, group, targetFormat string) (string, error) {
	return a.RouteService.ImportRouteFromFormat(name, model, apiUrl, apiKey, group, targetFormat)
}

// GetAppSettings 获取应用设置
func (a *AppService) GetAppSettings() map[string]interface{} {
	autoStartEnabled := false
	if a.AutoStart != nil {
		autoStartEnabled = a.AutoStart.IsAutoStartEnabled()
	}

	return map[string]interface{}{
		"minimizeToTray":   a.Config.MinimizeToTray,
		"autoStart":        a.Config.AutoStart,
		"autoStartEnabled": autoStartEnabled,
	}
}

// SetMinimizeToTray 设置关闭时最小化到托盘
func (a *AppService) SetMinimizeToTray(enabled bool) error {
	log.Infof("Setting minimize to tray: %v", enabled)
	a.Config.MinimizeToTray = enabled

	if err := a.Config.Save(); err != nil {
		log.Errorf("Failed to save config: %v", err)
		return fmt.Errorf("failed to save config: %v", err)
	}

	log.Info("Minimize to tray setting updated successfully")
	return nil
}

// SetAutoStart 设置开机自启动
func (a *AppService) SetAutoStart(enabled bool) error {
	log.Infof("Setting auto-start: %v", enabled)

	if a.AutoStart == nil {
		log.Error("Auto-start manager not initialized")
		return fmt.Errorf("auto-start manager not initialized")
	}

	if enabled {
		if err := a.AutoStart.EnableAutoStart(); err != nil {
			log.Errorf("Failed to enable auto-start: %v", err)
			return fmt.Errorf("failed to enable auto-start: %v", err)
		}
	} else {
		if err := a.AutoStart.DisableAutoStart(); err != nil {
			log.Errorf("Failed to disable auto-start: %v", err)
			return fmt.Errorf("failed to disable auto-start: %v", err)
		}
	}

	a.Config.AutoStart = enabled
	if err := a.Config.Save(); err != nil {
		log.Errorf("Failed to save config: %v", err)
		return fmt.Errorf("failed to save config: %v", err)
	}

	log.Info("Auto-start setting updated successfully")
	return nil
}

// ClearStats 清除统计数据
func (a *AppService) ClearStats() error {
	err := a.RouteService.ClearStats()
	if err != nil {
		return fmt.Errorf("failed to clear statistics: %v", err)
	}

	log.Info("Statistics cleared successfully")
	return nil
}

// LogFile 全局日志文件句柄（由 main 包设置）
var LogFile interface {
	Close() error
}

// SetEnableFileLog 设置是否启用文件日志
func (a *AppService) SetEnableFileLog(enabled bool) error {
	log.Infof("Setting enable file log: %v", enabled)
	a.Config.EnableFileLog = enabled

	if err := a.Config.Save(); err != nil {
		log.Errorf("Failed to save config: %v", err)
		return fmt.Errorf("failed to save config: %v", err)
	}

	// 注意：实际的日志文件启用/禁用逻辑在 main 包中处理
	log.Info("File log setting updated successfully")
	return nil
}

// RestartApp 重启应用
func (a *AppService) RestartApp() error {
	log.Info("Restarting application...")

	// 获取当前可执行文件路径
	executable, err := os.Executable()
	if err != nil {
		log.Errorf("Failed to get executable path: %v", err)
		return fmt.Errorf("failed to get executable path: %v", err)
	}

	// 启动新进程
	cmd := exec.Command(executable)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		log.Errorf("Failed to start new process: %v", err)
		return fmt.Errorf("failed to start new process: %v", err)
	}

	log.Info("New process started, quitting current process...")

	// 退出当前应用
	if a.App != nil {
		a.App.Quit()
	}
	return nil
}

// ClearAllRoutes 清空所有路由数据
func (a *AppService) ClearAllRoutes() error {
	err := a.RouteService.ClearAllRoutes()
	if err != nil {
		log.Errorf("Failed to clear all routes: %v", err)
		return fmt.Errorf("failed to clear all routes: %v", err)
	}
	log.Info("All routes cleared successfully")
	return nil
}

// HasMultiModelRoutes 检测是否存在包含逗号分隔多模型的旧数据
func (a *AppService) HasMultiModelRoutes() (bool, error) {
	return a.RouteService.HasMultiModelRoutes()
}
