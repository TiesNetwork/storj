// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package satellitedb

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spacemonkeygo/monkit/v3"
	"github.com/zeebo/errs"
	"go.uber.org/zap"

	"storj.io/common/pb"
	"storj.io/common/storj"
	"storj.io/private/version"
	"storj.io/storj/private/dbutil/cockroachutil"
	"storj.io/storj/private/dbutil/pgutil"
	"storj.io/storj/private/tagsql"
	"storj.io/storj/satellite/overlay"
	"storj.io/storj/satellite/satellitedb/dbx"
)

var (
	mon = monkit.Package()
)

var _ overlay.DB = (*overlaycache)(nil)

type overlaycache struct {
	db *satelliteDB
}

// SelectAllStorageNodesUpload returns all nodes that qualify to store data, organized as reputable nodes and new nodes.
func (cache *overlaycache) SelectAllStorageNodesUpload(ctx context.Context, selectionCfg overlay.NodeSelectionConfig) (reputable, new []*overlay.SelectedNode, err error) {
	for {
		reputable, new, err = cache.selectAllStorageNodesUpload(ctx, selectionCfg)
		if err != nil {
			if cockroachutil.NeedsRetry(err) {
				continue
			}
			return reputable, new, err
		}
		break
	}

	return reputable, new, err
}

func (cache *overlaycache) selectAllStorageNodesUpload(ctx context.Context, selectionCfg overlay.NodeSelectionConfig) (reputable, new []*overlay.SelectedNode, err error) {
	defer mon.Task()(&ctx)(&err)

	asOf := cache.db.AsOfSystemTimeClause(selectionCfg.AsOfSystemTime.DefaultInterval)

	query := `
		SELECT id, address, last_net, last_ip_port, vetted_at
			FROM nodes ` + asOf + `
			WHERE disqualified IS NULL
			AND unknown_audit_suspended IS NULL
			AND exit_initiated_at IS NULL
			AND type = $1
			AND free_disk >= $2
			AND last_contact_success > $3
	`
	args := []interface{}{
		// $1
		int(pb.NodeType_STORAGE),
		// $2
		selectionCfg.MinimumDiskSpace.Int64(),
		// $3
		time.Now().Add(-selectionCfg.OnlineWindow),
	}
	if selectionCfg.MinimumVersion != "" {
		version, err := version.NewSemVer(selectionCfg.MinimumVersion)
		if err != nil {
			return nil, nil, err
		}
		query += `AND (major > $4 OR (major = $5 AND (minor > $6 OR (minor = $7 AND patch >= $8)))) AND release`
		args = append(args,
			// $4 - $8
			version.Major, version.Major, version.Minor, version.Minor, version.Patch,
		)
	}

	rows, err := cache.db.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { err = errs.Combine(err, rows.Close()) }()

	var reputableNodes []*overlay.SelectedNode
	var newNodes []*overlay.SelectedNode
	for rows.Next() {
		var node overlay.SelectedNode
		node.Address = &pb.NodeAddress{}
		var lastIPPort sql.NullString
		var vettedAt *time.Time
		err = rows.Scan(&node.ID, &node.Address.Address, &node.LastNet, &lastIPPort, &vettedAt)
		if err != nil {
			return nil, nil, err
		}
		if lastIPPort.Valid {
			node.LastIPPort = lastIPPort.String
		}

		if vettedAt == nil {
			newNodes = append(newNodes, &node)
			continue
		}
		reputableNodes = append(reputableNodes, &node)
	}

	return reputableNodes, newNodes, Error.Wrap(rows.Err())
}

// SelectAllStorageNodesDownload returns all nodes that qualify to store data, organized as reputable nodes and new nodes.
func (cache *overlaycache) SelectAllStorageNodesDownload(ctx context.Context, onlineWindow time.Duration, asOf overlay.AsOfSystemTimeConfig) (nodes []*overlay.SelectedNode, err error) {
	for {
		nodes, err = cache.selectAllStorageNodesDownload(ctx, onlineWindow, asOf)
		if err != nil {
			if cockroachutil.NeedsRetry(err) {
				continue
			}
			return nodes, err
		}
		break
	}

	return nodes, err
}

func (cache *overlaycache) selectAllStorageNodesDownload(ctx context.Context, onlineWindow time.Duration, asOfConfig overlay.AsOfSystemTimeConfig) (_ []*overlay.SelectedNode, err error) {
	defer mon.Task()(&ctx)(&err)

	asOf := cache.db.AsOfSystemTimeClause(asOfConfig.DefaultInterval)

	query := `
		SELECT id, address, last_net, last_ip_port
			FROM nodes ` + asOf + `
			WHERE disqualified IS NULL
			AND exit_finished_at IS NULL
			AND last_contact_success > $1
	`
	args := []interface{}{
		// $1
		time.Now().Add(-onlineWindow),
	}

	rows, err := cache.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { err = errs.Combine(err, rows.Close()) }()

	var nodes []*overlay.SelectedNode
	for rows.Next() {
		var node overlay.SelectedNode
		node.Address = &pb.NodeAddress{}
		var lastIPPort sql.NullString
		err = rows.Scan(&node.ID, &node.Address.Address, &node.LastNet, &lastIPPort)
		if err != nil {
			return nil, err
		}
		if lastIPPort.Valid {
			node.LastIPPort = lastIPPort.String
		}
		nodes = append(nodes, &node)
	}
	return nodes, Error.Wrap(rows.Err())
}

// GetNodesNetwork returns the /24 subnet for each storage node, order is not guaranteed.
func (cache *overlaycache) GetNodesNetwork(ctx context.Context, nodeIDs []storj.NodeID) (nodeNets []string, err error) {
	for {
		nodeNets, err = cache.getNodesNetwork(ctx, nodeIDs)
		if err != nil {
			if cockroachutil.NeedsRetry(err) {
				continue
			}
			return nodeNets, err
		}
		break
	}

	return nodeNets, err
}

func (cache *overlaycache) getNodesNetwork(ctx context.Context, nodeIDs []storj.NodeID) (nodeNets []string, err error) {
	defer mon.Task()(&ctx)(&err)

	var rows tagsql.Rows
	rows, err = cache.db.Query(ctx, cache.db.Rebind(`
		SELECT last_net FROM nodes
			WHERE id = any($1::bytea[])
		`), pgutil.NodeIDArray(nodeIDs),
	)
	if err != nil {
		return nil, err
	}
	defer func() { err = errs.Combine(err, rows.Close()) }()

	for rows.Next() {
		var ip string
		err = rows.Scan(&ip)
		if err != nil {
			return nil, err
		}
		nodeNets = append(nodeNets, ip)
	}
	return nodeNets, Error.Wrap(rows.Err())
}

