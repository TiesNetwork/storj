package adminql

import (
	"time"

	"github.com/graphql-go/graphql"
	"github.com/skyrings/skyring-common/tools/uuid"
	"storj.io/storj/satellite/admin/service"
)

const (
	// TotalUsageType is a graphql type for total usage
	TotalUsageType = "TotalUsage"
	// UsageLimitType is a graphql type for usage limit
	UsageLimitType = "UsageLimit"

	// FieldEgress is a field name for egress
	FieldEgress = "egress"
	// FieldEgressLimit is a field name for egress limit
	FieldEgressLimit = "egressLimit"
	// FieldObject is a field name for object
	FieldObject = "object"
	// FieldStorage is a field name for storage
	FieldStorage = "storage"
	// FieldStorageLimit is a field name for storage limit
	FieldStorageLimit = "storageLimit"
)

// graphqlTotalUsage creates *graphql.Object type representation of satellite.admin.TotalUsage
// TODO: simplify
func graphqlTotalUsage() *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: TotalUsageType,
		Fields: graphql.Fields{
			FieldUserID: &graphql.Field{
				Type: graphql.ID,
			},
			FieldSince: &graphql.Field{
				Type: graphql.DateTime,
			},
			FieldBefore: &graphql.Field{
				Type: graphql.DateTime,
			},
			FieldEgress: &graphql.Field{
				Type: graphql.Float,
			},
			FieldObject: &graphql.Field{
				Type: graphql.Float,
			},
			FieldStorage: &graphql.Field{
				Type: graphql.Float,
			},
		},
	})
}

// graphqlTotalUsage creates *graphql.Object type representation of satellite.admin.TotalUsage
func graphqlUsageLimit() *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: UsageLimitType,
		Fields: graphql.Fields{
			FieldEgress: &graphql.Field{
				Type: bigInt,
			},
			FieldEgressLimit: &graphql.Field{
				Type: bigInt,
			},
			FieldStorage: &graphql.Field{
				Type: bigInt,
			},
			FieldStorageLimit: &graphql.Field{
				Type: bigInt,
			},
		},
	})
}

func graphqlTotalUsageQueryArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldUserID: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.ID),
		},
		FieldSince: &graphql.ArgumentConfig{
			Type: graphql.DateTime,
		},
		FieldBefore: &graphql.ArgumentConfig{
			Type: graphql.DateTime,
		},
	}
}

func graphqlTotalUsageQueryResolve(service *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		inputID, _ := p.Args[FieldUserID].(string)
		since, _ := p.Args[FieldSince].(time.Time)
		before, _ := p.Args[FieldBefore].(time.Time)

		if since.IsZero() {
			since = time.Now()
		}
		if before.IsZero() {
			before = time.Now()
		}

		id, err := uuid.Parse(inputID)
		if err != nil {
			return nil, err
		}

		return service.GetTotalUsageForUser(p.Context, *id, since, before)
	}
}

func graphqlUpdateUsageLimitMutationArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldProjectID: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.ID),
		},
		FieldStorageLimit: &graphql.ArgumentConfig{
			Type: bigInt,
		},
	}
}

func graphqlUpdateUsageLimitMutationResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		inputID, _ := p.Args[FieldProjectID].(string)

		projectID, err := uuid.Parse(inputID)
		if err != nil {
			return nil, err
		}

		var storageLimit *int64

		if value, set := p.Args[FieldStorageLimit].(int64); set {
			storageLimit = &value
		}
		if nil != storageLimit {
			return s.UpdateUsageLimit(p.Context, *projectID, *storageLimit)
		}
		return s.GetUsageLimit(p.Context, *projectID)
	}
}

func graphqlUsageLimitQueryArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldProjectID: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.ID),
		},
	}
}

func graphqlUsageLimitQueryResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		inputID, _ := p.Args[FieldProjectID].(string)

		projectID, err := uuid.Parse(inputID)
		if err != nil {
			return nil, err
		}
		return s.GetUsageLimit(p.Context, *projectID)
	}
}

func graphqlProjectUsageLimitResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		project := p.Source.(*service.Project)
		return s.GetUsageLimit(p.Context, project.ProjectID)
	}
}
