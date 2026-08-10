// Package exception is deprecated. Use package errors instead.
//
// All types in this package are aliases to their equivalents in
// github.com/alanfokco/agentscope-go/v2/pkg/agentscope/errors.
// New code should import the errors package directly. This package
// will be removed in a future major version.
package exception

import (
	agenterrors "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/errors"
)

// Deprecated: Use errors.AgentErrorI.
type AgentError = agenterrors.AgentErrorI

// Deprecated: Use errors.DeveloperError.
type DeveloperError = agenterrors.DeveloperError

// Deprecated: Use errors.ToolNotFoundError.
type ToolNotFoundError = agenterrors.ToolNotFoundError

// Deprecated: Use errors.ToolInterruptedError.
type ToolInterruptedError = agenterrors.ToolInterruptedError

// Deprecated: Use errors.ToolJSONDecodeError.
type ToolJSONDecodeError = agenterrors.ToolJSONDecodeError

// Deprecated: Use errors.ToolGroupInactiveError.
type ToolGroupInactiveError = agenterrors.ToolGroupInactiveError

// Deprecated: Use errors.ToolExecutionError.
type ToolExecutionError = agenterrors.ToolExecutionError

// Deprecated: Use errors.ToolImplError.
type ToolImplError = agenterrors.ToolImplError

// Deprecated: Use errors.IsAgentError.
var IsAgentError = agenterrors.IsAgentError

// Deprecated: Use errors.GetAgentMessage.
var GetAgentMessage = agenterrors.GetAgentMessage
