package agent

import "sort"

// Catalog returns serializable metadata from the actual registry, including discovery.
func (r *Registry) Catalog() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	disabled := r.preferences.snapshot()
	result := []map[string]any{{"definition": searchToolDefinition(), "namespace": "discovery", "scope": ToolScopeBoth, "read_only": true, "risk": ToolRiskLow}}
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t := r.tools[name]
		result = append(result, map[string]any{"disabled": disabled[name], "definition": t.Definition, "namespace": t.Namespace, "scope": t.Scope, "read_only": t.ReadOnly, "idempotent": t.Idempotent, "risk": t.Risk, "group_action": t.GroupAction})
	}
	return result
}
