// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package satellitedb

import (
	"context"

	"github.com/zeebo/errs"

	"storj.io/storj/satellite/admin"
	"storj.io/storj/satellite/admin/service"
	"storj.io/storj/satellite/satellitedb/dbx"
)

// ensures that AdminDB implements admin.DB.
var _ admin.DB = (*AdminDB)(nil)

// AdminDB contains access to different satellite databases.
type AdminDB struct {
	db *satelliteDB
	tx *dbx.Tx

	methods dbx.Methods
}

// Nodes is getter a for Nodes repository.
func (db *AdminDB) Nodes() service.Nodes {
	return &nodes{db.methods}
}

// WithTx is a method for executing and retrying transaction.
func (db *AdminDB) WithTx(ctx context.Context, fn func(context.Context, admin.DBTx) error) error {
	if db.db == nil {
		return errs.New("DB is not initialized!")
	}

	return db.db.WithTx(ctx, func(ctx context.Context, tx *dbx.Tx) error {
		dbTx := &AdminDBTx{
			AdminDB: &AdminDB{
				// Need to expose dbx.DB for when database methods need access to check database driver type
				db:      db.db,
				tx:      tx,
				methods: tx,
			},
		}
		return fn(ctx, dbTx)
	})
}

// AdminDBTx extends Database with transaction scope.
type AdminDBTx struct {
	*AdminDB
}

// Commit is a method for committing and closing transaction.
func (db *AdminDBTx) Commit() error {
	if db.tx == nil {
		return errs.New("begin transaction before commit it!")
	}

	return db.tx.Commit()
}

// Rollback is a method for rollback and closing transaction.
func (db *AdminDBTx) Rollback() error {
	if db.tx == nil {
		return errs.New("begin transaction before rollback it!")
	}

	return db.tx.Rollback()
}
