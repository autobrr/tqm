package regex

import "github.com/dlclark/regexp2"

type Expressions struct {
	Patterns []*Pattern
}

type Pattern struct {
	Expression *regexp2.Regexp
}
