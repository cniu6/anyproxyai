#!/usr/bin/env python3
"""
数据库测试数据生成脚本
用于生成测试数据以验证按小时聚合和请求次数统计功能
"""

import sqlite3
import random
from datetime import datetime, timedelta
import os

# 配置
DB_PATH = "routes.db"  # 数据库路径，如需测试主数据库请改为 "data/proxy.db"

# 测试模型列表
TEST_MODELS = [
    "gpt-4",
    "gpt-4-turbo",
    "gpt-3.5-turbo",
    "claude-3-opus-20240229",
    "claude-3-sonnet-20240229",
    "claude-3-haiku-20240307",
    "gemini-1.5-pro",
    "gemini-1.5-flash",
]

# 生成天数（包括今天）
DAYS_TO_GENERATE = 30


def init_database():
    """初始化数据库表结构"""
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()

    # 创建 model_routes 表
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS model_routes (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            model TEXT NOT NULL,
            api_url TEXT NOT NULL,
            api_key TEXT,
            "group" TEXT,
            format TEXT DEFAULT 'openai',
            enabled INTEGER DEFAULT 1,
            target_route_id INTEGER DEFAULT 0,
            forwarding_enabled INTEGER DEFAULT 0,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    """)

    # 创建索引
    cursor.execute("CREATE INDEX IF NOT EXISTS idx_model_routes_model ON model_routes(model)")
    cursor.execute("CREATE INDEX IF NOT EXISTS idx_model_routes_enabled ON model_routes(enabled)")

    # 创建 request_logs 表
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS request_logs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            model TEXT NOT NULL,
            route_id INTEGER,
            request_tokens INTEGER DEFAULT 0,
            response_tokens INTEGER DEFAULT 0,
            total_tokens INTEGER DEFAULT 0,
            request_count INTEGER DEFAULT 1,
            success INTEGER DEFAULT 1,
            error_message TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (route_id) REFERENCES model_routes(id) ON DELETE SET NULL
        )
    """)

    # 创建索引
    cursor.execute("CREATE INDEX IF NOT EXISTS idx_request_logs_model ON request_logs(model)")
    cursor.execute("CREATE INDEX IF NOT EXISTS idx_request_logs_created_at ON request_logs(created_at)")

    # 创建按小时聚合的唯一索引
    cursor.execute("""
        CREATE UNIQUE INDEX IF NOT EXISTS idx_request_logs_model_hour
        ON request_logs(model, substr(created_at, 1, 13))
    """)

    conn.commit()
    conn.close()

    print(f"✓ 数据库初始化完成: {DB_PATH}")


def insert_test_routes():
    """插入测试路由数据"""
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()

    # 清空现有路由
    cursor.execute("DELETE FROM model_routes")

    # 为每个模型创建路由
    for model in TEST_MODELS:
        name = model.split("-")[0].capitalize()  # 从模型名提取路由名
        cursor.execute("""
            INSERT INTO model_routes (name, model, api_url, api_key, "group", format, enabled)
            VALUES (?, ?, ?, ?, ?, ?, 1)
        """, (
            name,
            model,
            "https://api.test.com/v1",
            f"sk-test-{model}",
            "Test Group",
            "openai" if model.startswith("gpt") else ("claude" if "claude" in model else "gemini")
        ))

    conn.commit()
    conn.close()

    print(f"✓ 插入了 {len(TEST_MODELS)} 条测试路由")


