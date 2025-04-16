package expression

import "github.com/expr-lang/expr/vm"

type Expressions struct {
	Ignores []*vm.Program
	Removes []*vm.Program
	Labels  []*LabelExpression
	Tags    []*TagExpression
}

type LabelExpression struct {
	Name    string
	Updates []*vm.Program
}

type TagExpression struct {
	Name     string
	Mode     string
	UploadKb *int
	Updates  []*vm.Program
}
