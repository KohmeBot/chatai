package persona

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/plugin/v2"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
)

const agentRules = `
你是群聊中的一个 Agent。当前输入只包含本次触发事件，不包含历史记录。
需要背景时再调用 read_group_context 或 read_user_context；需要长期记忆时再调用印象工具。
需要回复时必须调用 send_message、at_user 或 poke_user，不能只在最终答案里写准备发送的内容。
不要为了“了解情况”无条件读取全部工具，只读取完成当前请求真正需要的信息。
发送完成后用简短最终答案结束，不要重复发送。
`

func AgentRules() string { return agentRules }

type Options struct {
	AgentModel      model.LargeModel
	VisionModel     model.LargeModel
	ImpressionModel model.LargeModel
	MaxSteps        int
	ContextLimit    int
	WebMaxBytes     int
	ScheduleMaxSec  int
	RepeatEnable    bool
	RepeatCount     int
	ImpressionEvery time.Duration
	ImpressionMin   int
	ExtraTools      []agent.Tool
}

type Persona struct {
	groupID int64
	env     plugin.Env
	db      *gorm.DB
	gc      groupContext
	opts    Options
	tools   *agent.Registry

	mu             sync.Mutex
	lastImpression time.Time
}

func NewPersona(groupID int64, env plugin.Env, db *gorm.DB, opts Options) *Persona {
	if opts.ContextLimit <= 0 {
		opts.ContextLimit = 30
	}
	if opts.WebMaxBytes <= 0 {
		opts.WebMaxBytes = 512 * 1024
	}
	if opts.ScheduleMaxSec <= 0 {
		opts.ScheduleMaxSec = 86400
	}
	if opts.ImpressionMin <= 0 {
		opts.ImpressionMin = 20
	}
	p := &Persona{groupID: groupID, env: env, db: db, opts: opts, tools: agent.NewRegistry(), lastImpression: time.Now()}
	p.registerBuiltinTools()
	for _, tool := range opts.ExtraTools {
		_ = p.tools.Register(tool)
	}
	if opts.ImpressionModel != nil && opts.ImpressionEvery > 0 {
		go p.impressionLoop()
	}
	return p
}

func (p *Persona) RegisterTool(tool agent.Tool) error { return p.tools.Register(tool) }

// UpdateContext 始终记录消息，但只有明确 @/回复机器人或戳机器人时才启动 Agent。
func (p *Persona) UpdateContext(ctx *zero.Ctx) error {
	msg := newMessage(ctx)
	if msg.IsEmpty() {
		return nil
	}
	p.gc.AppendMsg(msg, 0)
	if p.opts.RepeatEnable && p.gc.ShouldRepeat(msg, p.opts.RepeatCount) {
		segments := ctx.Event.Message
		id := ctx.SendGroupMessage(p.groupID, segments)
		p.recordBotMessage(ctx, segments, id)
		return nil
	}
	triggered := ctx.Event.IsToMe || (msg.MsgType == MsgTypePoke && msg.TargetUser.UserId == ctx.Event.SelfID)
	if !triggered {
		return nil
	}
	go func() {
		if err := p.run(ctx, msg, ""); err != nil {
			p.env.Error(ctx, err)
		}
	}()
	return nil
}

func (p *Persona) run(ctx *zero.Ctx, msg GroupMessage, scheduledInstruction string) error {
	prompt := p.eventPrompt(msg, scheduledInstruction)
	if msg.Url != "" && p.opts.VisionModel != nil {
		description, err := p.describeImage(msg.Url)
		if err == nil && description != "" {
			prompt += "\n图片解析结果：" + description
		}
	}
	runner := agent.Runner{Model: p.opts.AgentModel, Tools: p.tools, MaxSteps: p.opts.MaxSteps}
	runCtx := &agent.RunContext{Context: context.Background(), GroupID: p.groupID, UserID: msg.User.UserId, Values: map[string]any{"zero_ctx": ctx, "persona": p}}
	_, err := runner.Run(runCtx, prompt, "")
	return err
}