// Get looks up the node by nodeID.
func (cache *overlaycache) Get(ctx context.Context, id storj.NodeID) (dossier *overlay.NodeDossier, err error) {
	defer mon.Task()(&ctx)(&err)

	if id.IsZero() {
		return nil, overlay.ErrEmptyNode
	}

	node, err := cache.db.Get_Node_By_Id(ctx, dbx.Node_Id(id.Bytes()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, overlay.ErrNodeNotFound.New("%v", id)
	}
	if err != nil {
		return nil, err
	}

	return convertDBNode(ctx, node)
}

// GetOnlineNodesForGetDelete returns a map of nodes for the supplied nodeIDs.
func (cache *overlaycache) GetOnlineNodesForGetDelete(ctx context.Context, nodeIDs []storj.NodeID, onlineWindow time.Duration) (nodes map[storj.NodeID]*overlay.SelectedNode, err error) {
	for {
		nodes, err = cache.getOnlineNodesForGetDelete(ctx, nodeIDs, onlineWindow)
		if err != nil {
			if cockroachutil.NeedsRetry(err) {
				continue
			}
			return nodes, err
		}
		break
	}

	return nodes, err
}

func (cache *overlaycache) getOnlineNodesForGetDelete(ctx context.Context, nodeIDs []storj.NodeID, onlineWindow time.Duration) (_ map[storj.NodeID]*overlay.SelectedNode, err error) {
	defer mon.Task()(&ctx)(&err)

	var rows tagsql.Rows
	rows, err = cache.db.Query(ctx, cache.db.Rebind(`
		SELECT last_net, id, address, last_ip_port
		FROM nodes
		WHERE id = any($1::bytea[])
			AND disqualified IS NULL
			AND exit_finished_at IS NULL
			AND last_contact_success > $2
	`), pgutil.NodeIDArray(nodeIDs), time.Now().Add(-onlineWindow))
	if err != nil {
		return nil, err
	}
	defer func() { err = errs.Combine(err, rows.Close()) }()

	nodes := make(map[storj.NodeID]*overlay.SelectedNode)
	for rows.Next() {
		var node overlay.SelectedNode
		node.Address = &pb.NodeAddress{Transport: pb.NodeTransport_TCP_TLS_GRPC}

		var lastIPPort sql.NullString
		err = rows.Scan(&node.LastNet, &node.ID, &node.Address.Address, &lastIPPort)
		if err != nil {
			return nil, err
		}
		if lastIPPort.Valid {
			node.LastIPPort = lastIPPort.String
		}

		nodes[node.ID] = &node
	}

	return nodes, Error.Wrap(rows.Err())
}

// KnownOffline filters a set of nodes to offline nodes.
func (cache *overlaycache) KnownOffline(ctx context.Context, criteria *overlay.NodeCriteria, nodeIDs storj.NodeIDList) (offlineNodes storj.NodeIDList, err error) {
	for {
		offlineNodes, err = cache.knownOffline(ctx, criteria, nodeIDs)
		if err != nil {
			if cockroachutil.NeedsRetry(err) {
				continue
			}
			return offlineNodes, err
		}
		break
	}

	return offlineNodes, err
}

func (cache *overlaycache) knownOffline(ctx context.Context, criteria *overlay.NodeCriteria, nodeIds storj.NodeIDList) (offlineNodes storj.NodeIDList, err error) {
	defer mon.Task()(&ctx)(&err)

	if len(nodeIds) == 0 {
		return nil, Error.New("no ids provided")
	}

	asOf := cache.db.AsOfSystemTimeClause(criteria.AsOfSystemTimeInterval)

	// get offline nodes
	var rows tagsql.Rows
	rows, err = cache.db.Query(ctx, cache.db.Rebind(`
		SELECT id FROM nodes `+asOf+`
			WHERE id = any($1::bytea[])
			AND last_contact_success < $2
		`), pgutil.NodeIDArray(nodeIds), time.Now().Add(-criteria.OnlineWindow),
	)
	if err != nil {
		return nil, err
	}
	defer func() { err = errs.Combine(err, rows.Close()) }()

	for rows.Next() {
		var id storj.NodeID
		err = rows.Scan(&id)
		if err != nil {
			return nil, err
		}
		offlineNodes = append(offlineNodes, id)
	}
	return offlineNodes, Error.Wrap(rows.Err())
}

// KnownUnreliableOrOffline filters a set of nodes to unreliable or offlines node, independent of new.
func (cache *overlaycache) KnownUnreliableOrOffline(ctx context.Context, criteria *overlay.NodeCriteria, nodeIDs storj.NodeIDList) (badNodes storj.NodeIDList, err error) {
	for {
		badNodes, err = cache.knownUnreliableOrOffline(ctx, criteria, nodeIDs)
		if err != nil {
			if cockroachutil.NeedsRetry(err) {
				continue
			}
			return badNodes, err
		}
		break
	}

	return badNodes, err
}

func (cache *overlaycache) knownUnreliableOrOffline(ctx context.Context, criteria *overlay.NodeCriteria, nodeIDs storj.NodeIDList) (badNodes storj.NodeIDList, err error) {
	defer mon.Task()(&ctx)(&err)

	if len(nodeIDs) == 0 {
		return nil, Error.New("no ids provided")
	}

	asOf := cache.db.AsOfSystemTimeClause(criteria.AsOfSystemTimeInterval)

	// get reliable and online nodes
	var rows tagsql.Rows
	rows, err = cache.db.Query(ctx, cache.db.Rebind(`
		SELECT id FROM nodes `+asOf+`
			WHERE id = any($1::bytea[])
			AND disqualified IS NULL
			AND unknown_audit_suspended IS NULL
			AND exit_finished_at IS NULL
			AND last_contact_success > $2
		`), pgutil.NodeIDArray(nodeIDs), time.Now().Add(-criteria.OnlineWindow),
	)
	if err != nil {
		return nil, err
	}
	defer func() { err = errs.Combine(err, rows.Close()) }()

	goodNodes := make(map[storj.NodeID]struct{}, len(nodeIDs))
	for rows.Next() {
		var id storj.NodeID
		err = rows.Scan(&id)
		if err != nil {
			return nil, err
		}
		goodNodes[id] = struct{}{}
	}
	for _, id := range nodeIDs {
		if _, ok := goodNodes[id]; !ok {
			badNodes = append(badNodes, id)
		}
	}
	return badNodes, Error.Wrap(rows.Err())
}

// KnownReliable filters a set of nodes to reliable (online and qualified) nodes.
func (cache *overlaycache) KnownReliable(ctx context.Context, onlineWindow time.Duration, nodeIDs storj.NodeIDList) (nodes []*pb.Node, err error) {
	for {
		nodes, err = cache.knownReliable(ctx, onlineWindow, nodeIDs)
		if err != nil {
			if cockroachutil.NeedsRetry(err) {
				continue
			}
			return nodes, err
		}
		break
	}

	return nodes, err
}

func (cache *overlaycache) knownReliable(ctx context.Context, onlineWindow time.Duration, nodeIDs storj.NodeIDList) (nodes []*pb.Node, err error) {
	defer mon.Task()(&ctx)(&err)

	if len(nodeIDs) == 0 {
		return nil, Error.New("no ids provided")
	}

	// get online nodes
	rows, err := cache.db.Query(ctx, cache.db.Rebind(`
		SELECT id, last_net, last_ip_port, address, protocol
			FROM nodes
			WHERE id = any($1::bytea[])
			AND disqualified IS NULL
			AND unknown_audit_suspended IS NULL
			AND exit_finished_at IS NULL
			AND last_contact_success > $2
		`), pgutil.NodeIDArray(nodeIDs), time.Now().Add(-onlineWindow),
	)
	if err != nil {
		return nil, err
	}
	defer func() { err = errs.Combine(err, rows.Close()) }()

	for rows.Next() {
		row := &dbx.Node{}
		err = rows.Scan(&row.Id, &row.LastNet, &row.LastIpPort, &row.Address, &row.Protocol)
		if err != nil {
			return nil, err
		}
		node, err := convertDBNode(ctx, row)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, &node.Node)
	}
	return nodes, Error.Wrap(rows.Err())
}

// Reliable returns all reliable nodes.
func (cache *overlaycache) Reliable(ctx context.Context, criteria *overlay.NodeCriteria) (nodes storj.NodeIDList, err error) {
	for {
		nodes, err = cache.reliable(ctx, criteria)
		if err != nil {
			if cockroachutil.NeedsRetry(err) {
				continue
			}
			return nodes, err
		}
		break
	}

	return nodes, err
}

