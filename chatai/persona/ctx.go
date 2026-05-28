package persona

import (
	"fmt"
	"sync"
	"time"
)

type groupContext struct {
	mu       sync.RWMutex
	msgs     []GroupMessage
	abstract abstract
	// 戳一戳限流
	pokeMp map[int64]time.Time
	// 复读过的消息
	lastRepeat string
}

func (g *groupContext) AppendMsg(msg GroupMessage, duration time.Duration) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.msgs = append(g.msgs, msg)
	cutoff := time.Now().Add(-duration)
	count := 0
	for i := len(g.msgs) - 1; i >= 0; i-- {
		if g.msgs[i].CreatedAt.Before(cutoff) {
			break
		}
		count++
	}
	return count

}

func (g *groupContext) Refer(msgId int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, msg := range g.msgs {
		if msg.MsgID == msgId {
			msg.Refer = true
			g.msgs[i] = msg
			return
		}
	}

}

func (g *groupContext) CanPoke(qq int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pokeMp == nil {
		g.pokeMp = map[int64]time.Time{}
	}
	// 每个人cd为8s
	now := time.Now()
	last := g.pokeMp[qq]
	if now.Sub(last) < 8*time.Second {
		return false
	}
	g.pokeMp[qq] = now
	return true
}

func (g *groupContext) UpdateAbstract(ab abstract) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if count := g.cleanBefore(ab.endTime); count > 0 {
		// 成功清理，才进行摘要更新
		g.abstract = ab
	}
}

func (g *groupContext) Flush() {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 以半个小时作为节点，如果最新的消息距今已超过30分钟，则认为起了一个新的话题，需要刷新所有上下文记忆
	now := time.Now()
	if len(g.msgs) == 0 {
		g.clear()
		return
	}

	last := g.msgs[len(g.msgs)-1]

	if now.Sub(last.CreatedAt) >= 30*time.Minute {
		g.clear()
		return
	}
}

func (g *groupContext) clear() {
	g.msgs = nil
	g.abstract = abstract{}
	g.lastRepeat = ""
}

func (g *groupContext) RepeatThis(content string) (repeat bool, repeated bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.msgs) < 3 {
		return false, false
	}

	last3 := g.msgs[len(g.msgs)-3:]
	for _, msg := range last3 {
		if msg.MsgType != MsgTypeText {
			return false, false
		}
		if msg.Content != content {
			return false, false
		}
	}

	// 三条都一样，检查是否已经复读过了
	repeated = g.lastRepeat == content

	if !repeated {
		g.lastRepeat = content
	}

	return true, repeated
}

func (g *groupContext) Context() MsgContext {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if len(g.msgs) == 0 {
		return MsgContext{}
	}
	fm := formatMessages(g.msgs)

	startTime := g.abstract.startTime
	if startTime.IsZero() {
		startTime = g.msgs[0].CreatedAt
	}

	return MsgContext{
		abstract:           g.abstract,
		groupMsgContent:    fm,
		startTime:          startTime,
		endTime:            g.msgs[len(g.msgs)-1].CreatedAt,
		needUpdateAbstract: g.needUpdateAbstract(fm),
	}

}

func (g *groupContext) needUpdateAbstract(ctxText string) bool {
	// 判断当前是否需要更新摘要
	if runeLen(ctxText) >= 1500 {
		// 上下文长度超过1500，需要更新摘要
		return true
	}
	return false

}

func (g *groupContext) cleanBefore(t time.Time) int {
	var idx int
	for _, msg := range g.msgs {
		if !msg.CreatedAt.After(t) {
			idx++
		}
	}
	g.msgs = g.msgs[idx:]
	return idx

}

type MsgContext struct {
	abstract           abstract
	groupMsgContent    string
	startTime          time.Time
	endTime            time.Time
	needUpdateAbstract bool
}

type abstract struct {
	// 摘要内容
	content string
	// 摘要开始时间
	startTime time.Time
	// 摘要结束时间
	endTime time.Time
}

func (a abstract) String() string {
	if a.IsEmpty() {
		return "最近没有摘要"
	}
	return fmt.Sprintf("从%s到%s的聊天内容摘要:\n%s", formatTime(a.startTime), formatTime(a.endTime), a.content)
}
func (a abstract) IsEmpty() bool {
	return a.content == ""
}
