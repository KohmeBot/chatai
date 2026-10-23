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
			Definition: agent.Function("list_scheduled_tasks", "查看当前用户在本群的持久化任务和最近执行结果，每页最多50项", map[string]any{"offset": integerProperty("分页偏移，默认0")}),
			Namespace:  "schedule", ReadOnly: true, Risk: agent.ToolRiskLow,
			Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
				if rc == nil || rc.UserID <= 0 {
					return nil, errors.New("缺少发起用户")
				}
				if p.opts.Scheduler == nil {
					return nil, errors.New("定时任务未启用")
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
				return p.opts.Scheduler.List(p.groupID, rc.UserID, in.Offset)
			},
		},
		{
			Definition: agent.Function("delete_scheduled_task", "删除当前用户在本群的任务；已经开始的执行可能仍会完成。仅在用户要求取消任务时使用", map[string]any{"task_id": integerProperty("任务 ID")}, "task_id"),
			Namespace:  "schedule", Risk: agent.ToolRiskHigh,
			Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
				if rc == nil || rc.UserID <= 0 {
					return nil, errors.New("缺少发起用户")
				}
				if p.opts.Scheduler == nil {
					return nil, errors.New("定时任务未启用")
				}
				var in struct {
					ID uint `json:"task_id"`
				}
				if err := json.Unmarshal(raw, &in); err != nil {
					return nil, err
				}
				if in.ID == 0 {
					return nil, errors.New("无效任务 ID")
				}
				if err := p.opts.Scheduler.Delete(p.groupID, rc.UserID, in.ID); err != nil {
					return nil, err
				}
				return map[string]bool{"deleted": true}, nil
			},
		},
	}
}

func (p *Persona) handleScheduleTask(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	if p.opts.Scheduler == nil {
		return nil, errors.New("定时任务未启用")
	}
	var input schedule.Input
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	return p.opts.Scheduler.Create(p.groupID, rc.UserID, ctx.Event.SelfID, input)
}

// RunScheduled rebuilds an event using the current connection instead of retaining a message context.
func (p *Persona) RunScheduled(t schedule.Task) error {
	ctx := zero.GetBot(t.BotID)
	if ctx == nil {
		return errors.New("机器人连接已断开")
	}
	ctx.Event = &zero.Event{SelfID: t.BotID, GroupID: t.GroupID, UserID: t.UserID, PostType: "message", MessageType: "group", Time: time.Now().Unix()}
	ctx.State = zero.State{}
	return p.run(ctx, GroupMessage{User: User{UserId: t.UserID, Nickname: "定时任务"}, CreatedAt: time.Now(), MsgType: MsgTypeText}, t.Instruction)
}
