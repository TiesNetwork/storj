package adminql

import (
	"time"

	"github.com/graphql-go/graphql"
	"github.com/skyrings/skyring-common/tools/uuid"
	"storj.io/storj/satellite/admin/service"
)

const (
	// UserType is a graphql type for user
	UserType = "User"

	// FieldUserID is a field name for userId
	FieldUserID = "userId"
	// FieldEmail is a field name for email
	FieldEmail = "email"
	// FieldPassword is a field name for password
	FieldPassword = "password"
	// FieldFullName is a field name for "first name"
	FieldFullName = "fullName"
	// FieldShortName is a field name for "last name"
	FieldShortName = "shortName"
	// FieldProjects is a field name for projects
	FieldProjects = "projects"
)

func graphqlUser(s *service.Service, types *TypeCreator) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: UserType,
		Fields: graphql.Fields{
			FieldUserID: &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
			},
			FieldEmail: &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			FieldFullName: &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			FieldShortName: &graphql.Field{
				Type: graphql.String,
			},
			FieldCreatedAt: &graphql.Field{
				Type: graphql.NewNonNull(graphql.DateTime),
			},
			FieldPartnerID: &graphql.Field{
				Type: graphql.String,
			},
			FieldProjects: &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(types.project))),
				Resolve: graphqlUserProjectsResolve(s),
			},
		},
	})
}

func graphqlUserQueryArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldUserID: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.ID),
		},
	}
}

func graphqlUserQueryResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		inputID, _ := p.Args[FieldUserID].(string)

		id, err := uuid.Parse(inputID)
		if err != nil {
			return nil, err
		}

		return s.GetUser(p.Context, id)
	}
}

func graphqlUserByEmailQueryArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldEmail: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.String),
		},
	}
}

func graphqlUserByEmailQueryResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		email, _ := p.Args[FieldEmail].(string)

		return s.GetUserByEmail(p.Context, email)
	}
}

func graphqlCreateUserMutationArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldEmail: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.String),
		},
		FieldFullName: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.String),
		},
		FieldPassword: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.String),
		},
		FieldShortName: &graphql.ArgumentConfig{
			Type: graphql.String,
		},
		FieldCreatedAt: &graphql.ArgumentConfig{
			Type: graphql.DateTime,
		},
		FieldPartnerID: &graphql.ArgumentConfig{
			Type: graphql.ID,
		},
	}
}

func graphqlCreateUserMutationResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		email, _ := p.Args[FieldEmail].(string)
		fullName, _ := p.Args[FieldFullName].(string)
		password, _ := p.Args[FieldPassword].(string)
		shortName, _ := p.Args[FieldShortName].(string)
		createdAt, _ := p.Args[FieldCreatedAt].(time.Time)
		partnerID, _ := p.Args[FieldPartnerID].(string)

		if createdAt.IsZero() {
			createdAt = time.Now()
		}

		return s.CreateUser(p.Context, email, fullName, password, shortName, createdAt, partnerID)
	}
}

func graphqlUpdateUserMutationArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldUserID: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.ID),
		},
		FieldEmail: &graphql.ArgumentConfig{
			Type: graphql.String,
		},
		FieldFullName: &graphql.ArgumentConfig{
			Type: graphql.String,
		},
		FieldPassword: &graphql.ArgumentConfig{
			Type: graphql.String,
		},
		FieldShortName: &graphql.ArgumentConfig{
			Type: graphql.String,
		},
	}
}

func graphqlUpdateUserMutationResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		inputID, _ := p.Args[FieldUserID].(string)

		userID, err := uuid.Parse(inputID)
		if err != nil {
			return nil, err
		}

		var emailRef *string
		var fullNameRef *string
		var passwordRef *string
		var shortNameRef *string

		if value, set := p.Args[FieldEmail].(string); set {
			emailRef = &value
		}
		if value, set := p.Args[FieldFullName].(string); set {
			fullNameRef = &value
		}
		if value, set := p.Args[FieldPassword].(string); set {
			passwordRef = &value
		}
		if value, set := p.Args[FieldShortName].(string); set {
			shortNameRef = &value
		}

		return s.UpdateUser(p.Context, *userID, emailRef, fullNameRef, passwordRef, shortNameRef)
	}
}
