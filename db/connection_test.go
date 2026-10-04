package db

import (
	"testing"

	"vietelite-mcp/go-server/config"
)

func TestGetPool_UnknownService(t *testing.T) {
	pool, err := GetPool("unknown_service")
	if err == nil {
		t.Errorf("expected error for unknown service, got nil pool: %v", pool)
	}
}

func TestInitPools_Success(t *testing.T) {
	cfg := config.DBConfig{
		Host:       "127.0.0.1",
		Port:       "3306",
		User:       "root",
		Password:   "password",
		IdentityDB: "identity_db",
		ClassDB:    "class_db",
		FinanceDB:  "finance_db",
		CRMDB:      "crm_db",
	}

	err := InitPools(cfg)
	if err != nil {
		t.Fatalf("unexpected error initializing pools: %v", err)
	}

	services := []string{"identity", "class", "finance", "crm"}
	for _, svc := range services {
		pool, err := GetPool(svc)
		if err != nil {
			t.Errorf("expected pool for service %q, got error: %v", svc, err)
		}
		if pool == nil {
			t.Errorf("expected non-nil pool for service %q", svc)
		}
	}
}
