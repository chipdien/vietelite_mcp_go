package tools

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestRegisterAcademicTools(t *testing.T) {
	s := server.NewMCPServer("Test", "1.0.0")
	RegisterAcademicTools(s)
}

func TestAcademicToolsValidation(t *testing.T) {
	tests := []struct {
		name    string
		handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		req     mcp.CallToolRequest
		wantErr bool
	}{
		{
			name:    "get_class_detail_missing_class_id",
			handler: handleGetClassDetail,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "get_class_detail",
					Arguments: map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name:    "get_session_detail_missing_session_id",
			handler: handleGetSessionDetail,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "get_session_detail",
					Arguments: map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name:    "list_students_at_risk_missing_class_id",
			handler: handleListStudentsAtRisk,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "list_students_at_risk",
					Arguments: map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name:    "list_ungraded_homework_missing_class_id",
			handler: handleListUngradedHomework,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "list_ungraded_homework",
					Arguments: map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name:    "get_homework_submissions_missing_session_id",
			handler: handleGetHomeworkSubmissions,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "get_homework_submissions",
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

func TestFetchStudentProfilesEmpty(t *testing.T) {
	profiles := fetchStudentProfiles(context.Background(), nil)
	if len(profiles) != 0 {
		t.Errorf("expected empty map for nil student IDs")
	}

	profilesZero := fetchStudentProfiles(context.Background(), []int{0, -1})
	if len(profilesZero) != 0 {
		t.Errorf("expected empty map for invalid student IDs")
	}
}
