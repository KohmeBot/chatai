package persona

import (
	"encoding/json"
	"fmt"

	"github.com/kohmebot/chatai/chatai/agent"
)

func (p *Persona) builtinTools() [][]agent.Tool {
	shared := [][]agent.Tool{
		p.workTools(),
		p.imageTools(),
		p.skillTools(),
		p.chatActionTools(),
		p.mediaTools(),
		p.rawMessageTools(),
		p.groupMemberTools(),
		p.contextTools(),
		p.scheduleTools(),
		p.webTools(),
	}
	if p.private != nil {
		return append(shared,
			p.privateContextTools(),
			p.privateProfileTools(),
		)
	}
	groupOnly := [][]agent.Tool{
		p.groupMemoryTools(),
	}
	for _, tools := range groupOnly {
		for i := range tools {
			tools[i].Scope = agent.ToolScopeGroup
		}
	}
	return append(append(shared, p.structuredMemoryTools()), groupOnly...)
}

// RegisterToolCatalog lists the built-in capabilities of both conversation modes.
// The registry is for administration and is never used to execute tools.
func RegisterToolCatalog(catalog *agent.Registry, opts Options) {
	seen := make(map[string]bool)
	for _, template := range []*Persona{{opts: opts}, {opts: opts, private: &PrivateSession{}}} {
		for _, tools := range template.builtinTools() {
			for _, tool := range tools {
				name := tool.Definition.Function.Name
				if seen[name] {
					continue
				}
				seen[name] = true
				if err := catalog.Register(tool); err != nil {
					panic(fmt.Sprintf("register builtin tool %q: %v", name, err))
				}
			}
		}
	}
}

// registerBuiltinTools binds tool handlers to the current conversation.
func (p *Persona) registerBuiltinTools() {
	for _, tools := range p.builtinTools() {
		for _, tool := range tools {
			if tool.Namespace == "skill" && p.opts.Skills != nil {
				handler := tool.Handler
				tool.Handler = func(ctx *agent.RunContext, raw json.RawMessage) (any, error) {
					var input struct {
						Name string `json:"name"`
					}
					if err := json.Unmarshal(raw, &input); err != nil {
						return nil, err
					}
					if input.Name != "" {
						if err := p.opts.Skills.CheckEnabled(input.Name); err != nil {
							return nil, err
						}
					}
					return handler(ctx, raw)
				}
			}
			if err := p.tools.Register(tool); err != nil {
				panic(fmt.Sprintf("register builtin tool %q: %v", tool.Definition.Function.Name, err))
			}
		}
	}
}

func integerProperty(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
