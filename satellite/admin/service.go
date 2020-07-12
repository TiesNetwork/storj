package admin

import (
	"storj.io/storj/satellite/accounting"
	"storj.io/storj/satellite/admin/service"
	"storj.io/storj/satellite/console"
)

// ServiceConfig is a configuration for administration service
type ServiceConfig service.Config

// NewService returns new instance of Service.
func NewService(consoleDB console.DB, projectDB accounting.ProjectAccounting, storagenodeDB accounting.StoragenodeAccounting, adminDB DB, projectUsage *accounting.Service, config *ServiceConfig) *service.Service {
	return service.NewService(consoleDB, projectDB, storagenodeDB, adminDB.Nodes(), projectUsage, (*service.Config)(config))
}
