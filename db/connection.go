package db

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"vietelite_mcp_go/config"
)

var (
	pools   = make(map[string]*sql.DB)
	poolsMu sync.RWMutex

	validServices = map[string]bool{
		"identity": true,
		"class":    true,
		"finance":  true,
		"crm":      true,
	}
)

// InitPools creates and stores MySQL connection pools for the four database services.
// It is atomic and rollback-safe: if any connection fails to open, all partially opened
// pools are closed and the global state remains unchanged.
func InitPools(cfg config.DBConfig) error {
	tempPools := make(map[string]*sql.DB)

	services := map[string]string{
		"identity": cfg.IdentityDB,
		"class":    cfg.ClassDB,
		"finance":  cfg.FinanceDB,
		"crm":      cfg.CRMDB,
	}

	for service, dbName := range services {
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
			cfg.User, cfg.Password, cfg.Host, cfg.Port, dbName)

		db, err := sql.Open("mysql", dsn)
		if err != nil {
			for _, opened := range tempPools {
				_ = opened.Close()
			}
			return fmt.Errorf("failed to open pool for service %s: %w", service, err)
		}

		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)

		tempPools[service] = db
	}

	poolsMu.Lock()
	for _, oldPool := range pools {
		if oldPool != nil {
			_ = oldPool.Close()
		}
	}
	pools = tempPools
	poolsMu.Unlock()

	return nil
}

// GetPool returns the connection pool for the requested service name.
// Returns an error if the service name is not one of identity, class, finance, or crm,
// or if the pool has not been initialized.
func GetPool(service string) (*sql.DB, error) {
	if !validServices[service] {
		return nil, fmt.Errorf("unknown database service: %s", service)
	}

	poolsMu.RLock()
	defer poolsMu.RUnlock()

	pool, ok := pools[service]
	if !ok || pool == nil {
		return nil, fmt.Errorf("database pool not initialized for service: %s", service)
	}
	return pool, nil
}
