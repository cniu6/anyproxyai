package database

import (
	"database/sql"
	"time"

	log "github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// ModelRoute 模型路由表结构
type ModelRoute struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Model            string    `json:"model"`
	APIUrl           string    `json:"api_url"`
	APIKey           string    `json:"api_key"`
	Group            string    `json:"group"`
	Format           string    `json:"format"`    // 新增：格式类型 (openai, claude, gemini)
	Enabled          bool      `json:"enabled"`
	TargetRouteID    int64     `json:"target_route_id"`    // 转发目标路由ID，0表示不转发
	ForwardingEnabled bool     `json:"forwarding_enabled"` // 转发开关，true表示启用转发
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// RequestLog 请求日志表结构（按小时聚合）
type RequestLog struct {
	ID             int64     `json:"id"`
	Model          string    `json:"model"`
	RouteID        int64     `json:"route_id"`
	RequestTokens  int       `json:"request_tokens"`
	ResponseTokens int       `json:"response_tokens"`
	TotalTokens    int       `json:"total_tokens"`
	RequestCount   int       `json:"request_count"` // 请求次数
	Success        bool      `json:"success"`
	ErrorMessage   string    `json:"error_message"`
	CreatedAt      time.Time `json:"created_at"`
}

func InitDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// 创建表
	if err := createTables(db); err != nil {
		db.Close()
		return nil, err
	}

	log.Info("Database initialized successfully")
	return db, nil
}

func createTables(db *sql.DB) error {
	schema := `
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
	);

	CREATE INDEX IF NOT EXISTS idx_model_routes_model ON model_routes(model);
	CREATE INDEX IF NOT EXISTS idx_model_routes_enabled ON model_routes(enabled);
	CREATE INDEX IF NOT EXISTS idx_model_routes_group ON model_routes("group");

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
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_request_logs_model_hour ON request_logs(model, substr(created_at, 1, 13));
	CREATE INDEX IF NOT EXISTS idx_request_logs_model ON request_logs(model);
	CREATE INDEX IF NOT EXISTS idx_request_logs_route_id ON request_logs(route_id);
	CREATE INDEX IF NOT EXISTS idx_request_logs_created_at ON request_logs(created_at);
	CREATE INDEX IF NOT EXISTS idx_request_logs_success ON request_logs(success);
	`

	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// 添加 format 列（如果不存在）- 数据库迁移
	migration := `
	ALTER TABLE model_routes ADD COLUMN format TEXT DEFAULT 'openai';
	`
	// 忽略错误，因为列可能已经存在
	db.Exec(migration)

	// 添加 target_route_id 列（如果不存在）- 数据库迁移
	targetRouteMigration := `
	ALTER TABLE model_routes ADD COLUMN target_route_id INTEGER DEFAULT 0;
	`
	// 忽略错误，因为列可能已经存在
	db.Exec(targetRouteMigration)

	// 添加 forwarding_enabled 列（如果不存在）- 数据库迁移
	forwardingEnabledMigration := `
	ALTER TABLE model_routes ADD COLUMN forwarding_enabled INTEGER DEFAULT 0;
	`
	// 忽略错误，因为列可能已经存在
	db.Exec(forwardingEnabledMigration)

	// 添加唯一约束 (name, model) - 组合键
	uniqueConstraintMigration := `
	CREATE UNIQUE INDEX IF NOT EXISTS idx_model_routes_name_model ON model_routes(name, model);
	`
	db.Exec(uniqueConstraintMigration)

	// 添加 request_count 列（如果不存在）- 数据库迁移
	requestCountMigration := `
	ALTER TABLE request_logs ADD COLUMN request_count INTEGER DEFAULT 1;
	`
	// 忽略错误，因为列可能已经存在
	db.Exec(requestCountMigration)

	// 创建按小时聚合的唯一索引（如果不存在）
	hourlyIndexMigration := `
	CREATE UNIQUE INDEX IF NOT EXISTS idx_request_logs_model_hour ON request_logs(model, substr(created_at, 1, 13));
	`
	db.Exec(hourlyIndexMigration)

	return nil
}

// ClearAllRoutes 清空所有路由数据并重置自增ID
func ClearAllRoutes(db *sql.DB) error {
	// 先删除相关的请求日志
	_, err := db.Exec(`DELETE FROM request_logs`)
	if err != nil {
		return err
	}

	// 删除所有路由
	_, err = db.Exec(`DELETE FROM model_routes`)
	if err != nil {
		return err
	}

	// 重置自增ID
	_, err = db.Exec(`DELETE FROM sqlite_sequence WHERE name='model_routes'`)
	if err != nil {
		return err
	}

	return nil
}

// HasMultiModelRoutes 检测是否存在包含逗号分隔多模型的旧数据
func HasMultiModelRoutes(db *sql.DB) (bool, error) {
	query := `SELECT COUNT(*) FROM model_routes WHERE model LIKE '%,%'`
	var count int
	err := db.QueryRow(query).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
