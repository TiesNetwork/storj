package service

import (
	"github.com/zeebo/errs"

	"golang.org/x/crypto/bcrypt"

	"storj.io/storj/satellite/accounting"
	"storj.io/storj/satellite/console"
)

// Error messages
const (
	userDoesNotExistErrMsg     = "There is no account on this Satellite for id you have requested"
	projectDoesNotExistErrMsg  = "There is no project on this Satellite for id you have requested"
	apiKeyDoesNotExistErrMsg   = "There is no API key on this Satellite for id you have requested"
	apiKeyWithNameExistsErrMsg = "An API Key with this name already exists in this project, please use a different name"
	emailUsedErrMsg            = "This email is already in use, try another"
	passwordIncorrectErrMsg    = "Your password needs at least %d characters long"
	projLimitExceededErrMsg    = "Sorry, you have exceeded the number of projects you can create"
	unauthorizedErrMsg         = "You are not authorized to perform this action"
)

// Error describes internal console error.
var (
	Error = errs.Class("service error")
)

// Service is handling administration related logic
//
// architecture: Service
type Service struct {
	consoleDB    console.DB
	accountingDB accounting.ProjectAccounting

	projectUsage *accounting.Service

	passwordCost int
}

// NewService returns new instance of Service.
func NewService(consoleDB console.DB, accountingDB accounting.ProjectAccounting, projectUsage *accounting.Service) *Service {
	return &Service{
		consoleDB:    consoleDB,
		accountingDB: accountingDB,
		projectUsage: projectUsage,
		passwordCost: bcrypt.DefaultCost,
	}
}

// OrderDirection is an ordering direction type
type OrderDirection int

const (
	// OrderASC is an ascending order
	OrderASC OrderDirection = iota
	// OrderDSC is an descending order
	OrderDSC
)

// IsValid checks if OrderDirection is valid
func (o OrderDirection) IsValid() bool {
	return o >= OrderASC && o <= OrderDSC
}