func (cache *overlaycache) reliable(ctx context.Context, criteria *overlay.NodeCriteria) (nodes storj.NodeIDList, err error) {
	asOf := cache.db.AsOfSystemTimeClause(criteria.AsOfSystemTimeInterval)

	// get reliable and online nodes
	rows, err := cache.db.Query(ctx, cache.db.Rebind(`
		SELECT id FROM nodes `+asOf+`
		WHERE disqualified IS NULL
		AND unknown_audit_suspended IS NULL
		AND exit_finished_at IS NULL
		AND last_contact_success > ?
	`), time.Now().Add(-criteria.OnlineWindow))
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errs.Combine(err, rows.Close())
	}()

	for rows.Next() {
		var id storj.NodeID
		err = rows.Scan(&id)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, id)
	}
	return nodes, Error.Wrap(rows.Err())
}

// BatchUpdateStats updates multiple storagenode's stats in one transaction.
func (cache *overlaycache) BatchUpdateStats(ctx context.Context, updateRequests []*overlay.UpdateRequest, batchSize int, now time.Time) (failed storj.NodeIDList, err error) {
	defer mon.Task()(&ctx)(&err)
	if len(updateRequests) == 0 {
		return failed, nil
	}

	// ensure updates happen in-order
	sort.Slice(updateRequests, func(i, k int) bool {
		return updateRequests[i].NodeID.Less(updateRequests[k].NodeID)
	})

	doUpdate := func(updateSlice []*overlay.UpdateRequest) (duf storj.NodeIDList, err error) {
		appendAll := func() {
			for _, ur := range updateRequests {
				duf = append(duf, ur.NodeID)
			}
		}

		doAppendAll := true
		err = cache.db.WithTx(ctx, func(ctx context.Context, tx *dbx.Tx) (err error) {
			_, err = tx.Tx.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE")
			if err != nil {
				return err
			}
			var allSQL string
			for _, updateReq := range updateSlice {
				dbNode, err := tx.Get_Node_By_Id(ctx, dbx.Node_Id(updateReq.NodeID.Bytes()))
				if err != nil {
					doAppendAll = false
					return err
				}

				// do not update reputation if node is disqualified
				if dbNode.Disqualified != nil {
					continue
				}
				// do not update reputation if node has gracefully exited
				if dbNode.ExitFinishedAt != nil {
					continue
				}

				isUp := updateReq.AuditOutcome != overlay.AuditOffline
				auditHistoryResponse, err := cache.updateAuditHistoryWithTx(ctx, tx, updateReq.NodeID, now, isUp, updateReq.AuditHistory)
				if err != nil {
					doAppendAll = false
					return err
				}

				updateNodeStats := cache.populateUpdateNodeStats(dbNode, updateReq, auditHistoryResponse, now)

				sql := buildUpdateStatement(updateNodeStats)

				allSQL += sql
			}

			if allSQL != "" {
				results, err := tx.Tx.Exec(ctx, allSQL)
				if err != nil {
					return err
				}

				_, err = results.RowsAffected()
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			if doAppendAll {
				appendAll()
			}
			return duf, Error.Wrap(err)
		}
		return duf, nil
	}

	var errlist errs.Group
	length := len(updateRequests)
	for i := 0; i < length; i += batchSize {
		end := i + batchSize
		if end > length {
			end = length
		}

		failedBatch, err := doUpdate(updateRequests[i:end])
		if err != nil && len(failedBatch) > 0 {
			for _, fb := range failedBatch {
				errlist.Add(err)
				failed = append(failed, fb)
			}
		}
	}
	return failed, errlist.Err()
}

// UpdateStats all parts of single storagenode's stats.
func (cache *overlaycache) UpdateStats(ctx context.Context, updateReq *overlay.UpdateRequest, now time.Time) (stats *overlay.NodeStats, err error) {
	defer mon.Task()(&ctx)(&err)
	nodeID := updateReq.NodeID

	var dbNode *dbx.Node
	err = cache.db.WithTx(ctx, func(ctx context.Context, tx *dbx.Tx) (err error) {
		_, err = tx.Tx.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE")
		if err != nil {
			return err
		}
		dbNode, err = tx.Get_Node_By_Id(ctx, dbx.Node_Id(nodeID.Bytes()))
		if err != nil {
			return err
		}
		// do not update reputation if node is disqualified
		if dbNode.Disqualified != nil {
			return nil
		}
		// do not update reputation if node has gracefully exited
		if dbNode.ExitFinishedAt != nil {
			return nil
		}

		isUp := updateReq.AuditOutcome != overlay.AuditOffline
		auditHistoryResponse, err := cache.updateAuditHistoryWithTx(ctx, tx, updateReq.NodeID, now, isUp, updateReq.AuditHistory)
		if err != nil {
			return err
		}

		updateFields := cache.populateUpdateFields(dbNode, updateReq, auditHistoryResponse, now)
		dbNode, err = tx.Update_Node_By_Id(ctx, dbx.Node_Id(nodeID.Bytes()), updateFields)
		if err != nil {
			return err
		}

		// Cleanup containment table too
		_, err = tx.Delete_PendingAudits_By_NodeId(ctx, dbx.PendingAudits_NodeId(nodeID.Bytes()))
		return err
	})
	if err != nil {
		return nil, Error.Wrap(err)
	}

	// TODO: Allegedly tx.Get_Node_By_Id and tx.Update_Node_By_Id should never return a nil value for dbNode,
	// however we've seen from some crashes that it does. We need to track down the cause of these crashes
	// but for now we're adding a nil check to prevent a panic.
	if dbNode == nil {
		return nil, Error.New("unable to get node by ID: %v", nodeID)
	}
	return getNodeStats(dbNode), nil
}

// UpdateNodeInfo updates the following fields for a given node ID:
// wallet, email for node operator, free disk, and version.
func (cache *overlaycache) UpdateNodeInfo(ctx context.Context, nodeID storj.NodeID, nodeInfo *overlay.InfoResponse) (stats *overlay.NodeDossier, err error) {
	defer mon.Task()(&ctx)(&err)

	var updateFields dbx.Node_Update_Fields
	if nodeInfo != nil {
		if nodeInfo.Type != pb.NodeType_INVALID {
			updateFields.Type = dbx.Node_Type(int(nodeInfo.Type))
		}
		if nodeInfo.Operator != nil {
			walletFeatures, err := encodeWalletFeatures(nodeInfo.Operator.GetWalletFeatures())
			if err != nil {
				return nil, Error.Wrap(err)
			}

			updateFields.Wallet = dbx.Node_Wallet(nodeInfo.Operator.GetWallet())
			updateFields.Email = dbx.Node_Email(nodeInfo.Operator.GetEmail())
			updateFields.WalletFeatures = dbx.Node_WalletFeatures(walletFeatures)
		}
		if nodeInfo.Capacity != nil {
			updateFields.FreeDisk = dbx.Node_FreeDisk(nodeInfo.Capacity.GetFreeDisk())
		}
		if nodeInfo.Version != nil {
			semVer, err := version.NewSemVer(nodeInfo.Version.GetVersion())
			if err != nil {
				return nil, errs.New("unable to convert version to semVer")
			}
			updateFields.Major = dbx.Node_Major(int64(semVer.Major))
			updateFields.Minor = dbx.Node_Minor(int64(semVer.Minor))
			updateFields.Patch = dbx.Node_Patch(int64(semVer.Patch))
			updateFields.Hash = dbx.Node_Hash(nodeInfo.Version.GetCommitHash())
			updateFields.Timestamp = dbx.Node_Timestamp(nodeInfo.Version.Timestamp)
			updateFields.Release = dbx.Node_Release(nodeInfo.Version.GetRelease())
		}
	}

	updatedDBNode, err := cache.db.Update_Node_By_Id(ctx, dbx.Node_Id(nodeID.Bytes()), updateFields)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	return convertDBNode(ctx, updatedDBNode)
}

// DisqualifyNode disqualifies a storage node.
func (cache *overlaycache) DisqualifyNode(ctx context.Context, nodeID storj.NodeID) (err error) {
	defer mon.Task()(&ctx)(&err)
	updateFields := dbx.Node_Update_Fields{}
	updateFields.Disqualified = dbx.Node_Disqualified(time.Now().UTC())

	dbNode, err := cache.db.Update_Node_By_Id(ctx, dbx.Node_Id(nodeID.Bytes()), updateFields)
	if err != nil {
		return err
	}
	if dbNode == nil {
		return errs.New("unable to get node by ID: %v", nodeID)
	}
	return nil
}

// SuspendNodeUnknownAudit suspends a storage node for unknown audits.
func (cache *overlaycache) SuspendNodeUnknownAudit(ctx context.Context, nodeID storj.NodeID, suspendedAt time.Time) (err error) {
	defer mon.Task()(&ctx)(&err)
	updateFields := dbx.Node_Update_Fields{}
	updateFields.UnknownAuditSuspended = dbx.Node_UnknownAuditSuspended(suspendedAt.UTC())

	dbNode, err := cache.db.Update_Node_By_Id(ctx, dbx.Node_Id(nodeID.Bytes()), updateFields)
	if err != nil {
		return err
	}
	if dbNode == nil {
		return errs.New("unable to get node by ID: %v", nodeID)
	}
	return nil
}

// UnsuspendNodeUnknownAudit unsuspends a storage node for unknown audits.
func (cache *overlaycache) UnsuspendNodeUnknownAudit(ctx context.Context, nodeID storj.NodeID) (err error) {
	defer mon.Task()(&ctx)(&err)
	updateFields := dbx.Node_Update_Fields{}
	updateFields.UnknownAuditSuspended = dbx.Node_UnknownAuditSuspended_Null()

	dbNode, err := cache.db.Update_Node_By_Id(ctx, dbx.Node_Id(nodeID.Bytes()), updateFields)
	if err != nil {
		return err
	}
	if dbNode == nil {
		return errs.New("unable to get node by ID: %v", nodeID)
	}
	return nil
}

// AllPieceCounts returns a map of node IDs to piece counts from the db.
// NB: a valid, partial piece map can be returned even if node ID parsing error(s) are returned.
func (cache *overlaycache) AllPieceCounts(ctx context.Context) (_ map[storj.NodeID]int, err error) {
	defer mon.Task()(&ctx)(&err)

	// NB: `All_Node_Id_Node_PieceCount_By_PieceCount_Not_Number` selects node
	// ID and piece count from the nodes table where piece count is not zero.
	rows, err := cache.db.All_Node_Id_Node_PieceCount_By_PieceCount_Not_Number(ctx)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	pieceCounts := make(map[storj.NodeID]int)
	nodeIDErrs := errs.Group{}
	for _, row := range rows {
		nodeID, err := storj.NodeIDFromBytes(row.Id)
		if err != nil {
			nodeIDErrs.Add(err)
			continue
		}
		pieceCounts[nodeID] = int(row.PieceCount)
	}

	return pieceCounts, nodeIDErrs.Err()
}

func (cache *overlaycache) UpdatePieceCounts(ctx context.Context, pieceCounts map[storj.NodeID]int) (err error) {
	defer mon.Task()(&ctx)(&err)
	if len(pieceCounts) == 0 {
		return nil
	}

	// TODO: pass in the apprioriate struct to database, rather than constructing it here
	type NodeCount struct {
		ID    storj.NodeID
		Count int64
	}
	var counts []NodeCount

	for nodeid, count := range pieceCounts {
		counts = append(counts, NodeCount{
			ID:    nodeid,
			Count: int64(count),
		})
	}
	sort.Slice(counts, func(i, k int) bool {
		return counts[i].ID.Less(counts[k].ID)
	})

	var nodeIDs []storj.NodeID
	var countNumbers []int64
	for _, count := range counts {
		nodeIDs = append(nodeIDs, count.ID)
		countNumbers = append(countNumbers, count.Count)
	}

	_, err = cache.db.ExecContext(ctx, `
		UPDATE nodes
			SET piece_count = update.count
		FROM (
			SELECT unnest($1::bytea[]) as id, unnest($2::bigint[]) as count
		) as update
		WHERE nodes.id = update.id
	`, pgutil.NodeIDArray(nodeIDs), pgutil.Int8Array(countNumbers))

	return Error.Wrap(err)
}

// GetExitingNodes returns nodes who have initiated a graceful exit and is not disqualified, but have not completed it.
func (cache *overlaycache) GetExitingNodes(ctx context.Context) (exitingNodes []*overlay.ExitStatus, err error) {
	for {
		exitingNodes, err = cache.getExitingNodes(ctx)
		if err != nil {
			if cockroachutil.NeedsRetry(err) {
				continue
			}
			return exitingNodes, err
		}
		break
	}

	return exitingNodes, err
}

func (cache *overlaycache) getExitingNodes(ctx context.Context) (exitingNodes []*overlay.ExitStatus, err error) {
	defer mon.Task()(&ctx)(&err)

	rows, err := cache.db.Query(ctx, cache.db.Rebind(`
		SELECT id, exit_initiated_at, exit_loop_completed_at, exit_finished_at, exit_success FROM nodes
		WHERE exit_initiated_at IS NOT NULL
		AND exit_finished_at IS NULL
		AND disqualified is NULL
	`))
	if err != nil {
		return nil, err
	}
	defer func() { err = errs.Combine(err, rows.Close()) }()

	for rows.Next() {
		var exitingNodeStatus overlay.ExitStatus
		err = rows.Scan(&exitingNodeStatus.NodeID, &exitingNodeStatus.ExitInitiatedAt, &exitingNodeStatus.ExitLoopCompletedAt, &exitingNodeStatus.ExitFinishedAt, &exitingNodeStatus.ExitSuccess)
		if err != nil {
			return nil, err
		}
		exitingNodes = append(exitingNodes, &exitingNodeStatus)
	}
	return exitingNodes, Error.Wrap(rows.Err())
}

// GetExitStatus returns a node's graceful exit status.
func (cache *overlaycache) GetExitStatus(ctx context.Context, nodeID storj.NodeID) (exitStatus *overlay.ExitStatus, err error) {
	for {
		exitStatus, err = cache.getExitStatus(ctx, nodeID)
		if err != nil {
			if cockroachutil.NeedsRetry(err) {
				continue
			}
			return exitStatus, err
		}
		break
	}

	return exitStatus, err
}

func (cache *overlaycache) getExitStatus(ctx context.Context, nodeID storj.NodeID) (_ *overlay.ExitStatus, err error) {
	defer mon.Task()(&ctx)(&err)

	rows, err := cache.db.Query(ctx, cache.db.Rebind(`
		SELECT id, exit_initiated_at, exit_loop_completed_at, exit_finished_at, exit_success
		FROM nodes
		WHERE id = ?
	`), nodeID)
	if err != nil {
		return nil, Error.Wrap(err)
	}
	defer func() { err = errs.Combine(err, rows.Close()) }()

	exitStatus := &overlay.ExitStatus{}
	if rows.Next() {
		err = rows.Scan(&exitStatus.NodeID, &exitStatus.ExitInitiatedAt, &exitStatus.ExitLoopCompletedAt, &exitStatus.ExitFinishedAt, &exitStatus.ExitSuccess)
		if err != nil {
			return nil, err
		}
	}

	return exitStatus, Error.Wrap(rows.Err())
}

// GetGracefulExitCompletedByTimeFrame returns nodes who have completed graceful exit within a time window (time window is around graceful exit completion).
func (cache *overlaycache) GetGracefulExitCompletedByTimeFrame(ctx context.Context, begin, end time.Time) (exitedNodes storj.NodeIDList, err error) {
	for {
		exitedNodes, err = cache.getGracefulExitCompletedByTimeFrame(ctx, begin, end)
		if err != nil {
			if cockroachutil.NeedsRetry(err) {
				continue
			}
			return exitedNodes, err
		}
		break
	}

	return exitedNodes, err
}

func (cache *overlaycache) getGracefulExitCompletedByTimeFrame(ctx context.Context, begin, end time.Time) (exitedNodes storj.NodeIDList, err error) {
	defer mon.Task()(&ctx)(&err)

	rows, err := cache.db.Query(ctx, cache.db.Rebind(`
		SELECT id FROM nodes
		WHERE exit_initiated_at IS NOT NULL
		AND exit_finished_at IS NOT NULL
		AND exit_finished_at >= ?
		AND exit_finished_at < ?
	`), begin, end)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errs.Combine(err, rows.Close())
	}()

	for rows.Next() {
		var id storj.NodeID
		err = rows.Scan(&id)
		if err != nil {
			return nil, err
		}
		exitedNodes = append(exitedNodes, id)
	}
	return exitedNodes, Error.Wrap(rows.Err())
}

// GetGracefulExitIncompleteByTimeFrame returns nodes who have initiated, but not completed graceful exit within a time window (time window is around graceful exit initiation).
func (cache *overlaycache) GetGracefulExitIncompleteByTimeFrame(ctx context.Context, begin, end time.Time) (exitingNodes storj.NodeIDList, err error) {
	for {
		exitingNodes, err = cache.getGracefulExitIncompleteByTimeFrame(ctx, begin, end)
		if err != nil {
			if cockroachutil.NeedsRetry(err) {
				continue
			}
			return exitingNodes, err
		}
		break
	}

	return exitingNodes, err
}

func (cache *overlaycache) getGracefulExitIncompleteByTimeFrame(ctx context.Context, begin, end time.Time) (exitingNodes storj.NodeIDList, err error) {
	defer mon.Task()(&ctx)(&err)

	rows, err := cache.db.Query(ctx, cache.db.Rebind(`
		SELECT id FROM nodes
		WHERE exit_initiated_at IS NOT NULL
		AND exit_finished_at IS NULL
		AND exit_initiated_at >= ?
		AND exit_initiated_at < ?
	`), begin, end)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errs.Combine(err, rows.Close())
	}()

	// TODO return more than just ID
	for rows.Next() {
		var id storj.NodeID
		err = rows.Scan(&id)
		if err != nil {
			return nil, err
		}
		exitingNodes = append(exitingNodes, id)
	}
	return exitingNodes, Error.Wrap(rows.Err())
}

