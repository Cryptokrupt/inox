//go:build unix

package local_db_ns

import (
	"path/filepath"
	"sync"
	"testing"

	core "github.com/inoxlang/inox/internal/core"
	"github.com/inoxlang/inox/internal/globals/fs_ns"
	"github.com/inoxlang/inox/internal/permkind"
	"github.com/inoxlang/inox/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestOpenDatabase(t *testing.T) {

	t.Run("open same database sequentially", func(t *testing.T) {
		dir, _ := filepath.Abs(t.TempDir())
		dir += "/"
		tempFilepath := filepath.Join(dir, "data.db")

		pattern := core.PathPattern(dir + "...")

		ctxConfig := core.ContextConfig{
			Permissions: []core.Permission{
				core.FilesystemPermission{Kind_: permkind.Read, Entity: pattern},
				core.FilesystemPermission{Kind_: permkind.Create, Entity: pattern},
				core.FilesystemPermission{Kind_: permkind.WriteStream, Entity: pattern},
			},
			HostResolutions: map[core.Host]core.Value{
				core.Host("ldb://main"): core.Path(tempFilepath),
			},
			Filesystem: fs_ns.NewMemFilesystem(MAX_MEM_FS_STORAGE_SIZE),
		}

		ctx1 := core.NewContexWithEmptyState(ctxConfig, nil)

		_db, err := openDatabase(ctx1, core.Path(tempFilepath))
		if !assert.NoError(t, err) {
			return
		}
		defer _db.Close(ctx1)

		ctx2 := core.NewContexWithEmptyState(ctxConfig, nil)

		db, err := openDatabase(ctx2, core.Path(tempFilepath))
		if !assert.NoError(t, err) {
			return
		}
		assert.Same(t, db, _db)
	})

	t.Run("open same database in parallel", func(t *testing.T) {
		dir, _ := filepath.Abs(t.TempDir())
		dir += "/"
		tempFilepath := filepath.Join(dir, "data.db")

		pattern := core.PathPattern(dir + "...")

		ctxConfig := core.ContextConfig{
			Permissions: []core.Permission{
				core.FilesystemPermission{Kind_: permkind.Read, Entity: pattern},
				core.FilesystemPermission{Kind_: permkind.Create, Entity: pattern},
				core.FilesystemPermission{Kind_: permkind.WriteStream, Entity: pattern},
			},
			HostResolutions: map[core.Host]core.Value{
				core.Host("ldb://main"): core.Path(tempFilepath),
			},
			Filesystem: fs_ns.NewMemFilesystem(MAX_MEM_FS_STORAGE_SIZE),
		}

		wg := new(sync.WaitGroup)
		wg.Add(2)

		var db1, db2 *LocalDatabase

		go func() {
			defer wg.Done()

			//open database in first context
			ctx1 := core.NewContexWithEmptyState(ctxConfig, nil)

			_db1, err := openDatabase(ctx1, core.Path(tempFilepath))
			if !assert.NoError(t, err) {
				return
			}
			db1 = _db1

			defer func() {
				db1.Close(ctx1)
			}()
		}()

		go func() {
			defer wg.Done()
			//open same database in second context
			ctx2 := core.NewContexWithEmptyState(ctxConfig, nil)

			_db2, err := openDatabase(ctx2, core.Path(tempFilepath))
			if !assert.NoError(t, err) {
				return
			}
			db2 = _db2
		}()
		wg.Wait()

		assert.Same(t, db1, db2)
	})
}

