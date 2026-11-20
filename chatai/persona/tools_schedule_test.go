package persona

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/schedule"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func TestScheduleToolsPersistAndScopeToActor(t *testing.T) {
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(schedule.Models()...))
	s := schedule.New(db)
	p := &Persona{groupID: 10, opts: Options{Scheduler: s}}
	rc := &agent.RunContext{Context: context.Background(), GroupID: 10, UserID: 20, Values: map[string]any{"zero_ctx": &zero.Ctx{Event: &zero.Event{SelfID: 30}}}}
	result, err := p.handleScheduleTask(rc, []byte(`{"instruction":"提醒喝水","delay_seconds":60}`))
	require.NoError(t, err)
	task := result.(*schedule.Task)
	require.Equal(t, int64(10), task.GroupID)
	require.Equal(t, int64(20), task.UserID)
	require.Equal(t, int64(30), task.BotID)
	tools := p.scheduleTools()
	rc.UserID = 21
	_, err = tools[2].Handler(rc, []byte(`{"task_id":1}`))
	require.Error(t, err)
	items, err := tools[1].Handler(rc, []byte(`{}`))
	require.NoError(t, err)
	require.Empty(t, items)
	rc.UserID = 20
	_, err = tools[2].Handler(rc, []byte(`{"task_id":1}`))
	require.NoError(t, err)
}

func TestPrivateScheduleToolsAreScopedToSession(t *testing.T) {
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(schedule.Models()...))
	s := schedule.New(db)
	const user, bot = int64(20), int64(30)
	p := NewPrivatePersona(PrivateSession{BotID: bot, UserID: user}, nil, db, Options{Scheduler: s})
	require.Contains(t, p.ToolNames(), "schedule_task")
	require.Contains(t, p.ToolNames(), "list_scheduled_tasks")
	require.Contains(t, p.ToolNames(), "delete_scheduled_task")
	in := schedule.Input{Instruction: "提醒喝水", DelaySeconds: 60}
	otherGroup, err := s.Create(10, user, bot, in)
	require.NoError(t, err)
	otherBot, err := s.CreatePrivate(user, bot+1, in)
	require.NoError(t, err)
	otherUser, err := s.CreatePrivate(user+1, bot, in)
	require.NoError(t, err)
	rc := &agent.RunContext{Context: context.Background(), UserID: user, Values: map[string]any{
		"zero_ctx": &zero.Ctx{Event: &zero.Event{SelfID: bot, UserID: user, GroupID: 10, PostType: "message", DetailType: "private"}},
	}}
	tools := p.scheduleTools()
	result, err := tools[0].Handler(rc, json.RawMessage(`{"instruction":"私聊提醒喝水","delay_seconds":60}`))
	require.NoError(t, err)
	task := result.(*schedule.Task)
	require.True(t, task.IsPrivate())
	require.Equal(t, user, task.UserID)
	require.Equal(t, bot, task.BotID)
	items, err := tools[1].Handler(rc, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, task.ID, items.([]schedule.Task)[0].ID)
	for _, id := range []uint{otherGroup.ID, otherBot.ID, otherUser.ID} {
		_, err := tools[2].Handler(rc, json.RawMessage(fmt.Sprintf(`{"task_id":%d}`, id)))
		require.Error(t, err)
	}
	require.Error(t, s.Delete(10, user, task.ID))
	_, err = tools[2].Handler(rc, json.RawMessage(fmt.Sprintf(`{"task_id":%d}`, task.ID)))
	require.NoError(t, err)
}

func TestRunScheduledDeliversToPrivateSession(t *testing.T) {
	const bot, user = int64(987654336), int64(20)
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&PrivateChatMessageRecord{}, &PrivateUserMemory{}))
	caller := &deliveryTestCaller{response: &zero.APIResponse{Status: "ok", Data: gjson.Parse(`{"message_id":42}`)}}
	zero.APICallers.Store(bot, caller)
	t.Cleanup(func() { zero.APICallers.Delete(bot) })
	llm := &scheduledReplyTestModel{}
	p := NewPrivatePersona(PrivateSession{BotID: bot, UserID: user}, nil, db, Options{AgentModel: llm, MaxSteps: 2, ProgressAfter: time.Hour})
	task := schedule.Task{BotID: bot, UserID: user, Instruction: "提醒喝水"}
	wrongUser := task
	wrongUser.UserID++
	require.Error(t, p.RunScheduled(wrongUser))
	require.NoError(t, p.RunScheduled(task))
	require.Len(t, caller.requests, 1)
	require.Equal(t, "send_private_msg", caller.requests[0].Action)
	require.EqualValues(t, user, caller.requests[0].Params["user_id"])
	require.NotNil(t, llm.event)
	require.Equal(t, "scheduled_task_due", llm.event.Kind)
	require.NotNil(t, llm.event.ScheduledTask)
	require.Equal(t, task.Instruction, llm.event.ScheduledTask.Instruction)
	message, err := p.privateMessageByID(42)
	require.NoError(t, err)
	require.Equal(t, bot, message.User.UserId)
}

type scheduledReplyTestModel struct{ event *agentEventEnvelope }

func (m *scheduledReplyTestModel) Request(req *model.Request, res *model.Response) error {
	if m.event == nil {
		prompt, _, _ := strings.Cut(req.Question, "\n\n")
		m.event = new(agentEventEnvelope)
		if err := json.Unmarshal([]byte(prompt), m.event); err != nil {
			return err
		}
		res.ToolCalls = []model.ToolCall{{ID: "search", Type: "function",
			Function: model.ToolCallFunction{Name: "search_tools", Arguments: `{"query":"send_message"}`}}}
		return nil
	}
	res.ToolCalls = []model.ToolCall{{ID: "reply", Type: "function",
		Function: model.ToolCallFunction{Name: "send_message", Arguments: `{"text":"记得喝水","_finish_after_success":true}`}}}
	return nil
}