// UpdateExitStatus is used to update a node's graceful exit status.
func (cache *overlaycache) UpdateExitStatus(ctx context.Context, request *overlay.ExitStatusRequest) (_ *overlay.NodeDossier, err error) {
	defer mon.Task()(&ctx)(&err)

	nodeID := request.NodeID

	updateFields := populateExitStatusFields(request)

	dbNode, err := cache.db.Update_Node_By_Id(ctx, dbx.Node_Id(nodeID.Bytes()), updateFields)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	if dbNode == nil {
		return nil, Error.Wrap(errs.New("unable to get node by ID: %v", nodeID))
	}

	return convertDBNode(ctx, dbNode)
}

func populateExitStatusFields(req *overlay.ExitStatusRequest) dbx.Node_Update_Fields {
	dbxUpdateFields := dbx.Node_Update_Fields{}

	if !req.ExitInitiatedAt.IsZero() {
		dbxUpdateFields.ExitInitiatedAt = dbx.Node_ExitInitiatedAt(req.ExitInitiatedAt)
	}
	if !req.ExitLoopCompletedAt.IsZero() {
		dbxUpdateFields.ExitLoopCompletedAt = dbx.Node_ExitLoopCompletedAt(req.ExitLoopCompletedAt)
	}
	if !req.ExitFinishedAt.IsZero() {
		dbxUpdateFields.ExitFinishedAt = dbx.Node_ExitFinishedAt(req.ExitFinishedAt)
	}
	dbxUpdateFields.ExitSuccess = dbx.Node_ExitSuccess(req.ExitSuccess)

	return dbxUpdateFields
}

