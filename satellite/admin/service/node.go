package service

import (
	"context"
	"time"

	"github.com/zeebo/errs"
	"storj.io/common/pb"
	"storj.io/common/storj"
)

// Nodes exposes methods to manage Nodes table in database.
//
// architecture: Database
type Nodes interface {
	// GetByWallet is a method for querying node by wallet address from the database.
	GetByWallet(ctx context.Context, walletAddress string, nodeType pb.NodeType) ([]*Node, error)
}

// Node is a database object that describes Node entity.
type Node struct {
	ID                    storj.NodeID `json:"nodeID"`
	Type                  string
	Address               string
	LastNet               string
	Email                 string
	Wallet                string
	FreeBandwidth         int64
	FreeDisk              int64
	PieceCount            int64
	Timestamp             time.Time
	Release               bool
	AuditSuccessCount     int64
	TotalAuditCount       int64
	UptimeSuccessCount    int64
	TotalUptimeCount      int64
	CreatedAt             time.Time
	UpdatedAt             time.Time
	LastContactSuccessAt  time.Time
	LastContactFailureAt  time.Time
	Contained             bool
	DisqualifiedAt        *time.Time
	AuditReputationAlpha  float64
	AuditReputationBeta   float64
	UptimeReputationAlpha float64
	UptimeReputationBeta  float64
	ExitInitiatedAt       *time.Time
	ExitLoopCompletedAt   *time.Time
	ExitFinishedAt        *time.Time
	ExitSuccess           bool
}

// NodeUsage is a service object that describes Node usage.
type NodeUsage struct {
	NodeID         storj.NodeID `json:"nodeID"`
	StartTime      time.Time
	EndTime        time.Time
	PutTotal       int64
	GetTotal       int64
	GetAuditTotal  int64
	GetRepairTotal int64
	PutRepairTotal int64
	AtRestTotal    float64
}

// GetNodesByWallet is a method for querying nodes by wallet address from the database.
func (s *Service) GetNodesByWallet(ctx context.Context, walletAddr string) ([]*Node, error) {
	return s.nodesDB.GetByWallet(ctx, walletAddr, pb.NodeType_STORAGE)
}

// GetNodeUsage is a method for querying node usage by node ID and time period from the database.
func (s *Service) GetNodeUsage(ctx context.Context, nodeIDString string, start time.Time, end time.Time) (*NodeUsage, error) {
	nodeID, err := storj.NodeIDFromString(nodeIDString)
	if nil != err {
		return nil, err
	}
	if !start.Before(end) {
		return nil, errs.New(timeStartNotBeforeEndErrMsg)
	}
	rs, err := s.storagenodeDB.GetRollup(ctx, nodeID, start, end)
	nu := NodeUsage{
		NodeID:    nodeID,
		StartTime: start,
		EndTime:   end,
	}
	for _, r := range rs {
		nu.PutTotal += r.PutTotal
		nu.GetTotal += r.GetTotal
		nu.GetAuditTotal += r.GetAuditTotal
		nu.GetRepairTotal += r.GetRepairTotal
		nu.PutRepairTotal += r.PutRepairTotal
		nu.AtRestTotal += r.AtRestTotal
	}
	return &nu, nil
}
