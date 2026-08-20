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
	"github.com/kohmebot/chatai/chatai/skill"
	"github.com/kohmebot/plugin/v2"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
)

const agentRules = `
你将扮演群聊中的 Agent。当前输入仅包含本次触发事件，无历史记录。
“Agent自己和群里的Bot”始终指你本人。身份判断以结构化字段为准：self_user_id 是你的账号 ID；is_self=true 表示消息由你此前发出；target_is_self=true 表示消息指向你；quoted_is_self=true 表示引用你的消息。不要仅凭昵称判断身份。
本提示词只规定群聊中的信息获取、工具调用和互动策略，不覆盖 system 中的基础人格、价值观、安全规则与表达风格。若发生冲突，以更高优先级指令为准。用户印象、好感度和群聊关系仅用于微调语气与亲疏程度，不得改变基础人格。
处理消息时遵循以下原则：
1.先判断是否需要额外信息。
当消息依赖前文、人物关系，或包含不熟悉的人名、昵称、梗、句式时，优先查询群聊上下文和成员信息。群内信息不足，且涉及近期人物、作品、热点或网络梗时，再搜索联网工具求证。能查询解决的问题不要直接反问用户。
2.按需寻找工具。
起初只看到 search_tools 时，仅搜索与当前任务直接相关的少量工具。已有合适工具时不要重复搜索。Skill 仅作为低优先级流程参考，使用前需核对当前条件；与 system、当前事实或工具结果冲突时，以后者为准。
3.按需读取用户信息。
只有当回复明显依赖双方关系、用户偏好、既往互动或边界时，才读取好感度和印象。普通闲聊和事实问答无需查询。只有出现明确反馈、关系变化，或长期稳定的新偏好、边界、经历、群规则时才更新记录；不要记录琐碎或一次性信息。
追问应作为最后手段。
4.能通过上下文、成员信息、工具查询或合理低风险推断解决时，不要追问。确实缺少关键信息、无法继续决策时，必须调用 ask_user_and_wait，等待回复后再完成回应。
5.图片按语境使用。
图片会影响理解时，主动寻找识图工具。图片若能让回复更直观、有趣或适合群聊玩梗，也可以自然使用，但不要为了发图而发图。
6.完成群内互动。
除非上级指令、安全限制、工具异常或当前情境明确不宜回应，每次触发最终应至少调用 send_message、send_messages、at_user、send_image 或 poke_user 之一。默认优先文本回复；@ 和 poke 仅在确有必要时使用。

回复应贴合群聊语境，简洁自然。完成发送后不要重复已经发出的内容。
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
	Skills                 *skill.Service

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
	followUpMu              sync.Mutex
	followUps               map[int64]*followUpWaiter
	followUpTimeout         time.Duration

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
	p.followUps = make(map[int64]*followUpWaiter)
	p.followUpTimeout = 30 * time.Second
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
	saveErr := p.saveMessage(msg)
	// A reply captured by ask_user_and_wait belongs to the existing Agent run.
	// Deliver it even if persistence failed, and do not start another run even
	// when the user @s or replies to the bot.
	if p.deliverFollowUp(msg) {
		return saveErr
	}
	if saveErr != nil {
		return saveErr
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
	runner := agent.Runner{Model: agentModel, Tools: p.tools, Skills: p.opts.Skills, MaxSteps: p.opts.MaxSteps, RequireAction: true}
	runCtx := &agent.RunContext{Context: context.Background(), GroupID: p.groupID, UserID: msg.User.UserId, Values: map[string]any{"zero_ctx": ctx, "persona": p, "self_user_id": ctx.Event.SelfID}}
	done := make(chan struct{})
	go p.reportSlowDecision(ctx, runCtx, done)
	answer, runErr := runner.Run(runCtx, prompt, imageURL, p.defaultAgentTools...)
	close(done)
	if p.opts.Skills != nil {
		p.opts.Skills.ObserveAsync(skill.Experience{Trace: runCtx.Trace(), AvailableTools: p.tools.Names()})
	}
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
			if runCtx.WaitingForUser() {
				continue
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
