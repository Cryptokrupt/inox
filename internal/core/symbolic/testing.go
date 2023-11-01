package symbolic

import (
	"fmt"

	parse "github.com/inoxlang/inox/internal/parse"
	pprint "github.com/inoxlang/inox/internal/pretty_print"
)

const (
	TEST_ITEM_META__NAME_PROPNAME     = "name"
	TEST_ITEM_META__FS_PROPNAME       = "fs"
	TEST_ITEM_META__PROGRAM_PROPNAME  = "program"
	TEST_ITEM_META__PASS_LIVE_FS_COPY = "pass-live-fs-copy-to-subtests"
)

var (
	TEST_ITEM__EXPECTED_META_VALUE = NewMultivalue(ANY_STR_LIKE, NewInexactRecord(map[string]Serializable{
		TEST_ITEM_META__NAME_PROPNAME:     ANY_STR_LIKE,
		TEST_ITEM_META__FS_PROPNAME:       ANY_FS_SNAPSHOT_IL,
		TEST_ITEM_META__PASS_LIVE_FS_COPY: ANY_BOOL,
		TEST_ITEM_META__PROGRAM_PROPNAME:  ANY_ABS_NON_DIR_PATH,
	}, nil))
)

// A TestSuite represents a symbolic TestSuite.
type TestSuite struct {
	UnassignablePropsMixin
	_ int
}

func (s *TestSuite) Test(v Value, state RecTestCallState) bool {
	state.StartCall()
	defer state.FinishCall()

	switch v.(type) {
	case *TestSuite:
		return true
	default:
		return false
	}
}

func (s *TestSuite) Run(ctx *Context, options ...Option) (*LThread, *Error) {
	return &LThread{}, nil
}

func (s *TestSuite) WidestOfType() Value {
	return &TestSuite{}
}

func (s *TestSuite) GetGoMethod(name string) (*GoFunction, bool) {
	switch name {
	case "run":
		return &GoFunction{fn: s.Run}, true
	}
	return nil, false
}

func (s *TestSuite) Prop(name string) Value {
	method, ok := s.GetGoMethod(name)
	if !ok {
		panic(FormatErrPropertyDoesNotExist(name, s))
	}
	return method
}

func (*TestSuite) PropertyNames() []string {
	return []string{"run"}
}

func (s *TestSuite) PrettyPrint(w PrettyPrintWriter, config *pprint.PrettyPrintConfig) {
	w.WriteName("test-suite")
	return
}

// A TestCase represents a symbolic TestCase.
type TestCase struct {
	UnassignablePropsMixin
	_ int
}

func (s *TestCase) Test(v Value, state RecTestCallState) bool {
	state.StartCall()
	defer state.FinishCall()

	switch v.(type) {
	case *TestCase:
		return true
	default:
		return false
	}
}

func (s *TestCase) WidestOfType() Value {
	return &TestCase{}
}

func (s *TestCase) GetGoMethod(name string) (*GoFunction, bool) {
	return nil, false
}

func (s *TestCase) Prop(name string) Value {
	method, ok := s.GetGoMethod(name)
	if !ok {
		panic(FormatErrPropertyDoesNotExist(name, s))
	}
	return method
}

func (*TestCase) PropertyNames() []string {
	return nil
}

func (s *TestCase) PrettyPrint(w PrettyPrintWriter, config *pprint.PrettyPrintConfig) {
	w.WriteName("test-case")
}

func checkTestItemMeta(m *Record, node parse.Node, state *State) error {
	if !m.hasProperty(TEST_ITEM_META__PROGRAM_PROPNAME) {
		return nil
	}
	if state.projectFilesystem == nil {
		state.addError(makeSymbolicEvalError(node, state, PROGRAM_TESTING_ONLY_SUPPORTED_IN_PROJECTS))
		return nil
	}

	program, ok := m.Prop(TEST_ITEM_META__PROGRAM_PROPNAME).(*Path)
	if !ok || program.pattern == nil || program.pattern.absoluteness != AbsolutePath || program.pattern.dirConstraint != DirPath {
		return nil
	}

	if program.hasValue {
		info, err := state.projectFilesystem.Stat(program.value)
		if err != nil {
			return fmt.Errorf("failed to get info of file %s: %w", program.value, err)
		}
		if !info.Mode().IsRegular() {
			state.addError(makeSymbolicEvalError(node, state, fmtNotRegularFile(program.value)))
		}
	}

	return nil
}
