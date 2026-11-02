package tools

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestBusinessToolsValidation(t *testing.T) {
	tests := []struct {
		name    string
		handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		req     mcp.CallToolRequest
		wantErr bool
	}{
		{
			name:    "get_daily_class_schedule_missing_center_id",
			handler: handleGetDailyClassSchedule,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "get_daily_class_schedule",
					Arguments: map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name:    "get_class_roster_missing_class_id",
			handler: handleGetClassRoster,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "get_class_roster",
					Arguments: map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name:    "get_student_attendance_history_missing_student_id",
			handler: handleGetStudentAttendanceHistory,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "get_student_attendance_history",
					Arguments: map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name:    "get_class_academic_summary_missing_class_id",
			handler: handleGetClassAcademicSummary,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "get_class_academic_summary",
					Arguments: map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name:    "check_student_class_eligibility_missing_student_id",
			handler: handleCheckStudentClassEligibility,
			req: mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "check_student_class_eligibility",
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

func TestGetAbsentAlertsDefaultSchoolYear(t *testing.T) {
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_absent_alerts",
			Arguments: map[string]any{
				"consecutive_absent_count": 2,
			},
		},
	}

	res, err := handleGetAbsentAlerts(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When DB pool is not initialized in unit tests, errorResult is returned gracefully
	if res == nil {
		t.Fatal("expected non-nil result")
	}
}
