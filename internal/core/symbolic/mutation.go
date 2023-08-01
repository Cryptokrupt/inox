package symbolic

import (
	"bufio"

	pprint "github.com/inoxlang/inox/internal/pretty_print"
	"github.com/inoxlang/inox/internal/utils"
)

var (
	ANY_MUTATION = &Mutation{}
)

// An Mutation represents a symbolic Mutation.
type Mutation struct {
	_ int
}

func (r *Mutation) Test(v SymbolicValue) bool {
	_, ok := v.(Iterable)

	return ok
}

func (r *Mutation) PrettyPrint(w *bufio.Writer, config *pprint.PrettyPrintConfig, depth int, parentIndentCount int) {
	utils.Must(w.Write(utils.StringAsBytes("%mutation")))
	return
}

func (r *Mutation) WidestOfType() SymbolicValue {
	return ANY_MUTATION
}
