package persona

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type groupQueryTestCaller struct {
	requests     []zero.APIRequest
	missingUser  int64
	wrongGroupID bool
}

func (c *groupQueryTestCaller) CallAPI(req zero.APIRequest) (zero.APIResponse, error) {
	c.requests = append(c.requests, req)
	groupID := req.Params["group_id"].(int64)
	if c.wrongGroupID {
		groupID++
	}
	switch req.Action {
	case "get_group_member_info":
		userID := req.Params["user_id"].(int64)
		if userID == c.missingUser {
			return zero.APIResponse{Status: "failed", RetCode: 100}, nil
		}
		payload := fmt.Sprintf(`{"group_id":%d,"user_id":%d,"role":"member"}`, groupID, userID)
		return zero.APIResponse{Status: "ok", Data: gjson.Parse(payload)}, nil
	case "get_group_member_list":
		payload := fmt.Sprintf(`[{"group_id":%d,"user_id":42}]`, groupID)
		return zero.APIResponse{Status: "ok", Data: gjson.Parse(payload)}, nil
	}
	return zero.APIResponse{Status: "failed", RetCode: 100}, nil
}

func TestGroupQueryToolsResolveScopeAndVerifyMembership(t *testing.T) {
	const botID, userID, currentGroup, otherGroup = int64(987654345), int64(42), int64(123), int64(456)
	caller := &groupQueryTestCaller{}
	zero.APICallers.Store(botID, caller)
	t.Cleanup(func() { zero.APICallers.Delete(botID) })
	ctx := zero.GetBot(botID)
	ctx.Event = &zero.Event{PostType: "message", DetailType: "group", SelfID: botID, UserID: userID, GroupID: currentGroup}
	rc := &agent.RunContext{UserID: userID, Values: map[string]any{"zero_ctx": ctx, "self_user_id": botID}}
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&PrivateChatMessageRecord{}))
	group := NewPersona(currentGroup, nil, db, Options{})
	private := NewPrivatePersona(PrivateSession{BotID: botID, UserID: userID}, nil, db, Options{})
	stamp := time.Date(2026, 8, 6, 8, 5, 0, 0, time.UTC)
	require.NoError(t, db.Create(&[]ChatMessageRecord{
		{GroupID: currentGroup, UserID: userID, CreatedAt: stamp, Content: "当前群"},
		{GroupID: otherGroup, UserID: userID, CreatedAt: stamp, Content: "目标群", QuotedMessageID: 42, QuotedContent: "未核验的引用", URL: "https://example.com/unverified"},
		{GroupID: otherGroup, UserID: botID, CreatedAt: stamp.Add(time.Minute), Content: "目标群机器人"},
	}).Error)
	require.NoError(t, private.saveMessage(GroupMessage{User: User{UserId: userID}, Content: "私聊正文", CreatedAt: stamp}))

	result, err := group.handleReadGroupContext(rc, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Equal(t, currentGroup, result.(contextQueryResult).GroupID)
	require.Equal(t, "当前群", result.(contextQueryResult).Messages[0].Content)
	require.Empty(t, caller.requests)
	result, err = group.handleGetGroupMemberList(rc, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Equal(t, currentGroup, result.([]groupMemberInfo)[0].GroupID)
	require.Len(t, caller.requests, 1)

	caller.requests = nil
	const target = `{"group_id":456}`
	caller.missingUser = userID
	_, err = group.handleReadGroupContext(rc, json.RawMessage(target))
	require.ErrorContains(t, err, "无法确认请求人和机器人")
	require.Len(t, caller.requests, 1)
	caller.missingUser = 0
	caller.requests = nil
	result, err = group.handleReadGroupContext(rc, json.RawMessage(target))
	require.NoError(t, err)
	require.Equal(t, otherGroup, result.(contextQueryResult).GroupID)
	require.Len(t, caller.requests, 2)

	ctx.Event.DetailType = "private" // A temporary private message still includes the source group number.
	caller.requests = nil
	const ranges = `"ranges":[{"start":"2026-08-06T08:00:00Z","end":"2026-08-06T09:00:00Z"}]`
	for _, tc := range []struct {
		handler agent.Handler
		args    string
	}{
		{private.handleReadGroupContext, `{}`},
		{private.handleReadUserContext, `{"user_id":42}`},
		{private.handleReadMessagesByTime, `{` + ranges + `}`},
		{private.handleGetGroupMemberInfo, `{"user_id":42}`},
		{private.handleGetGroupMemberList, `{}`},
	} {
		_, err := tc.handler(rc, json.RawMessage(tc.args))
		require.ErrorContains(t, err, "必须提供 group_id")
	}
	require.Empty(t, caller.requests)
	for _, missing := range []int64{userID, botID} {
		caller.missingUser = missing
		_, err := private.handleReadGroupContext(rc, json.RawMessage(target))
		require.ErrorContains(t, err, "无法确认请求人和机器人")
	}
	caller.missingUser, caller.wrongGroupID = 0, true
	_, err = private.handleGetGroupMemberList(rc, json.RawMessage(target))
	require.ErrorContains(t, err, "无法确认请求人和机器人")
	caller.wrongGroupID = false

	caller.requests = nil
	result, err = private.handleGetGroupMemberInfo(rc, json.RawMessage(`{"group_id":456,"user_id":42}`))
	require.NoError(t, err)
	require.Equal(t, otherGroup, result.(groupMemberInfo).GroupID)
	require.Equal(t, userID, caller.requests[0].Params["user_id"])
	require.Equal(t, botID, caller.requests[1].Params["user_id"])
	for _, request := range caller.requests[:2] {
		require.Equal(t, true, request.Params["no_cache"])
	}
	result, err = private.handleGetGroupMemberList(rc, json.RawMessage(target))
	require.NoError(t, err)
	require.Equal(t, otherGroup, result.([]groupMemberInfo)[0].GroupID)
	for _, tc := range []struct {
		handler agent.Handler
		args    string
	}{
		{private.handleReadGroupContext, `{"group_id":456,"limit":1,"offset":1}`},
		{private.handleReadUserContext, `{"group_id":456,"user_id":42}`},
		{private.handleReadMessagesByTime, `{"group_id":456,"user_id":42,` + ranges + `}`},
	} {
		result, err := tc.handler(rc, json.RawMessage(tc.args))
		require.NoError(t, err)
		page := result.(contextQueryResult)
		require.Equal(t, otherGroup, page.GroupID)
		require.Len(t, page.Messages, 1)
		require.Equal(t, "目标群", page.Messages[0].Content)
		require.Empty(t, page.Messages[0].QuotedContent)
		require.Empty(t, page.Messages[0].ImageURL)
	}
	require.NotContains(t, group.ToolNames(), "read_private_context")
	for _, scope := range []*Persona{group, private} {
		require.Subset(t, scope.ToolNames(), []string{"get_group_member_info", "get_group_member_list", "read_group_context", "read_user_context", "read_messages_by_time"})
	}
}
