package persona

import (
	"sync"
	"time"
)

// groupContext 是仅在工具被调用时才会读取的内存消息缓冲区。
type groupContext struct {
	mu         sync.RWMutex
	msgs       []GroupMessage
	lastRepeat GroupMessage
}

// ShouldRepeat 在连续相同消息达到阈值时仅触发一次。
func (g *groupContext) ShouldRepeat(msg GroupMessage, threshold int) bool {
	if threshold < 2 {
		threshold = 3
	}
	switch msg.MsgType {
	case MsgTypeText, MsgTypeImg, MsgTypeAt, MsgTypeReply:
	default:
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	consecutive := 0
	for i := len(g.msgs) - 1; i >= 0; i-- {
		if !g.msgs[i].ContentEqual(msg) {
			break
		}
		consecutive++
	}
	if consecutive < threshold {
		if consecutive == 1 && g.lastRepeat.ContentEqual(msg) {
			// 相同内容在中间出现过其他消息后，视为新的一轮复读。
			g.lastRepeat = GroupMessage{}
		}
		return false
	}
	if g.lastRepeat.ContentEqual(msg) {
		return false
	}
	g.lastRepeat = msg
	return true
}

func (g *groupContext) AppendMsg(msg GroupMessage, _ time.Duration) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.msgs = append(g.msgs, msg)
	if len(g.msgs) > 500 {
		g.msgs = append([]GroupMessage(nil), g.msgs[len(g.msgs)-500:]...)
	}
	return len(g.msgs)
}

func (g *groupContext) Snapshot(limit int) []GroupMessage {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if limit <= 0 || limit > len(g.msgs) {
		limit = len(g.msgs)
	}
	return append([]GroupMessage(nil), g.msgs[len(g.msgs)-limit:]...)
}

func (g *groupContext) Since(since time.Time) []GroupMessage {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make([]GroupMessage, 0)
	for _, msg := range g.msgs {
		if msg.CreatedAt.After(since) {
			result = append(result, msg)
		}
	}
	return result
}

func (g *groupContext) UserSnapshot(userID int64, limit int) []GroupMessage {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make([]GroupMessage, 0, limit)
	for i := len(g.msgs) - 1; i >= 0 && len(result) < limit; i-- {
		if g.msgs[i].User.UserId == userID {
			result = append(result, g.msgs[i])
		}
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}
