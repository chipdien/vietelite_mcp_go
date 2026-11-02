package tools

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestRegisterAggregateTools(t *testing.T) {
	s := server.NewMCPServer("Test", "1.0.0")
	RegisterAggregateTools(s)
}

func TestAggregateToolsValidation(t *testing.T) {
	// get_center_dashboard requires center_id
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "get_center_dashboard",
			Arguments: map[string]any{},
		},
	}
	res, err := handleGetCenterDashboard(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError=true when center_id is missing")
	}

	// get_network_overview with empty params
	netReq := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "get_network_overview",
			Arguments: map[string]any{},
		},
	}
	netRes, err := handleGetNetworkOverview(context.Background(), netReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if netRes == nil {
		t.Fatalf("expected non-nil result")
	}
}
