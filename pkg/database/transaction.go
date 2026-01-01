package database

import (
	"context"

	"gorm.io/gorm"
)

type txKey struct{}

// TransactionManager handles database transactions
type TransactionManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type transactionManager struct {
	db *gorm.DB
}

// NewTransactionManager creates a new transaction manager
func NewTransactionManager(db *gorm.DB) TransactionManager {
	return &transactionManager{db: db}
}

// Do executes the given function within a database transaction
func (tm *transactionManager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return tm.db.Transaction(func(tx *gorm.DB) error {
		ctxWithTx := context.WithValue(ctx, txKey{}, tx)
		return fn(ctxWithTx)
	})
}

// GetExecutor returns the transaction DB if present in context, otherwise the default DB
// It ensures that repository methods use the active transaction if one exists
func GetExecutor(defaultDB *gorm.DB, ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return tx
	}
	return defaultDB.WithContext(ctx)
}
