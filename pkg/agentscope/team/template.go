package team

import "strings"

// SubAgentTemplate defines a reusable blueprint for creating worker agents.
type SubAgentTemplate struct {
	Type                 string // routing key (e.g. "researcher", "coder")
	Description          string // agent-readable description
	SystemPromptTemplate string // format string with placeholders
}

// TemplateVars holds the values for substituting template placeholders.
type TemplateVars struct {
	TeamName          string
	TeamDescription   string
	MemberName        string
	MemberDescription string
	LeaderName        string
}

// Render substitutes placeholders in the template with the given values.
func (t *SubAgentTemplate) Render(vars TemplateVars) string {
	r := strings.NewReplacer(
		"{team_name}", vars.TeamName,
		"{team_description}", vars.TeamDescription,
		"{member_name}", vars.MemberName,
		"{member_description}", vars.MemberDescription,
		"{leader_name}", vars.LeaderName,
	)
	return r.Replace(t.SystemPromptTemplate)
}

// DefaultTemplate is the default sub-agent template.
var DefaultTemplate = SubAgentTemplate{
	Type:        "default",
	Description: "A general-purpose team member",
	SystemPromptTemplate: "You are {member_name}, a member of team '{team_name}' led by {leader_name}.\n\n" +
		"Team purpose: {team_description}\n\n" +
		"Your role: {member_description}\n\n" +
		"Communicate with the team leader through the team_say tool. " +
		"Focus on your assigned tasks and report results back to the leader.",
}
