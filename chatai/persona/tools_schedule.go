package persona

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
)

func (p *Persona) scheduleTools() []agent.Tool {
	return []agent.Tool{
		{
			Definition: agent.Function("schedule_task", "创建一次性定时任务，到期后由 Agent 再次决定如何执行", map[string]any{
				"delay_seconds": integerProperty("延迟秒数"),
				"instruction":   stringProperty("到期时交给 Agent 的任务说明"),
			}, "delay_seconds", "instruction"),
			SearchTerms: []string{"定时任务", "提醒", "稍后执行", "延迟"},
			Handler:     p.handleScheduleTask,
		},
	}
}

func (p *Persona) handleScheduleTask(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		Delay       int    `json:"delay_seconds"`
		Instruction string `json:"instruction"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if input.Delay <= 0 || input.Delay > p.opts.ScheduleMaxSec {
		return nil, fmt.Errorf("delay_seconds must be between 1 and %d", p.opts.ScheduleMaxSec)
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	taskID := strconv.FormatInt(time.Now().UnixNano(), 36)
	time.AfterFunc(time.Duration(input.Delay)*time.Second, func() {
		_ = p.run(ctx, GroupMessage{User: User{UserId: rc.UserID, Nickname: "定时任务"}, CreatedAt: time.Now(), MsgType: MsgTypeText}, input.Instruction)
	})
	return map[string]any{"task_id": taskID, "run_at": time.Now().Add(time.Duration(input.Delay) * time.Second)}, nil
}
