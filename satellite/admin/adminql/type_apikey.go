package adminql

import (
	"github.com/graphql-go/graphql"
	"github.com/skyrings/skyring-common/tools/uuid"
	"storj.io/storj/satellite/admin/service"
	//"time"
	// "github.com/graphql-go/graphql"
	// "storj.io/storj/satellite/admin/service"
)

const (
	// APIKeyType is a graphql type for APIKey
	APIKeyType = "ApiKey"
	// APIKeyCreateType is a graphql type for APIKey
	APIKeyCreateType = "ApiKeyCreate"

	// FieldAPIKey is a field name for APIKey
	FieldAPIKey = "apiKey"
	// FieldAPIKeyID is a field name for APIKeyID
	FieldAPIKeyID = "apiKeyID"
	// FieldToken is a field name for Token
	FieldToken = "token"
)

// APIKeyCreate is a data structure that describes created APIKey entity
type APIKeyCreate struct {
	APIKey *service.APIKey `json:"apiKey"`
	Token  string          `json:"token"`
}

func graphqlAPIKey() *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: APIKeyType,
		Fields: graphql.Fields{
			FieldAPIKeyID: &graphql.Field{
				Type: graphql.ID,
			},
			FieldProjectID: &graphql.Field{
				Type: graphql.ID,
			},
			FieldPartnerID: &graphql.Field{
				Type: graphql.ID,
			},
			FieldName: &graphql.Field{
				Type: graphql.String,
			},
			FieldCreatedAt: &graphql.Field{
				Type: graphql.DateTime,
			},
		},
	})
}

func graphqlAPIKeyCreate(types *TypeCreator) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: APIKeyCreateType,
		Fields: graphql.Fields{
			FieldToken: &graphql.Field{
				Type: graphql.String,
			},
			FieldAPIKey: &graphql.Field{
				Type: types.apiKey,
			},
		},
	})
}

func graphqlCreateAPIKeyMutationArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldProjectID: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.ID),
		},
		FieldName: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.String),
		},
	}
}

func graphqlCreateAPIKeyMutationResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		inputID, _ := p.Args[FieldProjectID].(string)
		projectID, err := uuid.Parse(inputID)
		if err != nil {
			return nil, err
		}
		name, _ := p.Args[FieldName].(string)

		key, token, err := s.CreateAPIKey(p.Context, *projectID, name)
		if err != nil {
			return nil, err
		}

		return &APIKeyCreate{
			APIKey: key,
			Token:  token,
		}, nil
	}
}
func graphqlDeleteAPIKeyMutationArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldAPIKeyID: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(graphql.ID))),
		},
	}
}

func graphqlDeleteAPIKeyMutationResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		paramKeysID, _ := p.Args[FieldAPIKeyID].([]interface{})

		var keyIds []uuid.UUID
		var keys []service.APIKey
		for _, id := range paramKeysID {
			keyID, err := uuid.Parse(id.(string))
			if err != nil {
				return nil, err
			}

			key, err := s.GetAPIKey(p.Context, *keyID)
			if err != nil {
				return nil, err
			}

			keyIds = append(keyIds, *keyID)
			keys = append(keys, *key)
		}

		err := s.DeleteAPIKeys(p.Context, keyIds)
		if err != nil {
			return nil, err
		}

		return keys, nil
	}
}

func graphqlProjectAPIKeysArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		CursorArg: &graphql.ArgumentConfig{
			Type: graphqlCursorInput(),
		},
	}
}

func graphqlProjectAPIKeysResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		project := p.Source.(*service.Project)
		cursor := cursorFromMap(p.Args[CursorArg])
		if nil == cursor {
			cursor = &Cursor{}
		}
		return s.GetAPIKeysPageByProjectID(p.Context, project.ProjectID, cursor.Limit, cursor.Offset, cursor.Order(), cursor.Search())
	}
}

func graphqlAPIKeyQueryArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldAPIKeyID: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.ID),
		},
	}
}

func graphqlAPIKeyQueryResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		inputID, _ := p.Args[FieldAPIKeyID].(string)
		apiKeyID, err := uuid.Parse(inputID)
		if err != nil {
			return nil, err
		}
		return s.GetAPIKey(p.Context, *apiKeyID)
	}
}
