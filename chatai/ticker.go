package chatai

import (
	"time"
)

type GroupTicker struct {
	mp  map[int64]*time.Ticker
	on  func(groupId int64)
	dur time.Duration
}

func NewGroupTicker(groups []int64, dur time.Duration, on func(groupId int64)) *GroupTicker {
	mp := make(map[int64]*time.Ticker)
	for _, group := range groups {
		mp[group] = time.NewTicker(dur)
	}

	g := &GroupTicker{
		on:  on,
		dur: dur,
		mp:  mp,
	}
	for groupId, ticker := range g.mp {
		go g.loop(groupId, ticker)
	}
	return g
}

func (g *GroupTicker) Update(group int64) {
	t, ok := g.mp[group]
	if ok {
		t.Reset(g.dur)
	}

}

func (g *GroupTicker) loop(groupId int64, ticker *time.Ticker) {
	defer ticker.Stop()
	for range ticker.C {
		g.on(groupId)
	}
}
