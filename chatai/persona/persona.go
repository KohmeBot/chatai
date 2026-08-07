package persona

import (
	"context"
	"fmt"
	"strings"
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
收到事件后的第一步是判断是否需要群聊上下文、该用户的上下文或按时间查询消息；需要就立即搜索并调用相应工具。能从上下文查明的信息不要反问用户“说了什么”或要求用户重复。需要长期记忆时再调用印象工具。
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
	ProgressAfter   time.Duration
	ProgressTips    []string
	WebSearchPrefer time.Duration
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

	mu                      sync.Mutex
	repeatMu                sync.Mutex
	repeatWindow            []GroupMessage
	repeatTriggered         bool
	searchMu                sync.Mutex
	preferredSearchProvider string
	preferredSearchUntil    time.Time
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
	if opts.ProgressAfter <= 0 {
		opts.ProgressAfter = 15 * time.Second
	}
	validProgressTips := make([]string, 0, len(opts.ProgressTips))
	for _, tip := range opts.ProgressTips {
		if tip = strings.TrimSpace(tip); tip != "" {
			validProgressTips = append(validProgressTips, tip)
		}
	}
	if len(validProgressTips) == 0 {
		opts.ProgressTips = []string{"正在处理，请稍等一下。", "还在处理中，很快就好。", "正在整理结果，请稍候。"}
	} else {
		opts.ProgressTips = validProgressTips
	}
	if opts.WebSearchPrefer <= 0 {
		opts.WebSearchPrefer = time.Hour
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
		if p.shouldRepeat(msg, p.opts.RepeatCount) {
			segments := repeatMessage(ctx.Event.Message)
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
	agentModel, imageURL := p.modelForMessage(msg)
	runner := agent.Runner{Model: agentModel, Tools: p.tools, MaxSteps: p.opts.MaxSteps, RequireAction: true}
	runCtx := &agent.RunContext{Context: context.Background(), GroupID: p.groupID, UserID: msg.User.UserId, Values: map[string]any{"zero_ctx": ctx, "persona": p}}
	done := make(chan struct{})
	go p.reportSlowDecision(ctx, runCtx, done)
	answer, runErr := runner.Run(runCtx, prompt, imageURL)
	close(done)
	if runCtx.ActionPerformed() {
		return runErr
	}
	finalDecision := strings.TrimSpace(answer)
	if finalDecision == "" && runErr != nil {
		latest := conciseProgress(runCtx.LatestDecision())
		if latest != "" && !strings.HasPrefix(latest, "准备执行：") {
			finalDecision = "决策步数已到上限。基于目前已有信息，我的判断是：" + latest
		}
	}
	if finalDecision != "" {
		logrus.Warnf("[Agent][group=%d user=%d][最终文本直发] 模型未调用群聊动作，直接发送最后决策；error=%v", p.groupID, msg.User.UserId, runErr)
		segments := message.Message{message.Text(finalDecision)}
		id := ctx.SendGroupMessage(p.groupID, segments)
		return p.recordBotMessage(ctx, segments, id)
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

func (p *Persona) modelForMessage(msg GroupMessage) (model.LargeModel, string) {
	if msg.Url != "" && p.opts.VisionModel != nil {
		// 不再先把图片压缩成一段描述再交给文本 Agent。视觉模型直接接收
		// 完整触发事件和原图，并继续参与后续工具调用轮次。
		return p.opts.VisionModel, msg.Url
	}
	return p.opts.AgentModel, ""
}

func (p *Persona) reportSlowDecision(ctx *zero.Ctx, runCtx *agent.RunContext, done <-chan struct{}) {
	ticker := time.NewTicker(p.opts.ProgressAfter)
	defer ticker.Stop()
	tipIndex := 0
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			select {
			case <-done:
				return
			default:
			}
			if runCtx.ActionPerformed() {
				return
			}
			tip := p.opts.ProgressTips[tipIndex%len(p.opts.ProgressTips)]
			tipIndex++
			segments := message.Message{message.At(runCtx.UserID), message.Text(" " + tip)}
			id := ctx.SendGroupMessage(p.groupID, segments)
			if err := p.recordBotMessage(ctx, segments, id); err != nil {
				logrus.Warnf("[Agent][group=%d user=%d][进度消息记录失败] %v", p.groupID, runCtx.UserID, err)
			}
		}
	}
}

func conciseProgress(raw string) string {
	raw = strings.Join(strings.Fields(raw), " ")
	const maxRunes = 160
	runes := []rune(raw)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "……"
	}
	return raw
}

func (p *Persona) eventPrompt(msg GroupMessage, scheduled string) string {
	if scheduled != "" {
		return fmt.Sprintf("定时任务到期。群号：%d。任务内容：%s", p.groupID, scheduled)
	}
	return fmt.Sprintf("群号：%d\n当前触发事件：\n%s", p.groupID, formatMessage(msg))
}
