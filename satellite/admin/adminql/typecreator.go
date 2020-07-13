// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package adminql

import (
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"

	"storj.io/storj/satellite/admin/service"
)

// TypeCreator handles graphql type creation and error checking
type TypeCreator struct {
	query    *graphql.Object
	mutation *graphql.Object

	userTotalUsage    *graphql.Object
	projectTotalUsage *graphql.Object
	totalUsage        *graphql.Object
	user              *graphql.Object
	project           *graphql.Object
	apiKey            *graphql.Object
	apiKeyCreate      *graphql.Object
	usageLimit        *graphql.Object
	storageNode       *graphql.Object
	storageNodeUsage  *graphql.Object

	cursor *graphql.InputObject
}

// Create create types and check for error
func (c *TypeCreator) Create(log *zap.Logger, s *service.Service) error {

	// entities
	c.totalUsage = graphqlTotalUsage()
	if err := c.totalUsage.Error(); err != nil {
		return err
	}

	c.usageLimit = graphqlUsageLimit()
	if err := c.usageLimit.Error(); err != nil {
		return err
	}

	c.apiKey = graphqlAPIKey()
	if err := c.apiKey.Error(); err != nil {
		return err
	}
	c.storageNodeUsage = graphqlStorageNodeUsage()
	if err := c.storageNodeUsage.Error(); err != nil {
		return err
	}

	// hierarchical entities
	c.apiKeyCreate = graphqlAPIKeyCreate(c)
	if err := c.apiKeyCreate.Error(); err != nil {
		return err
	}
	c.userTotalUsage = graphqlUserTotalUsage(c)
	if err := c.userTotalUsage.Error(); err != nil {
		return err
	}
	c.projectTotalUsage = graphqlProjectTotalUsage(c)
	if err := c.projectTotalUsage.Error(); err != nil {
		return err
	}

	// composite entities
	c.project = graphqlProject(s, c)
	if err := c.project.Error(); err != nil {
		return err
	}
	c.user = graphqlUser(s, c)
	if err := c.user.Error(); err != nil {
		return err
	}
	c.storageNode = graphqlStorageNode(s, c)
	if err := c.storageNode.Error(); err != nil {
		return err
	}

	// root objects
	c.query = rootQuery(s, c)
	if err := c.query.Error(); err != nil {
		return err
	}

	c.mutation = rootMutation(log, s, c)
	if err := c.mutation.Error(); err != nil {
		return err
	}

	return nil
}

// RootQuery returns instance of query *graphql.Object
func (c *TypeCreator) RootQuery() *graphql.Object {
	return c.query
}

// RootMutation returns instance of mutation *graphql.Object
func (c *TypeCreator) RootMutation() *graphql.Object {
	return c.mutation
}
