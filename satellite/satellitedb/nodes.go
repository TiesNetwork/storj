// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package satellitedb

import (
	"context"

	"storj.io/common/pb"
	"storj.io/common/storj"
	"storj.io/storj/satellite/admin/service"
	"storj.io/storj/satellite/satellitedb/dbx"
)

// ensures that users implements console.Users.
var _ service.Nodes = (*nodes)(nil)

type nodes struct {
	dbx.Methods
}

// SaveTallies records raw tallies of at rest data to the database
func (db *nodes) GetByWallet(ctx context.Context, walletAddress string, nodeType pb.NodeType) (nodes []*service.Node, err error) {
	defer mon.Task()(&ctx)(&err)
	if len(walletAddress) == 0 {
		return nil, Error.New("In GetByWallet with empty walletAddress")
	}

	nodesDB, err := db.All_Node_By_Wallet_And_Type_OrderBy_Asc_Id(ctx, dbx.Node_Wallet(walletAddress), dbx.Node_Type(int(nodeType)))
	if nil != err {
		return nil, Error.Wrap(err)
	}
	nodes = make([]*service.Node, len(nodesDB))
	for n, nodeDB := range nodesDB {
		nodes[n], err = nodeFromDBX(nodeDB)
		if nil != err {
			return nil, Error.Wrap(err)
		}
	}
	return nodes, Error.Wrap(err)
}

func nodeFromDBX(nodeDB *dbx.Node) (node *service.Node, err error) {
	id := storj.NodeID{}
	id.Scan(nodeDB.Id)
	return &service.Node{
		ID:                    id,
		Type:                  pb.NodeType_name[int32(nodeDB.Type)],
		Address:               nodeDB.Address,
		LastNet:               nodeDB.LastNet,
		Email:                 nodeDB.Email,
		Wallet:                nodeDB.Wallet,
		FreeBandwidth:         nodeDB.FreeBandwidth,
		FreeDisk:              nodeDB.FreeDisk,
		PieceCount:            nodeDB.PieceCount,
		Timestamp:             nodeDB.Timestamp,
		Release:               nodeDB.Release,
		AuditSuccessCount:     nodeDB.AuditSuccessCount,
		TotalAuditCount:       nodeDB.TotalAuditCount,
		UptimeSuccessCount:    nodeDB.UptimeSuccessCount,
		TotalUptimeCount:      nodeDB.TotalUptimeCount,
		CreatedAt:             nodeDB.CreatedAt,
		UpdatedAt:             nodeDB.UpdatedAt,
		LastContactSuccessAt:  nodeDB.LastContactSuccess,
		LastContactFailureAt:  nodeDB.LastContactFailure,
		Contained:             nodeDB.Contained,
		DisqualifiedAt:        nodeDB.Disqualified,
		AuditReputationAlpha:  nodeDB.AuditReputationAlpha,
		AuditReputationBeta:   nodeDB.AuditReputationBeta,
		UptimeReputationAlpha: nodeDB.UptimeReputationAlpha,
		UptimeReputationBeta:  nodeDB.UptimeReputationBeta,
		ExitInitiatedAt:       nodeDB.ExitInitiatedAt,
		ExitLoopCompletedAt:   nodeDB.ExitLoopCompletedAt,
		ExitFinishedAt:        nodeDB.ExitFinishedAt,
		ExitSuccess:           nodeDB.ExitSuccess,
	}, nil
}
