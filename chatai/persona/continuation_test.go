package persona

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestContinuationBusyGateIsolationAndExplicitBypass(t *testing.T) {
	p := NewPersona(1, nil, nil, Options{})
	other := NewPersona(2, nil, nil, Options{})
	p.renewContinuation(10, time.Now())
	deadline := p.continuationDeadline(10, time.Now())
	require.True(t, p.beginMessage(10, false))
	require.False(t, p.beginMessage(10, false))
	require.Equal(t, deadline, p.continuationDeadline(10, time.Now()), "dropped messages do not halve the window")
	require.True(t, p.beginMessage(11, false), "other users are independent")
	require.True(t, other.beginMessage(10, false), "other groups are independent")
	require.True(t, p.beginMessage(10, true), "explicit triggers bypass an active continuation")
	p.endMessage(10)
	require.False(t, p.beginMessage(10, false), "the explicit run is still active")
	p.endMessage(10)
	require.True(t, p.beginMessage(10, false), "resume when all runs finish")
	p.endMessage(10)
	p.endMessage(11)
	other.endMessage(10)
	require.Empty(t, p.activeMessages)
	require.Empty(t, other.activeMessages)
}

func TestContinuationBusyGateConcurrentBurst(t *testing.T) {
	p := NewPersona(1, nil, nil, Options{})
	var admitted atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if p.beginMessage(10, false) {
				admitted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	require.Equal(t, int32(1), admitted.Load())
	p.endMessage(10)
	require.True(t, p.beginMessage(10, false))
	p.endMessage(10)
}

func TestContinuationBusyGateReleaseOnEarlyReturn(t *testing.T) {
	p := NewPersona(1, nil, nil, Options{})
	func() {
		require.True(t, p.beginMessage(10, true))
		defer p.endMessage(10)
		require.False(t, p.beginMessage(10, false), "explicit runs also suppress passive continuation")
		return // Classification rejection/error or a failed run must release the slot.
	}()
	require.True(t, p.beginMessage(10, false))
	p.endMessage(10)
}

func TestContinuationWindowIsolationExpiryAndHalving(t *testing.T) {
	p := NewPersona(1, nil, nil, Options{})
	other := NewPersona(2, nil, nil, Options{})
	now := time.Now()
	p.renewContinuation(10, now)
	deadline := p.continuationDeadline(10, now)
	require.Equal(t, now.Add(time.Minute), deadline)
	require.True(t, p.continuationDeadline(11, now).IsZero())
	require.True(t, other.continuationDeadline(10, now).IsZero())
	p.halveContinuation(10, deadline, now.Add(20*time.Second))
	require.Equal(t, now.Add(40*time.Second), p.continuationDeadline(10, now))
	p.renewContinuation(10, now.Add(30*time.Second))
	p.halveContinuation(10, deadline, now.Add(31*time.Second))
	require.Equal(t, now.Add(90*time.Second), p.continuationDeadline(10, now))
	require.True(t, p.continuationDeadline(10, now.Add(90*time.Second)).IsZero())
}

func TestContinuationOnlyRenewsForSuccessfulDialogue(t *testing.T) {
	p := NewPersona(1, nil, nil, Options{ContinuationWindow: 120 * time.Second})
	rc := &agent.RunContext{UserID: 10, Values: map[string]any{}}
	segments := message.Message{message.Text("回答")}
	p.rememberBotDialogue(rc, segments, 1)
	require.True(t, p.continuationDeadline(10, time.Now()).IsZero())
	rc.Values[continuationTurnKey] = true
	p.rememberBotDialogue(rc, segments, 0)
	require.True(t, p.continuationDeadline(10, time.Now()).IsZero())
	p.rememberBotDialogue(rc, segments, 1)
	require.InDelta(t, 120, time.Until(p.continuationDeadline(10, time.Now())).Seconds(), 1)
}

type continuationTestModel struct {
	answer  string
	calls   int
	request *model.Request
}

func (m *continuationTestModel) Request(req *model.Request, res *model.Response) error {
	m.calls++
	m.request = req
	res.Answer = m.answer
	return nil
}

func TestContinuationDecision(t *testing.T) {
	for _, answer := range []string{`{"reply":true}`, `{"reply":false}`, `{}`, `invalid`} {
		t.Run(answer, func(t *testing.T) {
			m := &continuationTestModel{answer: answer}
			p := NewPersona(1, nil, memoryTestDB(t), Options{ContinuationModel: m})
			msg := GroupMessage{User: User{UserId: 10}, Content: "那明天呢", MsgType: MsgTypeText, CreatedAt: time.Now()}
			require.NoError(t, p.saveMessage(msg))
			ok, err := p.shouldContinue(msg, 99)
			require.NoError(t, err)
			require.False(t, ok)
			require.Zero(t, m.calls)
			p.renewContinuation(10, time.Now())
			ok, err = p.shouldContinue(msg, 99)
			if answer == `{}` || answer == `invalid` {
				require.Error(t, err)
				require.False(t, ok)
			} else {
				require.NoError(t, err)
				require.Equal(t, answer == `{"reply":true}`, ok)
			}
			require.Equal(t, 1, m.calls)
			require.Empty(t, m.request.Tools)
			require.True(t, json.Valid([]byte(m.request.Question)))
			require.Contains(t, m.request.Question, "那明天呢")
			if answer == `{"reply":false}` {
				require.InDelta(t, 30, time.Until(p.continuationDeadline(10, time.Now())).Seconds(), 1)
			}
		})
	}
}
