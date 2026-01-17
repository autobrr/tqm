package expression

import "github.com/expr-lang/expr/vm"

const (
	TagModeAdd    = "add"
	TagModeRemove = "remove"
	TagModeFull   = "full"
)

type CompiledExpression struct {
	Program *vm.Program
	Text    string
}

type Expressions struct {
	Ignores  []CompiledExpression
	Removes  []CompiledExpression
	Pauses   []CompiledExpression
	Priority *CompiledExpression
	Labels   []*LabelExpression
	Tags     []*TagExpression
}

type LabelExpression struct {
	Name    string
	Updates []CompiledExpression
}

type TagExpression struct {
	Name     string
	Mode     string
	UploadKb *int
	Updates  []CompiledExpression
}