func TestLocalDatabase(t *testing.T) {

	for _, inMemory := range []bool{true, false} {

		name := "in_memory"
		HOST := core.Host("ldb://main")

		if !inMemory {
			name = "filesystem"
		}

		setup := func(ctxHasTransaction bool) (*LocalDatabase, *Context, *core.Transaction) {
			core.ResetResourceMap()

			config := LocalDatabaseConfig{
				InMemory: inMemory,
			}

			ctxConfig := core.ContextConfig{}

			if !inMemory {
				dir, _ := filepath.Abs(t.TempDir())
				dir += "/"
				tempFilepath := filepath.Join(dir, "data.db")
				pattern := core.PathPattern(dir + "...")

				ctxConfig = core.ContextConfig{
					Permissions: []core.Permission{
						core.FilesystemPermission{Kind_: permkind.Read, Entity: pattern},
						core.FilesystemPermission{Kind_: permkind.Create, Entity: pattern},
						core.FilesystemPermission{Kind_: permkind.WriteStream, Entity: pattern},
					},
					HostResolutions: map[core.Host]core.Value{
						HOST: core.Path(tempFilepath),
					},
					Filesystem: fs_ns.NewMemFilesystem(MAX_MEM_FS_STORAGE_SIZE),
				}
				config.Host = HOST
				config.Path = core.Path(tempFilepath)
			}

			ctx := core.NewContexWithEmptyState(ctxConfig, nil)

			var tx *core.Transaction
			if ctxHasTransaction {
				tx = core.StartNewTransaction(ctx)
			}

			ldb, err := openLocalDatabaseWithConfig(ctx, config)
			assert.NoError(t, err)

			return ldb, ctx, tx
		}

		t.Run(name, func(t *testing.T) {
			t.Run("context has a transaction", func(t *testing.T) {
				ctxHasTransactionFromTheSart := true

				t.Run("Get non existing", func(t *testing.T) {
					ldb, ctx, tx := setup(ctxHasTransactionFromTheSart)
					defer ldb.Close(ctx)

					v, ok := ldb.Get(ctx, Path("/a"))
					assert.False(t, bool(ok))
					assert.Equal(t, core.Nil, v)

					assert.NoError(t, tx.Rollback(ctx))
				})

				t.Run("Set -> Get -> commit", func(t *testing.T) {
					ldb, ctx, tx := setup(ctxHasTransactionFromTheSart)
					defer ldb.Close(ctx)

					key := Path("/a")
					r := ldb.GetFullResourceName(key)
					ldb.Set(ctx, key, Int(1))
					if !assert.False(t, core.TryAcquireResource(r)) {
						return
					}

					v, ok := ldb.Get(ctx, key)
					assert.True(t, bool(ok))
					assert.Equal(t, Int(1), v)
					assert.False(t, core.TryAcquireResource(r))

					// //we check that the database transaction is not commited yet
					// ldb.underlying.db.View(func(txn *Tx) error {
					// 	_, err := txn.Get(string(key))
					// 	assert.ErrorIs(t, err, errNotFound)
					// 	return nil
					// })

					assert.NoError(t, tx.Commit(ctx))
					assert.True(t, core.TryAcquireResource(r))
					core.ReleaseResource(r)

					//we check that the database transaction is commited
					ldb.kv.db.View(func(txn *Tx) error {
						item, err := txn.Get(string(key))
						if !assert.NoError(t, err) {
							return nil
						}

						v, err := core.ParseRepr(ctx, utils.StringAsBytes(item))
						assert.NoError(t, err)
						assert.Equal(t, Int(1), v)
						return nil
					})
				})

				t.Run("Set -> rollback", func(t *testing.T) {
					ldb, ctx, tx := setup(ctxHasTransactionFromTheSart)
					defer ldb.Close(ctx)

					key := Path("/a")
					r := ldb.GetFullResourceName(key)
					ldb.Set(ctx, key, Int(1))
					if !assert.False(t, core.TryAcquireResource(r)) {
						return
					}

					v, ok := ldb.Get(ctx, key)
					assert.True(t, bool(ok))
					assert.Equal(t, Int(1), v)

					// //we check that the database transaction is not commited yet
					// ldb.underlying.db.View(func(txn *Tx) error {
					// 	_, err := txn.Get(string(key))
					// 	assert.ErrorIs(t, err, errNotFound)
					// 	return nil
					// })

					assert.NoError(t, tx.Rollback(ctx))
					assert.True(t, core.TryAcquireResource(r))
					core.ReleaseResource(r)

					// //we check that the database transaction is not commited
					// ldb.underlying.db.View(func(txn *Tx) error {
					// 	_, err := txn.Get(string(key))
					// 	assert.ErrorIs(t, err, errNotFound)
					// 	return nil
					// })

					//same
					v, ok = ldb.Get(ctx, key)
					assert.True(t, core.TryAcquireResource(r))
					core.ReleaseResource(r)
					assert.Equal(t, core.Nil, v)
					assert.False(t, bool(ok))
				})

			})

			t.Run("context has no transaction", func(t *testing.T) {
				ctxHasTransactionFromTheSart := false

				t.Run("Get non existing", func(t *testing.T) {
					ldb, ctx, _ := setup(ctxHasTransactionFromTheSart)
					defer ldb.Close(ctx)

					v, ok := ldb.Get(ctx, Path("/a"))
					assert.False(t, bool(ok))
					assert.Equal(t, core.Nil, v)
				})

				t.Run("Set then Get", func(t *testing.T) {
					ldb, ctx, _ := setup(ctxHasTransactionFromTheSart)
					defer ldb.Close(ctx)

					key := Path("/a")
					ldb.Set(ctx, key, Int(1))

					v, ok := ldb.Get(ctx, key)
					assert.True(t, bool(ok))
					assert.Equal(t, Int(1), v)

					//we check that the database transaction is commited
					ldb.kv.db.View(func(txn *Tx) error {
						item, err := txn.Get(string(key))
						if !assert.NoError(t, err) {
							return nil
						}

						v, err := core.ParseRepr(ctx, utils.StringAsBytes(item))
						assert.NoError(t, err)
						assert.Equal(t, Int(1), v)
						return nil
					})
				})
			})

			t.Run("context gets transaction in the middle of the execution", func(t *testing.T) {
				ctxHasTransactionFromTheSart := false

				t.Run("Set with no tx then set with tx", func(t *testing.T) {
					ldb, ctx, _ := setup(ctxHasTransactionFromTheSart)
					defer ldb.Close(ctx)

					//first call to Set
					key := Path("/a")
					ldb.Set(ctx, key, Int(1))

					//attach transaction
					core.StartNewTransaction(ctx)

					//second call to Set
					ldb.Set(ctx, key, Int(2))

					v, ok := ldb.Get(ctx, key)
					assert.True(t, bool(ok))
					assert.Equal(t, Int(2), v)
				})
			})
		})
	}
}
