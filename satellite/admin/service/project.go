package service

import (
	"context"
	"time"

	"github.com/skyrings/skyring-common/tools/uuid"
	"github.com/zeebo/errs"

	"storj.io/storj/satellite/console"
)

var (
	// ErrProjLimit is error type of project limit.
	ErrProjLimit = errs.Class("project limit error")
)

// Project is a data structure that describes Project entity
type Project struct {
	ProjectID uuid.UUID `json:"projectId"`

	Name        string    `json:"name"`
	Description string    `json:"description"`
	PartnerID   uuid.UUID `json:"partnerId"`
	OwnerID     uuid.UUID `json:"ownerId"`
	RateLimit   *int      `json:"rateLimit"`

	CreatedAt time.Time `json:"createdAt"`
}

// CreateProject is a method for creating new project
func (s *Service) CreateProject(
	ctx context.Context,
	ownerID uuid.UUID,
	name string,
	description string,
	createdAt time.Time,
) (*Project, error) {

	u, err := s.consoleDB.Users().Get(ctx, ownerID)
	if err != nil {
		return nil, errs.New(userDoesNotExistErrMsg)
	}

	err = s.checkProjectLimit(ctx, u.ID)
	if err != nil {
		return nil, ErrProjLimit.Wrap(err)
	}

	var p *console.Project

	err = s.consoleDB.WithTx(ctx, func(ctx context.Context, tx console.DBTx) error {
		p, err = tx.Projects().Insert(ctx,
			&console.Project{
				Description: description,
				Name:        name,
				OwnerID:     u.ID,
				PartnerID:   u.PartnerID,
				CreatedAt:   createdAt,
			},
		)
		if err != nil {
			return Error.Wrap(err)
		}

		_, err = tx.ProjectMembers().Insert(ctx, u.ID, p.ID)
		if err != nil {
			return Error.Wrap(err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return mapProject(p), nil
}

// GetProject is a method for searching projects
func (s *Service) GetProject(
	ctx context.Context,
	projectID uuid.UUID,
) (*Project, error) {
	p, err := s.consoleDB.Projects().Get(ctx, projectID)
	if err != nil {
		return nil, errs.New(projectDoesNotExistErrMsg)
	}

	return mapProject(p), nil
}

// GetProjectsByUserID is a method for searching projects
func (s *Service) GetProjectsByUserID(
	ctx context.Context,
	userID uuid.UUID,
) ([]*Project, error) {
	ps, err := s.consoleDB.Projects().GetByUserID(ctx, userID)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	return mapProjects(ps), nil
}

func mapProjects(ps []console.Project) []*Project {
	projects := make([]*Project, len(ps))
	for i, p := range ps {
		projects[i] = mapProject(&p)
	}
	return projects
}

func mapProject(p *console.Project) *Project {
	return &Project{
		ProjectID:   p.ID,
		Name:        p.Name,
		Description: p.Description,
		PartnerID:   p.PartnerID,
		OwnerID:     p.OwnerID,
		RateLimit:   p.RateLimit,
		CreatedAt:   p.CreatedAt,
	}
}

// checkProjectLimit is used to check if user is able to create a new project
func (s *Service) checkProjectLimit(ctx context.Context, userID uuid.UUID) (err error) {
	registrationToken, err := s.consoleDB.RegistrationTokens().GetByOwnerID(ctx, userID)
	if err != nil {
		return err
	}

	projects, err := s.consoleDB.Projects().GetByUserID(ctx, userID)
	if err != nil {
		return Error.Wrap(err)
	}
	if len(projects) >= registrationToken.ProjectLimit {
		return ErrProjLimit.Wrap(errs.New(projLimitExceededErrMsg))
	}

	return nil
}
