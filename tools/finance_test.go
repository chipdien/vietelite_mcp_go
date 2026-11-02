package tools

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestRegisterFinanceTools(t *testing.T) {
	s := server.NewMCPServer("Test", "1.0.0")
	RegisterFinanceTools(s)
}

func TestFinanceToolsValidation(t *testing.T) {
	tests := []struct {
		name    string
		handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		req     mcp.CallToolRequest
		wantErr bool
	}{
		{
			name:    "get_invoice_detail_missing_invoice_id",
			handler: handleGetInvoiceDetail,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "get_invoice_detail",
					Arguments: map[string]any{},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tt.handler(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr && !res.IsError {
				t.Errorf("expected IsError=true for invalid arguments")
			}
		})
	}
}
