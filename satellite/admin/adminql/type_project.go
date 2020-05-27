package adminql

import (
	"time"

	"github.com/graphql-go/graphql"
	"github.com/skyrings/skyring-common/tools/uuid"
	"storj.io/storj/satellite/admin/service"
)

const (
	// ProjectType is a graphql type for project
	ProjectType = "Project"

	// FieldProjectID is a field name for project ID
	FieldProjectID = "projectID"
	// FieldOwnerID is a field name for owner ID
	FieldOwnerID = "ownerID"
	// FieldDescription is a field name for description
	FieldDescription = "description"
	// FieldRateLimit is a field name for rateLimit
	FieldRateLimit = "rateLimit"
	// FieldAPIKeys is a field name for apiKeys
	FieldAPIKeys = "apiKeys"
	// FieldProjectUsage is a field name for project usage
	FieldProjectUsage = "projectUsage"
)

func graphqlProject(s *service.Service, types *TypeCreator) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: ProjectType,
		Fields: graphql.Fields{
			FieldProjectID: &graphql.Field{
				Type: graphql.ID,
			},
			FieldName: &graphql.Field{
				Type: graphql.String,
			},
			FieldDescription: &graphql.Field{
				Type: graphql.String,
			},
			FieldPartnerID: &graphql.Field{
				Type: graphql.ID,
			},
			FieldOwnerID: &graphql.Field{
				Type: graphql.ID,
			},
			FieldRateLimit: &graphql.Field{
				Type: graphql.Int,
			},
			FieldCreatedAt: &graphql.Field{
				Type: graphql.DateTime,
			},
			FieldAPIKeys: &graphql.Field{
				Type:    graphql.NewList(types.apiKey),
				Args:    graphqlProjectAPIKeysArgs(),
				Resolve: graphqlProjectAPIKeysResolve(s),
			},
			FieldProjectUsage: &graphql.Field{
				Type:    types.usageLimit,
				Resolve: graphqlProjectUsageLimitResolve(s),
			},
		},
	})
}

func graphqlCreateProjectMutationArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldOwnerID: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.ID),
		},
		FieldName: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.String),
		},
		FieldDescription: &graphql.ArgumentConfig{
			Type: graphql.String,
		},
		FieldCreatedAt: &graphql.ArgumentConfig{
			Type: graphql.DateTime,
		},
	}
}

func graphqlCreateProjectMutationResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		inputID, _ := p.Args[FieldOwnerID].(string)
		ownerID, err := uuid.Parse(inputID)
		if err != nil {
			return nil, err
		}
		name, _ := p.Args[FieldName].(string)
		description, _ := p.Args[FieldDescription].(string)
		createdAt, _ := p.Args[FieldCreatedAt].(time.Time)

		if createdAt.IsZero() {
			createdAt = time.Now()
		}

		return s.CreateProject(p.Context, *ownerID, name, description, createdAt)
	}
}

func graphqlUserProjectsResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		user := p.Source.(*service.User)
		return s.GetProjectsByUserID(p.Context, user.UserID)
	}
}

func graphqlProjectQueryArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldProjectID: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.ID),
		},
	}
}

func graphqlProjectQueryResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		inputID, _ := p.Args[FieldProjectID].(string)
		projectID, err := uuid.Parse(inputID)
		if err != nil {
			return nil, err
		}
		return s.GetProject(p.Context, *projectID)
	}
}
