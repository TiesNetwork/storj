package admin

import "storj.io/storj/satellite/admin/service"

// DB contains access to different satellite administration databases.
//
// architecture: Database
type DB interface {
	// Nodes is a getter for Nodes repository.
	Nodes() service.Nodes
}

// DBTx extends Database with transaction scope.
type DBTx interface {
	DB
	// Commit is a method for committing and closing transaction.
	Commit() error
	// Rollback is a method for rollback and closing transaction.
	Rollback() error
}
