package core

import (
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/inoxlang/inox/internal/utils"
	"github.com/oklog/ulid/v2"
)

const (
	DEFAULT_TRANSACTION_TIMEOUT = Duration(20 * time.Second)
	TX_TIMEOUT_OPTION_NAME      = "timeout"
)

var (
	ErrTransactionAlreadyStarted               = errors.New("transaction has already started")
	ErrTransactionShouldBeStartedBySameContext = errors.New("a transaction should be started by the same context that created it")
	ErrCannotAddIrreversibleEffect             = errors.New("cannot add irreversible effect to transaction")
	ErrCtxAlreadyHasTransaction                = errors.New("context already has a transaction")
	ErrFinishedTransaction                     = errors.New("transaction is finished")
	ErrAlreadySetTransactionEndCallback        = errors.New("transaction end callback is already set")
	ErrRunningTransactionExpected              = errors.New("running transaction expected")
	ErrEffectsNotAllowedInReadonlyTransaction  = errors.New("effects are not allowed in a readonly transaction")
)

// A Transaction is analogous to a database transaction but behaves a little bit differently.
// A Transaction can be started, commited and rolled back. Effects (reversible or not) such as FS changes are added to it.
// Actual database transactions or data containers can also register a callback with the OnEnd method, in order to execute logic
// when the transaction commits or rolls back.
type Transaction struct {
	ulid           ulid.ULID
	ctx            *Context
	lock           sync.RWMutex
	startTime      time.Time
	endTime        time.Time
	effects        []Effect
	values         map[any]any
	endCallbackFns map[any]TransactionEndCallbackFn
	finished       atomic.Bool
	timeout        Duration
	isReadonly     bool
}

type TransactionEndCallbackFn func(tx *Transaction, success bool)

// newTransaction creates a new empty unstarted transaction.
// ctx will not be aware of it until the transaction is started.
func newTransaction(ctx *Context, readonly bool, options ...Option) *Transaction {
	tx := &Transaction{
		ctx:            ctx,
		isReadonly:     readonly,
		ulid:           ulid.Make(),
		values:         make(map[any]any),
		endCallbackFns: make(map[any]TransactionEndCallbackFn),
		timeout:        DEFAULT_TRANSACTION_TIMEOUT,
	}

	for _, opt := range options {
		switch opt.Name {
		case TX_TIMEOUT_OPTION_NAME:
			tx.timeout = opt.Value.(Duration)
		}
	}

	return tx
}

// StartNewTransaction creates a new transaction and starts it immediately.
func StartNewTransaction(ctx *Context, options ...Option) *Transaction {
	tx := newTransaction(ctx, false, options...)
	tx.Start(ctx)
	return tx
}

// StartNewReadonlyTransaction creates a new readonly transaction and starts it immediately.
func StartNewReadonlyTransaction(ctx *Context) *Transaction {
	tx := newTransaction(ctx, true)
	tx.Start(ctx)
	return tx
}

func (tx *Transaction) IsFinished() bool {
	return tx.finished.Load()
}

// Start attaches tx to the passed context and creates a goroutine that will roll it back on timeout or context cancellation.
// The passed context must be the same context that created the transaction.
// ErrFinishedTransaction will be returned if Start is called on a finished transaction.
func (tx *Transaction) Start(ctx *Context) error {
	if tx.IsFinished() {
		return ErrFinishedTransaction
	}

	if ctx != tx.ctx {
		panic(ErrTransactionShouldBeStartedBySameContext)
	}

	tx.lock.Lock()
	defer tx.lock.Unlock()

	if tx.startTime != (time.Time{}) {
		panic(ErrTransactionAlreadyStarted)
	}

	if tx.ctx.currentTx != nil {
		panic(ErrCtxAlreadyHasTransaction)
	}

	// spawn a goroutine that rollbacks the transaction when the associated context is done or
	// if the timeout duration has ellapsed.
	go func() {
		select {
		case <-ctx.Done():
			tx.Rollback(ctx)
		case <-time.After(time.Duration(tx.timeout)):
			if !tx.IsFinished() {
				ctx.Logger().Print(tx.ulid.String(), "transaction timed out")
				tx.Rollback(ctx)
			}
		}
	}()

	tx.startTime = time.Now()
	tx.ctx.setTx(tx)
	return nil
}

