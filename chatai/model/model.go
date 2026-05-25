package model

import (
	"fmt"
	"strings"
)

type NewModelFunc = func(Config) LargeModel

var modelFn = map[string]NewModelFunc{}

func RegisterModel(prefix string, fn NewModelFunc) {
	_, ok := modelFn[prefix]
	if ok {
		panic(fmt.Sprintf("model prefix %s already exists", prefix))
	}

	modelFn[prefix] = fn
}

func NewLargeModel(conf Config) LargeModel {
	prefix := strings.Split(conf.Name, ":")[0]
	if _, ok := modelFn[prefix]; !ok {
		panic(fmt.Sprintf("model %s not exists", prefix))
	}
	conf.Name = strings.TrimPrefix(conf.Name, prefix+":")
	return modelFn[prefix](conf)
}