func (p *Persona) eventPrompt(msg GroupMessage, scheduled string) string {
	if scheduled != "" {
		return fmt.Sprintf("定时任务到期。群号：%d。任务内容：%s", p.groupID, scheduled)
	}
	return fmt.Sprintf("群号：%d\n当前事件类型：%s\n发起人：%s\n目标：%s\n消息ID：%d\n完整文本：%s", p.groupID, MsgTypeString(msg.MsgType), msg.User.String(), msg.TargetUser.String(), msg.MsgID, msg.Content)
}

func (p *Persona) describeImage(imageURL string) (string, error) {
	res := new(model.Response)
	err := p.opts.VisionModel.Request(&model.Request{Question: "准确描述图片内容；如果是表情包，说明文字和表达的情绪。", ImageURL: imageURL}, res)
	if err != nil {
		return "", err
	}
	if res.ErrorMsg != "" {
		return "", errors.New(res.ErrorMsg)
	}
	return res.Answer, nil
}

func (p *Persona) registerBuiltinTools() {
	integer := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	stringProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	register := func(t agent.Tool) { _ = p.tools.Register(t) }

	register(agent.Tool{Definition: agent.Function("read_group_context", "按需读取当前群最近的聊天上下文", map[string]any{"limit": integer("返回条数，默认使用配置值")}), Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
		var in struct {
			Limit int `json:"limit"`
		}
		_ = json.Unmarshal(raw, &in)
		if in.Limit <= 0 || in.Limit > p.opts.ContextLimit {
			in.Limit = p.opts.ContextLimit
		}
		return formatMessages(p.gc.Snapshot(in.Limit)), nil
	}})
	register(agent.Tool{Definition: agent.Function("read_user_context", "按需读取某个用户在当前群最近的发言", map[string]any{"user_id": integer("用户 QQ 号"), "limit": integer("返回条数")}, "user_id"), Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
		var in struct {
			UserID int64 `json:"user_id"`
			Limit  int   `json:"limit"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if in.Limit <= 0 || in.Limit > p.opts.ContextLimit {
			in.Limit = p.opts.ContextLimit
		}
		return formatMessages(p.gc.UserSnapshot(in.UserID, in.Limit)), nil
	}})
	register(agent.Tool{Definition: agent.Function("read_user_impression", "读取对某个用户的长期印象", map[string]any{"user_id": integer("用户 QQ 号")}, "user_id"), Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
		var in struct {
			UserID int64 `json:"user_id"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		return new(UserImpression).Get(p.db, in.UserID)
	}})
	register(agent.Tool{Definition: agent.Function("read_group_impression", "读取对当前群的长期印象", map[string]any{}), Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
		return new(GroupImpression).Get(p.db, p.groupID)
	}})
	register(agent.Tool{Definition: agent.Function("send_message", "向当前群发送一句话，可选择引用消息", map[string]any{"text": stringProp("要发送的话"), "reply_message_id": integer("可选，引用的消息 ID")}, "text"), Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
		var in struct {
			Text    string `json:"text"`
			ReplyID int64  `json:"reply_message_id"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		ctx, err := zeroContext(rc)
		if err != nil {
			return nil, err
		}
		segments := message.Message{}
		if in.ReplyID > 0 {
			segments = append(segments, message.Reply(in.ReplyID))
		}
		segments = append(segments, message.Text(in.Text))
		id := ctx.SendGroupMessage(p.groupID, segments)
		p.recordBotMessage(ctx, segments, id)
		return map[string]any{"message_id": id}, nil
	}})
	register(agent.Tool{Definition: agent.Function("at_user", "在当前群 @ 某人并发送文字", map[string]any{"user_id": integer("用户 QQ 号"), "text": stringProp("要说的话")}, "user_id", "text"), Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
		var in struct {
			UserID int64  `json:"user_id"`
			Text   string `json:"text"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		ctx, err := zeroContext(rc)
		if err != nil {
			return nil, err
		}
		segments := message.Message{message.At(in.UserID), message.Text(" " + in.Text)}
		id := ctx.SendGroupMessage(p.groupID, segments)
		p.recordBotMessage(ctx, segments, id)
		return map[string]any{"message_id": id}, nil
	}})
	register(agent.Tool{Definition: agent.Function("poke_user", "在当前群戳一戳某人", map[string]any{"user_id": integer("用户 QQ 号")}, "user_id"), Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
		var in struct {
			UserID int64 `json:"user_id"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		ctx, err := zeroContext(rc)
		if err != nil {
			return nil, err
		}
		ctx.CallAction("send_poke", zero.Params{"group_id": p.groupID, "user_id": in.UserID})
		return "ok", nil
	}})
	register(agent.Tool{Definition: agent.Function("schedule_task", "创建一次性定时任务，到期后由 Agent 再次决定如何执行", map[string]any{"delay_seconds": integer("延迟秒数"), "instruction": stringProp("到期时交给 Agent 的任务说明")}, "delay_seconds", "instruction"), Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
		var in struct {
			Delay       int    `json:"delay_seconds"`
			Instruction string `json:"instruction"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if in.Delay <= 0 || in.Delay > p.opts.ScheduleMaxSec {
			return nil, fmt.Errorf("delay_seconds must be between 1 and %d", p.opts.ScheduleMaxSec)
		}
		ctx, err := zeroContext(rc)
		if err != nil {
			return nil, err
		}
		taskID := strconv.FormatInt(time.Now().UnixNano(), 36)
		time.AfterFunc(time.Duration(in.Delay)*time.Second, func() {
			_ = p.run(ctx, GroupMessage{User: User{UserId: rc.UserID, Nickname: "定时任务"}, CreatedAt: time.Now(), MsgType: MsgTypeText}, in.Instruction)
		})
		return map[string]any{"task_id": taskID, "run_at": time.Now().Add(time.Duration(in.Delay) * time.Second)}, nil
	}})
	register(agent.Tool{Definition: agent.Function("browse_web", "读取公开网页正文；仅在确实需要外部资料时调用", map[string]any{"url": stringProp("http 或 https 网页地址")}, "url"), Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
		var in struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		return p.readWeb(rc, in.URL)
	}})
}

