package persona

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/stretchr/testify/require"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func TestWorkFilesPersistAndRejectEscape(t *testing.T) {
	p := &Persona{opts: Options{WorkDirectory: t.TempDir()}}
	_, err := p.handleWriteWorkFile(nil, json.RawMessage(`{"path":"reports/a.bin","content":"AAEC/w==","encoding":"base64"}`))
	require.NoError(t, err)
	result, err := p.handleReadWorkFile(nil, json.RawMessage(`{"path":"reports/a.bin","encoding":"base64"}`))
	require.NoError(t, err)
	require.Equal(t, "AAEC/w==", result.(map[string]any)["content"])
	_, err = p.handleWriteWorkFile(nil, json.RawMessage(`{"path":"reports/a.bin","content":"replacement"}`))
	require.Error(t, err)
	_, err = p.handleWriteWorkFile(nil, json.RawMessage(`{"path":"../escape","content":"bad"}`))
	require.Error(t, err)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(p.opts.WorkDirectory, "link")); err == nil {
		_, err = p.handleWriteWorkFile(nil, json.RawMessage(`{"path":"link/escape","content":"bad"}`))
		require.Error(t, err)
	}
}

func TestWorkCommandCreatesDeliverableAndReportsFailure(t *testing.T) {
	p := &Persona{opts: Options{WorkDirectory: t.TempDir(), WorkCommandEnable: true}}
	command := "printf hello > output.txt"
	if runtime.GOOS == "windows" {
		command = "Set-Content -LiteralPath output.txt -Value hello"
	}
	raw, err := json.Marshal(map[string]any{"command": command})
	require.NoError(t, err)
	result, err := p.handleExecuteCommand(nil, raw)
	require.NoError(t, err)
	require.Equal(t, 0, result.(map[string]any)["exit_code"])
	data, err := os.ReadFile(filepath.Join(p.opts.WorkDirectory, "output.txt"))
	require.NoError(t, err)
	require.Contains(t, string(data), "hello")
	result, err = p.handleExecuteCommand(nil, json.RawMessage(`{"command":"exit 7"}`))
	require.Error(t, err)
	require.Equal(t, 7, result.(map[string]any)["exit_code"])
	p.opts.WorkCommandEnable = false
	_, err = p.handleExecuteCommand(nil, raw)
	require.Error(t, err)
}

func TestWorkToolRegistrationAndFileUpload(t *testing.T) {
	p := &Persona{groupID: 123, opts: Options{WorkDirectory: t.TempDir()}, tools: agent.NewRegistry()}
	p.registerBuiltinTools()
	require.Contains(t, p.ToolNames(), "send_file")
	require.NotContains(t, p.ToolNames(), "execute_command")
	require.Contains(t, searchNames(p.tools.Search("生成文件", 5)), "write_work_file")
	require.NoError(t, os.WriteFile(filepath.Join(p.opts.WorkDirectory, "report.txt"), []byte("report"), 0600))
	caller := &deliveryTestCaller{response: &zero.APIResponse{Status: "ok"}}
	zero.APICallers.Store(987654329, caller)
	t.Cleanup(func() { zero.APICallers.Delete(987654329) })
	rc := &agent.RunContext{Values: map[string]any{"zero_ctx": zero.GetBot(987654329)}}
	_, err := p.handleSendFile(rc, json.RawMessage(`{"path":"report.txt"}`))
	require.NoError(t, err)
	require.Equal(t, "upload_group_file", caller.requests[0].Action)
	require.Equal(t, int64(123), caller.requests[0].Params["group_id"])
	caller.response = &zero.APIResponse{Status: "failed", RetCode: 100}
	_, err = p.handleSendFile(rc, json.RawMessage(`{"path":"report.txt"}`))
	require.Error(t, err)
}

func TestWorkOutputIsBounded(t *testing.T) {
	b := new(workOutput)
	n, err := b.Write(make([]byte, 100000))
	require.NoError(t, err)
	require.Equal(t, 100000, n)
	require.Len(t, b.data, 64<<10)
	require.True(t, b.truncated)
}
