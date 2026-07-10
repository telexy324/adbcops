package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"aiops-platform/backend/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	maxOpenConnections    = 25
	maxIdleConnections    = 5
	connectionMaxLifetime = 30 * time.Minute
	connectionMaxIdleTime = 5 * time.Minute
)

// Connection exposes both GORM and the underlying SQL connection pool.
type Connection struct {
	GORM *gorm.DB
	SQL  *sql.DB
}

// Open creates a PostgreSQL connection pool and verifies connectivity.
func Open(ctx context.Context, cfg config.DatabaseConfig) (*Connection, error) {
	gormDB, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL driver: %w", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("get PostgreSQL connection pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConnections)
	sqlDB.SetMaxIdleConns(maxIdleConnections)
	sqlDB.SetConnMaxLifetime(connectionMaxLifetime)
	sqlDB.SetConnMaxIdleTime(connectionMaxIdleTime)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("verify PostgreSQL connection: %w", err)
	}

	return &Connection{GORM: gormDB, SQL: sqlDB}, nil
}

// Close releases the connection pool.
func (c *Connection) Close() error {
	if c == nil || c.SQL == nil {
		return nil
	}
	return c.SQL.Close()
}
