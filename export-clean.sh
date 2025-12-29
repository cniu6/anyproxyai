#!/bin/bash
# 导出干净的项目脚本 (Bash/Linux/macOS)
# 使用方法: chmod +x export-clean.sh && ./export-clean.sh

echo "正在清理项目文件..."

# 删除构建产物
echo "清理构建产物..."
rm -rf build/bin
rm -rf build/darwin
rm -rf build/linux

# 删除依赖和缓存
echo "清理依赖和缓存..."
rm -rf frontend/node_modules
rm -rf frontend/dist
rm -rf frontend/.vite
rm -rf frontend/wailsjs
rm -rf frontend/.cache
rm -f frontend/package-lock.json
rm -f frontend/yarn.lock
rm -f frontend/pnpm-lock.yaml
rm -f frontend/.env
rm -f frontend/.env.local
rm -f frontend/.env.*.local

# 删除数据库和日志
echo "清理数据库和日志..."
rm -f *.db
rm -f *.db-shm
rm -f *.db-wal
rm -rf log
rm -rf logs
rm -f *.log
rm -rf data

# 删除配置文件
echo "清理配置文件..."
rm -f config.json

# 删除临时文件
echo "清理临时文件..."
rm -rf tmp
rm -rf temp

echo ""
echo "清理完成!"
echo ""
echo "现在可以创建压缩包或提交到 Git:"
echo "1. 创建 tar.gz: tar -czf anyproxyai-clean.tar.gz --exclude='.git' --exclude='anyproxyai-clean.tar.gz' ."
echo "2. 创建 ZIP:     zip -r anyproxyai-clean.zip -x '*.git/*' -x 'anyproxyai-clean.zip' ."
echo "3. Git 提交:    git add . && git commit -m 'Clean project export'"