func convertDBNode(ctx context.Context, info *dbx.Node) (_ *overlay.NodeDossier, err error) {
	if info == nil {
		return nil, Error.New("missing info")
	}

	id, err := storj.NodeIDFromBytes(info.Id)
	if err != nil {
		return nil, err
	}
	ver, err := version.NewSemVer(fmt.Sprintf("%d.%d.%d", info.Major, info.Minor, info.Patch))
	if err != nil {
		return nil, err
	}

	exitStatus := overlay.ExitStatus{NodeID: id}
	exitStatus.ExitInitiatedAt = info.ExitInitiatedAt
	exitStatus.ExitLoopCompletedAt = info.ExitLoopCompletedAt
	exitStatus.ExitFinishedAt = info.ExitFinishedAt
	exitStatus.ExitSuccess = info.ExitSuccess

	node := &overlay.NodeDossier{
		Node: pb.Node{
			Id: id,
			Address: &pb.NodeAddress{
				Address:   info.Address,
				Transport: pb.NodeTransport(info.Protocol),
			},
		},
		Type: pb.NodeType(info.Type),
		Operator: pb.NodeOperator{
			Email:          info.Email,
			Wallet:         info.Wallet,
			WalletFeatures: decodeWalletFeatures(info.WalletFeatures),
		},
		Capacity: pb.NodeCapacity{
			FreeDisk: info.FreeDisk,
		},
		Reputation: *getNodeStats(info),
		Version: pb.NodeVersion{
			Version:    ver.String(),
			CommitHash: info.Hash,
			Timestamp:  info.Timestamp,
			Release:    info.Release,
		},
		Contained:             info.Contained,
		Disqualified:          info.Disqualified,
		UnknownAuditSuspended: info.UnknownAuditSuspended,
		OfflineSuspended:      info.OfflineSuspended,
		OfflineUnderReview:    info.UnderReview,
		PieceCount:            info.PieceCount,
		ExitStatus:            exitStatus,
		CreatedAt:             info.CreatedAt,
		LastNet:               info.LastNet,
	}
	if info.LastIpPort != nil {
		node.LastIPPort = *info.LastIpPort
	}

	return node, nil
}

// encodeWalletFeatures encodes wallet features into comma separated list string.
func encodeWalletFeatures(features []string) (string, error) {
	var errGroup errs.Group

	for _, feature := range features {
		if strings.Contains(feature, ",") {
			errGroup.Add(errs.New("error encoding %s, can not contain separator \",\"", feature))
		}
	}
	if err := errGroup.Err(); err != nil {
		return "", Error.Wrap(err)
	}

	return strings.Join(features, ","), nil
}

// decodeWalletFeatures decodes comma separated wallet features list string.
func decodeWalletFeatures(encoded string) []string {
	if encoded == "" {
		return nil
	}

	return strings.Split(encoded, ",")
}

func getNodeStats(dbNode *dbx.Node) *overlay.NodeStats {
	nodeStats := &overlay.NodeStats{
		Latency90:                   dbNode.Latency90,
		VettedAt:                    dbNode.VettedAt,
		AuditCount:                  dbNode.TotalAuditCount,
		AuditSuccessCount:           dbNode.AuditSuccessCount,
		LastContactSuccess:          dbNode.LastContactSuccess,
		LastContactFailure:          dbNode.LastContactFailure,
		AuditReputationAlpha:        dbNode.AuditReputationAlpha,
		AuditReputationBeta:         dbNode.AuditReputationBeta,
		Disqualified:                dbNode.Disqualified,
		UnknownAuditReputationAlpha: dbNode.UnknownAuditReputationAlpha,
		UnknownAuditReputationBeta:  dbNode.UnknownAuditReputationBeta,
		UnknownAuditSuspended:       dbNode.UnknownAuditSuspended,
		OfflineUnderReview:          dbNode.UnderReview,
		OfflineSuspended:            dbNode.OfflineSuspended,
		OnlineScore:                 dbNode.OnlineScore,
	}
	return nodeStats
}

