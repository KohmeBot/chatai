package persona

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kohmebot/chatai/chatai/agent"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func (p *Persona) groupMemberTools() []agent.Tool {
	return []agent.Tool{
		{
			Definition: agent.Function("get_group_member_info", "获取指定群成员的详细信息。群聊默认查当前群，私聊须提供群号。no_cache=true 可获取较新的信息", map[string]any{
				"group_id": groupQueryProperty(),
				"user_id":  integerProperty("成员 QQ 号"),
				"no_cache": map[string]any{"type": "boolean", "default": false, "description": "是否不使用缓存，默认 false"},
			}, "user_id"),
			Namespace:   "members",
			ReadOnly:    true,
			Idempotent:  true,
			Risk:        agent.ToolRiskLow,
			SearchTerms: []string{"群成员信息", "成员资料", "群名片", "成员角色", "群主", "管理员", "入群时间", "最后发言", "成员等级", "专属头衔"},
			Handler:     p.handleGetGroupMemberInfo,
		},
		{
			Definition:  agent.Function("get_group_member_list", "获取群成员列表。群聊默认查当前群，私聊须提供群号。部分字段可通过成员详情补查", map[string]any{"group_id": groupQueryProperty()}),
			Namespace:   "members",
			ReadOnly:    true,
			Idempotent:  true,
			Risk:        agent.ToolRiskLow,
			SearchTerms: []string{"群成员列表", "群成员", "成员名单", "群里有谁", "群友列表", "所有成员"},
			Handler:     p.handleGetGroupMemberList,
		},
	}
}

type groupMemberInfo struct {
	GroupID         int64  `json:"group_id"`
	UserID          int64  `json:"user_id"`
	Nickname        string `json:"nickname"`
	Card            string `json:"card"`
	Sex             string `json:"sex"`
	Age             int32  `json:"age"`
	Area            string `json:"area"`
	JoinTime        int32  `json:"join_time"`
	LastSentTime    int32  `json:"last_sent_time"`
	Level           string `json:"level"`
	Role            string `json:"role"`
	Unfriendly      bool   `json:"unfriendly"`
	Title           string `json:"title"`
	TitleExpireTime int32  `json:"title_expire_time"`
	CardChangeable  bool   `json:"card_changeable"`
}

func (p *Persona) handleGetGroupMemberInfo(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		GroupID *int64 `json:"group_id"`
		UserID  int64  `json:"user_id"`
		NoCache bool   `json:"no_cache"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}

	if err := validateUserID(input.UserID); err != nil {
		return nil, err
	}
	groupID, err := p.resolveGroupQuery(rc, input.GroupID)
	if err != nil {
		return nil, err
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	return fetchGroupMemberInfo(ctx, groupID, input.UserID, input.NoCache)
}

func fetchGroupMemberInfo(ctx *zero.Ctx, groupID, userID int64, noCache bool) (groupMemberInfo, error) {
	response := ctx.CallAction("get_group_member_info", zero.Params{"group_id": groupID, "user_id": userID, "no_cache": noCache})
	if response.Status != "ok" || response.RetCode != 0 {
		return groupMemberInfo{}, errors.New("无法读取群成员资料")
	}
	var member groupMemberInfo
	if err := json.Unmarshal([]byte(response.Data.Raw), &member); err != nil {
		return groupMemberInfo{}, fmt.Errorf("decode group member info: %w", err)
	}
	if member.GroupID != groupID || member.UserID != userID {
		return groupMemberInfo{}, errors.New("无法验证群成员资料的群号和用户 QQ")
	}
	return member, nil
}

func (p *Persona) handleGetGroupMemberList(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		GroupID *int64 `json:"group_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	groupID, err := p.resolveGroupQuery(rc, input.GroupID)
	if err != nil {
		return nil, err
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	response := ctx.CallAction("get_group_member_list", zero.Params{"group_id": groupID})
	if response.Status != "ok" || response.RetCode != 0 || !response.Data.IsArray() {
		return nil, errors.New("无法读取群成员列表")
	}
	var members []groupMemberInfo
	if err := json.Unmarshal([]byte(response.Data.Raw), &members); err != nil {
		return nil, fmt.Errorf("decode group member list: %w", err)
	}
	for _, member := range members {
		if member.GroupID != 0 && member.GroupID != groupID {
			return nil, errors.New("群成员列表包含其他群的资料")
		}
	}
	return members, nil
}
