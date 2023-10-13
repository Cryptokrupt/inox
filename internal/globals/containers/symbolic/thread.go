package containers

import (
	"bufio"

	"github.com/inoxlang/inox/internal/core/symbolic"
	pprint "github.com/inoxlang/inox/internal/pretty_print"

	"github.com/inoxlang/inox/internal/utils"
)

var _ = []symbolic.Iterable{(*Thread)(nil)}

type Thread struct {
	symbolic.UnassignablePropsMixin
	_ int
}

func (*Thread) Test(v symbolic.SymbolicValue, state symbolic.RecTestCallState) bool {
	state.StartCall()
	defer state.FinishCall()

	_, ok := v.(*Thread)
	return ok
}

func (t *Thread) GetGoMethod(name string) (*symbolic.GoFunction, bool) {
	switch name {
	case "push":
		return symbolic.WrapGoMethod(t.Push), true
	}
	return nil, false
}

func (t *Thread) Prop(name string) symbolic.SymbolicValue {
	return symbolic.GetGoMethodOrPanic(name, t)
}

func (*Thread) PropertyNames() []string {
	return []string{"push"}
}

func (*Thread) Push(ctx *symbolic.Context, elems ...symbolic.SymbolicValue) {

}

func (*Thread) PrettyPrint(w *bufio.Writer, config *pprint.PrettyPrintConfig, depth int, parentIndentCount int) {
	utils.Must(w.Write(utils.StringAsBytes("%thread")))
	return
}

func (t *Thread) IteratorElementKey() symbolic.SymbolicValue {
	return &symbolic.Any{}
}

func (*Thread) IteratorElementValue() symbolic.SymbolicValue {
	return &symbolic.Any{}
}

func (*Thread) WidestOfType() symbolic.SymbolicValue {
	return &Thread{}
}
