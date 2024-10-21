package model

type Key struct {
	GroupId int64
	UserId  int64
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	// 输入的问题
	Question string
	// 历史记录
	History []Message
}

type Response struct {
	// 返回的结果
	Answer string
	// 本次调用的输入token数量
	InputToken int64
	// 本次调用的输出token数量
	OutToken int64
}

type LargeModel interface {
	Request(Request *Request, response *Response) error
}