// updateReputation uses the Beta distribution model to determine a node's reputation.
// lambda is the "forgetting factor" which determines how much past info is kept when determining current reputation score.
// w is the normalization weight that affects how severely new updates affect the current reputation distribution.
func updateReputation(isSuccess bool, alpha, beta, lambda, w float64, totalCount int64) (newAlpha, newBeta float64, updatedCount int64) {
	// v is a single feedback value that allows us to update both alpha and beta
	var v float64 = -1
	if isSuccess {
		v = 1
	}
	newAlpha = lambda*alpha + w*(1+v)/2
	newBeta = lambda*beta + w*(1-v)/2
	return newAlpha, newBeta, totalCount + 1
}

func buildUpdateStatement(update updateNodeStats) string {
	if update.NodeID.IsZero() {
		return ""
	}
	atLeastOne := false
	sql := "UPDATE nodes SET "
	if update.VettedAt.set {
		atLeastOne = true
		sql += fmt.Sprintf("vetted_at = '%s'", update.VettedAt.value.Format(time.RFC3339Nano))
	}
	if update.TotalAuditCount.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		sql += fmt.Sprintf("total_audit_count = %d", update.TotalAuditCount.value)
	}
	if update.AuditReputationAlpha.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		sql += fmt.Sprintf("audit_reputation_alpha = %f", update.AuditReputationAlpha.value)
	}
	if update.AuditReputationBeta.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		sql += fmt.Sprintf("audit_reputation_beta = %f", update.AuditReputationBeta.value)
	}
	if update.UnknownAuditReputationAlpha.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		sql += fmt.Sprintf("unknown_audit_reputation_alpha = %f", update.UnknownAuditReputationAlpha.value)
	}
	if update.UnknownAuditReputationBeta.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		sql += fmt.Sprintf("unknown_audit_reputation_beta = %f", update.UnknownAuditReputationBeta.value)
	}
	if update.Disqualified.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		sql += fmt.Sprintf("disqualified = '%s'", update.Disqualified.value.Format(time.RFC3339Nano))
	}
	if update.UnknownAuditSuspended.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		if update.UnknownAuditSuspended.isNil {
			sql += "unknown_audit_suspended = NULL"
		} else {
			sql += fmt.Sprintf("unknown_audit_suspended = '%s'", update.UnknownAuditSuspended.value.Format(time.RFC3339Nano))
		}
	}
	if update.LastContactSuccess.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		sql += fmt.Sprintf("last_contact_success = '%s'", update.LastContactSuccess.value.Format(time.RFC3339Nano))
	}
	if update.LastContactFailure.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		sql += fmt.Sprintf("last_contact_failure = '%s'", update.LastContactFailure.value.Format(time.RFC3339Nano))
	}
	if update.AuditSuccessCount.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		sql += fmt.Sprintf("audit_success_count = %d", update.AuditSuccessCount.value)
	}
	if update.Contained.set {
		if atLeastOne {
			sql += ","
		}

		atLeastOne = true
		sql += fmt.Sprintf("contained = %t", update.Contained.value)
	}
	if update.OnlineScore.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		sql += fmt.Sprintf("online_score = %f", update.OnlineScore.value)
	}
	if update.OfflineUnderReview.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		if update.OfflineUnderReview.isNil {
			sql += "under_review = NULL"
		} else {
			sql += fmt.Sprintf("under_review = '%s'", update.OfflineUnderReview.value.Format(time.RFC3339Nano))
		}
	}
	if update.OfflineSuspended.set {
		if atLeastOne {
			sql += ","
		}
		atLeastOne = true
		if update.OfflineSuspended.isNil {
			sql += "offline_suspended = NULL"
		} else {
			sql += fmt.Sprintf("offline_suspended = '%s'", update.OfflineSuspended.value.Format(time.RFC3339Nano))
		}
	}
	if !atLeastOne {
		return ""
	}
	hexNodeID := hex.EncodeToString(update.NodeID.Bytes())

	sql += fmt.Sprintf(" WHERE nodes.id = decode('%v', 'hex');\n", hexNodeID)
	sql += fmt.Sprintf("DELETE FROM pending_audits WHERE pending_audits.node_id = decode('%v', 'hex');\n", hexNodeID)

	return sql
}

type int64Field struct {
	set   bool
	value int64
}

type float64Field struct {
	set   bool
	value float64
}

type boolField struct {
	set   bool
	value bool
}

type timeField struct {
	set   bool
	isNil bool
	value time.Time
}

type updateNodeStats struct {
	NodeID                      storj.NodeID
	VettedAt                    timeField
	TotalAuditCount             int64Field
	AuditReputationAlpha        float64Field
	AuditReputationBeta         float64Field
	Disqualified                timeField
	UnknownAuditReputationAlpha float64Field
	UnknownAuditReputationBeta  float64Field
	UnknownAuditSuspended       timeField
	LastContactSuccess          timeField
	LastContactFailure          timeField
	AuditSuccessCount           int64Field
	Contained                   boolField
	OfflineUnderReview          timeField
	OfflineSuspended            timeField
	OnlineScore                 float64Field
}

