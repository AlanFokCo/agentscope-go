package exception

import "fmt"

// AgentError is an error that should be shown to the LLM as a tool result
// message. The model can potentially recover from these by retrying or
// choosing a different approach.
type AgentError interface {
	error
	AgentMessage() string
}

// DeveloperError is an error that indicates a bug in the tool implementation.
// These should be propagated to the developer, not shown to the LLM.
type DeveloperError interface {
	error
	IsDeveloperError()
}

// IsAgentError returns true if err implements AgentError.
func IsAgentError(err error) bool {
	_, ok := err.(AgentError)
	return ok
}

// GetAgentMessage returns the agent-facing message if err is an AgentError,
// or err.Error() otherwise.
func GetAgentMessage(err error) string {
	if ae, ok := err.(AgentError); ok {
		return ae.AgentMessage()
	}
	return err.Error()
}

// ToolNotFoundError indicates the requested tool does not exist or is not active.
type ToolNotFoundError struct {
	ToolName string
}

func (e *ToolNotFoundError) Error() string {
	return fmt.Sprintf("tool %q not found or not active", e.ToolName)
}

func (e *ToolNotFoundError) AgentMessage() string {
	return fmt.Sprintf("Tool %q is not available. Use a different tool or check the tool name.", e.ToolName)
}

// ToolInterruptedError indicates the tool execution was interrupted by the user.
type ToolInterruptedError struct {
	ToolName string
	Reason   string
}

func (e *ToolInterruptedError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("tool %q interrupted: %s", e.ToolName, e.Reason)
	}
	return fmt.Sprintf("tool %q was interrupted by the user", e.ToolName)
}

func (e *ToolInterruptedError) AgentMessage() string {
	if e.Reason != "" {
		return fmt.Sprintf("Tool %q was interrupted: %s. Try a different approach.", e.ToolName, e.Reason)
	}
	return fmt.Sprintf("Tool %q was interrupted by the user. Try a different approach.", e.ToolName)
}

// ToolJSONDecodeError indicates the tool call arguments could not be parsed as JSON.
type ToolJSONDecodeError struct {
	ToolName string
	Input    string
	Err      error
}

func (e *ToolJSONDecodeError) Error() string {
	return fmt.Sprintf("tool %q: JSON decode error: %v", e.ToolName, e.Err)
}

func (e *ToolJSONDecodeError) AgentMessage() string {
	return fmt.Sprintf("Failed to parse arguments for tool %q. Ensure the input is valid JSON. Error: %v", e.ToolName, e.Err)
}

func (e *ToolJSONDecodeError) Unwrap() error { return e.Err }

// ToolGroupInactiveError indicates the tool belongs to an inactive tool group.
type ToolGroupInactiveError struct {
	ToolName  string
	GroupName string
}

func (e *ToolGroupInactiveError) Error() string {
	return fmt.Sprintf("tool %q belongs to inactive group %q", e.ToolName, e.GroupName)
}

func (e *ToolGroupInactiveError) AgentMessage() string {
	return fmt.Sprintf("Tool %q is in group %q which is not active. Activate the group first using reset_tools.", e.ToolName, e.GroupName)
}

// ToolExecutionError wraps an error from tool execution that the LLM can see.
type ToolExecutionError struct {
	ToolName string
	Err      error
}

func (e *ToolExecutionError) Error() string {
	return fmt.Sprintf("tool %q execution failed: %v", e.ToolName, e.Err)
}

func (e *ToolExecutionError) AgentMessage() string {
	return fmt.Sprintf("Tool %q failed: %v", e.ToolName, e.Err)
}

func (e *ToolExecutionError) Unwrap() error { return e.Err }

// ToolImplError indicates a bug in the tool implementation (developer error).
type ToolImplError struct {
	ToolName string
	Detail   string
}

func (e *ToolImplError) Error() string {
	return fmt.Sprintf("tool %q implementation error: %s", e.ToolName, e.Detail)
}

func (e *ToolImplError) IsDeveloperError() {}
