package model

import (
	"github.com/kohmebot/plugin/pkg/gopool"
	zero "github.com/wdvxdr1123/ZeroBot"
	"strings"
)

// Batch 异步批量提交大模型调用
type Batch struct {
	m  LargeModel
	on OnResponse
}

type OnResponse func(ctx *zero.Ctx, request *Request, response *Response, err error)

func NewBatch(m LargeModel, on OnResponse) *Batch {
	return &Batch{
		m:  m,
		on: on,
	}
}

func (b *Batch) Submit(ctx *zero.Ctx, key Key, questions []string) {
	// TODO 在短时间内实现批量提交
	gopool.Go(func() {
		b.doRequest(ctx, questions)
	})
}

func (b *Batch) doRequest(ctx *zero.Ctx, questions []string) {
	question := strings.Join(questions, "\n")
	req := &Request{Question: question}
	resp := &Response{}
	err := b.m.Request(req, resp)
	b.on(ctx, req, resp, err)
}

func (b *Batch) GetModel() LargeModel {
	return b.m
}
