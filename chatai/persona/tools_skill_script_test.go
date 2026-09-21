package persona

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/skill"
	"github.com/kohmebot/plugin/v2"
	"github.com/kohmebot/pyrunner/pyrunnersdk"
	"github.com/stretchr/testify/require"
)

type skillRunnerEnv struct {
	plugin.Env
	runner plugin.Plugin
}

func (e skillRunnerEnv) GetPlugin(name string) (plugin.Plugin, bool) {
	return e.runner, name == "pyrunner" && e.runner != nil
}

type skillRunnerPlugin struct {
	plugin.Plugin
	run func(context.Context, pyrunnersdk.Request) (pyrunnersdk.Result, error)
}

func (p skillRunnerPlugin) Run(ctx context.Context, req pyrunnersdk.Request) (pyrunnersdk.Result, error) {
	return p.run(ctx, req)
}

func TestRunSkillScriptSDK(t *testing.T) {
	d, err := skill.NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, d.Save("demo", "", "---\nname: demo\ndescription: demo\n---\nRun scripts/main.py", false))
	require.NoError(t, d.Save("demo", "scripts/main.py", "import helper", false))
	require.NoError(t, d.Save("demo", "scripts/helper.py", "value = 1", false))
	s := skill.NewService(nil, nil, skill.Options{Enabled: true})
	s.Directory = d
	p := &Persona{opts: Options{Skills: s}, tools: agent.NewRegistry()}
	p.registerBuiltinTools()
	require.Contains(t, searchNames(p.tools.Search("执行脚本 python", 5)), "run_skill_script")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, fail := range []bool{false, true} {
		var snapshot string
		p.env = skillRunnerEnv{runner: skillRunnerPlugin{run: func(runCtx context.Context, req pyrunnersdk.Request) (pyrunnersdk.Result, error) {
			snapshot = req.Path
			require.Equal(t, "scripts/main.py", req.EntryPoint)
			require.Equal(t, []string{"--value", "a b; c"}, req.Args)
			require.Empty(t, req.Script)
			data, err := os.ReadFile(filepath.Join(req.Path, "scripts/helper.py"))
			require.NoError(t, err)
			require.Equal(t, "value = 1", string(data))
			deadline, ok := runCtx.Deadline()
			require.True(t, ok)
			parent, _ := ctx.Deadline()
			require.Equal(t, parent, deadline)
			if fail {
				return pyrunnersdk.Result{Stdout: "partial", Stderr: "boom", ExitCode: 1}, errors.New("failed")
			}
			return pyrunnersdk.Result{Success: true, Stdout: "ok", SandboxBackend: "docker"}, nil
		}}}
		result, err := p.runSkillScriptTool().Handler(&agent.RunContext{Context: ctx}, json.RawMessage(`{"name":"demo","file":"scripts/main.py","args":["--value","a b; c"]}`))
		if fail {
			require.Error(t, err)
			require.Equal(t, "partial", result.(map[string]any)["stdout"])
		} else {
			require.NoError(t, err)
			require.Equal(t, "docker", result.(map[string]any)["sandbox_backend"])
		}
		_, err = os.Stat(snapshot)
		require.True(t, os.IsNotExist(err))
	}
	p.env = skillRunnerEnv{}
	_, err = p.runSkillScriptTool().Handler(nil, json.RawMessage(`{"name":"demo","file":"scripts/main.py"}`))
	require.ErrorContains(t, err, "enable the pyrunner")
	_, err = p.runSkillScriptTool().Handler(nil, json.RawMessage(`{"name":"demo","file":"scripts/main.py","timeout_seconds":0}`))
	require.ErrorContains(t, err, "timeout_seconds")

	// Cancellation of the Agent run must interrupt an in-flight SDK call,
	// preserve its partial diagnostics, and clean up its snapshot.
	runCtx, stop := context.WithCancel(context.Background())
	defer stop()
	var cancelledSnapshot string
	p.env = skillRunnerEnv{runner: skillRunnerPlugin{run: func(ctx context.Context, req pyrunnersdk.Request) (pyrunnersdk.Result, error) {
		cancelledSnapshot = req.Path
		stop()
		<-ctx.Done()
		return pyrunnersdk.Result{Stdout: "interrupted", ExitCode: -1}, ctx.Err()
	}}}
	result, err := p.runSkillScriptTool().Handler(&agent.RunContext{Context: runCtx}, json.RawMessage(`{"name":"demo","file":"scripts/main.py"}`))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "interrupted", result.(map[string]any)["stdout"])
	_, err = os.Stat(cancelledSnapshot)
	require.True(t, os.IsNotExist(err))
}
