package http_ns

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aohorodnyk/mimeheader"
	jsoniter "github.com/inoxlang/inox/internal/jsoniter"
	netaddr "github.com/inoxlang/inox/internal/netaddr"

	"github.com/inoxlang/inox/internal/core"
	"github.com/inoxlang/inox/internal/utils"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	http_ns_symb "github.com/inoxlang/inox/internal/globals/http_ns/symbolic"
)

const DEFAULT_ACCEPT_HEADER = "*/*"

var (
	METHODS_WITH_NO_BODY = []string{"GET", "HEAD", "OPTIONS"}
	METHODS              = []string{"GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"}

	_ core.Serializable        = (*HttpRequest)(nil)
	_ core.PotentiallySharable = (*HttpRequest)(nil)
)

// HttpRequest is considered immutable from the viewpoint of Inox code, it should NOT be mutated.
type HttpRequest struct {
	isClientSide bool
	ULID         ulid.ULID
	ULIDString   string

	//accessible from inox
	Method             core.Str  //.url.Method from the *http.Request ("GET" if empty)
	URL                core.URL  //.url.URL from the *http.Request
	Path               core.Path //.url.Path from the *http.Request (already escaped)
	Body               *core.Reader
	Cookies            []*http.Cookie
	ParsedAcceptHeader mimeheader.AcceptHeader
	AcceptHeader       string
	ContentType        mimeheader.MimeType
	Session            *Session
	NewSession         bool

	headers     *core.Record //not set by default
	headersLock sync.Mutex

	//
	CreationTime      time.Time
	HeaderNames       []string
	UserAgent         string
	Hostname          string
	RemoteAddrAndPort netaddr.RemoteAddrWithPort //empty for client side requests
	RemoteIpAddr      netaddr.RemoteIpAddr       //empty for client side requests
	request           *http.Request
}

func NewClientSideRequest(r *http.Request) (*HttpRequest, error) {
	u := r.URL.String()

	if !strings.Contains(u, "://") {
		return nil, fmt.Errorf("cannot resolve URL of client side request")
	}

	return &HttpRequest{
		request:      r,
		isClientSide: true,
		URL:          core.URL(u),
	}, nil
}

func NewServerSideRequest(r *http.Request, logger zerolog.Logger, server *HttpsServer) (*HttpRequest, error) {
	id := ulid.Make()
	now := time.Now()

	addrAndPort := netaddr.RemoteAddrWithPort(r.RemoteAddr)
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)

	// method
	method := r.Method
	if method == "" {
		method = "GET"
	}

	switch method {
	case "GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE":
	default:
		return nil, fmt.Errorf("unknown method: %s", method)
	}

	// full URL
	url := r.URL.String()
	if !strings.Contains(url, "://") {
		if server == nil {
			return nil, fmt.Errorf("cannot resolve URL of request")
		}
		url = string(server.listeningAddr) + url
	}

	//hostname
	hostname, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		if strings.Contains(err.Error(), "missing port") {
			hostname = r.Host
		} else {
			hostname = "failed-to-split-host-port"
		}
	}

	// Content-Type header
	var contentType mimeheader.MimeType
	if !utils.SliceContains(METHODS_WITH_NO_BODY, string(method)) {
		mtype, err := mimeheader.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		contentType = mtype
	}

	// Accept header
	acceptHeaderValue := r.Header.Get("Accept")
	if acceptHeaderValue == "" {
		acceptHeaderValue = DEFAULT_ACCEPT_HEADER
	}

	//User-Agent header
	agent := r.Header.Get("User-Agent")

	//Header names
	headerNames := make([]string, 0, len(r.Header))
	for name, _ := range r.Header {
		headerNames = append(headerNames, name)
	}

	req := &HttpRequest{
		ULID:       id,
		ULIDString: id.String(),

		Method:             core.Str(method),
		URL:                core.URL(url),
		Path:               core.Path(r.URL.Path),
		RemoteAddrAndPort:  addrAndPort,
		RemoteIpAddr:       netaddr.RemoteIpAddr(ip),
		Body:               core.WrapReader(r.Body, &sync.Mutex{}),
		Cookies:            r.Cookies(),
		request:            r,
		ParsedAcceptHeader: mimeheader.ParseAcceptHeader(acceptHeaderValue),
		AcceptHeader:       acceptHeaderValue,
		ContentType:        contentType,

		CreationTime: now,
		Hostname:     hostname,
		UserAgent:    agent,
		HeaderNames:  headerNames,
	}

	session, err := getSession(req.request)
	if err == nil {
		req.Session = session
	} else if err == ErrSessionNotFound {
		logger.Print("no session id found, create new one")
		req.Session = addNewSession(server)
		req.NewSession = true
	} else {
		return nil, err
	}

	return req, nil
}

func (req *HttpRequest) Request() *http.Request {
	return req.request
}

func (req *HttpRequest) IsGetOrHead() bool {
	return req.Method == "GET" || req.Method == "HEAD"
}

func (req *HttpRequest) AcceptAny() bool {
	for _, h := range req.ParsedAcceptHeader.MHeaders {
		if h.MimeType.Type == "*" && h.MimeType.Subtype == "*" {
			return true
		}
	}
	return false
}

func (req *HttpRequest) IsSharable(originState *core.GlobalState) (bool, string) {
	return true, ""
}

func (req *HttpRequest) Share(originState *core.GlobalState) {
	//no op
}

func (req *HttpRequest) IsShared() bool {
	return true
}

func (req *HttpRequest) ForceLock() {
	//no op
}

func (req *HttpRequest) ForceUnlock() {
	//no op
}

func (req *HttpRequest) GetGoMethod(name string) (*core.GoFunction, bool) {
	return nil, false
}

func (req *HttpRequest) Prop(ctx *core.Context, name string) core.Value {
	switch name {
	case "method":
		return req.Method
	case "url":
		return req.URL
	case "path":
		return req.Path
	case "body":
		return req.Body
	case "headers":
		req.headersLock.Lock()
		defer req.headersLock.Unlock()
		if req.headers != nil {
			return req.headers
		}
		keys := make([]string, len(req.request.Header))
		vals := make([]core.Serializable, len(req.request.Header))

		i := 0
		for name, headerValues := range req.request.Header {
			keys[i] = name

			singleHeaderValues := make([]core.Serializable, len(headerValues))
			for i, val := range headerValues {
				singleHeaderValues[i] = core.Str(val)
			}

			vals[i] = core.NewTuple(singleHeaderValues)
			i++
		}
		req.headers = core.NewRecordFromKeyValLists(keys, vals)
		return req.headers
	case "cookies":
		//TODO
		return nil
	default:
		method, ok := req.GetGoMethod(name)
		if !ok {
			panic(core.FormatErrPropertyDoesNotExist(name, req))
		}
		return method
	}
}

func (*HttpRequest) SetProp(ctx *core.Context, name string, value core.Value) error {
	return core.ErrCannotSetProp
}

func (*HttpRequest) PropertyNames(ctx *core.Context) []string {
	return http_ns_symb.HTTP_REQUEST_PROPNAMES
}

func (r *HttpRequest) WriteRepresentation(ctx *core.Context, w io.Writer, config *core.ReprConfig, depth int) error {
	return core.ErrNotImplementedYet
}
func (r *HttpRequest) WriteJSONRepresentation(ctx *core.Context, w *jsoniter.Stream, config core.JSONSerializationConfig, depth int) error {
	return core.ErrNotImplementedYet
}
