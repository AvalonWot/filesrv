package main

import (
	"regexp"

	"github.com/pkg/errors"
)

type Filters struct {
	regs []*regexp.Regexp
}

func NewFilters(rs []string) (*Filters, error) {
	regs := make([]*regexp.Regexp, 0, len(rs))
	for _, r := range rs {
		if c, err := regexp.Compile(r); err == nil {
			regs = append(regs, c)
		} else {
			return nil, errors.WithMessagef(err, "编译正则表达式: %s, 错误", r)
		}
	}
	return &Filters{regs: regs}, nil
}

func (f *Filters) Match(str string) bool {
	for _, r := range f.regs {
		if r.MatchString(str) {
			return true
		}
	}
	return false
}
