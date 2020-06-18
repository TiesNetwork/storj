package service

import (
	"context"
	"time"

	"github.com/skyrings/skyring-common/tools/uuid"
	"github.com/zeebo/errs"

	//"github.com/zeebo/errs"

	"storj.io/common/macaroon"
	"storj.io/common/pkcrypto"
	"storj.io/common/storj"
	"storj.io/storj/lib/uplink"
	"storj.io/storj/satellite/console"
)

const (
	defaultPageLimit = 10
	maxPageLimit     = 100
)

// APIKey is a data structure that describes APIKey entity
type APIKey struct {
	APIKeyID  uuid.UUID `json:"apiKeyId"`
	ProjectID uuid.UUID `json:"projectId"`
	PartnerID uuid.UUID `json:"partnerId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// CreateAPIKey is a method for searching APIKey
func (s *Service) CreateAPIKey(
	ctx context.Context,
	projectID uuid.UUID,
	name string,
) (*APIKey, string, error) {
	p, err := s.consoleDB.Projects().Get(ctx, projectID)
	if err != nil {
		return nil, "", errs.New(projectDoesNotExistErrMsg)
	}

	_, err = s.consoleDB.APIKeys().GetByNameAndProjectID(ctx, name, projectID)
	if err == nil {
		return nil, "", errs.New(apiKeyWithNameExistsErrMsg)
	}

	secret, err := macaroon.NewSecret()
	if err != nil {
		return nil, "", Error.Wrap(err)
	}

	key, err := macaroon.NewAPIKey(secret)
	if err != nil {
		return nil, "", Error.Wrap(err)
	}

	apikey := console.APIKeyInfo{
		Name:      name,
		ProjectID: projectID,
		Secret:    secret,
		PartnerID: p.PartnerID,
	}

	info, err := s.consoleDB.APIKeys().Create(ctx, key.Head(), apikey)
	if err != nil {
		return nil, "", Error.Wrap(err)
	}

	return mapAPIKey(info), key.Serialize(), nil
}

// DeleteAPIKeys deletes api key by id
func (s *Service) DeleteAPIKeys(ctx context.Context, ids []uuid.UUID) (err error) {
	var keysErr errs.Group

	for _, keyID := range ids {
		_, err := s.consoleDB.APIKeys().Get(ctx, keyID)
		if err != nil {
			keysErr.Add(err)
			continue
		}
	}

	if err = keysErr.Err(); err != nil {
		return Error.Wrap(err)
	}

	err = s.consoleDB.WithTx(ctx, func(ctx context.Context, tx console.DBTx) error {
		for _, keyToDeleteID := range ids {
			err = tx.APIKeys().Delete(ctx, keyToDeleteID)
			if err != nil {
				return Error.Wrap(err)
			}
		}

		return nil
	})
	return Error.Wrap(err)
}

// GetAPIKey is a method for searching APIKey
func (s *Service) GetAPIKey(
	ctx context.Context,
	apiKeyID uuid.UUID,
) (*APIKey, error) {
	k, err := s.consoleDB.APIKeys().Get(ctx, apiKeyID)
	if nil != err {
		return nil, errs.New(apiKeyDoesNotExistErrMsg)
	}
	return mapAPIKey(k), nil
}

// GetAPIKeysPageByProjectID returns paged api key list for given Project, search string and pagination
func (s *Service) GetAPIKeysPageByProjectID(
	ctx context.Context,
	projectID uuid.UUID,
	limit uint64,
	offset uint64,
	order OrderDirection,
	search string,
) ([]*APIKey, error) {

	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	if 0 == limit {
		limit = defaultPageLimit
	}

	page := &console.APIKeyPage{
		Search:         search,
		Limit:          uint(limit),
		Offset:         offset,
		Order:          1,
		OrderDirection: unmapOrderDirection(order),
	}
	page, err := s.consoleDB.APIKeys().GetPagedByProjectID(ctx, projectID, page)
	if nil != err {
		return nil, err
	}
	return mapAPIKeys(page.APIKeys), nil
}

// APIKeyToGatewayAccessKey generates gateway access key from APIKey token
func (s *Service) APIKeyToGatewayAccessKey(token string) (string, error) {
	if len(token) <= 0 {
		return "", errs.New(apiKeyTokenIsEmptuErrMsg)
	}
	defaultAPIKey, err := uplink.ParseAPIKey(token)
	if nil != err {
		return "", err
	}
	key, err := storj.NewKey(pkcrypto.SHA256Hash([]byte(token)))
	if nil != err {
		return "", err
	}
	accessKey, err := (&uplink.Scope{
		SatelliteAddr:    s.GetFullAddress(),
		APIKey:           defaultAPIKey,
		EncryptionAccess: uplink.NewEncryptionAccessWithDefaultKey(*key),
	}).Serialize()
	if err != nil {
		return "", err
	}
	return accessKey, nil
}

func unmapOrderDirection(order OrderDirection) console.OrderDirection {
	return console.OrderDirection(order + 1)
}

func mapAPIKeys(ps []console.APIKeyInfo) []*APIKey {
	keys := make([]*APIKey, len(ps))
	for i, k := range ps {
		keys[i] = mapAPIKey(&k)
	}
	return keys
}

func mapAPIKey(k *console.APIKeyInfo) *APIKey {
	return &APIKey{
		APIKeyID:  k.ID,
		ProjectID: k.ProjectID,
		PartnerID: k.PartnerID,
		Name:      k.Name,
		CreatedAt: k.CreatedAt,
	}
}