def generate_hourly_data():
    """生成按小时的测试数据"""
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()

    # 清空现有日志
    cursor.execute("DELETE FROM request_logs")

    total_records = 0
    now = datetime.now()

    # 为过去 N 天生成数据
    for day_offset in range(DAYS_TO_GENERATE):
        current_date = now - timedelta(days=day_offset)
        date_str = current_date.strftime("%Y-%m-%d")

        # 每天随机选择几个模型进行请求
        daily_models = random.sample(TEST_MODELS, k=random.randint(3, len(TEST_MODELS)))

        for model in daily_models:
            # 每个模型每天随机生成多个小时的数据
            hours_with_data = random.sample(range(24), k=random.randint(2, 12))

            for hour in hours_with_data:
                # 构造时间戳 (YYYY-MM-DD HH:MM:SS)
                hour_str = f"{hour:02d}"
                minute = random.randint(0, 59)
                second = random.randint(0, 59)
                created_at = f"{date_str} {hour_str}:{minute:02d}:{second:02d}"

                # 随机生成 token 数量
                request_tokens = random.randint(100, 5000)
                response_tokens = random.randint(200, 8000)
                total_tokens = request_tokens + response_tokens

                # 90% 成功率
                success = 1 if random.random() < 0.9 else 0

                # 同一小时内可能有多次请求（模拟聚合前的数据）
                # 但由于有唯一索引，同一模型同一小时只会有一条记录
                # 这里我们模拟聚合后的数据，直接设置 request_count
                request_count = random.randint(1, 50)

                cursor.execute("""
                    INSERT OR REPLACE INTO request_logs
                    (model, route_id, request_tokens, response_tokens, total_tokens,
                     request_count, success, error_message, created_at)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                """, (
                    model,
                    random.randint(1, len(TEST_MODELS)),  # 随机 route_id
                    request_tokens,
                    response_tokens,
                    total_tokens,
                    request_count,
                    success,
                    "" if success else "Rate limit exceeded",
                    created_at
                ))

                total_records += 1

    conn.commit()
    conn.close()

    print(f"✓ 生成了 {total_records} 条测试日志（过去 {DAYS_TO_GENERATE} 天）")


def verify_data():
    """验证生成的数据"""
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()

    print("\n=== 数据验证 ===")

    # 统计总记录数
    cursor.execute("SELECT COUNT(*) FROM request_logs")
    total_logs = cursor.fetchone()[0]
    print(f"总记录数: {total_logs}")

    # 统计总请求数
    cursor.execute("SELECT SUM(request_count) FROM request_logs")
    total_requests = cursor.fetchone()[0] or 0
    print(f"总请求数: {total_requests}")

    # 统计总 token
    cursor.execute("SELECT SUM(total_tokens) FROM request_logs")
    total_tokens = cursor.fetchone()[0] or 0
    print(f"总 Token: {total_tokens:,}")

    # 按模型统计
    print("\n=== 按模型统计 ===")
    cursor.execute("""
        SELECT
            model,
            SUM(request_count) as requests,
            SUM(total_tokens) as tokens
        FROM request_logs
        GROUP BY model
        ORDER BY tokens DESC
    """)

    for row in cursor.fetchall():
        print(f"{row[0]:30} | 请求: {row[1]:5} | Token: {row[2]:10,}")

    # 按日期统计
    print("\n=== 最近7天统计 ===")
    cursor.execute("""
        SELECT
            substr(created_at, 1, 10) as date,
            SUM(request_count) as requests,
            SUM(total_tokens) as tokens
        FROM request_logs
        WHERE substr(created_at, 1, 10) >= date('now', '-7 days')
        GROUP BY date
        ORDER BY date DESC
    """)

    for row in cursor.fetchall():
        print(f"{row[0]} | 请求: {row[1]:5} | Token: {row[2]:10,}")

    # 检查是否有按小时聚合
    print("\n=== 按小时聚合检查（今天） ===")
    cursor.execute("""
        SELECT
            substr(created_at, 1, 13) as hour,
            model,
            request_count
        FROM request_logs
        WHERE substr(created_at, 1, 10) = date('now')
        ORDER BY hour, model
        LIMIT 10
    """)

    for row in cursor.fetchall():
        print(f"{row[0]} | {row[1]:20} | 请求: {row[2]}")

    conn.close()


def main():
    """主函数"""
    print("=" * 50)
    print("数据库测试数据生成脚本")
    print("=" * 50)

    # 确保数据目录存在
    os.makedirs(os.path.dirname(DB_PATH), exist_ok=True)

    # 1. 初始化数据库
    init_database()

    # 2. 插入测试路由
    insert_test_routes()

    # 3. 生成测试数据
    generate_hourly_data()

    # 4. 验证数据
    verify_data()

    print("\n" + "=" * 50)
    print("✓ 测试数据生成完成！")
    print(f"  数据库位置: {os.path.abspath(DB_PATH)}")
    print("=" * 50)


if __name__ == "__main__":
    main()
