package persona

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kohmebot/chatai/chatai/agent"
)

func (p *Persona) groupMemberTools() []agent.Tool {
	return []agent.Tool{
		{
			Definition: agent.Function("get_group_member_info", "获取当前群指定成员的详细信息；缓存响应更快，no_cache=true 可获取较新的信息", map[string]any{
				"user_id":  integerProperty("成员 QQ 号"),
				"no_cache": map[string]any{"type": "boolean", "default": false, "description": "是否不使用缓存，默认 false"},
			}, "user_id"),
			SearchTerms: []string{"群成员信息", "成员资料", "群名片", "成员角色", "群主", "管理员", "入群时间", "最后发言", "成员等级", "专属头衔"},
			Handler:     p.handleGetGroupMemberInfo,
		},
		{
			Definition:  agent.Function("get_group_member_list", "获取当前群的成员列表；部分字段可能不如单独查询成员信息完整", map[string]any{}),
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
		UserID  int64 `json:"user_id"`
		NoCache bool  `json:"no_cache"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}

	if err := validateUserID(input.UserID); err != nil {
		return nil, err
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	var member groupMemberInfo
	if err := json.Unmarshal([]byte(ctx.GetThisGroupMemberInfo(input.UserID, input.NoCache).Raw), &member); err != nil {
		return nil, fmt.Errorf("decode group member info: %w", err)
	}
	return member, nil
}

func (p *Persona) handleGetGroupMemberList(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	members := make([]groupMemberInfo, 0)
	if err := json.Unmarshal([]byte(ctx.GetThisGroupMemberList().Raw), &members); err != nil {
		return nil, fmt.Errorf("decode group member list: %w", err)
	}
	return members, nil
}

func (p *Persona) validateCurrentGroupID(groupID int64) error {
	if groupID <= 0 {
		return errors.New("group_id must be positive")
	}
	if groupID != p.groupID {
		return fmt.Errorf("group_id must be the current group (%d)", p.groupID)
	}
	return nil
}