func (cache *overlaycache) populateUpdateNodeStats(dbNode *dbx.Node, updateReq *overlay.UpdateRequest, auditHistoryResponse *overlay.UpdateAuditHistoryResponse, now time.Time) updateNodeStats {
	// there are three audit outcomes: success, failure, and unknown
	// if a node fails enough audits, it gets disqualified
	// if a node gets enough "unknown" audits, it gets put into suspension
	// if a node gets enough successful audits, and is in suspension, it gets removed from suspension
	auditAlpha := dbNode.AuditReputationAlpha
	auditBeta := dbNode.AuditReputationBeta
	unknownAuditAlpha := dbNode.UnknownAuditReputationAlpha
	unknownAuditBeta := dbNode.UnknownAuditReputationBeta
	totalAuditCount := dbNode.TotalAuditCount
	vettedAt := dbNode.VettedAt

	var updatedTotalAuditCount int64

	switch updateReq.AuditOutcome {
	case overlay.AuditSuccess:
		// for a successful audit, increase reputation for normal *and* unknown audits
		auditAlpha, auditBeta, updatedTotalAuditCount = updateReputation(
			true,
			auditAlpha,
			auditBeta,
			updateReq.AuditLambda,
			updateReq.AuditWeight,
			totalAuditCount,
		)
		// we will use updatedTotalAuditCount from the updateReputation call above
		unknownAuditAlpha, unknownAuditBeta, _ = updateReputation(
			true,
			unknownAuditAlpha,
			unknownAuditBeta,
			updateReq.AuditLambda,
			updateReq.AuditWeight,
			totalAuditCount,
		)
	case overlay.AuditFailure:
		// for audit failure, only update normal alpha/beta
		auditAlpha, auditBeta, updatedTotalAuditCount = updateReputation(
			false,
			auditAlpha,
			auditBeta,
			updateReq.AuditLambda,
			updateReq.AuditWeight,
			totalAuditCount,
		)
	case overlay.AuditUnknown:
		// for audit unknown, only update unknown alpha/beta
		unknownAuditAlpha, unknownAuditBeta, updatedTotalAuditCount = updateReputation(
			false,
			unknownAuditAlpha,
			unknownAuditBeta,
			updateReq.AuditLambda,
			updateReq.AuditWeight,
			totalAuditCount,
		)
	case overlay.AuditOffline:
		// for audit offline, only update total audit count
		updatedTotalAuditCount = totalAuditCount + 1
	}

	mon.FloatVal("audit_reputation_alpha").Observe(auditAlpha)                //mon:locked
	mon.FloatVal("audit_reputation_beta").Observe(auditBeta)                  //mon:locked
	mon.FloatVal("unknown_audit_reputation_alpha").Observe(unknownAuditAlpha) //mon:locked
	mon.FloatVal("unknown_audit_reputation_beta").Observe(unknownAuditBeta)   //mon:locked
	mon.FloatVal("audit_online_score").Observe(auditHistoryResponse.NewScore) //mon:locked

	isUp := updateReq.AuditOutcome != overlay.AuditOffline

	updateFields := updateNodeStats{
		NodeID:                      updateReq.NodeID,
		TotalAuditCount:             int64Field{set: true, value: updatedTotalAuditCount},
		AuditReputationAlpha:        float64Field{set: true, value: auditAlpha},
		AuditReputationBeta:         float64Field{set: true, value: auditBeta},
		UnknownAuditReputationAlpha: float64Field{set: true, value: unknownAuditAlpha},
		UnknownAuditReputationBeta:  float64Field{set: true, value: unknownAuditBeta},
	}

	if vettedAt == nil && updatedTotalAuditCount >= updateReq.AuditsRequiredForVetting {
		updateFields.VettedAt = timeField{set: true, value: now}
	}

	// disqualification case a
	//   a) Success/fail audit reputation falls below audit DQ threshold
	auditRep := auditAlpha / (auditAlpha + auditBeta)
	if auditRep <= updateReq.AuditDQ {
		cache.db.log.Info("Disqualified", zap.String("DQ type", "audit failure"), zap.String("Node ID", updateReq.NodeID.String()))
		mon.Meter("bad_audit_dqs").Mark(1) //mon:locked
		updateFields.Disqualified = timeField{set: true, value: now}
	}

	// if unknown audit rep goes below threshold, suspend node. Otherwise unsuspend node.
	unknownAuditRep := unknownAuditAlpha / (unknownAuditAlpha + unknownAuditBeta)
	if unknownAuditRep <= updateReq.AuditDQ {
		if dbNode.UnknownAuditSuspended == nil {
			cache.db.log.Info("Suspended", zap.String("Node ID", updateFields.NodeID.String()), zap.String("Category", "Unknown Audits"))
			updateFields.UnknownAuditSuspended = timeField{set: true, value: now}
		}

		// disqualification case b
		//   b) Node is suspended (success/unknown reputation below audit DQ threshold)
		//        AND the suspended grace period has elapsed
		//        AND audit outcome is unknown or failed

		// if suspended grace period has elapsed and audit outcome was failed or unknown,
		// disqualify node. Set suspended to nil if node is disqualified
		// NOTE: if updateFields.Suspended is set, we just suspended the node so it will not be disqualified
		if updateReq.AuditOutcome != overlay.AuditSuccess {
			if dbNode.UnknownAuditSuspended != nil && !updateFields.UnknownAuditSuspended.set &&
				time.Since(*dbNode.UnknownAuditSuspended) > updateReq.SuspensionGracePeriod &&
				updateReq.SuspensionDQEnabled {
				cache.db.log.Info("Disqualified", zap.String("DQ type", "suspension grace period expired for unknown audits"), zap.String("Node ID", updateReq.NodeID.String()))
				mon.Meter("unknown_suspension_dqs").Mark(1) //mon:locked
				updateFields.Disqualified = timeField{set: true, value: now}
				updateFields.UnknownAuditSuspended = timeField{set: true, isNil: true}
			}
		}
	} else if dbNode.UnknownAuditSuspended != nil {
		cache.db.log.Info("Suspension lifted", zap.String("Category", "Unknown Audits"), zap.String("Node ID", updateFields.NodeID.String()))
		updateFields.UnknownAuditSuspended = timeField{set: true, isNil: true}
	}

	if isUp {
		updateFields.LastContactSuccess = timeField{set: true, value: now}
	} else {
		updateFields.LastContactFailure = timeField{set: true, value: now}
	}

	if updateReq.AuditOutcome == overlay.AuditSuccess {
		updateFields.AuditSuccessCount = int64Field{set: true, value: dbNode.AuditSuccessCount + 1}
	}

	// Updating node stats always exits it from containment mode
	updateFields.Contained = boolField{set: true, value: false}

	// only penalize node if online score is below threshold and
	// if it has enough completed windows to fill a tracking period
	penalizeOfflineNode := false
	if auditHistoryResponse.NewScore < updateReq.AuditHistory.OfflineThreshold && auditHistoryResponse.TrackingPeriodFull {
		penalizeOfflineNode = true
	}

	// always update online score
	updateFields.OnlineScore = float64Field{set: true, value: auditHistoryResponse.NewScore}

	// Suspension and disqualification for offline nodes
	if dbNode.UnderReview != nil {
		// move node in and out of suspension as needed during review period
		if !penalizeOfflineNode && dbNode.OfflineSuspended != nil {
			updateFields.OfflineSuspended = timeField{set: true, isNil: true}
		} else if penalizeOfflineNode && dbNode.OfflineSuspended == nil {
			updateFields.OfflineSuspended = timeField{set: true, value: now}
		}

		gracePeriodEnd := dbNode.UnderReview.Add(updateReq.AuditHistory.GracePeriod)
		trackingPeriodEnd := gracePeriodEnd.Add(updateReq.AuditHistory.TrackingPeriod)
		trackingPeriodPassed := now.After(trackingPeriodEnd)

		// after tracking period has elapsed, if score is good, clear under review
		// otherwise, disqualify node (if OfflineDQEnabled feature flag is true)
		if trackingPeriodPassed {
			if penalizeOfflineNode {
				if updateReq.AuditHistory.OfflineDQEnabled {
					cache.db.log.Info("Disqualified", zap.String("DQ type", "node offline"), zap.String("Node ID", updateReq.NodeID.String()))
					mon.Meter("offline_dqs").Mark(1) //mon:locked
					updateFields.Disqualified = timeField{set: true, value: now}
				}
			} else {
				updateFields.OfflineUnderReview = timeField{set: true, isNil: true}
				updateFields.OfflineSuspended = timeField{set: true, isNil: true}
			}
		}
	} else if penalizeOfflineNode {
		// suspend node for being offline and begin review period
		updateFields.OfflineUnderReview = timeField{set: true, value: now}
		updateFields.OfflineSuspended = timeField{set: true, value: now}
	}

	return updateFields
}

func (cache *overlaycache) populateUpdateFields(dbNode *dbx.Node, updateReq *overlay.UpdateRequest, auditHistoryResponse *overlay.UpdateAuditHistoryResponse, now time.Time) dbx.Node_Update_Fields {

	update := cache.populateUpdateNodeStats(dbNode, updateReq, auditHistoryResponse, now)
	updateFields := dbx.Node_Update_Fields{}
	if update.VettedAt.set {
		updateFields.VettedAt = dbx.Node_VettedAt(update.VettedAt.value)
	}
	if update.TotalAuditCount.set {
		updateFields.TotalAuditCount = dbx.Node_TotalAuditCount(update.TotalAuditCount.value)
	}
	if update.AuditReputationAlpha.set {
		updateFields.AuditReputationAlpha = dbx.Node_AuditReputationAlpha(update.AuditReputationAlpha.value)
	}
	if update.AuditReputationBeta.set {
		updateFields.AuditReputationBeta = dbx.Node_AuditReputationBeta(update.AuditReputationBeta.value)
	}
	if update.Disqualified.set {
		updateFields.Disqualified = dbx.Node_Disqualified(update.Disqualified.value)
	}
	if update.UnknownAuditReputationAlpha.set {
		updateFields.UnknownAuditReputationAlpha = dbx.Node_UnknownAuditReputationAlpha(update.UnknownAuditReputationAlpha.value)
	}
	if update.UnknownAuditReputationBeta.set {
		updateFields.UnknownAuditReputationBeta = dbx.Node_UnknownAuditReputationBeta(update.UnknownAuditReputationBeta.value)
	}
	if update.UnknownAuditSuspended.set {
		if update.UnknownAuditSuspended.isNil {
			updateFields.UnknownAuditSuspended = dbx.Node_UnknownAuditSuspended_Null()
		} else {
			updateFields.UnknownAuditSuspended = dbx.Node_UnknownAuditSuspended(update.UnknownAuditSuspended.value)
		}
	}
	if update.LastContactSuccess.set {
		updateFields.LastContactSuccess = dbx.Node_LastContactSuccess(update.LastContactSuccess.value)
	}
	if update.LastContactFailure.set {
		updateFields.LastContactFailure = dbx.Node_LastContactFailure(update.LastContactFailure.value)
	}
	if update.AuditSuccessCount.set {
		updateFields.AuditSuccessCount = dbx.Node_AuditSuccessCount(update.AuditSuccessCount.value)
	}
	if update.Contained.set {
		updateFields.Contained = dbx.Node_Contained(update.Contained.value)
	}
	if updateReq.AuditOutcome == overlay.AuditSuccess {
		updateFields.AuditSuccessCount = dbx.Node_AuditSuccessCount(dbNode.AuditSuccessCount + 1)
	}

	if update.OnlineScore.set {
		updateFields.OnlineScore = dbx.Node_OnlineScore(update.OnlineScore.value)
	}
	if update.OfflineSuspended.set {
		if update.OfflineSuspended.isNil {
			updateFields.OfflineSuspended = dbx.Node_OfflineSuspended_Null()
		} else {
			updateFields.OfflineSuspended = dbx.Node_OfflineSuspended(update.OfflineSuspended.value)
		}
	}
	if update.OfflineUnderReview.set {
		if update.OfflineUnderReview.isNil {
			updateFields.UnderReview = dbx.Node_UnderReview_Null()
		} else {
			updateFields.UnderReview = dbx.Node_UnderReview(update.OfflineUnderReview.value)
		}
	}

	return updateFields
}

