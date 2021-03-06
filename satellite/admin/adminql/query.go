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

	// UserTotalUsageQuery is a query name for total usage for user
	UserTotalUsageQuery = "userTotalUsage"
	// ProjectTotalUsageQuery is a query name for total usage for Project
	ProjectTotalUsageQuery = "projectTotalUsage"
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
	// StorageNodesByWalletQuery is a query name for nodes by wallet address
	StorageNodesByWalletQuery = "nodesByWallet"
	// StorageNodeUsageQuery is a query name for node usage
	StorageNodeUsageQuery = "nodeUsage"
)

// rootQuery creates query for graphql populated by AccountsClient
func rootQuery(service *service.Service, types *TypeCreator) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: Query,
		Fields: graphql.Fields{
			UserTotalUsageQuery: &graphql.Field{
				Type:    types.userTotalUsage,
				Args:    graphqlUserTotalUsageQueryArgs(),
				Resolve: graphqlUserTotalUsageQueryResolve(service),
			},
			ProjectTotalUsageQuery: &graphql.Field{
				Type:    types.projectTotalUsage,
				Args:    graphqlProjectTotalUsageQueryArgs(),
				Resolve: graphqlProjectTotalUsageQueryResolve(service),
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
			StorageNodesByWalletQuery: &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(types.storageNode))),
				Args:    graphqlStorageNodeQueryArgs(),
				Resolve: graphqlStorageNodeQueryResolve(service),
			},
			StorageNodeUsageQuery: &graphql.Field{
				Type:    graphql.NewNonNull(types.storageNodeUsage),
				Args:    graphqlStorageNodeUsageQueryArgs(),
				Resolve: graphqlStorageNodeUsageQueryResolve(service),
			},
		},
	})
}
