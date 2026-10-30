package persona

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type closingTestModel struct{ calls int }

func TestBareMentionStartsAgentAndPersistsOriginalMessage(t *testing.T) {
	const botID = 987654330
	caller := &deliveryTestCaller{response: &zero.APIResponse{Status: "ok", Data: gjson.Parse(`{"message_id":42}`)}}
	zero.APICallers.Store(botID, caller)
	t.Cleanup(func() { zero.APICallers.Delete(botID) })
	ctx := zero.GetBot(botID)
	ctx.Event = &zero.Event{SelfID: botID, GroupID: 123, UserID: 1, IsToMe: true,
		NativeMessage: json.RawMessage(`[{"type":"at","data":{"qq":"987654330"}}]`)}
	llm := &closingTestModel{}
	p := &Persona{groupID: 123, db: memoryTestDB(t), tools: agent.NewRegistry(),
		opts: Options{AgentModel: llm, MaxSteps: 1, RunTimeout: time.Minute, ProgressAfter: time.Hour}}
	require.NoError(t, p.UpdateContext(ctx))
	require.Equal(t, 2, llm.calls, "a stripped bare mention must enter the Agent and its final response path")
	var record ChatMessageRecord
	require.NoError(t, p.db.Where("user_id = ?", 1).First(&record).Error)
	require.Equal(t, MsgTypeAt, record.MsgType)
	require.Contains(t, record.Content, "@987654330")
	require.EqualValues(t, botID, record.TargetUserID)
	require.JSONEq(t, string(ctx.Event.NativeMessage), record.RawSegments)
	require.Empty(t, ctx.Event.Message)
}

func TestScheduledRunCanFinishWithoutGroupAction(t *testing.T) {
	caller := &deliveryTestCaller{response: &zero.APIResponse{Status: "ok", Data: gjson.Parse(`{"message_id":42}`)}}
	const botID = 987654328
	zero.APICallers.Store(botID, caller)
	t.Cleanup(func() { zero.APICallers.Delete(botID) })
	ctx := zero.GetBot(botID)
	ctx.Event = &zero.Event{SelfID: botID, GroupID: 123}
	llm := &closingTestModel{}
	p := &Persona{
		groupID: 123, db: memoryTestDB(t), tools: agent.NewRegistry(),
		opts: Options{AgentModel: llm, MaxSteps: 1, RunTimeout: time.Minute, ProgressAfter: time.Hour},
	}
	require.NoError(t, p.run(ctx, GroupMessage{User: User{UserId: 1}}, "检查状态，无变化时无需通知"))
	require.Equal(t, 1, llm.calls)
	require.Empty(t, caller.requests)
}

func (m *closingTestModel) Request(req *model.Request, res *model.Response) error {
	m.calls++
	if m.calls == 1 {
		res.Answer = "internal draft"
	} else {
		res.Answer = "基于现有资料的最终回复"
	}
	return nil
}

func TestPersonaDeliversClosingAnswerAndRecordsIt(t *testing.T) {
	caller := &deliveryTestCaller{response: &zero.APIResponse{Status: "ok", Data: gjson.Parse(`{"message_id":42}`)}}
	const botID = 987654329
	zero.APICallers.Store(botID, caller)
	t.Cleanup(func() { zero.APICallers.Delete(botID) })
	ctx := zero.GetBot(botID)
	ctx.Event = &zero.Event{SelfID: botID, GroupID: 123}
	llm := &closingTestModel{}
	p := &Persona{
		groupID: 123, db: memoryTestDB(t), tools: agent.NewRegistry(),
		opts: Options{AgentModel: llm, MaxSteps: 1, RunTimeout: time.Minute, ProgressAfter: time.Hour},
	}
	err := p.run(ctx, GroupMessage{User: User{UserId: 1}, Content: "question"}, "")
	require.NoError(t, err)
	require.Equal(t, 2, llm.calls)
	require.Len(t, caller.requests, 1)
	require.Equal(t, "send_group_msg", caller.requests[0].Action)
	var records []ChatMessageRecord
	require.NoError(t, p.db.Find(&records).Error)
	require.Len(t, records, 1)
	require.Equal(t, "基于现有资料的最终回复", records[0].Content)
}
