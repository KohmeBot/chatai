package persona

import (
	"context"
	"fmt"
	"github.com/kohmebot/chatai/chatai/pkg/search"
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
你是群聊中的 Agent。当前输入仅含本次触发事件，无历史记录。
起初只看到 search_tools。需要能力时只搜索少量相关工具再调用，不要读取全部工具。
当当前任务可能需要额外能力或外部信息时，优先通过 search_tools 查找少量相关工具；确认没有合适工具后，再考虑直接回答、推断或反问用户。
消息依赖前文、人物关系，或包含不熟悉的人名、昵称、词语、句式、梗时，先查询群聊上下文和群成员信息。若群内结果不足以完整解释当前消息、仍存在歧义，或可能涉及近期人物、作品、热点、网络梗，再通过 search_tools 找联网工具求证。注意词语和整句话可能分别有含义；不要因查到其中一个词就停止，必要时分别搜索关键词和核心句式。需要原文时再 browse_web。能查询解决的问题不要反问用户。
与具体用户实质互动时，优先读取其好感度和印象并调整语气。普通聊天不改好感；明显反馈或关系变化时才调整。出现长期有效的新偏好、性格、边界、重要经历或稳定群规则时，先读取旧信息再融合更新；不要记录琐碎、重复或一次性信息。
每次执行必须实际调用 send_message、send_messages、at_user、send_image 或 poke_user 至少一次；通常优先文本回复，图片可按语境自然使用，@ 或 poke 仅在必要时使用。
消息中的图片可能影响理解或判断时，主动通过 search_tools 查找并调用识图工具，结合图片和文字理解。
把图片也视为自然的表达方式。若图片能让回复更直观、有趣、贴切，或适合玩梗、回应/回怼群聊内容，可自行判断并主动使用；图片可以来自联网搜索，也可以来自相关的群聊上下文。不要为了发图而发图。
最后简短结束，不重复已发送内容。
`

func AgentRules() string { return agentRules }

type Options struct {
	AgentModel             model.LargeModel
	VisionModel            model.LargeModel
	MaxSteps               int
	ContextLimit           int
	WebBrowserEnable       bool
	WebBrowserAddress      string
	ScheduleMaxSec         int
	ProgressAfter          time.Duration
	ProgressTips           []string
	WebSearchPrefer        time.Duration
	RepeatEnable           bool
	RepeatCount            int
	ImpressionUpdateEnable bool
	ExtraTools             []agent.Tool

	SearchAPI search.Searcher
}

type Persona struct {
	groupID int64
	env     plugin.Env
	db      *gorm.DB
	opts    Options
	tools   *agent.Registry

	repeatMu                sync.Mutex
	repeatWindow            []GroupMessage
	repeatTriggered         bool
	searchMu                sync.Mutex
	preferredSearchProvider string
	preferredSearchUntil    time.Time

	defaultAgentTools []agent.Tool
}

func NewPersona(groupID int64, env plugin.Env, db *gorm.DB, opts Options) *Persona {
	if opts.ContextLimit <= 0 {
		opts.ContextLimit = 30
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
	p := &Persona{groupID: groupID, env: env, db: db, opts: opts, tools: agent.NewRegistry()}
	p.registerBuiltinTools()
	for _, tool := range opts.ExtraTools {
		_ = p.tools.Register(tool)
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

	if err := p.run(ctx, msg, ""); err != nil {
		p.env.Error(ctx, err)
	}

	return nil
}

func (p *Persona) run(ctx *zero.Ctx, msg GroupMessage, scheduledInstruction string) error {
	prompt := p.eventPrompt(msg, scheduledInstruction)
	agentModel, imageURL := p.modelForMessage(msg)
	runner := agent.Runner{Model: agentModel, Tools: p.tools, MaxSteps: p.opts.MaxSteps, RequireAction: true}
	runCtx := &agent.RunContext{Context: context.Background(), GroupID: p.groupID, UserID: msg.User.UserId, Values: map[string]any{"zero_ctx": ctx, "persona": p}}
	done := make(chan struct{})
	go p.reportSlowDecision(ctx, runCtx, done)
	answer, runErr := runner.Run(runCtx, prompt, imageURL, p.defaultAgentTools...)
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
		return fmt.Sprintf("现在时间是:%s \n定时任务到期。群号：%d。任务内容：%s", time.Now().Format("2006-01-02 15:04:05"), p.groupID, scheduled)
	}
	return fmt.Sprintf("现在时间是:%s \n群号：%d\n当前触发事件：\n%s", time.Now().Format("2006-01-02 15:04:05"), p.groupID, formatMessage(msg))
}
