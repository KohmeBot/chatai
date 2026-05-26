package model

import (
	"github.com/sirupsen/logrus"
	"sync"
	"time"
)

var session = chatSession{
	sMap: make(map[Key][]chat),
}

func init() {
	ticker := time.NewTicker(time.Minute * 5)

	go func() {
		for range ticker.C {
			count := session.Clear()
			if count > 0 {
				logrus.Infof("clean %d chats ", count)
			}
		}
	}()

}

type chat struct {
	Request    string
	AIResponse string
	Ts         time.Time
}

type chatSession struct {
	rw   sync.RWMutex
	sMap map[Key][]chat
}

func (c *chatSession) Append(key Key, req Request, resp Response) {
	c.rw.Lock()
	defer c.rw.Unlock()

	sess := c.sMap[key]

	sess = append(sess, chat{
		Request:    req.Question,
		AIResponse: resp.Answer,
		Ts:         time.Now(),
	})

	if len(sess) > 20 {
		// 保持最新的20条
		sess = sess[1:]
	}

	c.sMap[key] = sess
}

func (c *chatSession) GetHistory(key Key) []Message {
	c.rw.RLock()
	defer c.rw.RUnlock()

	sess := c.sMap[key]

	res := make([]Message, 0, len(sess))

	for _, v := range sess {
		res = append(res, Message{
			Role:    "user",
			Content: v.Request,
		}, Message{
			Role:    "assistant",
			Content: v.AIResponse,
		})
	}
	return res
}

func (c *chatSession) Clear() int {
	c.rw.Lock()
	defer c.rw.Unlock()
	var count int
	// 如果最后一个记录大于20分钟，则删除该条会话
	for k, v := range c.sMap {
		if len(v) == 0 {
			delete(c.sMap, k)
			continue
		}
		if time.Now().Sub(v[len(v)-1].Ts) > time.Minute*20 {
			delete(c.sMap, k)
			count += len(v)
		}
	}

	return count
}
