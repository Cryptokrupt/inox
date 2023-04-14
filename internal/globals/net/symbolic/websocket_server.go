package internal

import (
	symbolic "github.com/inox-project/inox/internal/core/symbolic"
	http_symbolic "github.com/inox-project/inox/internal/globals/http/symbolic"
)

type WebsocketServer struct {
	symbolic.UnassignablePropsMixin
	_ int
}

func (s *WebsocketServer) Test(v symbolic.SymbolicValue) bool {
	_, ok := v.(*WebsocketServer)
	return ok
}

func (s WebsocketServer) Clone(clones map[uintptr]symbolic.SymbolicValue) symbolic.SymbolicValue {
	return &WebsocketServer{}
}

func (s *WebsocketServer) GetGoMethod(name string) (*symbolic.GoFunction, bool) {
	switch name {
	case "upgrade":
		return symbolic.WrapGoMethod(s.Upgrade), true
	case "close":
		return symbolic.WrapGoMethod(s.Close), true
	}
	return nil, false
}

func (s *WebsocketServer) Prop(name string) symbolic.SymbolicValue {
	return symbolic.GetGoMethodOrPanic(name, s)
}

func (*WebsocketServer) PropertyNames() []string {
	return []string{"upgrade", "close"}
}

func (s *WebsocketServer) Upgrade(ctx *symbolic.Context, rw *http_symbolic.HttpResponseWriter, req *http_symbolic.HttpRequest) (*WebsocketConnection, *symbolic.Error) {
	return &WebsocketConnection{}, nil
}

func (s *WebsocketServer) Close(ctx *symbolic.Context) *symbolic.Error {
	return nil
}

func (s *WebsocketServer) Widen() (symbolic.SymbolicValue, bool) {
	return nil, false
}

func (s *WebsocketServer) IsWidenable() bool {
	return false
}

func (s *WebsocketServer) String() string {
	return "websocket-server"
}

func (s *WebsocketServer) WidestOfType() symbolic.SymbolicValue {
	return &WebsocketServer{}
}
