package persona

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/pyrunner/pyrunnersdk"
)

func skillContext(rc *agent.RunContext) context.Context {
	if rc != nil && rc.Context != nil {
		return rc.Context
	}
	return context.Background()
}

func (p *Persona) listSkillFilesTool() agent.Tool {
	return agent.Tool{
		Definition: agent.Function("list_skill_files", "列出一个 Skill 内的相对文件路径和大小，包括 scripts、references、assets；不读取正文", map[string]any{"name": stringProperty("Skill 文件夹名")}, "name"),
		Namespace:  "skill", ReadOnly: true, Idempotent: true, Risk: agent.ToolRiskLow,
		SearchTerms: []string{"技能文件", "目录", "脚本", "skill files", "scripts"},
		Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
			var in struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, err
			}
			return p.opts.Skills.Directory.Files(skillContext(rc), in.Name)
		},
	}
}

func (p *Persona) runSkillScriptTool() agent.Tool {
	return agent.Tool{
		Definition: agent.Function("run_skill_script", "通过 pyrunner 沙箱运行目录 Skill 的 Python 脚本。先读取 SKILL.md 和相关脚本。file 必须是该 Skill 内的 .py 相对路径；工作目录为整个 Skill 的临时副本，支持辅助模块、资源及根目录依赖清单。返回输出、退出码、超时与沙箱信息；运行产生的文件不持久保存。默认60秒，最多180秒且受本轮剩余时间限制", map[string]any{
			"name":            stringProperty("Skill 文件夹名"),
			"file":            stringProperty("Python 入口相对路径，例如 scripts/report.py"),
			"args":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "传给脚本的命令行参数，不经过 shell"},
			"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 180, "description": "执行总超时（包含依赖安装），默认60秒"},
		}, "name", "file"),
		Namespace: "skill", Risk: agent.ToolRiskHigh,
		SearchTerms: []string{"执行脚本", "运行脚本", "python", "py", "沙箱", "skill", "scripts"},
		Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
			var in struct {
				Name    string   `json:"name"`
				File    string   `json:"file"`
				Args    []string `json:"args"`
				Timeout *int     `json:"timeout_seconds"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, err
			}
			seconds := 60
			if in.Timeout != nil {
				seconds = *in.Timeout
			}
			if seconds < 1 || seconds > 180 {
				return nil, fmt.Errorf("timeout_seconds must be 1..180")
			}
			if p.env == nil {
				return nil, fmt.Errorf("pyrunner unavailable: plugin environment missing")
			}
			// Resolve on invocation, after plugins have booted; initialization order
			// must not prevent chatai's other skill tools from working.
			runner, err := pyrunnersdk.NewInvoker(p.env)
			if err != nil {
				return nil, fmt.Errorf("enable the pyrunner plugin to execute skill scripts: %w", err)
			}
			ctx, cancel := context.WithTimeout(skillContext(rc), time.Duration(seconds)*time.Second)
			defer cancel()
			dir, cleanup, err := p.opts.Skills.Directory.Snapshot(ctx, in.Name, in.File)
			if err != nil {
				return nil, err
			}
			defer cleanup()
			result, err := runner.Run(ctx, pyrunnersdk.Request{Path: dir, EntryPoint: in.File, Args: in.Args})
			payload := map[string]any{
				"name": in.Name, "file": in.File, "success": result.Success,
				"stdout": result.Stdout, "stderr": result.Stderr, "output": result.Output,
				"exit_code": result.ExitCode, "timed_out": result.TimedOut,
				"output_truncated": result.OutputTruncated, "duration_ms": result.Duration.Milliseconds(),
				"environment": result.Environment, "sandbox_backend": result.SandboxBackend,
				"dependencies_installed": result.DependenciesInstalled,
			}
			if err == nil && !result.Success {
				err = fmt.Errorf("skill script failed (exit_code=%d, timed_out=%t)", result.ExitCode, result.TimedOut)
			}
			return payload, err
		},
	}
}
