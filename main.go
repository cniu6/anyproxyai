package main

import (
	"embed"
	_ "embed"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"openai-router-go/internal/config"
	"openai-router-go/internal/database"
	"openai-router-go/internal/router"
	"openai-router-go/internal/service"
	"openai-router-go/internal/system"
	"openai-router-go/services"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

//go:embed assets/icon.png assets/icon-dark.png
var trayIcons embed.FS

// 全局日志文件句柄
var logFile *os.File

// setupFileLogging 设置文件日志
func setupFileLogging() (*os.File, error) {
	logDir := "log"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %v", err)
	}

	today := time.Now().Format("2006-01-02")
	logPath := filepath.Join(logDir, today+".log")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %v", err)
	}

	mw := io.MultiWriter(os.Stdout, file)
	log.SetOutput(mw)

	return file, nil
}

// checkPortAvailable 检查端口是否可用
func checkPortAvailable(host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is already in use: %v", port, err)
	}
	listener.Close()
	return nil
}

// showPortInUseError 显示端口占用错误对话框
func showPortInUseError(port int) {
	system.ShowErrorDialog(
		"Port Already In Use",
		fmt.Sprintf("API port %d is already in use.\n\nPlease check if another instance is running or change the port in config.json.", port),
	)
}

// loadTrayIcon 加载托盘图标
func loadTrayIcon(path string) []byte {
	data, err := trayIcons.ReadFile(path)
	if err != nil {
		log.Printf("failed to load tray icon %s: %v", path, err)
		return nil
	}
	return data
}

// main function serves as the application's entry point.
func main() {
	// 初始化日志
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	log.SetLevel(log.InfoLevel)

	// 加载配置
	cfg := config.LoadConfig()

	// 如果启用了文件日志，设置文件日志
	if cfg.EnableFileLog {
		var err error
		logFile, err = setupFileLogging()
		if err != nil {
			log.Warnf("Failed to setup file logging: %v", err)
		} else {
			log.Info("File logging enabled, logs will be saved to log/ directory")
			defer logFile.Close()
		}
	}

	// 检查端口是否被占用
	if err := checkPortAvailable(cfg.Host, cfg.Port); err != nil {
		log.Errorf("Port check failed: %v", err)
		showPortInUseError(cfg.Port)
		os.Exit(1)
	}
	log.Infof("Port %d is available", cfg.Port)

	// 初始化数据库
	db, err := database.InitDB(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// 创建服务
	routeService := service.NewRouteService(db, cfg)
	proxyService := service.NewProxyService(routeService, cfg)

	// 自动压缩7天前的日志数据到天级别
	log.Info("Checking for old logs to compress...")
	if err := routeService.AutoCompressOldLogs(); err != nil {
		log.Warnf("Failed to auto-compress old logs: %v", err)
		// 不影响应用启动，仅记录警告
	}

	// 初始化开机自启动管理器
	autoStart := system.NewAutoStart()

	// 创建应用服务实例（使用 services 包）
	appSvc := services.NewAppService(routeService, proxyService, cfg, autoStart)

	// 启动后台 API 服务器
	go func() {
		gin.SetMode(gin.ReleaseMode)
		r := router.SetupAPIRouter(cfg, routeService, proxyService)
		addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
		log.Infof("API server started at %s/api", addr)
		if err := r.Run(addr); err != nil {
			log.Errorf("Failed to start API server: %v", err)
		}
	}()

	// 创建 Wails v3 应用
	log.Info("Starting Wails v3 GUI application...")

	// 加载应用图标
	appIcon := loadTrayIcon("assets/icon.png")

	app := application.New(application.Options{
		Name:        "AnyProxyAi",
		Description: "Universal AI API Proxy Router with Multi-Format Support",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(appSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	appSvc.SetApp(app)

	// 创建主窗口
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "AnyProxyAi Manager",
		Width:            1280,
		Height:           800,
		MinWidth:         600,
		MinHeight:        300,
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})

	var mainWindowCentered bool

	// 聚焦主窗口的辅助函数
	focusMainWindow := func() {
		if runtime.GOOS == "windows" {
			mainWindow.SetAlwaysOnTop(true)
			mainWindow.Focus()
			go func() {
				time.Sleep(150 * time.Millisecond)
				mainWindow.SetAlwaysOnTop(false)
			}()
			return
		}
		mainWindow.Focus()
	}

	// 显示主窗口的辅助函数
	showMainWindow := func(withFocus bool) {
		if !mainWindowCentered {
			mainWindow.Center()
			mainWindowCentered = true
		}
		if mainWindow.IsMinimised() {
			mainWindow.UnMinimise()
		}
		mainWindow.Show()
		if withFocus {
			focusMainWindow()
		}
	}

	// 初始显示窗口
	showMainWindow(false)

	// 注册窗口关闭事件钩子 - 最小化到托盘
	mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if cfg.MinimizeToTray {
			log.Info("Minimizing to tray instead of closing")
			mainWindow.Hide()
			e.Cancel()
		}
	})

	// macOS 特定事件处理
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(event *application.ApplicationEvent) {
		showMainWindow(true)
	})

	app.Event.OnApplicationEvent(events.Mac.ApplicationDidBecomeActive, func(event *application.ApplicationEvent) {
		if mainWindow.IsVisible() {
			mainWindow.Focus()
			return
		}
		showMainWindow(true)
	})

	// 创建系统托盘 (Wails v3 原生)
	systray := app.SystemTray.New()
	systray.SetTooltip("AnyProxyAi")

	// 设置托盘图标
	if lightIcon := loadTrayIcon("assets/icon.png"); len(lightIcon) > 0 {
		systray.SetIcon(lightIcon)
	}
	if darkIcon := loadTrayIcon("assets/icon-dark.png"); len(darkIcon) > 0 {
		systray.SetDarkModeIcon(darkIcon)
	}

	// 托盘菜单文本（根据语言设置）
	showWindowText := "Show Window"
	quitText := "Quit"
	if cfg.Language == "zh-CN" {
		showWindowText = "显示主窗口"
		quitText = "退出"
	}

	// 创建托盘菜单
	trayMenu := application.NewMenu()
	trayMenu.Add(showWindowText).OnClick(func(ctx *application.Context) {
		showMainWindow(true)
	})
	trayMenu.AddSeparator()
	trayMenu.Add(quitText).OnClick(func(ctx *application.Context) {
		log.Info("Quit from tray menu")
		app.Quit()
	})
	systray.SetMenu(trayMenu)

	// 托盘点击事件
	systray.OnClick(func() {
		if !mainWindow.IsVisible() {
			showMainWindow(true)
			return
		}
		if !mainWindow.IsFocused() {
			focusMainWindow()
		}
	})

	// 运行应用
	err = app.Run()
	if err != nil {
		log.Fatal(err)
	}
}
