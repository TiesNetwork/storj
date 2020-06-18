// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package adminql

import (
	"github.com/graphql-go/graphql"

	"storj.io/storj/satellite/admin/service"
)

const (
	// Query is immutable graphql request
	Query = "Query"

	// TotalUsageQuery is a query name for total usage
	TotalUsageQuery = "totalUsage"
	// UserQuery is a query name for user
	UserQuery = "user"
	// UserByEmailQuery is a query name for user by email
	UserByEmailQuery = "userByEmail"
	// ProjectQuery is a query name for project
	ProjectQuery = "project"
	// APIKeyQuery is a query name for API key
	APIKeyQuery = "apiKey"
	// GatewayAccessKeyQuery is a query name for gateway access key
	GatewayAccessKeyQuery = "gatewayAccessKey"
	// UsageLimitQuery is a query name for usage limit
	UsageLimitQuery = "usageLimit"
)

// rootQuery creates query for graphql populated by AccountsClient
func rootQuery(service *service.Service, types *TypeCreator) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: Query,
		Fields: graphql.Fields{
			TotalUsageQuery: &graphql.Field{
				Type:    types.totalUsage,
				Args:    graphqlTotalUsageQueryArgs(),
				Resolve: graphqlTotalUsageQueryResolve(service),
			},
			UserQuery: &graphql.Field{
				Type:    types.user,
				Args:    graphqlUserQueryArgs(),
				Resolve: graphqlUserQueryResolve(service),
			},
			UserByEmailQuery: &graphql.Field{
				Type:    types.user,
				Args:    graphqlUserByEmailQueryArgs(),
				Resolve: graphqlUserByEmailQueryResolve(service),
			},
			ProjectQuery: &graphql.Field{
				Type:    types.project,
				Args:    graphqlProjectQueryArgs(),
				Resolve: graphqlProjectQueryResolve(service),
			},
			APIKeyQuery: &graphql.Field{
				Type:    types.apiKey,
				Args:    graphqlAPIKeyQueryArgs(),
				Resolve: graphqlAPIKeyQueryResolve(service),
			},
			GatewayAccessKeyQuery: &graphql.Field{
				Type:    graphql.NewNonNull(graphql.String),
				Args:    graphqlGatewayAccessKeyQueryArgs(),
				Resolve: graphqlGatewayAccessKeyQueryResolve(service),
			},
			UsageLimitQuery: &graphql.Field{
				Type:    types.usageLimit,
				Args:    graphqlUsageLimitQueryArgs(),
				Resolve: graphqlUsageLimitQueryResolve(service),
			},
		},
	})
}
