package persona

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/plugin/v2"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
)

const agentRules = `
你是群聊中的一个 Agent。当前输入只包含本次触发事件，不包含历史记录。
你起初只会看到 search_tools。需要任何能力时，先用它搜索与当前任务有关的少量工具，再调用加载后的工具。
需要背景时再搜索并调用群聊上下文、用户上下文或按时间查询消息的工具；需要长期记忆时再调用印象工具。
遇到不认识的词、不了解的事情、无法确认的事实或可能变化的最新信息时，不要猜测：先用 search_tools 搜索“联网搜索”，再调用 search_web 求证；需要原文时再调用 browse_web。
每次执行都必须成功调用 send_message、send_messages、at_user 或 poke_user 至少一次，不能只在最终答案里写准备发送的内容，也不能静默结束。
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
	opts    Options
	tools   *agent.Registry

	mu sync.Mutex
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
	p := &Persona{groupID: groupID, env: env, db: db, opts: opts, tools: agent.NewRegistry()}
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
	if err := p.saveMessage(msg); err != nil {
		return err
	}
	if p.opts.RepeatEnable {
		repeat, err := p.shouldRepeat(msg, p.opts.RepeatCount)
		if err != nil {
			return err
		}
		if repeat {
			segments := ctx.Event.Message
			id := ctx.SendGroupMessage(p.groupID, segments)
			return p.recordBotMessage(ctx, segments, id)
		}
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
		if err != nil {
			logrus.Warnf("[Agent][group=%d user=%d][图片解析失败] %v", p.groupID, msg.User.UserId, err)
		} else if description != "" {
			prompt += "\n图片解析结果：" + description
		}
	}
	runner := agent.Runner{Model: p.opts.AgentModel, Tools: p.tools, MaxSteps: p.opts.MaxSteps, RequireAction: true}
	runCtx := &agent.RunContext{Context: context.Background(), GroupID: p.groupID, UserID: msg.User.UserId, Values: map[string]any{"zero_ctx": ctx, "persona": p}}
	_, runErr := runner.Run(runCtx, prompt, "")
	if runCtx.ActionPerformed() {
		return runErr
	}
	logrus.Warnf("[Agent][group=%d user=%d][兜底动作] 决策链未完成群聊动作，发送兜底消息；error=%v", p.groupID, msg.User.UserId, runErr)
	segments := message.Message{message.Text("……刚才走神了，再叫我一次吧。")}
	id := ctx.SendGroupMessage(p.groupID, segments)
	recordErr := p.recordBotMessage(ctx, segments, id)
	if runErr != nil {
		return runErr
	}
	return recordErr
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
