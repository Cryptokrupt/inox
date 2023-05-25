package internal

import (
	"errors"
	"log"
	"sync"

	core "github.com/inoxlang/inox/internal/core"
	symbolic "github.com/inoxlang/inox/internal/core/symbolic"
	"github.com/inoxlang/inox/internal/permkind"
)

var (
	ErrInvalidDatabaseDirpath = errors.New("invalid database dir path")
	ErrDatabaseAlreadyOpen    = errors.New("database is already open")
	ErrCannotResolveDatabase  = errors.New("cannot resolve database")
	ErrDatabaseClosed         = errors.New("database is closed")
	ErrCannotFindDatabaseHost = errors.New("cannot find corresponding host of database")
	ErrInvalidDatabaseHost    = errors.New("host of database is invalid")
	ErrInvalidPathKey         = errors.New("invalid path used as local database key")
	ErrDatabaseNotSupported   = errors.New("database is not supported")

	dbRegistry = databaseRegistry{
		resolutions:   map[core.Host]core.Path{},
		openDatabases: map[core.Path]*LocalDatabase{},
	}
)

func init() {
	core.RegisterSymbolicGoFunction(openDatabase, func(ctx *symbolic.Context, r symbolic.ResourceName) (*SymbolicLocalDatabase, *symbolic.Error) {
		return &SymbolicLocalDatabase{}, nil
	})
}

func NewLocalDbNamespace() *Record {
	return core.NewRecordFromMap(core.ValMap{
		//
		"open": core.ValOf(openDatabase),
	})
}

// openDatabase opens a local database, read, create & write permissions are required.
func openDatabase(ctx *Context, r ResourceName) (*LocalDatabase, error) {

	var pth Path

	switch resource := r.(type) {
	case Host:
		if resource.Scheme() != "ldb" {
			return nil, ErrCannotResolveDatabase
		}
		data, ok := ctx.GetHostResolutionData(resource).(Path)
		if !ok {
			return nil, ErrCannotResolveDatabase
		}
		pth = data
	case Path:
		pth = resource
	default:
		return nil, ErrCannotResolveDatabase
	}

	if !pth.IsDirPath() {
		return nil, ErrInvalidDatabaseDirpath
	}

	patt := PathPattern(pth.ToAbs(ctx.GetFileSystem()) + "...")

	for _, kind := range []core.PermissionKind{permkind.Read, permkind.Create, permkind.WriteStream} {
		perm := FilesystemPermission{Kind_: kind, Entity: patt}
		if err := ctx.CheckHasPermission(perm); err != nil {
			return nil, err
		}
	}

	host, ok := ctx.GetHostFromResolutionData(pth)
	if !ok {
		return nil, ErrCannotFindDatabaseHost
	}

	if host.Scheme() != "ldb" {
		return nil, ErrInvalidDatabaseHost
	}

	dbRegistry.lock.Lock()

	db, ok := dbRegistry.openDatabases[pth]
	if ok {
		dbRegistry.lock.Unlock()
		log.Println("db already exists !")
		return db, nil
	}

	defer func() {
		dbRegistry.lock.Unlock()
	}()
	db, err := NewLocalDatabase(LocalDatabaseConfig{Path: pth, Host: host})
	if err == nil {
		dbRegistry.openDatabases[pth] = db
		return db, nil
	}

	return nil, err
}

// A LocalDatabase is a database thats stores data on the filesystem.
type LocalDatabase struct {
	host       Host
	path       Path
	underlying underlying
}

type LocalDatabaseConfig struct {
	Host     Host
	Path     Path
	InMemory bool
}

func NewLocalDatabase(config LocalDatabaseConfig) (*LocalDatabase, error) {

	if config.InMemory {
		config.Path = ""
	}

	underlying, err := openUnderlying(config)
	if err != nil {
		return nil, err
	}

	localDB := &LocalDatabase{
		path:       config.Path,
		underlying: underlying,
	}

	return localDB, nil
}

func (ldb *LocalDatabase) Close() {
	ldb.underlying.close()
}

func (ldb *LocalDatabase) Get(ctx *Context, key Path) (Value, Bool) {
	return ldb.underlying.get(ctx, key, ldb)
}

func (ldb *LocalDatabase) Has(ctx *Context, key Path) Bool {
	return ldb.underlying.has(ctx, key, ldb)
}

func (ldb *LocalDatabase) Set(ctx *Context, key Path, value Value) {
	ldb.underlying.set(ctx, key, value, ldb)
}

func (ldb *LocalDatabase) GetFullResourceName(key Path) ResourceName {
	return getFullResourceName(ldb.host, ldb.path)
}

func getFullResourceName(host Host, pth Path) ResourceName {
	return URL(string(host) + string(pth))
}

func (ldb *LocalDatabase) Prop(ctx *core.Context, name string) Value {
	method, ok := ldb.GetGoMethod(name)
	if !ok {
		panic(core.FormatErrPropertyDoesNotExist(name, ldb))
	}
	return method
}

func (*LocalDatabase) SetProp(ctx *core.Context, name string, value core.Value) error {
	return core.ErrCannotSetProp
}

func (ldb *LocalDatabase) GetGoMethod(name string) (*GoFunction, bool) {
	switch name {
	case "get":
		return core.WrapGoMethod(ldb.Get), true
	case "has":
		return core.WrapGoMethod(ldb.Has), true
	case "set":
		return core.WrapGoMethod(ldb.Set), true
	case "close":
		return core.WrapGoMethod(ldb.Close), true
	}
	return nil, false
}

func (ldb *LocalDatabase) PropertyNames(ctx *Context) []string {
	return []string{"get", "has", "set", "close"}
}

type databaseRegistry struct {
	lock          sync.Mutex
	resolutions   map[Host]Path
	openDatabases map[Path]*LocalDatabase
}
