// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package pgutil

import (
	"context"
	"strconv"

	"storj.io/storj/private/dbutil/dbschema"
)

// QueryData loads all data from tables.
func QueryData(ctx context.Context, db dbschema.Queryer, schema *dbschema.Schema) (*dbschema.Data, error) {
	return dbschema.QueryData(ctx, db, schema, func(columnName string) string {
		quoted := strconv.Quote(columnName)
		return `quote_nullable(` + quoted + `) as ` + quoted
	})
}
