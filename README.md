# AnyProxyAi

<div align="center">

**Universal AI API Proxy Router with Multi-Format Support**

[![Build Status](https://github.com/cniu6/anyproxyai/workflows/Build%20All%20Platforms/badge.svg)](https://github.com/cniu6/anyproxyai/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![Wails](https://img.shields.io/badge/Wails-v2-blue)](https://wails.io/)

[English](README.md) | [简体中文](README_CN.md)

**💰 This tool is valued at $100 USD - developed with Claude AI assistance at equivalent cost**

</div>

## 📖 Introduction

AnyProxyAi is a universal AI API proxy router that supports multiple API formats (OpenAI, Claude, Gemini) with automatic format conversion, load balancing, and intelligent routing. It provides a unified local endpoint for all your AI API needs.

### Why AnyProxyAi?

- **Multi-Format Support**: Seamlessly convert between OpenAI, Claude (Anthropic), and Gemini API formats
- **Unified Endpoint**: Single local API endpoint for all AI services
- **Load Balancing**: Automatic routing across multiple API endpoints
- **Usage Statistics**: Real-time monitoring of requests, tokens, and success rates
- **Cross-Platform**: Native desktop app for Windows, macOS, and Linux
- **Multi-Language**: Supports Chinese and English, follows system language by default

## 📸 Screenshots

<p align="center">
  <img src="https://github.com/cniu6/anyproxyai/blob/master/img/png1.png?raw=true" alt="Home Page" width="80%">
  <br/>Home Page - Dashboard
</p>

<p align="center">
  <img src="https://github.com/cniu6/anyproxyai/blob/master/img/png2.png?raw=true" alt="Model List" width="80%">
  <br/>Model List - Route Management
</p>

<p align="center">
  <img src="https://github.com/cniu6/anyproxyai/blob/master/img/png3.png?raw=true" alt="Statistics" width="80%">
  <br/>Usage Statistics - Heatmap & Charts
</p>

<p align="center">
  <img src="https://github.com/cniu6/anyproxyai/blob/master/img/png4.png?raw=true" alt="Add Route" width="80%">
  <br/>Add Route Dialog
</p>

<p align="center">
  <img src="https://github.com/cniu6/anyproxyai/blob/master/img/png5.png?raw=true" alt="Settings" width="80%">
  <br/>Settings Page
</p>

<p align="center">
  <img src="https://github.com/cniu6/anyproxyai/blob/master/img/png6.png?raw=true" alt="Language Switch" width="80%">
  <br/>Language Switch (Persistent)
</p>

<p align="center">
  <img src="https://github.com/cniu6/anyproxyai/blob/master/img/png7.png?raw=true" alt="Dark Theme" width="80%">
  <br/>Dark Theme
</p>

## ✨ Features

### Core Features

| Feature | Description |
|---------|-------------|
| 🔄 **API Format Conversion** | Automatic conversion between OpenAI, Claude, and Gemini formats |
| 🔀 **Smart Routing** | Route requests to different backends based on model name |
| 🔁 **Proxy Redirect** | Use `proxy_auto` keyword to redirect to any configured model |
| 📊 **Real-time Stats** | Monitor requests, errors, and token usage |
| 📈 **Historical Data** | SQLite-based statistics with heatmap visualization |
| 🎯 **Model Ranking** | Track most used models and their performance |
| 🌐 **Multi-Language** | Chinese/English support, follows system language, switchable from top-right corner (persistent) |

### Supported API Formats

| Format | Input | Output | Streaming | Stream Conversion |
|--------|-------|--------|-----------|-------------------|
| OpenAI | ✅ | ✅ | ✅ | ✅ |
| Claude (Anthropic) | ✅ | ✅ | ✅ | ✅ |
| Gemini | ✅ | ✅ | ✅ | ✅ |
| Claude Code | ✅ | ✅ | ✅ | ✅ |

### UI Features

| Feature | Description |
|---------|-------------|
| 🖥️ **Cross-platform Desktop App** | Windows, macOS, Linux support |
| 🎨 **Dark/Light Theme** | Toggle between themes |
| 🌐 **Language Switch** | Switch language from top-right corner popup (persistent) |
| 📋 **System Tray** | Minimize to system tray |
| 📝 **Route Management** | Add, edit, delete, import/export routes |
| 📊 **Usage Dashboard** | Heatmap, charts, and statistics |

## 🚀 Quick Start

### Download

[📥 Download Latest Release](https://github.com/cniu6/anyproxyai/releases/latest)

**Available Builds:**
- `anyproxyai-windows-amd64.exe` - Windows x64
- `anyproxyai-windows-arm64.exe` - Windows ARM64
- `anyproxyai-linux-amd64` - Linux x64
- `anyproxyai-linux-arm64` - Linux ARM64
- `anyproxyai-darwin-amd64.zip` - macOS Intel
- `anyproxyai-darwin-arm64.zip` - macOS Apple Silicon

#### Windows
1. Download `anyproxyai-windows-amd64.exe`
2. Run the executable
3. Allow firewall access if prompted

#### macOS
1. Download `anyproxyai-darwin-arm64.zip` (Apple Silicon) or `anyproxyai-darwin-amd64.zip` (Intel)
2. Extract and move `anyproxyai.app` to Applications
3. First run: Right-click → Open (bypass Gatekeeper)

#### Linux
```bash
chmod +x anyproxyai-linux-amd64
./anyproxyai-linux-amd64
```

### Setup

#### 1. Add API Route

Click "添加路由" (Add Route) and configure:

| Field | Description | Example |
|-------|-------------|---------|
| **Name** | Friendly name | `GPT-4 Turbo` |
| **Model** | Model identifier | `gpt-4-turbo` |
| **API URL** | Backend API URL | `https://api.openai.com` |
| **API Key** | Your API key | `sk-xxx...` |
| **Group** | Optional grouping | `OpenAI` |
| **Format** | API format type | `openai` / `claude` / `gemini` |

#### 2. Configure Your Application

Use the local proxy endpoint in your application:

**OpenAI Compatible:**
```
API Base URL: http://localhost:8080/api
API Key: (any value)
```

**Claude/Anthropic:**
```
API Base URL: http://localhost:8080/api/anthropic
API Key: (any value)
```

**Claude Code:**
```
API Base URL: http://localhost:8080/api/claudecode
API Key: (any value)
```

**Gemini:**
```
API Base URL: http://localhost:8080/api/gemini
API Key: (any value)
```

#### 3. Use Proxy Redirect (Optional)

Enable redirect and set `proxy_auto` as your model name to automatically route to your configured target model.

## 📖 Architecture

```
┌─────────────────┐     ┌─────────────────────────────────────────────────────┐
│  Your App       │────▶│                 AnyProxyAi                          │
│  (Any SDK)      │     │  localhost:8080                                     │
└─────────────────┘     │                                                     │
                        │  ┌─────────────────────────────────────────────┐   │
                        │  │              API Router                      │   │
                        │  │  /api/v1/*        → OpenAI format           │   │
                        │  │  /api/anthropic/* → Claude format           │   │
                        │  │  /api/claudecode/*→ Claude Code format      │   │
                        │  │  /api/gemini/*    → Gemini format           │   │
                        │  └─────────────────────────────────────────────┘   │
                        │                        │                            │
                        │  ┌─────────────────────▼─────────────────────────┐ │
                        │  │           Format Adapters                      │ │
                        │  │  ┌──────────┐ ┌──────────┐ ┌──────────┐       │ │
                        │  │  │ OpenAI   │ │ Claude   │ │ Gemini   │       │ │
                        │  │  │ Adapter  │ │ Adapter  │ │ Adapter  │       │ │
                        │  │  └──────────┘ └──────────┘ └──────────┘       │ │
                        │  └───────────────────────────────────────────────┘ │
                        │                        │                            │
                        │  ┌─────────────────────▼─────────────────────────┐ │
                        │  │           Backend Routes                       │ │
                        │  │  ┌──────────┐ ┌──────────┐ ┌──────────┐       │ │
                        │  │  │ OpenAI   │ │ Claude   │ │ Gemini   │       │ │
                        │  │  │ API      │ │ API      │ │ API      │       │ │
                        │  │  └──────────┘ └──────────┘ └──────────┘       │ │
                        │  └───────────────────────────────────────────────┘ │
                        └─────────────────────────────────────────────────────┘
```

### Request Flow

1. **Receive Request**: Local proxy receives API request
2. **Route Matching**: Find matching route by model name
3. **Format Detection**: Detect source format from request path
4. **Adapter Selection**: Choose appropriate adapter based on route format
5. **Request Transformation**: Convert request to target format
6. **Backend Call**: Forward to actual API endpoint
7. **Response Transformation**: Convert response back to source format
8. **Statistics**: Log request metrics

### Adapter Matrix

| Source → Target | OpenAI | Claude | Gemini |
|-----------------|--------|--------|--------|
| **OpenAI** | Pass-through | claude-to-openai | gemini-to-openai |
| **Claude** | openai-to-claude | Pass-through | gemini-to-claude |
| **Gemini** | openai-to-gemini | claude-to-gemini | Pass-through |

## 🔧 Configuration

### config.json

```json
{
  "host": "localhost",
  "port": 8080,
  "database_path": "routes.db",
  "local_api_key": "sk-local-default-key",
  "redirect_enabled": true,
  "redirect_keyword": "proxy_auto",
  "redirect_target_model": "gpt-4-turbo",
  "minimize_to_tray": true,
  "auto_start": false
}
```

### Route Configuration

Routes are stored in SQLite database (`routes.db`) with the following schema:

| Field | Type | Description |
|-------|------|-------------|
| `name` | TEXT | Display name |
| `model` | TEXT | Model identifier (used for routing) |
| `api_url` | TEXT | Backend API base URL |
| `api_key` | TEXT | API authentication key |
| `group` | TEXT | Optional grouping |
| `format` | TEXT | API format: `openai`, `claude`, `gemini` |
| `enabled` | INTEGER | 1=enabled, 0=disabled |

## 🛠️ Development

### Requirements

- Go 1.22+
- Node.js 18+
- Wails CLI v2

### Development Mode

```bash
# Install Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Install frontend dependencies
cd frontend && npm install && cd ..

# Run in dev mode
wails dev
```

### Build

```bash
# Build for current platform
wails build

# Build for specific platform
wails build -platform windows/amd64
wails build -platform linux/amd64
wails build -platform darwin/arm64
```

### Project Structure

```
anyproxyai/
├── main.go                    # Application entry, Wails bindings
├── wails.json                 # Wails configuration
├── config.json                # Runtime configuration
│
├── internal/                  # Go backend modules
│   ├── adapters/              # API format converters
│   │   ├── adapter.go         # Adapter interface
│   │   ├── anthropic.go       # Claude adapter
│   │   ├── gemini.go          # Gemini adapter
│   │   ├── openai_to_claude.go
│   │   ├── claude_to_openai.go
│   │   └── ...
│   ├── config/                # Configuration management
│   ├── database/              # SQLite database
│   ├── router/                # HTTP router (Gin)
│   ├── service/               # Business logic
│   │   ├── proxy_service.go   # Proxy & streaming
│   │   └── route_service.go   # Route management
│   └── system/                # System tray, autostart
│
└── frontend/                  # Vue 3 frontend
    ├── src/
    │   ├── App.vue            # Main application
    │   ├── i18n/              # Internationalization
    │   └── components/        # UI components
    └── wailsjs/               # Wails bindings
```

## 🔨 GitHub Actions Build

This project uses GitHub Actions for automated multi-platform builds.

### Trigger Build

Builds are triggered automatically when:
- Push to `main` or `master` branch
- Create a tag starting with `v` (e.g., `v1.0.0`)
- Pull request to `main` or `master`
- Manual trigger via workflow_dispatch

**💡 Tip: Use commit messages starting with `package(` to indicate build-related changes:**
```bash
# Examples (use one of these formats):
git commit -m "package(release): v1.0.0"
# or
git commit -m "package(build): fix linux arm64 build"
```

### Download Artifacts

1. Go to **Actions** tab in GitHub repository
2. Click on the completed workflow run
3. Download individual artifacts:
   - `anyproxyai-windows-amd64`
   - `anyproxyai-windows-arm64`
   - `anyproxyai-linux-amd64`
   - `anyproxyai-linux-arm64`
   - `anyproxyai-darwin-amd64`
   - `anyproxyai-darwin-arm64`

### Create Release

Tag a version to automatically create a GitHub Release:
```bash
git tag v1.0.0
git push origin v1.0.0
```

## ❓ FAQ

### Installation

**Q: Windows shows security warning?**
A: Click "More info" → "Run anyway". The app is not code-signed.

**Q: macOS shows "cannot be opened" error?**
A: Right-click → Open → Open. Or allow in System Preferences → Security & Privacy.

**Q: Port 8080 is already in use?**
A: Edit `config.json` and change the `port` value.

### Usage

**Q: How does format conversion work?**
A: The proxy detects the incoming request format from the URL path and converts it to the target format based on the route's `format` setting.

**Q: What is `proxy_auto`?**
A: A special keyword that redirects to your configured target model, allowing you to use a single model name across different applications.

**Q: Are token counts accurate?**
A: Token counts are estimates based on response data. Actual billing may differ.

**Q: How to switch language?**
A: Click the language icon in the top-right corner to open the language switch popup. The setting is persistent.

### Development

**Q: How to add a new adapter?**
A: Implement the `Adapter` interface in `internal/adapters/` and register it in `adapter.go`.

## 📄 License

This project is open source under the [MIT License](LICENSE).

---

<div align="center">
Made with ❤️ and Claude AI
</div>
