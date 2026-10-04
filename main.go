package main

import (
	"log"

	"vietelite-mcp/go-server/config"
	"vietelite-mcp/go-server/db"
	"vietelite-mcp/go-server/tools"

	"github.com/mark3labs/mcp-go/server"
)

func main() {
	cfg := config.LoadConfig()

	if err := db.InitPools(cfg); err != nil {
		log.Fatalf("Failed to initialize database pools: %v", err)
	}

	s := server.NewMCPServer(
		"VietElite DB MCP Server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	tools.RegisterGenericTools(s)

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
