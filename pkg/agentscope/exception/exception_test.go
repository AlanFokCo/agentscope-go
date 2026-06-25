package exception

import (
	"fmt"
	"testing"
)

func TestAgentErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		isAgent bool
	}{
		{
			"ToolNotFoundError",
			&ToolNotFoundError{ToolName: "bash"},
			true,
		},
		{
			"ToolInterruptedError",
			&ToolInterruptedError{ToolName: "bash", Reason: "user canceled"},
			true,
		},
		{
			"ToolJSONDecodeError",
			&ToolJSONDecodeError{ToolName: "grep", Input: "{bad", Err: fmt.Errorf("unexpected end")},
			true,
		},
		{
			"ToolGroupInactiveError",
			&ToolGroupInactiveError{ToolName: "edit", GroupName: "advanced"},
			true,
		},
		{
			"ToolExecutionError",
			&ToolExecutionError{ToolName: "bash", Err: fmt.Errorf("exit status 1")},
			true,
		},
		{
			"plain error",
			fmt.Errorf("something broke"),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAgentError(tt.err); got != tt.isAgent {
				t.Errorf("IsAgentError() = %v, want %v", got, tt.isAgent)
			}
			msg := GetAgentMessage(tt.err)
			if msg == "" {
				t.Error("GetAgentMessage returned empty string")
			}
		})
	}
}

func TestDeveloperError(t *testing.T) {
	err := &ToolImplError{ToolName: "read", Detail: "nil schema"}
	var de DeveloperError = err
	_ = de

	if IsAgentError(err) {
		t.Error("ToolImplError should not be an AgentError")
	}
}
