package persona

import (
	"context"
	"testing"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/schedule"
	"github.com/stretchr/testify/require"
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