func zeroContext(rc *agent.RunContext) (*zero.Ctx, error) {
	ctx, ok := rc.Values["zero_ctx"].(*zero.Ctx)
	if !ok || ctx == nil {
		return nil, errors.New("zero context unavailable")
	}
	return ctx, nil
}

func (p *Persona) recordBotMessage(ctx *zero.Ctx, segments message.Message, id int64) {
	p.gc.AppendMsg(GroupMessage{User: User{UserId: ctx.Event.SelfID, Nickname: "你"}, Content: segments.ExtractPlainText(), MsgType: getMsgType(segments), MsgID: id, CreatedAt: time.Now()}, 0)
}

func (p *Persona) readWeb(rc *agent.RunContext, rawURL string) (string, error) {
	u, err := validatePublicURL(rc, rawURL)
	if err != nil {
		return "", err
	}
	req, _ := http.NewRequestWithContext(rc, http.MethodGet, u.String(), nil)
	req.Header.Set("User-Agent", "kohme-chatai-agent/1.0")
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		_, err := validatePublicURL(rc, req.URL.String())
		return err
	}}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("web returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(p.opts.WebMaxBytes)))
	if err != nil {
		return "", err
	}
	text := regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<[^>]+>`).ReplaceAllString(string(body), " ")
	return strings.Join(strings.Fields(html.UnescapeString(text)), " "), nil
}

func validatePublicURL(ctx context.Context, rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("invalid http(s) URL")
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return nil, err
	}
	for _, addr := range addresses {
		if addr.IP.IsLoopback() || addr.IP.IsPrivate() || addr.IP.IsUnspecified() || addr.IP.IsLinkLocalUnicast() {
			return nil, errors.New("private or local addresses are not allowed")
		}
	}
	return u, nil
}
