package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/wdvxdr1123/ZeroBot/message"
)

const defaultFollowUpTimeout = 30 * time.Second

type followUpWaiter struct {
	userID int64
	reply  chan GroupMessage
}

func (p *Persona) followUpTools() []agent.Tool {
	return []agent.Tool{{
		Definition: agent.Function("ask_user_and_wait", "当当前任务因缺少用户信息而无法继续时，向用户提出一个必要的问题，在当前群 @ 指定用户并发送追问，然后监听该用户接下来30秒内的第一条群消息；这是可见且会阻塞当前决策链的副作用，仅在上下文和其他只读工具都无法补足信息时使用", map[string]any{
			"user_id":  integerProperty("要追问的用户 QQ 号；通常是触发当前对话的用户"),
			"question": stringProperty("紧跟在 @ 后发送的明确、简短追问"),
		}, "user_id", "question"),
		Namespace:   "chat",
		Risk:        agent.ToolRiskMedium,
		SearchTerms: []string{"反问用户", "追问用户", "询问用户", "等待用户回复", "补充信息", "澄清问题", "ask user", "follow up"},
		// This intentionally is not a completed GroupAction: after the reply is
		// returned, the Agent must still send its final response to the group.
		DecisionBoundary: true,
		Handler:          p.handleAskUserAndWait,
	}}
}

func (p *Persona) handleAskUserAndWait(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		UserID   int64  `json:"user_id"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if err := validateUserID(input.UserID); err != nil {
		return nil, err
	}
	input.Question = strings.TrimSpace(input.Question)
	if input.Question == "" {
		return nil, errors.New("question must not be empty")
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}

	waiter, err := p.registerFollowUp(input.UserID)
	if err != nil {
		return nil, err
	}
	keepWaiting := true
	defer func() {
		if keepWaiting {
			p.cancelFollowUp(waiter)
		}
	}()

	segments := message.Message{message.At(input.UserID), message.Text(" " + input.Question)}
	messageID := ctx.SendGroupMessage(p.groupID, segments)
	timeout := p.followUpTimeout
	if timeout <= 0 {
		timeout = defaultFollowUpTimeout
	}
	// Start the 30-second window immediately after the question is sent.
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	rc.SetWaitingForUser(true)
	defer rc.SetWaitingForUser(false)
	if err := p.recordBotMessage(ctx, segments, messageID); err != nil {
		return nil, err
	}
	var done <-chan struct{}
	if rc.Context != nil {
		done = rc.Done()
	}

	select {
	case reply := <-waiter.reply:
		keepWaiting = false
		return followUpReplyResult(messageID, reply), nil
	case <-timer.C:
		if p.cancelFollowUp(waiter) {
			keepWaiting = false
			return map[string]any{
				"question_message_id": messageID,
				"timed_out":           true,
				"waited_seconds":      int(timeout.Seconds()),
				"instruction":         "用户未在时限内回复；请基于已有信息继续决策并给出最终回应。",
			}, nil
		}
		// Delivery won the race with the timer and already queued the reply.
		reply := <-waiter.reply
		keepWaiting = false
		return followUpReplyResult(messageID, reply), nil
	case <-done:
		if p.cancelFollowUp(waiter) {
			keepWaiting = false
			return nil, rc.Err()
		}
		reply := <-waiter.reply
		keepWaiting = false
		return followUpReplyResult(messageID, reply), nil
	}
}

func (p *Persona) registerFollowUp(userID int64) (*followUpWaiter, error) {
	p.followUpMu.Lock()
	defer p.followUpMu.Unlock()
	if p.followUps == nil {
		p.followUps = make(map[int64]*followUpWaiter)
	}
	if _, exists := p.followUps[userID]; exists {
		return nil, fmt.Errorf("already waiting for user %d in this group", userID)
	}
	waiter := &followUpWaiter{userID: userID, reply: make(chan GroupMessage, 1)}
	p.followUps[userID] = waiter
	return waiter, nil
}

func (p *Persona) deliverFollowUp(msg GroupMessage) bool {
	if msg.User.UserId <= 0 {
		return false
	}
	p.followUpMu.Lock()
	defer p.followUpMu.Unlock()
	waiter, exists := p.followUps[msg.User.UserId]
	if !exists {
		return false
	}
	delete(p.followUps, msg.User.UserId)
	waiter.reply <- msg
	return true
}

// cancelFollowUp removes waiter only if it is still the active wait for the
// user. False means an incoming message already claimed this waiter.
func (p *Persona) cancelFollowUp(waiter *followUpWaiter) bool {
	p.followUpMu.Lock()
	defer p.followUpMu.Unlock()
	if p.followUps[waiter.userID] != waiter {
		return false
	}
	delete(p.followUps, waiter.userID)
	return true
}

func followUpReplyResult(questionMessageID int64, reply GroupMessage) map[string]any {
	return map[string]any{
		"question_message_id": questionMessageID,
		"timed_out":           false,
		"reply": map[string]any{
			"user_id":        reply.User.UserId,
			"nickname":       reply.User.Nickname,
			"message_id":     reply.MsgID,
			"message_type":   reply.MsgType,
			"content":        reply.Content,
			"image_url":      reply.Url,
			"formatted_text": formatMessage(reply),
		},
		"instruction": "这是被追问用户在时限内发出的第一条群消息；请结合它继续决策并给出最终回应。",
	}
}
