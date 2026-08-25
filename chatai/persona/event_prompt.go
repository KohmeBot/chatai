package persona

import (
	"encoding/json"
	"strings"
	"time"
)

// agentEventEnvelope is the protocol boundary between the chat adapter and the
// model. User-controlled text remains data inside this envelope instead of
// being mixed with natural-language harness instructions.
type agentEventEnvelope struct {
	Kind          string             `json:"kind"`
	CurrentTime   string             `json:"current_time"`
	GroupID       int64              `json:"group_id"`
	SelfUserID    int64              `json:"self_user_id"`
	Actor         eventActor         `json:"actor"`
	Message       *timeMessageResult `json:"message,omitempty"`
	ScheduledTask *scheduledTask     `json:"scheduled_task,omitempty"`
}

type eventActor struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	IsSelf   bool   `json:"is_self"`
}

type scheduledTask struct {
	Instruction string `json:"instruction"`
}

func (p *Persona) eventPrompt(msg GroupMessage, scheduled string, selfUserID int64) string {
	envelope := agentEventEnvelope{
		Kind:        "group_message",
		CurrentTime: time.Now().Format(time.RFC3339),
		GroupID:     p.groupID,
		SelfUserID:  selfUserID,
		Actor: eventActor{
			UserID: msg.User.UserId, Nickname: msg.User.Nickname,
			IsSelf: selfUserID > 0 && msg.User.UserId == selfUserID,
		},
	}
	if strings.TrimSpace(scheduled) != "" {
		envelope.Kind = "scheduled_task_due"
		envelope.ScheduledTask = &scheduledTask{Instruction: scheduled}
	} else {
		result := timeMessageResults([]GroupMessage{msg}, selfUserID)[0]
		envelope.Message = &result
	}
	encoded, _ := json.Marshal(envelope)
	return string(encoded)
}
