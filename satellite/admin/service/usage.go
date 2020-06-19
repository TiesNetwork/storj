package service

import (
	"context"
	"time"

	"github.com/skyrings/skyring-common/tools/uuid"
	"github.com/zeebo/errs"

	"storj.io/common/memory"
	"storj.io/storj/satellite/accounting"
)

// TotalUsage is an object that describes data usage aggregation for multiple projects
type TotalUsage struct {
	UserID  uuid.UUID `json:"userId"`
	Since   time.Time `json:"since"`
	Before  time.Time `json:"before"`
	Egress  int64     `json:"egress"`
	Object  float64   `json:"object"`
	Storage float64   `json:"storage"`
}

// UsageLimit is an object that describes data usage limit for project
type UsageLimit struct {
	Egress       int64 `json:"egress"`
	EgressLimit  int64 `json:"egressLimit"`
	Storage      int64 `json:"storage"`
	StorageLimit int64 `json:"storageLimit"`
}

// UpdateUsageLimit updates usage limit for user project
func (s *Service) UpdateUsageLimit(ctx context.Context, projectID uuid.UUID, limit int64) (*UsageLimit, error) {
	p, err := s.consoleDB.Projects().Get(ctx, projectID)
	if err != nil {
		return nil, errs.New(projectDoesNotExistErrMsg)
	}
	err = s.projectDB.UpdateProjectUsageLimit(ctx, p.ID, memory.Size(limit))
	if err != nil {
		return nil, Error.Wrap(err)
	}
	return s.getUsageLimit(ctx, p.ID)
}

// GetUsageLimit queries usage limit for user project
func (s *Service) GetUsageLimit(ctx context.Context, projectID uuid.UUID) (*UsageLimit, error) {
	p, err := s.consoleDB.Projects().Get(ctx, projectID)
	if err != nil {
		return nil, errs.New(projectDoesNotExistErrMsg)
	}
	return s.getUsageLimit(ctx, p.ID)
}

func (s *Service) getUsageLimit(ctx context.Context, projectID uuid.UUID) (*UsageLimit, error) {
	bandwidthLimit, err := s.projectUsage.GetProjectBandwidthLimit(ctx, projectID)
	if err != nil {
		return nil, Error.Wrap(err)
	}
	bandwidthTotals, err := s.projectUsage.GetProjectBandwidthTotals(ctx, projectID)
	if err != nil {
		return nil, Error.Wrap(err)
	}
	storageLimit, err := s.projectUsage.GetProjectStorageLimit(ctx, projectID)
	if err != nil {
		return nil, Error.Wrap(err)
	}
	storageTotals, err := s.projectUsage.GetProjectStorageTotals(ctx, projectID)
	if err != nil {
		return nil, Error.Wrap(err)
	}
	return &UsageLimit{
		Egress:       bandwidthTotals,
		EgressLimit:  bandwidthLimit.Int64(),
		Storage:      storageTotals,
		StorageLimit: storageLimit.Int64(),
	}, nil
}

// GetTotalUsageForUser aggregates data usage for all user projects
func (s *Service) GetTotalUsageForUser(ctx context.Context, userID uuid.UUID, since time.Time, before time.Time) (*TotalUsage, error) {
	projects, err := s.consoleDB.Projects().GetByUserID(ctx, userID)
	if nil != err {
		return nil, err
	}
	total := TotalUsage{
		UserID: userID,
		Since:  since,
		Before: before,
	}
	for _, project := range projects {
		usage, err := s.projectDB.GetProjectTotal(ctx, project.ID, since, before)
		if nil != err {
			return nil, err
		}
		mergeUsage(&total, usage)
	}
	return &total, nil
}

func mergeUsage(t *TotalUsage, p *accounting.ProjectUsage) {
	t.Egress += p.Egress
	t.Object += p.ObjectCount
	t.Storage += p.Storage
}
