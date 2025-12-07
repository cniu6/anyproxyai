//go:build windows
// +build windows

package system

import (
	"context"
	_ "embed"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/energye/systray"
	log "github.com/sirupsen/logrus"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed icon.ico
var trayIconData []byte

// SystemTray 系统托盘管理器
type SystemTray struct {
	ctx          context.Context
	mShow        *systray.MenuItem
	mQuit        *systray.MenuItem
	isRunning    int32 // 使用 atomic
	mu           sync.RWMutex
	quitCallback func()
	stopCh       chan struct{}
	showCh       chan struct{} // 用于触发显示窗口
}

// NewSystemTray 创建系统托盘管理器
func NewSystemTray(ctx context.Context) *SystemTray {
	return &SystemTray{
		ctx:    ctx,
		stopCh: make(chan struct{}),
		showCh: make(chan struct{}, 1),
	}
}

// SetQuitCallback 设置退出回调
func (s *SystemTray) SetQuitCallback(callback func()) {
	s.quitCallback = callback
}

// ShowWindow 显示窗口
func (s *SystemTray) ShowWindow() {
	log.Info("System tray: Showing window")

	// 使用 goroutine 避免阻塞托盘消息循环
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("ShowWindow panic: %v", r)
			}
		}()

		runtime.WindowShow(s.ctx)
		runtime.WindowUnminimise(s.ctx)

		// 将窗口置于前台
		runtime.WindowSetAlwaysOnTop(s.ctx, true)
		time.Sleep(50 * time.Millisecond)
		runtime.WindowSetAlwaysOnTop(s.ctx, false)
	}()
}

// HideWindow 隐藏窗口
func (s *SystemTray) HideWindow() {
	log.Info("System tray: Hiding window")
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("HideWindow panic: %v", r)
			}
		}()
		runtime.WindowHide(s.ctx)
	}()
}

// QuitApp 退出应用
func (s *SystemTray) QuitApp() {
	log.Info("System tray: Quitting application")

	if !atomic.CompareAndSwapInt32(&s.isRunning, 1, 0) {
		return
	}

	// 安全关闭 stop channel
	s.mu.Lock()
	select {
	case <-s.stopCh:
		// already closed
	default:
		close(s.stopCh)
	}
	s.mu.Unlock()

	// 先退出托盘
	systray.Quit()

	// 调用退出回调或直接退出
	if s.quitCallback != nil {
		s.quitCallback()
	} else {
		os.Exit(0)
	}
}

// Setup 设置系统托盘
func (s *SystemTray) Setup() error {
	if !atomic.CompareAndSwapInt32(&s.isRunning, 0, 1) {
		return nil
	}

	// 在后台启动系统托盘
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("System tray setup panic: %v", r)
				atomic.StoreInt32(&s.isRunning, 0)
			}
		}()

		systray.Run(s.onReady, s.onExit)
	}()

	log.Info("System tray setup completed")
	return nil
}

// onReady 托盘就绪回调
func (s *SystemTray) onReady() {
	// 设置托盘图标
	if len(trayIconData) > 0 {
		systray.SetIcon(trayIconData)
	}
	systray.SetTitle("AnyProxyAi")
	systray.SetTooltip("AnyProxyAi - Click to open")

	// 设置左键单击直接打开窗口
	systray.SetOnClick(func(menu systray.IMenu) {
		s.ShowWindow()
	})

	// 设置左键双击也打开窗口
	systray.SetOnDClick(func(menu systray.IMenu) {
		s.ShowWindow()
	})

	// 添加菜单项 (使用英文以确保兼容性)
	s.mShow = systray.AddMenuItem("Open", "Open main window")
	s.mShow.Click(func() {
		s.ShowWindow()
	})

	systray.AddSeparator()

	s.mQuit = systray.AddMenuItem("Exit", "Exit AnyProxyAi")
	s.mQuit.Click(func() {
		log.Info("Quit menu clicked")
		s.QuitApp()
	})

	// 定期刷新保持托盘活跃
	go s.keepAlive()
}

// keepAlive 保持托盘活跃 - 修复 Windows 托盘无响应问题
func (s *SystemTray) keepAlive() {
	// 使用更短的间隔来保持托盘响应
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	refreshCount := 0

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			if atomic.LoadInt32(&s.isRunning) == 0 {
				return
			}

			refreshCount++

			// 每次都刷新 tooltip 以保持消息泵活跃
			systray.SetTooltip("AnyProxyAi - API Proxy Manager")

			// 每 30 秒刷新一次图标
			if refreshCount%6 == 0 && len(trayIconData) > 0 {
				systray.SetIcon(trayIconData)
			}
		}
	}
}

// handleStopChannel 监听停止信号
func (s *SystemTray) handleStopChannel() {
	<-s.stopCh
	// 停止信号收到，退出
}

// onExit 托盘退出回调
func (s *SystemTray) onExit() {
	atomic.StoreInt32(&s.isRunning, 0)
	log.Info("System tray exited")
}

// Quit 退出托盘
func (s *SystemTray) Quit() {
	if atomic.CompareAndSwapInt32(&s.isRunning, 1, 0) {
		s.mu.Lock()
		select {
		case <-s.stopCh:
			// already closed
		default:
			close(s.stopCh)
		}
		s.mu.Unlock()
		systray.Quit()
	}
}
