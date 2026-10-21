package agent

import "sort"

// Catalog returns serializable metadata from the actual registry, including discovery.
func (r *Registry) Catalog() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := []map[string]any{{"definition": searchToolDefinition(), "namespace": "discovery", "read_only": true, "risk": ToolRiskLow}}
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t := r.tools[name]
		result = append(result, map[string]any{"definition": t.Definition, "namespace": t.Namespace, "read_only": t.ReadOnly, "idempotent": t.Idempotent, "risk": t.Risk, "group_action": t.GroupAction})
	}
	return result
}
