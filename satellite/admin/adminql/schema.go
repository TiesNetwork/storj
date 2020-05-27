// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package adminql

import (
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"

	"storj.io/storj/satellite/admin/service"
)

func init() {
	// Fix for https://github.com/graphql-go/graphql/issues/504
	{
		field := &graphql.Field{
			Type: graphql.String,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				if field, ok := p.Source.(*graphql.FieldDefinition); ok {
					if field.DeprecationReason != "" {
						return field.DeprecationReason, nil
					}
				}
				return nil, nil
			},
		}
		graphql.FieldType.AddFieldConfig("deprecationReason", field)
		graphql.EnumValueType.AddFieldConfig("deprecationReason", field)
	}
}

// CreateSchema creates a schema for satellites console graphql api
func CreateSchema(log *zap.Logger, service *service.Service) (schema graphql.Schema, err error) {
	creator := TypeCreator{}

	err = creator.Create(log, service)
	if err != nil {
		return
	}

	return graphql.NewSchema(graphql.SchemaConfig{
		Query:    creator.RootQuery(),
		Mutation: creator.RootMutation(),
		Types: []graphql.Type{
			orderEnum,
			bigInt,
		},
	})
}
