package tools

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestRegisterPayrollTools(t *testing.T) {
	s := server.NewMCPServer("Test", "1.0.0")
	RegisterPayrollTools(s)
}

func TestPayrollToolsValidation(t *testing.T) {
	tests := []struct {
		name    string
		handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		req     mcp.CallToolRequest
		wantErr bool
	}{
		{
			name:    "get_my_payroll_missing_teacher_id",
			handler: handleGetMyPayroll,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "get_my_payroll",
					Arguments: map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name:    "get_teacher_payroll_detail_missing_payroll_id",
			handler: handleGetTeacherPayrollDetail,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "get_teacher_payroll_detail",
					Arguments: map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name:    "get_my_contract_missing_teacher_id",
			handler: handleGetMyContract,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "get_my_contract",
					Arguments: map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name:    "reconcile_payroll_vs_contract_missing_teacher_id",
			handler: handleReconcilePayrollVsContract,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "reconcile_payroll_vs_contract",
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
