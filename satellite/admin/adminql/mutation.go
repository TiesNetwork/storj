// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package adminql

import (
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
	"storj.io/storj/satellite/admin/service"
)

const (
	// Mutation is graphql request that modifies data
	Mutation = "Mutation"

	// CreateUserMutation is a mutation name for user creation
	CreateUserMutation = "createUser"
	// UpdateUserMutation is a mutation name for user updating
	UpdateUserMutation = "updateUser"
	// CreateAPIKeyMutation is a mutation name for api key creation
	CreateAPIKeyMutation = "createAPIKey"
	// DeleteAPIKeyMutation is a mutation name for api key deletion
	DeleteAPIKeyMutation = "deleteAPIKey"
	// CreateProjectMutation is a mutation name for project creation
	CreateProjectMutation = "createProject"
	// UpdateUsageLimitMutation is a mutation name for usage limit updating
	UpdateUsageLimitMutation = "updateUsageLimit"
)

// rootMutation creates mutation for graphql populated by AccountsClient
func rootMutation(log *zap.Logger, service *service.Service, types *TypeCreator) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: Mutation,
		Fields: graphql.Fields{
			CreateUserMutation: &graphql.Field{
				Type:    types.user,
				Args:    graphqlCreateUserMutationArgs(),
				Resolve: graphqlCreateUserMutationResolve(service),
			},
			UpdateUserMutation: &graphql.Field{
				Type:    types.user,
				Args:    graphqlUpdateUserMutationArgs(),
				Resolve: graphqlUpdateUserMutationResolve(service),
			},
			CreateProjectMutation: &graphql.Field{
				Type:    types.project,
				Args:    graphqlCreateProjectMutationArgs(),
				Resolve: graphqlCreateProjectMutationResolve(service),
			},
			UpdateUsageLimitMutation: &graphql.Field{
				Type:    types.usageLimit,
				Args:    graphqlUpdateUsageLimitMutationArgs(),
				Resolve: graphqlUpdateUsageLimitMutationResolve(service),
			},
			CreateAPIKeyMutation: &graphql.Field{
				Type:    types.apiKeyCreate,
				Args:    graphqlCreateAPIKeyMutationArgs(),
				Resolve: graphqlCreateAPIKeyMutationResolve(service),
			},
			DeleteAPIKeyMutation: &graphql.Field{
				Type:    graphql.NewList(types.apiKey),
				Args:    graphqlDeleteAPIKeyMutationArgs(),
				Resolve: graphqlDeleteAPIKeyMutationResolve(service),
			},
		},
	})
}
