package agent

// ToolScope describes which conversations can use a tool.
type ToolScope string

const (
	ToolScopeGroup   ToolScope = "group"
	ToolScopePrivate ToolScope = "private"
	ToolScopeBoth    ToolScope = "both"
)

// Valid reports whether the scope is an explicit, supported value.
func (s ToolScope) Valid() bool {
	return s == ToolScopeGroup || s == ToolScopePrivate || s == ToolScopeBoth
}

func (s ToolScope) allows(scope ToolScope) bool {
	return scope == "" || scope.Valid() && (s == "" || s == ToolScopeBoth || s == scope)
}

// ToolScope uses the conversation type, including temporary private sessions.
func (r *RunContext) ToolScope() ToolScope {
	if r != nil && r.Private {
		return ToolScopePrivate
	}
	return ToolScopeGroup
}
