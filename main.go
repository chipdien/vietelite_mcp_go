package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"strconv"

	"vietelite-mcp/go-server/config"
	"vietelite-mcp/go-server/db"
	"vietelite-mcp/go-server/logger"
	"vietelite-mcp/go-server/middleware"
	"vietelite-mcp/go-server/tools"

	"github.com/mark3labs/mcp-go/server"
)

func main() {
	transport := flag.String("transport", "stdio", "Transport mode: stdio or sse")
	host := flag.String("host", "", "Bind address for SSE transport (default: $MCP_HOST or 0.0.0.0)")
	port := flag.Int("port", 0, "HTTP port for SSE transport (default: $MCP_PORT or 8080)")
	flag.Parse()

	if err := logger.InitLogger("logs/mcp.log"); err != nil {
		log.Printf("Warning: failed to initialize log file: %v", err)
	}
	logger.Info("SERVER_START", "Initializing VietElite DB MCP Server...")

	cfg := config.LoadConfig()
	if *host == "" {
		*host = cfg.ServerHost
	}
	if *port == 0 {
		*port = cfg.ServerPort
	}

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

		authHandler := middleware.AuthMiddleware(cfg.AuthToken)(sseServer)

		mux := http.NewServeMux()
		mux.Handle("/sse", authHandler)
		mux.Handle("/message", authHandler)

		addr := net.JoinHostPort(*host, strconv.Itoa(*port))
		if cfg.AuthToken != "" {
			logger.Info("SERVER_READY", "MCP SSE Server with auth protection listening on http://%s/sse...", addr)
			log.Printf("MCP SSE Server with auth protection listening on http://%s/sse", addr)
		} else {
			logger.Info("SERVER_READY", "MCP SSE Server listening on http://%s/sse...", addr)
			log.Printf("MCP SSE Server listening on http://%s/sse", addr)
		}

		if err := http.ListenAndServe(addr, mux); err != nil {
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
