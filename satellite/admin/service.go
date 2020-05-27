package admin

import (
	"storj.io/storj/satellite/accounting"
	"storj.io/storj/satellite/admin/service"
	"storj.io/storj/satellite/console"
)

// NewService returns new instance of Service.
func NewService(consoleDB console.DB, accountingDB accounting.ProjectAccounting, projectUsage *accounting.Service) *service.Service {
	return service.NewService(consoleDB, accountingDB, projectUsage)
}