// DQNodesLastSeenBefore disqualifies all nodes where last_contact_success < cutoff except those already disqualified
// or gracefully exited.
func (cache *overlaycache) DQNodesLastSeenBefore(ctx context.Context, cutoff time.Time) (err error) {
	defer mon.Task()(&ctx)(&err)
	q := `UPDATE nodes SET disqualified = current_timestamp
			WHERE last_contact_success < $1
			AND disqualified is NULL
			AND exit_finished_at is NULL;`
	results, err := cache.db.ExecContext(ctx, q, cutoff)
	if err != nil {
		return err
	}
	n, err := results.RowsAffected()
	if err != nil {
		return err
	}
	mon.IntVal("stray_nodes_dq_count").Observe(n)
	return err
}

// UpdateCheckIn updates a single storagenode with info from when the the node last checked in.
func (cache *overlaycache) UpdateCheckIn(ctx context.Context, node overlay.NodeCheckInInfo, timestamp time.Time, config overlay.NodeSelectionConfig) (err error) {
	defer mon.Task()(&ctx)(&err)

	if node.Address.GetAddress() == "" {
		return Error.New("error UpdateCheckIn: missing the storage node address")
	}

	semVer, err := version.NewSemVer(node.Version.GetVersion())
	if err != nil {
		return Error.New("unable to convert version to semVer")
	}

	walletFeatures, err := encodeWalletFeatures(node.Operator.GetWalletFeatures())
	if err != nil {
		return Error.Wrap(err)
	}

	query := `
			INSERT INTO nodes
			(
				id, address, last_net, protocol, type,
				email, wallet, free_disk,
				last_contact_success,
				last_contact_failure,
				audit_reputation_alpha, audit_reputation_beta,
				unknown_audit_reputation_alpha, unknown_audit_reputation_beta,
				major, minor, patch, hash, timestamp, release,
				last_ip_port,
				wallet_features
			)
			VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, $8,
				CASE WHEN $9::bool IS TRUE THEN $18::timestamptz
					ELSE '0001-01-01 00:00:00+00'::timestamptz
				END,
				CASE WHEN $9::bool IS FALSE THEN $18::timestamptz
					ELSE '0001-01-01 00:00:00+00'::timestamptz
				END,
				$10, $11,
				$10, $11,
				$12, $13, $14, $15, $16, $17,
				$19,
				$20
			)
			ON CONFLICT (id)
			DO UPDATE
			SET
				address=$2,
				last_net=$3,
				protocol=$4,
				email=$6,
				wallet=$7,
				free_disk=$8,
				major=$12, minor=$13, patch=$14, hash=$15, timestamp=$16, release=$17,
				last_contact_success = CASE WHEN $9::bool IS TRUE
					THEN $18::timestamptz
					ELSE nodes.last_contact_success
				END,
				last_contact_failure = CASE WHEN $9::bool IS FALSE
					THEN $18::timestamptz
					ELSE nodes.last_contact_failure
				END,
				last_ip_port=$19,
				wallet_features=$20;
			`
	_, err = cache.db.ExecContext(ctx, query,
		// args $1 - $5
		node.NodeID.Bytes(), node.Address.GetAddress(), node.LastNet, node.Address.GetTransport(), int(pb.NodeType_STORAGE),
		// args $6 - $8
		node.Operator.GetEmail(), node.Operator.GetWallet(), node.Capacity.GetFreeDisk(),
		// args $9
		node.IsUp,
		// args $10 - $11
		1, 0,
		// args $12 - $17
		semVer.Major, semVer.Minor, semVer.Patch, node.Version.GetCommitHash(), node.Version.Timestamp, node.Version.GetRelease(),
		// args $18
		timestamp,
		// args $19
		node.LastIPPort,
		// args $20,
		walletFeatures,
	)
	if err != nil {
		return Error.Wrap(err)
	}

	return nil
}

var (
	// ErrVetting is the error class for the following test methods.
	ErrVetting = errs.Class("vetting error")
)

// TestVetNode directly sets a node's vetted_at timestamp to make testing easier.
func (cache *overlaycache) TestVetNode(ctx context.Context, nodeID storj.NodeID) (vettedTime *time.Time, err error) {
	updateFields := dbx.Node_Update_Fields{
		VettedAt: dbx.Node_VettedAt(time.Now().UTC()),
	}
	node, err := cache.db.Update_Node_By_Id(ctx, dbx.Node_Id(nodeID.Bytes()), updateFields)
	if err != nil {
		return nil, err
	}
	return node.VettedAt, nil
}

// TestUnvetNode directly sets a node's vetted_at timestamp to null to make testing easier.
func (cache *overlaycache) TestUnvetNode(ctx context.Context, nodeID storj.NodeID) (err error) {
	_, err = cache.db.Exec(ctx, `UPDATE nodes SET vetted_at = NULL WHERE nodes.id = $1;`, nodeID)
	if err != nil {
		return err
	}
	_, err = cache.Get(ctx, nodeID)
	return err
}

// IterateAllNodes will call cb on all known nodes (used in restore trash contexts).
func (cache *overlaycache) IterateAllNodes(ctx context.Context, cb func(context.Context, *overlay.SelectedNode) error) (err error) {
	defer mon.Task()(&ctx)(&err)

	var rows tagsql.Rows
	rows, err = cache.db.Query(ctx, cache.db.Rebind(`
		SELECT last_net, id, address, last_ip_port
		FROM nodes
	`))
	if err != nil {
		return Error.Wrap(err)
	}
	defer func() { err = errs.Combine(err, rows.Close()) }()

	for rows.Next() {
		var node overlay.SelectedNode
		node.Address = &pb.NodeAddress{Transport: pb.NodeTransport_TCP_TLS_GRPC}

		var lastIPPort sql.NullString
		err = rows.Scan(&node.LastNet, &node.ID, &node.Address.Address, &lastIPPort)
		if err != nil {
			return Error.Wrap(err)
		}
		if lastIPPort.Valid {
			node.LastIPPort = lastIPPort.String
		}

		err = cb(ctx, &node)
		if err != nil {
			return err
		}
	}

	return rows.Err()
}
