package main

import (
	"flag"
	"fmt"
	"log"

	"vietelite-mcp/go-server/config"
	"vietelite-mcp/go-server/db"
	"vietelite-mcp/go-server/logger"
	"vietelite-mcp/go-server/tools"

	"github.com/mark3labs/mcp-go/server"
)

func main() {
	transport := flag.String("transport", "stdio", "Transport mode: stdio or sse")
	port := flag.Int("port", 8080, "HTTP port for SSE transport")
	flag.Parse()

	if err := logger.InitLogger("logs/mcp.log"); err != nil {
		log.Printf("Warning: failed to initialize log file: %v", err)
	}
	logger.Info("SERVER_START", "Initializing VietElite DB MCP Server...")

	cfg := config.LoadConfig()

	if err := db.InitPools(cfg); err != nil {
		logger.Error("DB_INIT", "Failed to initialize database pools: %v", err)
		log.Fatalf("Failed to initialize database pools: %v", err)
	}
	logger.Info("DB_INIT", "Successfully connected to MySQL database pools")

	s := server.NewMCPServer(
		"VietElite DB MCP Server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	tools.RegisterGenericTools(s)
	tools.RegisterBusinessTools(s)

	if *transport == "sse" {
		sseServer := server.NewSSEServer(s)
		addr := fmt.Sprintf(":%d", *port)
		logger.Info("SERVER_READY", "MCP SSE Server listening on http://0.0.0.0:%d/sse...", *port)
		log.Printf("MCP SSE Server listening on http://0.0.0.0:%d/sse", *port)
		if err := sseServer.Start(addr); err != nil {
			logger.Error("SERVER_ERROR", "SSE Server error: %v", err)
			log.Fatalf("SSE Server error: %v", err)
		}
	} else {
		logger.Info("SERVER_READY", "MCP Server listening on stdio transport...")
		if err := server.ServeStdio(s); err != nil {
			logger.Error("SERVER_ERROR", "Server runtime error: %v", err)
			log.Fatalf("Server error: %v", err)
		}
	}
}
