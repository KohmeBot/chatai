package model

import (
	zero "github.com/wdvxdr1123/ZeroBot"
	"strings"
)

type BatchMap map[int64]Batch

func NewBatchMap() BatchMap {
	return BatchMap{}
}
func (b BatchMap) SetBatch(user int64, batch Batch) {
	b[user] = batch
}

func (b BatchMap) GetBatch(user int64) (bmp Batch, ok bool) {
	bmp, ok = b[user]
	return
}

func (b BatchMap) Has(user int64) bool {
	_, ok := b[user]
	return ok
}

// Batch 异步批量提交大模型调用
type Batch struct {
	m           LargeModel
	on          OnResponse
	keepHistory bool
}

type OnResponse func(ctx *zero.Ctx, request *Request, response *Response, err error)

func NewBatch(m LargeModel, on OnResponse, keepHistory bool) Batch {
	return Batch{
		m:           m,
		on:          on,
		keepHistory: keepHistory,
	}
}

func (b *Batch) Submit(ctx *zero.Ctx, key Key, questions []string) {

	b.doRequest(ctx, key, questions)

}

func (b *Batch) doRequest(ctx *zero.Ctx, key Key, questions []string) {
	question := strings.Join(questions, "\n")
	req := &Request{Question: question, History: session.GetHistory(key)}
	resp := &Response{}
	err := b.m.Request(req, resp)

	if err == nil && b.keepHistory {
		session.Append(key, *req, *resp)
	}

	b.on(ctx, req, resp, err)
}

func (b *Batch) GetModel() LargeModel {
	return b.m
}
