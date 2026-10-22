package persona

import (
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type closingTestModel struct{ calls int }

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
