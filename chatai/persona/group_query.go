package persona

import (
	"errors"

	"github.com/kohmebot/chatai/chatai/agent"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func groupQueryProperty() map[string]any {
	return map[string]any{
		"type": "integer", "minimum": 1,
		"description": "要查询的群号。群聊省略时查询当前群，私聊必填。跨会话查询会检查目标群的访问权限。",
	}
}

func (p *Persona) resolveGroupQuery(rc *agent.RunContext, requested *int64) (int64, error) {
	if requested == nil {
		if p.private != nil {
			return 0, errors.New("私聊查询群资料必须提供 group_id")
		}
		if p.groupID <= 0 {
			return 0, errors.New("当前群号不可用")
		}
		return p.groupID, nil
	}
	groupID := *requested
	if groupID <= 0 {
		return 0, errors.New("group_id must be positive")
	}
	if p.private == nil && groupID == p.groupID {
		return groupID, nil
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return 0, err
	}
	if ctx.Event == nil || ctx.Event.SelfID <= 0 || rc.UserID <= 0 || rc.UserID != ctx.Event.UserID {
		return 0, errors.New("无法验证当前会话身份")
	}
	if p.private != nil {
		if !p.matchesPrivateEvent(ctx) {
			return 0, errors.New("请求与当前私聊会话不匹配")
		}
	} else if ctx.Event.GroupID != p.groupID || zero.OnlyPrivate(ctx) {
		return 0, errors.New("请求与当前群聊会话不匹配")
	}
	for _, userID := range []int64{rc.UserID, ctx.Event.SelfID} {
		if _, err := fetchGroupMemberInfo(ctx, groupID, userID, true); err != nil {
			return 0, errors.New("无法确认请求人和机器人均为目标群成员")
		}
	}
	return groupID, nil
}
