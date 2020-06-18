package admin

import (
	"storj.io/storj/satellite/accounting"
	"storj.io/storj/satellite/admin/service"
	"storj.io/storj/satellite/console"
)

// Config is a configuration for administration service
type ServiceConfig service.Config

// NewService returns new instance of Service.
func NewService(consoleDB console.DB, accountingDB accounting.ProjectAccounting, projectUsage *accounting.Service, config *ServiceConfig) *service.Service {
	return service.NewService(consoleDB, accountingDB, projectUsage, (*service.Config)(config))
}