// OnEnd associates with k the callback function fn that will be called on the end of the transacion (success or failure),
// IMPORTANT NOTE: fn may be called in a goroutine different from the one that registered it.
// If a function is already associated with k the error ErrAlreadySetTransactionEndCallback is returned
func (tx *Transaction) OnEnd(k any, fn TransactionEndCallbackFn) error {
	if tx.IsFinished() {
		return ErrFinishedTransaction
	}

	tx.lock.Lock()
	defer tx.lock.Unlock()

	_, ok := tx.endCallbackFns[k]
	if ok {
		return ErrAlreadySetTransactionEndCallback
	}

	tx.endCallbackFns[k] = fn
	return nil
}

func (tx *Transaction) AddEffect(ctx *Context, effect Effect) error {
	if tx.IsFinished() {
		return ErrFinishedTransaction
	}

	if tx.isReadonly {
		return ErrEffectsNotAllowedInReadonlyTransaction
	}

	tx.lock.Lock()
	defer tx.lock.Unlock()

	if effect.Reversability(ctx) == Irreversible {
		return ErrCannotAddIrreversibleEffect
	}
	tx.effects = append(tx.effects, effect)

	return nil
}

func (tx *Transaction) Commit(ctx *Context) error {
	if !tx.finished.CompareAndSwap(false, true) {
		return ErrFinishedTransaction
	}

	tx.lock.Lock()
	defer func() {
		tx.lock.Unlock()
		tx.ctx.setTx(nil)
	}()

	tx.endTime = time.Now()

	for _, effect := range tx.effects {
		if err := effect.Apply(ctx); err != nil {
			for _, fn := range tx.endCallbackFns {
				fn(tx, true)
			}
			return fmt.Errorf("error when applying effet %#v: %w", effect, err)
		}
	}

	var callbackErrors []error

	for _, fn := range tx.endCallbackFns {
		func() {
			defer func() {
				if e := recover(); e != nil {
					defer utils.Recover()
					err := fmt.Errorf("%w: %s", utils.ConvertPanicValueToError(e), string(debug.Stack()))
					callbackErrors = append(callbackErrors, err)
				}
			}()
			fn(tx, true)
		}()
	}

	tx.endCallbackFns = nil

	return utils.CombineErrorsWithPrefixMessage("callback errors", callbackErrors...)
}

func (tx *Transaction) Rollback(ctx *Context) error {
	if !tx.finished.CompareAndSwap(false, true) {
		return ErrFinishedTransaction
	}

	tx.lock.Lock()
	defer func() {
		tx.lock.Unlock()
		tx.ctx.setTx(nil)
	}()

	tx.endTime = time.Now()

	var callbackErrors []error
	for _, fn := range tx.endCallbackFns {
		func() {
			defer func() {
				if e := recover(); e != nil {
					defer utils.Recover()
					err := fmt.Errorf("%w: %s", utils.ConvertPanicValueToError(e), string(debug.Stack()))
					callbackErrors = append(callbackErrors, err)
				}
			}()
			fn(tx, false)
		}()
	}

	tx.endCallbackFns = nil

	for _, effect := range tx.effects {
		if err := effect.Reverse(ctx); err != nil {
			return err
		}
	}

	return utils.CombineErrorsWithPrefixMessage("callback errors", callbackErrors...)
}

func (tx *Transaction) WaitFinished() <-chan struct{} {
	if tx.IsFinished() {
		return nil
	}
	finishedChan := make(chan struct{})

	tx.OnEnd(finishedChan, func(tx *Transaction, success bool) {
		finishedChan <- struct{}{}
	})
	return finishedChan
}

func (tx *Transaction) Prop(ctx *Context, name string) Value {
	method, ok := tx.GetGoMethod(name)
	if !ok {
		panic(FormatErrPropertyDoesNotExist(name, tx))
	}
	return method
}

func (*Transaction) SetProp(ctx *Context, name string, value Value) error {
	return ErrCannotSetProp
}

func (tx *Transaction) PropertyNames(ctx *Context) []string {
	return []string{"start", "commit", "rollback"}
}

func (tx *Transaction) GetGoMethod(name string) (*GoFunction, bool) {
	switch name {
	case "start":
		return WrapGoMethod(tx.Start), true
	case "commit":
		return WrapGoMethod(tx.Commit), true
	case "rollback":
		return WrapGoMethod(tx.Rollback), true
	}
	return nil, false
}
