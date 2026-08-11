package persona

import (
	"fmt"

	"github.com/kohmebot/chatai/chatai/agent"
)

// registerBuiltinTools 汇总各领域的内建工具。工具定义和处理逻辑放在对应领域文件中。
func (p *Persona) registerBuiltinTools() {
	groups := [][]agent.Tool{
		p.groupMemberTools(),
		p.contextTools(),
		p.imageTools(),
		p.memoryTools(),
		p.followUpTools(),
		p.groupActionTools(),
		p.scheduleTools(),
		p.webTools(),
	}
	for _, tools := range groups {
		for _, tool := range tools {
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
