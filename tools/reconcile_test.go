package tools

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestRegisterReconcileTools(t *testing.T) {
	s := server.NewMCPServer("Test", "1.0.0")
	RegisterReconcileTools(s)
}

func TestReconcileToolsExecution(t *testing.T) {
	// Reconcile tools have optional filters, so empty args should execute cleanly without crash
	s := server.NewMCPServer("Test", "1.0.0")
	RegisterReconcileTools(s)

	req1 := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "reconcile_invoice_vs_receipts",
			Arguments: map[string]any{},
		},
	}
	res1, err := handleReconcileInvoiceVsReceipts(context.Background(), req1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res1 == nil {
		t.Fatalf("expected non-nil result")
	}

	req2 := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "reconcile_bank_vs_receipts",
			Arguments: map[string]any{},
		},
	}
	res2, err := handleReconcileBankVsReceipts(context.Background(), req2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2 == nil {
		t.Fatalf("expected non-nil result")
	}
}
