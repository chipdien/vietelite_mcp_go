package config

import (
	"errors"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type DBConfig struct {
	Host       string
	Port       string
	User       string
	Password   string
	IdentityDB string
	ClassDB    string
	FinanceDB  string
	CRMDB      string
	AuthToken  string
	ServerPort int
	ServerHost string
}

func LoadConfig() DBConfig {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("Notice: could not load .env file: %v", err)
	}
	return DBConfig{
		Host:       getEnv("MYSQL_HOST", "127.0.0.1"),
		Port:       getEnv("MYSQL_PORT", "3306"),
		User:       getEnv("MYSQL_USER", "root"),
		Password:   getEnv("MYSQL_PASSWORD", ""),
		IdentityDB: getEnv("MYSQL_IDENTITY_DB", "identity_service"),
		ClassDB:    getEnv("MYSQL_CLASS_DB", "class_service"),
		FinanceDB:  getEnv("MYSQL_FINANCE_DB", "finance_service"),
		CRMDB:      getEnv("MYSQL_CRM_DB", "crm_service"),
		AuthToken:  getEnv("MCP_AUTH_TOKEN", ""),
		ServerPort: getEnvInt("MCP_PORT", 8080),
		ServerHost: getEnv("MCP_HOST", "0.0.0.0"),
	}
}

func getEnv(key string, defaultVal string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 65536 {
			return n
		}
		log.Printf("Notice: invalid %s=%q, using %d", key, v, defaultVal)
	}
	return defaultVal
}
