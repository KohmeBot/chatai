package persona

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
)

const workFileLimit = 4 << 20

func (p *Persona) workTools() []agent.Tool {
	if p.opts.WorkDirectory == "" {
		return nil
	}
	path := map[string]any{"path": stringProperty("相对当前群工作目录的路径，不允许越界")}
	tools := []agent.Tool{
		{Definition: agent.Function("work_info", "查看持久工作目录绝对路径、操作系统和命令执行是否可用；生成的文件保留，可用 send_file 交付", nil), Namespace: "work", ReadOnly: true, Idempotent: true, Risk: agent.ToolRiskLow, SearchTerms: []string{"工作目录", "环境", "生成文件", "workspace"}, Handler: func(_ *agent.RunContext, _ json.RawMessage) (any, error) {
			return map[string]any{"directory": p.opts.WorkDirectory, "os": runtime.GOOS, "command_enabled": p.opts.WorkCommandEnable}, nil
		}},
		{Definition: agent.Function("list_work_files", "列出工作目录下指定文件夹的直接子项，最多200项；默认根目录", path), Namespace: "work", ReadOnly: true, Idempotent: true, Risk: agent.ToolRiskLow, SearchTerms: []string{"目录", "文件", "list files"}, Handler: p.handleListWorkFiles},
		{Definition: agent.Function("read_work_file", "读取工作目录文件，最多4MiB；encoding 默认 utf8，二进制用 base64", map[string]any{"path": path["path"], "encoding": stringProperty("utf8 或 base64")}, "path"), Namespace: "work", ReadOnly: true, Idempotent: true, Risk: agent.ToolRiskLow, SearchTerms: []string{"读取文件", "read file"}, Handler: p.handleReadWorkFile},
		{Definition: agent.Function("write_work_file", "持久保存文件并创建父目录，最多4MiB；默认拒绝覆盖，覆盖已有文件必须显式 overwrite=true；二进制可用 base64", map[string]any{"path": path["path"], "content": stringProperty("文件内容"), "encoding": stringProperty("utf8 或 base64"), "overwrite": map[string]any{"type": "boolean"}}, "path", "content"), Namespace: "work", Risk: agent.ToolRiskMedium, SearchTerms: []string{"保存文件", "生成文件", "写文件", "write file"}, Handler: p.handleWriteWorkFile},
		{Definition: agent.Function("send_file", "将工作目录内文件上传到当前群文件；成功上传即完成文件交付，不要重复发送。OneBot 服务必须能访问同一个绝对路径", map[string]any{"path": path["path"], "name": stringProperty("可选的展示文件名，默认原文件名")}, "path"), Namespace: "chat", GroupAction: true, Risk: agent.ToolRiskMedium, SearchTerms: []string{"发送文件", "上传文件", "交付文件", "send file"}, Handler: p.handleSendFile},
	}
	if p.opts.WorkCommandEnable {
		tools = append(tools, agent.Tool{Definition: agent.Function("execute_command", "以宿主机器人进程权限执行命令（非沙箱）；默认在持久工作目录运行。Windows 使用 PowerShell，其他系统使用 /bin/sh。仅执行用户任务需要的操作，生成文件保存在工作目录并用 send_file 交付。默认60秒，最多180秒且受本轮时间限制；返回退出码、输出、超时和截断状态", map[string]any{"command": stringProperty("shell 命令"), "directory": stringProperty("工作目录内相对子目录，默认 ."), "timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 180}}, "command"), Namespace: "work", Risk: agent.ToolRiskHigh, SearchTerms: []string{"执行命令", "运行脚本", "生成文件", "shell", "python"}, Handler: p.handleExecuteCommand})
	}
	return tools
}

func (p *Persona) openWorkRoot() (*os.Root, error) {
	if p.opts.WorkDirectory == "" {
		return nil, fmt.Errorf("work tools disabled")
	}
	return os.OpenRoot(p.opts.WorkDirectory)
}

func workPath(path string) (string, error) {
	if path == "" {
		path = "."
	}
	if !filepath.IsLocal(path) {
		return "", fmt.Errorf("path must be relative to the work directory")
	}
	return filepath.Clean(path), nil
}

func (p *Persona) handleListWorkFiles(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	var in struct{ Path string }
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	path, err := workPath(in.Path)
	if err != nil {
		return nil, err
	}
	root, err := p.openWorkRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(201)
	if err != nil && err != io.EOF {
		return nil, err
	}
	truncated := len(entries) > 200
	if truncated {
		entries = entries[:200]
	}
	items := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		items = append(items, map[string]any{"name": e.Name(), "directory": e.IsDir(), "path": filepath.Join(path, e.Name())})
	}
	return map[string]any{"entries": items, "truncated": truncated}, nil
}

func (p *Persona) handleReadWorkFile(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	var in struct{ Path, Encoding string }
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	path, err := workPath(in.Path)
	if err != nil {
		return nil, err
	}
	root, err := p.openWorkRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, workFileLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > workFileLimit {
		return nil, fmt.Errorf("file exceeds 4MiB")
	}
	content := string(data)
	switch in.Encoding {
	case "", "utf8":
	case "base64":
		content = base64.StdEncoding.EncodeToString(data)
	default:
		return nil, fmt.Errorf("encoding must be utf8 or base64")
	}
	return map[string]any{"path": path, "content": content, "size": len(data)}, nil
}

func (p *Persona) handleWriteWorkFile(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	var in struct {
		Path, Content, Encoding string
		Overwrite               bool
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	path, err := workPath(in.Path)
	if err != nil {
		return nil, err
	}
	data := []byte(in.Content)
	switch in.Encoding {
	case "", "utf8":
	case "base64":
		data, err = base64.StdEncoding.DecodeString(in.Content)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("encoding must be utf8 or base64")
	}
	if len(data) > workFileLimit {
		return nil, fmt.Errorf("file exceeds 4MiB")
	}
	root, err := p.openWorkRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err = root.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if in.Overwrite {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := root.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return map[string]any{"path": path, "size": len(data)}, nil
}

// Resolve symlinks for APIs requiring an absolute pathname. This is not a
// command sandbox; concurrent host processes can still modify the filesystem.
func (p *Persona) absoluteWorkPath(path string) (string, error) {
	rel, err := workPath(path)
	if err != nil {
		return "", err
	}
	base, err := filepath.EvalSymlinks(p.opts.WorkDirectory)
	if err != nil {
		return "", err
	}
	abs, err := filepath.EvalSymlinks(filepath.Join(base, rel))
	if err != nil {
		return "", err
	}
	within, err := filepath.Rel(base, abs)
	if err != nil || !filepath.IsLocal(within) {
		return "", fmt.Errorf("path escapes work directory")
	}
	return abs, nil
}

func (p *Persona) handleSendFile(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var in struct{ Path, Name string }
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	path, err := p.absoluteWorkPath(in.Path)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if in.Name == "" {
		in.Name = filepath.Base(path)
	}
	if strings.ContainsAny(in.Name, "/\\\x00") || in.Name == "." || in.Name == ".." {
		return nil, fmt.Errorf("name must be a filename")
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	result := ctx.UploadGroupFile(p.groupID, path, in.Name, "")
	if result.Status != "ok" || result.RetCode != 0 {
		return nil, fmt.Errorf("file upload failed: status=%s retcode=%d", result.Status, result.RetCode)
	}
	return map[string]any{"uploaded": true, "name": in.Name, "size": stat.Size()}, nil
}

type workOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *workOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(data)
	remaining := (64 << 10) - len(b.data)
	if len(data) > remaining {
		data = data[:remaining]
		b.truncated = true
	}
	b.data = append(b.data, data...)
	return n, nil
}

func (p *Persona) handleExecuteCommand(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	if !p.opts.WorkCommandEnable {
		return nil, fmt.Errorf("command execution disabled")
	}
	var in struct {
		Command, Directory string
		Timeout            int `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Command) == "" {
		return nil, fmt.Errorf("command is required")
	}
	if in.Timeout == 0 {
		in.Timeout = 60
	}
	if in.Timeout < 1 || in.Timeout > 180 {
		return nil, fmt.Errorf("timeout_seconds must be 1..180")
	}
	dir, err := p.absoluteWorkPath(in.Directory)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(skillContext(rc), time.Duration(in.Timeout)*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", in.Command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", in.Command)
	}
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	output := new(workOutput)
	cmd.Stdout = output
	cmd.Stderr = output
	start := time.Now()
	err = cmd.Run()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	result := map[string]any{"success": err == nil, "exit_code": code, "output": string(output.data), "output_truncated": output.truncated, "timed_out": ctx.Err() == context.DeadlineExceeded, "duration_ms": time.Since(start).Milliseconds()}
	return result, err
}
