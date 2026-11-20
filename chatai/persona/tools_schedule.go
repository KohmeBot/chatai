package persona

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/schedule"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func (p *Persona) scheduleTools() []agent.Tool {
	target := "本群"
	if p.private != nil {
		target = "本次私聊"
	}
	return []agent.Tool{
		{
			Definition: agent.Function("schedule_task", "创建持久化任务，重启后恢复。仅在用户明确要求提醒或定期执行时使用。一次性填写 run_at（含时区的 RFC3339）或 delay_seconds；周期填写五段 cron（分 时 日 月 星期）与 IANA timezone，默认 Asia/Shanghai。三种时间参数只能选一个。离线错过的触发合并执行一次，执行中断不自动重试。", map[string]any{
				"name":          stringProperty("简短任务名称"),
				"instruction":   stringProperty("到期时交给 Agent 的完整任务说明，包含用户要求和交付方式"),
				"run_at":        stringProperty("一次性执行时间，如 2026-12-01T09:00:00+08:00"),
				"delay_seconds": integerProperty("一次性延迟秒数"),
				"cron":          stringProperty("周期 cron，如 0 9 * * 1-5 表示工作日 9 点"),
				"timezone":      stringProperty("IANA 时区，如 Asia/Shanghai"),
			}, "instruction"),
			Namespace: "schedule", Risk: agent.ToolRiskHigh,
			SearchTerms: []string{"定时任务", "提醒", "稍后执行", "延迟", "周期", "cron"}, Handler: p.handleScheduleTask,
		},
		{
			Definition: agent.Function("list_scheduled_tasks", "查看当前用户在"+target+"的持久化任务和最近执行结果，每页最多50项", map[string]any{"offset": integerProperty("分页偏移，默认0")}),
			Namespace:  "schedule", ReadOnly: true, Risk: agent.ToolRiskLow,
			Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
				if err := p.checkScheduleActor(rc); err != nil {
					return nil, err
				}
				var in struct {
					Offset int `json:"offset"`
				}
				if err := json.Unmarshal(raw, &in); err != nil {
					return nil, err
				}
				if in.Offset < 0 || in.Offset > 1000000 {
					return nil, errors.New("无效分页参数")
				}
				if p.private != nil {
					return p.opts.Scheduler.ListPrivate(p.private.BotID, p.private.UserID, in.Offset)
				}
				return p.opts.Scheduler.List(p.groupID, rc.UserID, in.Offset)
			},
		},
		{
			Definition: agent.Function("delete_scheduled_task", "删除当前用户在"+target+"的任务；已经开始的执行可能仍会完成。仅在用户要求取消任务时使用", map[string]any{"task_id": integerProperty("任务 ID")}, "task_id"),
			Namespace:  "schedule", Risk: agent.ToolRiskHigh,
			Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
				if err := p.checkScheduleActor(rc); err != nil {
					return nil, err
				}
				var in struct {
					ID uint `json:"task_id"`
				}
				if err := json.Unmarshal(raw, &in); err != nil {
					return nil, err
				}
				var err error
				if p.private != nil {
					err = p.opts.Scheduler.DeletePrivate(p.private.BotID, p.private.UserID, in.ID)
				} else {
					err = p.opts.Scheduler.Delete(p.groupID, rc.UserID, in.ID)
				}
				if err != nil {
					return nil, err
				}
				return map[string]bool{"deleted": true}, nil
			},
		},
	}
}

func (p *Persona) checkScheduleActor(rc *agent.RunContext) error {
	if p.opts.Scheduler == nil {
		return errors.New("定时任务未启用")
	}
	if rc == nil || rc.UserID <= 0 {
		return errors.New("缺少发起用户")
	}
	if p.private != nil && (p.private.BotID <= 0 || rc.UserID != p.private.UserID) {
		return errors.New("发起用户与当前私聊不匹配")
	}
	return nil
}

func (p *Persona) handleScheduleTask(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	if err := p.checkScheduleActor(rc); err != nil {
		return nil, err
	}
	var input schedule.Input
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	if ctx.Event == nil {
		return nil, errors.New("缺少消息事件")
	}
	if p.private != nil {
		if ctx.Event.SelfID != p.private.BotID || ctx.Event.UserID != p.private.UserID || !zero.OnlyPrivate(ctx) {
			return nil, errors.New("消息事件与当前私聊不匹配")
		}
		return p.opts.Scheduler.CreatePrivate(p.private.UserID, p.private.BotID, input)
	}
	return p.opts.Scheduler.Create(p.groupID, rc.UserID, ctx.Event.SelfID, input)
}

// RunScheduled rebuilds an event using the current connection instead of retaining a message context.
func (p *Persona) RunScheduled(t schedule.Task) error {
	if t.GroupID != p.groupID || t.IsPrivate() != (p.private != nil) {
		return errors.New("定时任务与当前会话不匹配")
	}
	detail := "group"
	if p.private != nil {
		if t.BotID != p.private.BotID || t.UserID != p.private.UserID {
			return errors.New("定时任务与当前私聊不匹配")
		}
		detail = "private"
		p.privateRunMu.Lock()
		defer p.privateRunMu.Unlock()
	}
	ctx := zero.GetBot(t.BotID)
	if ctx == nil {
		return errors.New("机器人连接已断开")
	}
	now := time.Now()
	ctx.Event = &zero.Event{SelfID: t.BotID, GroupID: t.GroupID, UserID: t.UserID, PostType: "message", MessageType: detail, DetailType: detail, Time: now.Unix()}
	ctx.State = zero.State{}
	return p.run(ctx, GroupMessage{User: User{UserId: t.UserID, Nickname: "定时任务"}, CreatedAt: now, MsgType: MsgTypeText}, t.Instruction)
}
