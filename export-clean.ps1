# 导出干净的项目脚本 (PowerShell)
# 使用方法: .\export-clean.ps1

Write-Host "正在清理项目文件..." -ForegroundColor Green

# 删除构建产物
Write-Host "清理构建产物..." -ForegroundColor Yellow
Remove-Item -Path "build/bin" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "build/darwin" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "build/linux" -Recurse -Force -ErrorAction SilentlyContinue

# 删除依赖和缓存
Write-Host "清理依赖和缓存..." -ForegroundColor Yellow
Remove-Item -Path "frontend/node_modules" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "frontend/dist" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "frontend/.vite" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "frontend/wailsjs" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "frontend/.cache" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "frontend/package-lock.json" -Force -ErrorAction SilentlyContinue

# 删除数据库和日志
Write-Host "清理数据库和日志..." -ForegroundColor Yellow
Remove-Item -Path "*.db" -Force -ErrorAction SilentlyContinue
Remove-Item -Path "*.db-shm" -Force -ErrorAction SilentlyContinue
Remove-Item -Path "*.db-wal" -Force -ErrorAction SilentlyContinue
Remove-Item -Path "log" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "logs" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "*.log" -Force -ErrorAction SilentlyContinue
Remove-Item -Path "data" -Recurse -Force -ErrorAction SilentlyContinue

# 删除配置文件
Write-Host "清理配置文件..." -ForegroundColor Yellow
Remove-Item -Path "config.json" -Force -ErrorAction SilentlyContinue

# 删除临时文件
Write-Host "清理临时文件..." -ForegroundColor Yellow
Remove-Item -Path "tmp" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "temp" -Recurse -Force -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "清理完成!" -ForegroundColor Green
Write-Host ""
Write-Host "现在可以创建压缩包或提交到 Git:" -ForegroundColor Cyan
Write-Host "1. 创建 ZIP:   Compress-Archive -Path . -DestinationPath anyproxyai-clean.zip -Force" -ForegroundColor White
Write-Host "2. Git 提交:   git add . && git commit -m 'Clean project export'" -ForegroundColor White
