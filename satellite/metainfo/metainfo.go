// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package metainfo

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/skyrings/skyring-common/tools/uuid"
	"github.com/spacemonkeygo/monkit/v3"
	"github.com/zeebo/errs"
	"go.uber.org/zap"

	"storj.io/common/errs2"
	"storj.io/common/identity"
	"storj.io/common/pb"
	"storj.io/common/rpc/rpcstatus"
	"storj.io/common/signing"
	"storj.io/common/storj"
	lrucache "storj.io/storj/pkg/cache"
	"storj.io/storj/pkg/macaroon"
	"storj.io/storj/private/context2"
	"storj.io/storj/private/dbutil"
	"storj.io/storj/satellite/accounting"
	"storj.io/storj/satellite/attribution"
	"storj.io/storj/satellite/console"
	"storj.io/storj/satellite/orders"
	"storj.io/storj/satellite/overlay"
	"storj.io/storj/satellite/rewards"
	"storj.io/uplink/eestream"
	"storj.io/uplink/storage/meta"
)

const (
	pieceHashExpiration = 24 * time.Hour
	satIDExpiration     = 24 * time.Hour
	lastSegment         = -1
	listLimit           = 1000

	deleteObjectPiecesSuccessThreshold = 0.75
)

var (
	mon = monkit.Package()
	// Error general metainfo error
	Error = errs.Class("metainfo error")
	// ErrNodeAlreadyExists pointer already has a piece for a node err
	ErrNodeAlreadyExists = errs.Class("metainfo error: node already exists")
)

// APIKeys is api keys store methods used by endpoint
//
// architecture: Database
type APIKeys interface {
	GetByHead(ctx context.Context, head []byte) (*console.APIKeyInfo, error)
}

// Revocations is the revocations store methods used by the endpoint
//
// architecture: Database
type Revocations interface {
	GetByProjectID(ctx context.Context, projectID uuid.UUID) ([][]byte, error)
}

// Endpoint metainfo endpoint.
//
// architecture: Endpoint
type Endpoint struct {
	log               *zap.Logger
	metainfo          *Service
	deletePieces      *DeletePiecesService
	orders            *orders.Service
	overlay           *overlay.Service
	attributions      attribution.DB
	partners          *rewards.PartnersService
	peerIdentities    overlay.PeerIdentities
	projectUsage      *accounting.Service
	projects          console.Projects
	apiKeys           APIKeys
	createRequests    *createRequests
	requiredRSConfig  RSConfig
	satellite         signing.Signer
	maxCommitInterval time.Duration
	limiterCache      *lrucache.ExpiringLRU
	limiterConfig     RateLimiterConfig
}

// NewEndpoint creates new metainfo endpoint instance.
func NewEndpoint(log *zap.Logger, metainfo *Service, deletePieces *DeletePiecesService,
	orders *orders.Service, cache *overlay.Service, attributions attribution.DB,
	partners *rewards.PartnersService, peerIdentities overlay.PeerIdentities,
	apiKeys APIKeys, projectUsage *accounting.Service, projects console.Projects,
	rsConfig RSConfig, satellite signing.Signer, maxCommitInterval time.Duration,
	limiterConfig RateLimiterConfig) *Endpoint {
	// TODO do something with too many params
	return &Endpoint{
		log:               log,
		metainfo:          metainfo,
		deletePieces:      deletePieces,
		orders:            orders,
		overlay:           cache,
		attributions:      attributions,
		partners:          partners,
		peerIdentities:    peerIdentities,
		apiKeys:           apiKeys,
		projectUsage:      projectUsage,
		projects:          projects,
		createRequests:    newCreateRequests(),
		requiredRSConfig:  rsConfig,
		satellite:         satellite,
		maxCommitInterval: maxCommitInterval,
		limiterCache: lrucache.New(lrucache.Options{
			Capacity:   limiterConfig.CacheCapacity,
			Expiration: limiterConfig.CacheExpiration,
		}),
		limiterConfig: limiterConfig,
	}
}

// Close closes resources
func (endpoint *Endpoint) Close() error { return nil }

// SegmentInfoOld returns segment metadata info
func (endpoint *Endpoint) SegmentInfoOld(ctx context.Context, req *pb.SegmentInfoRequestOld) (resp *pb.SegmentInfoResponseOld, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionRead,
		Bucket:        req.Bucket,
		EncryptedPath: req.Path,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(ctx, req.Bucket)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	pointer, _, err := endpoint.getPointer(ctx, keyInfo.ProjectID, req.Segment, req.Bucket, req.Path)
	if err != nil {
		return nil, err
	}

	return &pb.SegmentInfoResponseOld{Pointer: pointer}, nil
}

// CreateSegmentOld will generate requested number of OrderLimit with coresponding node addresses for them
func (endpoint *Endpoint) CreateSegmentOld(ctx context.Context, req *pb.SegmentWriteRequestOld) (resp *pb.SegmentWriteResponseOld, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionWrite,
		Bucket:        req.Bucket,
		EncryptedPath: req.Path,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(ctx, req.Bucket)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	if !req.Expiration.IsZero() && !req.Expiration.After(time.Now()) {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, "Invalid expiration time")
	}

	err = endpoint.validateRedundancy(ctx, req.Redundancy)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	exceeded, limit, err := endpoint.projectUsage.ExceedsStorageUsage(ctx, keyInfo.ProjectID)
	if err != nil {
		endpoint.log.Error("retrieving project storage totals", zap.Error(err))
	}
	if exceeded {
		endpoint.log.Sugar().Errorf("monthly project limits are %s of storage and bandwidth usage. This limit has been exceeded for storage for projectID %s",
			limit, keyInfo.ProjectID,
		)
		return nil, rpcstatus.Error(rpcstatus.ResourceExhausted, "Exceeded Usage Limit")
	}

	redundancy, err := eestream.NewRedundancyStrategyFromProto(req.GetRedundancy())
	if err != nil {
		return nil, err
	}

	maxPieceSize := eestream.CalcPieceSize(req.GetMaxEncryptedSegmentSize(), redundancy)

	request := overlay.FindStorageNodesRequest{
		RequestedCount: int(req.Redundancy.Total),
		FreeBandwidth:  maxPieceSize,
	}
	nodes, err := endpoint.overlay.FindStorageNodes(ctx, request)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	bucketID := createBucketID(keyInfo.ProjectID, req.Bucket)
	rootPieceID, addressedLimits, piecePrivateKey, err := endpoint.orders.CreatePutOrderLimits(ctx, bucketID, nodes, req.Expiration, maxPieceSize)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	if len(addressedLimits) > 0 {
		endpoint.createRequests.Put(addressedLimits[0].Limit.SerialNumber, &createRequest{
			Expiration: req.Expiration,
			Redundancy: req.Redundancy,
		})
	}

	return &pb.SegmentWriteResponseOld{AddressedLimits: addressedLimits, RootPieceId: rootPieceID, PrivateKey: piecePrivateKey}, nil
}

func calculateSpaceUsed(ptr *pb.Pointer) (segmentSize, totalStored int64) {
	inline := ptr.GetInlineSegment()
	if inline != nil {
		inlineSize := int64(len(inline))
		return inlineSize, inlineSize
	}
	segmentSize = ptr.GetSegmentSize()
	remote := ptr.GetRemote()
	if remote == nil {
		return 0, 0
	}
	minReq := remote.GetRedundancy().GetMinReq()
	pieceSize := segmentSize / int64(minReq)
	pieces := remote.GetRemotePieces()
	return segmentSize, pieceSize * int64(len(pieces))
}

// CommitSegmentOld commits segment metadata
func (endpoint *Endpoint) CommitSegmentOld(ctx context.Context, req *pb.SegmentCommitRequestOld) (resp *pb.SegmentCommitResponseOld, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionWrite,
		Bucket:        req.Bucket,
		EncryptedPath: req.Path,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(ctx, req.Bucket)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	err = endpoint.validateCommitSegment(ctx, req)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	err = endpoint.filterValidPieces(ctx, req.Pointer, req.OriginalLimits)
	if err != nil {
		return nil, err
	}

	path, err := CreatePath(ctx, keyInfo.ProjectID, req.Segment, req.Bucket, req.Path)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	exceeded, limit, err := endpoint.projectUsage.ExceedsStorageUsage(ctx, keyInfo.ProjectID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}
	if exceeded {
		endpoint.log.Sugar().Errorf("monthly project limits are %s of storage and bandwidth usage. This limit has been exceeded for storage for projectID %s.",
			limit, keyInfo.ProjectID,
		)
		return nil, rpcstatus.Error(rpcstatus.ResourceExhausted, "Exceeded Usage Limit")
	}

	// clear hashes so we don't store them
	for _, piece := range req.GetPointer().GetRemote().GetRemotePieces() {
		piece.Hash = nil
	}
	req.Pointer.PieceHashesVerified = true

	segmentSize, totalStored := calculateSpaceUsed(req.Pointer)

	// ToDo: Replace with hash & signature validation
	// Ensure neither uplink or storage nodes are cheating on us
	if req.Pointer.Type == pb.Pointer_REMOTE {
		//We cannot have more redundancy than total/min
		if float64(totalStored) > (float64(req.Pointer.SegmentSize)/float64(req.Pointer.Remote.Redundancy.MinReq))*float64(req.Pointer.Remote.Redundancy.Total) {
			endpoint.log.Sugar().Debugf("data size mismatch, got segment: %d, pieces: %d, RS Min, Total: %d,%d", req.Pointer.SegmentSize, totalStored, req.Pointer.Remote.Redundancy.MinReq, req.Pointer.Remote.Redundancy.Total)
			return nil, rpcstatus.Error(rpcstatus.InvalidArgument, "mismatched segment size and piece usage")
		}
	}

	if err := endpoint.projectUsage.AddProjectStorageUsage(ctx, keyInfo.ProjectID, segmentSize); err != nil {
		endpoint.log.Sugar().Errorf("Could not track new storage usage by project %q: %v", keyInfo.ProjectID, err)
		// but continue. it's most likely our own fault that we couldn't track it, and the only thing
		// that will be affected is our per-project bandwidth and storage limits.
	}

	err = endpoint.metainfo.UnsynchronizedPut(ctx, path, req.Pointer)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	if req.Pointer.Type == pb.Pointer_INLINE {
		// TODO or maybe use pointer.SegmentSize ??
		err = endpoint.orders.UpdatePutInlineOrder(ctx, keyInfo.ProjectID, req.Bucket, int64(len(req.Pointer.InlineSegment)))
		if err != nil {
			return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
		}
	}

	pointer, err := endpoint.metainfo.Get(ctx, path)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	if len(req.OriginalLimits) > 0 {
		endpoint.createRequests.Remove(req.OriginalLimits[0].SerialNumber)
	}

	return &pb.SegmentCommitResponseOld{Pointer: pointer}, nil
}

// DownloadSegmentOld gets Pointer incase of INLINE data or list of OrderLimit necessary to download remote data
func (endpoint *Endpoint) DownloadSegmentOld(ctx context.Context, req *pb.SegmentDownloadRequestOld) (resp *pb.SegmentDownloadResponseOld, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionRead,
		Bucket:        req.Bucket,
		EncryptedPath: req.Path,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(ctx, req.Bucket)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	bucketID := createBucketID(keyInfo.ProjectID, req.Bucket)

	exceeded, limit, err := endpoint.projectUsage.ExceedsBandwidthUsage(ctx, keyInfo.ProjectID, bucketID)
	if err != nil {
		endpoint.log.Error("retrieving project bandwidth total", zap.Error(err))
	}
	if exceeded {
		endpoint.log.Sugar().Errorf("monthly project limits are %s of storage and bandwidth usage. This limit has been exceeded for bandwidth for projectID %s.",
			limit, keyInfo.ProjectID,
		)
		return nil, rpcstatus.Error(rpcstatus.ResourceExhausted, "Exceeded Usage Limit")
	}

	pointer, _, err := endpoint.getPointer(ctx, keyInfo.ProjectID, req.Segment, req.Bucket, req.Path)
	if err != nil {
		return nil, err
	}

	if pointer.Type == pb.Pointer_INLINE {
		// TODO or maybe use pointer.SegmentSize ??
		err := endpoint.orders.UpdateGetInlineOrder(ctx, keyInfo.ProjectID, req.Bucket, int64(len(pointer.InlineSegment)))
		if err != nil {
			return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
		}
		return &pb.SegmentDownloadResponseOld{Pointer: pointer}, nil
	} else if pointer.Type == pb.Pointer_REMOTE && pointer.Remote != nil {
		limits, privateKey, err := endpoint.orders.CreateGetOrderLimitsOld(ctx, bucketID, pointer)
		if err != nil {
			if orders.ErrDownloadFailedNotEnoughPieces.Has(err) {
				endpoint.log.Sugar().Errorf("unable to create order limits for project id %s from api key id %s: %v.", keyInfo.ProjectID.String(), keyInfo.ID.String(), zap.Error(err))
			}
			return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
		}
		return &pb.SegmentDownloadResponseOld{Pointer: pointer, AddressedLimits: limits, PrivateKey: privateKey}, nil
	}

	return &pb.SegmentDownloadResponseOld{}, nil
}

// DeleteSegmentOld deletes segment metadata from satellite and returns OrderLimit array to remove them from storage node
func (endpoint *Endpoint) DeleteSegmentOld(ctx context.Context, req *pb.SegmentDeleteRequestOld) (resp *pb.SegmentDeleteResponseOld, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionDelete,
		Bucket:        req.Bucket,
		EncryptedPath: req.Path,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(ctx, req.Bucket)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	path, err := CreatePath(ctx, keyInfo.ProjectID, req.Segment, req.Bucket, req.Path)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	// TODO refactor to use []byte directly
	pointer, err := endpoint.metainfo.Get(ctx, path)
	if err != nil {
		if storj.ErrObjectNotFound.Has(err) {
			return nil, rpcstatus.Error(rpcstatus.NotFound, err.Error())
		}
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	err = endpoint.metainfo.UnsynchronizedDelete(ctx, path)

	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	if pointer.Type == pb.Pointer_REMOTE && pointer.Remote != nil {
		bucketID := createBucketID(keyInfo.ProjectID, req.Bucket)
		limits, privateKey, err := endpoint.orders.CreateDeleteOrderLimits(ctx, bucketID, pointer)
		if err != nil {
			return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
		}

		return &pb.SegmentDeleteResponseOld{AddressedLimits: limits, PrivateKey: privateKey}, nil
	}

	return &pb.SegmentDeleteResponseOld{}, nil
}

// ListSegmentsOld returns all Path keys in the Pointers bucket
func (endpoint *Endpoint) ListSegmentsOld(ctx context.Context, req *pb.ListSegmentsRequestOld) (resp *pb.ListSegmentsResponseOld, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionList,
		Bucket:        req.Bucket,
		EncryptedPath: req.Prefix,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	prefix, err := CreatePath(ctx, keyInfo.ProjectID, -1, req.Bucket, req.Prefix)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	items, more, err := endpoint.metainfo.List(ctx, prefix, string(req.StartAfter), req.Recursive, req.Limit, req.MetaFlags)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	segmentItems := make([]*pb.ListSegmentsResponseOld_Item, len(items))
	for i, item := range items {
		segmentItems[i] = &pb.ListSegmentsResponseOld_Item{
			Path:     []byte(item.Path),
			Pointer:  item.Pointer,
			IsPrefix: item.IsPrefix,
		}
	}

	return &pb.ListSegmentsResponseOld{Items: segmentItems, More: more}, nil
}

func createBucketID(projectID uuid.UUID, bucket []byte) []byte {
	entries := make([]string, 0)
	entries = append(entries, projectID.String())
	entries = append(entries, string(bucket))
	return []byte(storj.JoinPaths(entries...))
}

// filterValidPieces filter out the invalid remote pieces held by pointer.
//
// This method expect the pointer to be valid, so it has to be validated before
// calling it.
//
// The method always return a gRPC status error so the caller can directly
// return it to the client.
func (endpoint *Endpoint) filterValidPieces(ctx context.Context, pointer *pb.Pointer, originalLimits []*pb.OrderLimit) (err error) {
	defer mon.Task()(&ctx)(&err)

	if pointer.Type != pb.Pointer_REMOTE {
		return nil
	}

	remote := pointer.Remote

	peerIDMap, err := endpoint.mapNodesFor(ctx, remote.RemotePieces)
	if err != nil {
		return err
	}

	type invalidPiece struct {
		NodeID   storj.NodeID
		PieceNum int32
		Reason   string
	}

	var (
		remotePieces  []*pb.RemotePiece
		invalidPieces []invalidPiece
		lastPieceSize int64
		allSizesValid = true
	)
	for _, piece := range remote.RemotePieces {
		// Verify storagenode signature on piecehash
		peerID, ok := peerIDMap[piece.NodeId]
		if !ok {
			endpoint.log.Warn("Identity chain unknown for node. Piece removed from pointer",
				zap.Stringer("Node ID", piece.NodeId),
				zap.Int32("Piece ID", piece.PieceNum),
			)

			invalidPieces = append(invalidPieces, invalidPiece{
				NodeID:   piece.NodeId,
				PieceNum: piece.PieceNum,
				Reason:   "Identity chain unknown for node",
			})
			continue
		}
		signee := signing.SigneeFromPeerIdentity(peerID)

		limit := originalLimits[piece.PieceNum]
		if limit == nil {
			endpoint.log.Warn("There is not limit for the piece.  Piece removed from pointer",
				zap.Int32("Piece ID", piece.PieceNum),
			)

			invalidPieces = append(invalidPieces, invalidPiece{
				NodeID:   piece.NodeId,
				PieceNum: piece.PieceNum,
				Reason:   "No order limit for validating the piece hash",
			})
			continue
		}

		err = endpoint.validatePieceHash(ctx, piece, limit, signee)
		if err != nil {
			endpoint.log.Warn("Problem validating piece hash. Pieces removed from pointer", zap.Error(err))
			invalidPieces = append(invalidPieces, invalidPiece{
				NodeID:   piece.NodeId,
				PieceNum: piece.PieceNum,
				Reason:   err.Error(),
			})
			continue
		}

		if piece.Hash.PieceSize <= 0 || (lastPieceSize > 0 && lastPieceSize != piece.Hash.PieceSize) {
			allSizesValid = false
			break
		}
		lastPieceSize = piece.Hash.PieceSize

		remotePieces = append(remotePieces, piece)
	}

	if allSizesValid {
		redundancy, err := eestream.NewRedundancyStrategyFromProto(pointer.GetRemote().GetRedundancy())
		if err != nil {
			endpoint.log.Debug("pointer contains an invalid redundancy strategy", zap.Error(Error.Wrap(err)))
			return rpcstatus.Errorf(rpcstatus.InvalidArgument,
				"invalid redundancy strategy; MinReq and/or Total are invalid: %s", err,
			)
		}

		expectedPieceSize := eestream.CalcPieceSize(pointer.SegmentSize, redundancy)
		if expectedPieceSize != lastPieceSize {
			endpoint.log.Debug("expected piece size is different from provided",
				zap.Int64("expectedSize", expectedPieceSize),
				zap.Int64("actualSize", lastPieceSize),
			)
			return rpcstatus.Errorf(rpcstatus.InvalidArgument,
				"expected piece size is different from provided (%d != %d)",
				expectedPieceSize, lastPieceSize,
			)
		}
	} else {
		errMsg := "all pieces needs to have the same size"
		endpoint.log.Debug(errMsg)
		return rpcstatus.Error(rpcstatus.InvalidArgument, errMsg)
	}

	// We repair when the number of healthy files is less than or equal to the repair threshold
	// except for the case when the repair and success thresholds are the same (a case usually seen during testing).
	if numPieces := int32(len(remotePieces)); numPieces <= remote.Redundancy.RepairThreshold && numPieces < remote.Redundancy.SuccessThreshold {
		endpoint.log.Debug("Number of valid pieces is less than or equal to the repair threshold",
			zap.Int("totalReceivedPieces", len(remote.RemotePieces)),
			zap.Int("validPieces", len(remotePieces)),
			zap.Int("invalidPieces", len(invalidPieces)),
			zap.Int32("repairThreshold", remote.Redundancy.RepairThreshold),
		)

		errMsg := fmt.Sprintf("Number of valid pieces (%d) is less than or equal to the repair threshold (%d). Found %d invalid pieces",
			len(remotePieces),
			remote.Redundancy.RepairThreshold,
			len(remote.RemotePieces),
		)
		if len(invalidPieces) > 0 {
			errMsg = fmt.Sprintf("%s. Invalid Pieces:", errMsg)

			for _, p := range invalidPieces {
				errMsg = fmt.Sprintf("%s\nNodeID: %v, PieceNum: %d, Reason: %s",
					errMsg, p.NodeID, p.PieceNum, p.Reason,
				)
			}
		}

		return rpcstatus.Error(rpcstatus.InvalidArgument, errMsg)
	}

	if int32(len(remotePieces)) < remote.Redundancy.SuccessThreshold {
		endpoint.log.Debug("Number of valid pieces is less than the success threshold",
			zap.Int("totalReceivedPieces", len(remote.RemotePieces)),
			zap.Int("validPieces", len(remotePieces)),
			zap.Int("invalidPieces", len(invalidPieces)),
			zap.Int32("successThreshold", remote.Redundancy.SuccessThreshold),
		)

		errMsg := fmt.Sprintf("Number of valid pieces (%d) is less than the success threshold (%d). Found %d invalid pieces",
			len(remotePieces),
			remote.Redundancy.SuccessThreshold,
			len(remote.RemotePieces),
		)
		if len(invalidPieces) > 0 {
			errMsg = fmt.Sprintf("%s. Invalid Pieces:", errMsg)

			for _, p := range invalidPieces {
				errMsg = fmt.Sprintf("%s\nNodeID: %v, PieceNum: %d, Reason: %s",
					errMsg, p.NodeID, p.PieceNum, p.Reason,
				)
			}
		}

		return rpcstatus.Error(rpcstatus.InvalidArgument, errMsg)
	}

	remote.RemotePieces = remotePieces

	return nil
}

func (endpoint *Endpoint) mapNodesFor(ctx context.Context, pieces []*pb.RemotePiece) (map[storj.NodeID]*identity.PeerIdentity, error) {
	nodeIDList := storj.NodeIDList{}
	for _, piece := range pieces {
		nodeIDList = append(nodeIDList, piece.NodeId)
	}
	peerIDList, err := endpoint.peerIdentities.BatchGet(ctx, nodeIDList)
	if err != nil {
		endpoint.log.Error("retrieving batch of the peer identities of nodes", zap.Error(Error.Wrap(err)))
		return nil, rpcstatus.Error(rpcstatus.Internal, "retrieving nodes peer identities")
	}
	peerIDMap := make(map[storj.NodeID]*identity.PeerIdentity, len(peerIDList))
	for _, peerID := range peerIDList {
		peerIDMap[peerID.ID] = peerID
	}

	return peerIDMap, nil
}

// CreatePath creates a Segment path.
func CreatePath(ctx context.Context, projectID uuid.UUID, segmentIndex int64, bucket, path []byte) (_ storj.Path, err error) {
	defer mon.Task()(&ctx)(&err)
	if segmentIndex < -1 {
		return "", errors.New("invalid segment index")
	}
	segment := "l"
	if segmentIndex > -1 {
		segment = "s" + strconv.FormatInt(segmentIndex, 10)
	}

	entries := make([]string, 0)
	entries = append(entries, projectID.String())
	entries = append(entries, segment)
	if len(bucket) != 0 {
		entries = append(entries, string(bucket))
	}
	if len(path) != 0 {
		entries = append(entries, string(path))
	}
	return storj.JoinPaths(entries...), nil
}

// SetAttributionOld tries to add attribution to the bucket.
func (endpoint *Endpoint) SetAttributionOld(ctx context.Context, req *pb.SetAttributionRequestOld) (_ *pb.SetAttributionResponseOld, err error) {
	defer mon.Task()(&ctx)(&err)

	err = endpoint.setBucketAttribution(ctx, req.Header, req.BucketName, req.PartnerId)

	return &pb.SetAttributionResponseOld{}, err
}

// ProjectInfo returns allowed ProjectInfo for the provided API key
func (endpoint *Endpoint) ProjectInfo(ctx context.Context, req *pb.ProjectInfoRequest) (_ *pb.ProjectInfoResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:   macaroon.ActionProjectInfo,
		Time: time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	salt := sha256.Sum256(keyInfo.ProjectID[:])

	return &pb.ProjectInfoResponse{
		ProjectSalt: salt[:],
	}, nil
}

// GetBucket returns a bucket
func (endpoint *Endpoint) GetBucket(ctx context.Context, req *pb.BucketGetRequest) (resp *pb.BucketGetResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:     macaroon.ActionRead,
		Bucket: req.Name,
		Time:   time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	bucket, err := endpoint.metainfo.GetBucket(ctx, req.GetName(), keyInfo.ProjectID)
	if err != nil {
		if storj.ErrBucketNotFound.Has(err) {
			return nil, rpcstatus.Error(rpcstatus.NotFound, err.Error())
		}
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	// override RS to fit satellite settings
	convBucket, err := convertBucketToProto(ctx, bucket, endpoint.redundancyScheme())
	if err != nil {
		return resp, err
	}

	return &pb.BucketGetResponse{
		Bucket: convBucket,
	}, nil
}

// CreateBucket creates a new bucket
func (endpoint *Endpoint) CreateBucket(ctx context.Context, req *pb.BucketCreateRequest) (resp *pb.BucketCreateResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:     macaroon.ActionWrite,
		Bucket: req.Name,
		Time:   time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(ctx, req.Name)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	// TODO set default Redundancy if not set
	err = endpoint.validateRedundancy(ctx, req.GetDefaultRedundancyScheme())
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	// checks if bucket exists before updates it or makes a new entry
	_, err = endpoint.metainfo.GetBucket(ctx, req.GetName(), keyInfo.ProjectID)
	if err == nil {
		return nil, rpcstatus.Error(rpcstatus.AlreadyExists, "bucket already exists")
	}
	if !storj.ErrBucketNotFound.Has(err) {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	bucket, err := convertProtoToBucket(req, keyInfo.ProjectID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	bucket, err = endpoint.metainfo.CreateBucket(ctx, bucket)
	if err != nil {
		endpoint.log.Error("error while creating bucket", zap.String("bucketName", bucket.Name), zap.Error(err))
		return nil, rpcstatus.Error(rpcstatus.Internal, "unable to create bucket")
	}

	// override RS to fit satellite settings
	convBucket, err := convertBucketToProto(ctx, bucket, endpoint.redundancyScheme())
	if err != nil {
		endpoint.log.Error("error while converting bucket to proto", zap.String("bucketName", bucket.Name), zap.Error(err))
		return nil, rpcstatus.Error(rpcstatus.Internal, "unable to create bucket")
	}

	return &pb.BucketCreateResponse{
		Bucket: convBucket,
	}, nil
}

// DeleteBucket deletes a bucket
func (endpoint *Endpoint) DeleteBucket(ctx context.Context, req *pb.BucketDeleteRequest) (resp *pb.BucketDeleteResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:     macaroon.ActionDelete,
		Bucket: req.Name,
		Time:   time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(ctx, req.Name)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	err = endpoint.metainfo.DeleteBucket(ctx, req.Name, keyInfo.ProjectID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	return &pb.BucketDeleteResponse{}, nil
}

// ListBuckets returns buckets in a project where the bucket name matches the request cursor
func (endpoint *Endpoint) ListBuckets(ctx context.Context, req *pb.BucketListRequest) (resp *pb.BucketListResponse, err error) {
	defer mon.Task()(&ctx)(&err)
	action := macaroon.Action{
		Op:   macaroon.ActionRead,
		Time: time.Now(),
	}
	keyInfo, err := endpoint.validateAuth(ctx, req.Header, action)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	allowedBuckets, err := getAllowedBuckets(ctx, req.Header, action)
	if err != nil {
		return nil, err
	}

	listOpts := storj.BucketListOptions{
		Cursor:    string(req.Cursor),
		Limit:     int(req.Limit),
		Direction: storj.ListDirection(req.Direction),
	}
	bucketList, err := endpoint.metainfo.ListBuckets(ctx, keyInfo.ProjectID, listOpts, allowedBuckets)
	if err != nil {
		return nil, err
	}

	bucketItems := make([]*pb.BucketListItem, len(bucketList.Items))
	for i, item := range bucketList.Items {
		bucketItems[i] = &pb.BucketListItem{
			Name:      []byte(item.Name),
			CreatedAt: item.Created,
		}
	}

	return &pb.BucketListResponse{
		Items: bucketItems,
		More:  bucketList.More,
	}, nil
}

func getAllowedBuckets(ctx context.Context, header *pb.RequestHeader, action macaroon.Action) (_ macaroon.AllowedBuckets, err error) {
	key, err := getAPIKey(ctx, header)
	if err != nil {
		return macaroon.AllowedBuckets{}, rpcstatus.Errorf(rpcstatus.InvalidArgument, "Invalid API credentials: %v", err)
	}
	allowedBuckets, err := key.GetAllowedBuckets(ctx, action)
	if err != nil {
		return macaroon.AllowedBuckets{}, rpcstatus.Errorf(rpcstatus.Internal, "GetAllowedBuckets: %v", err)
	}
	return allowedBuckets, err
}

// SetBucketAttribution sets the bucket attribution.
func (endpoint *Endpoint) SetBucketAttribution(ctx context.Context, req *pb.BucketSetAttributionRequest) (resp *pb.BucketSetAttributionResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	err = endpoint.setBucketAttribution(ctx, req.Header, req.Name, req.PartnerId)

	return &pb.BucketSetAttributionResponse{}, err
}

// resolvePartnerID returns partnerIDBytes as parsed or UUID corresponding to header.UserAgent.
// returns empty uuid when neither is defined.
func (endpoint *Endpoint) resolvePartnerID(ctx context.Context, header *pb.RequestHeader, partnerIDBytes []byte) (uuid.UUID, error) {
	if len(partnerIDBytes) > 0 {
		partnerID, err := dbutil.BytesToUUID(partnerIDBytes)
		if err != nil {
			return uuid.UUID{}, rpcstatus.Errorf(rpcstatus.InvalidArgument, "unable to parse partner ID: %v", err)
		}
		return partnerID, nil
	}

	if len(header.UserAgent) == 0 {
		return uuid.UUID{}, nil
	}

	partner, err := endpoint.partners.ByUserAgent(ctx, string(header.UserAgent))
	if err != nil || partner.UUID == nil {
		return uuid.UUID{}, rpcstatus.Errorf(rpcstatus.InvalidArgument, "unable to resolve user agent %q: %v", string(header.UserAgent), err)
	}

	return *partner.UUID, nil
}

func (endpoint *Endpoint) setBucketAttribution(ctx context.Context, header *pb.RequestHeader, bucketName []byte, partnerIDBytes []byte) error {
	keyInfo, err := endpoint.validateAuth(ctx, header, macaroon.Action{
		Op:            macaroon.ActionList,
		Bucket:        bucketName,
		EncryptedPath: []byte(""),
		Time:          time.Now(),
	})
	if err != nil {
		return rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	partnerID, err := endpoint.resolvePartnerID(ctx, header, partnerIDBytes)
	if err != nil {
		return rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}
	if partnerID.IsZero() {
		return rpcstatus.Error(rpcstatus.InvalidArgument, "unknown user agent or partner id")
	}

	// check if attribution is set for given bucket
	_, err = endpoint.attributions.Get(ctx, keyInfo.ProjectID, bucketName)
	if err == nil {
		endpoint.log.Info("bucket already attributed", zap.ByteString("bucketName", bucketName), zap.Stringer("Partner ID", partnerID))
		return nil
	}

	if !attribution.ErrBucketNotAttributed.Has(err) {
		// try only to set the attribution, when it's missing
		endpoint.log.Error("error while getting attribution from DB", zap.Error(err))
		return rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	prefix, err := CreatePath(ctx, keyInfo.ProjectID, -1, bucketName, []byte{})
	if err != nil {
		return rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	items, _, err := endpoint.metainfo.List(ctx, prefix, "", true, 1, 0)
	if err != nil {
		endpoint.log.Error("error while listing segments", zap.Error(err))
		return rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	if len(items) > 0 {
		return rpcstatus.Errorf(rpcstatus.AlreadyExists, "bucket %q is not empty, PartnerID %q cannot be attributed", bucketName, partnerID)
	}

	// checks if bucket exists before updates it or makes a new entry
	bucket, err := endpoint.metainfo.GetBucket(ctx, bucketName, keyInfo.ProjectID)
	if err != nil {
		if storj.ErrBucketNotFound.Has(err) {
			return rpcstatus.Errorf(rpcstatus.NotFound, "bucket %q does not exist", bucketName)
		}
		endpoint.log.Error("error while getting bucket", zap.ByteString("bucketName", bucketName), zap.Error(err))
		return rpcstatus.Error(rpcstatus.Internal, "unable to set bucket attribution")
	}
	if !bucket.PartnerID.IsZero() {
		endpoint.log.Info("bucket already attributed", zap.ByteString("bucketName", bucketName), zap.Stringer("Partner ID", partnerID))
		return nil
	}

	// update bucket information
	bucket.PartnerID = partnerID
	_, err = endpoint.metainfo.UpdateBucket(ctx, bucket)
	if err != nil {
		endpoint.log.Error("error while updating bucket", zap.ByteString("bucketName", bucketName), zap.Error(err))
		return rpcstatus.Error(rpcstatus.Internal, "unable to set bucket attribution")
	}

	// update attribution table
	_, err = endpoint.attributions.Insert(ctx, &attribution.Info{
		ProjectID:  keyInfo.ProjectID,
		BucketName: bucketName,
		PartnerID:  partnerID,
	})
	if err != nil {
		endpoint.log.Error("error while inserting attribution to DB", zap.Error(err))
		return rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	return nil
}

func convertProtoToBucket(req *pb.BucketCreateRequest, projectID uuid.UUID) (bucket storj.Bucket, err error) {
	bucketID, err := uuid.New()
	if err != nil {
		return storj.Bucket{}, err
	}

	defaultRS := req.GetDefaultRedundancyScheme()
	defaultEP := req.GetDefaultEncryptionParameters()

	// TODO: resolve partner id
	var partnerID uuid.UUID
	err = partnerID.UnmarshalJSON(req.GetPartnerId())

	// bucket's partnerID should never be set
	// it is always read back from buckets DB
	if err != nil && !partnerID.IsZero() {
		return bucket, errs.New("Invalid uuid")
	}

	return storj.Bucket{
		ID:                  *bucketID,
		Name:                string(req.GetName()),
		ProjectID:           projectID,
		PartnerID:           partnerID,
		PathCipher:          storj.CipherSuite(req.GetPathCipher()),
		DefaultSegmentsSize: req.GetDefaultSegmentSize(),
		DefaultRedundancyScheme: storj.RedundancyScheme{
			Algorithm:      storj.RedundancyAlgorithm(defaultRS.GetType()),
			ShareSize:      defaultRS.GetErasureShareSize(),
			RequiredShares: int16(defaultRS.GetMinReq()),
			RepairShares:   int16(defaultRS.GetRepairThreshold()),
			OptimalShares:  int16(defaultRS.GetSuccessThreshold()),
			TotalShares:    int16(defaultRS.GetTotal()),
		},
		DefaultEncryptionParameters: storj.EncryptionParameters{
			CipherSuite: storj.CipherSuite(defaultEP.CipherSuite),
			BlockSize:   int32(defaultEP.BlockSize),
		},
	}, nil
}

func convertBucketToProto(ctx context.Context, bucket storj.Bucket, rs *pb.RedundancyScheme) (pbBucket *pb.Bucket, err error) {
	partnerID, err := bucket.PartnerID.MarshalJSON()
	if err != nil {
		return pbBucket, rpcstatus.Error(rpcstatus.Internal, "UUID marshal error")
	}
	return &pb.Bucket{
		Name:                    []byte(bucket.Name),
		PathCipher:              pb.CipherSuite(int(bucket.PathCipher)),
		PartnerId:               partnerID,
		CreatedAt:               bucket.Created,
		DefaultSegmentSize:      bucket.DefaultSegmentsSize,
		DefaultRedundancyScheme: rs,
		DefaultEncryptionParameters: &pb.EncryptionParameters{
			CipherSuite: pb.CipherSuite(int(bucket.DefaultEncryptionParameters.CipherSuite)),
			BlockSize:   int64(bucket.DefaultEncryptionParameters.BlockSize),
		},
	}, nil
}

// BeginObject begins object
func (endpoint *Endpoint) BeginObject(ctx context.Context, req *pb.ObjectBeginRequest) (resp *pb.ObjectBeginResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionWrite,
		Bucket:        req.Bucket,
		EncryptedPath: req.EncryptedPath,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	if !req.ExpiresAt.IsZero() && !req.ExpiresAt.After(time.Now()) {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, "Invalid expiration time")
	}

	// use only satellite values for Redundancy Scheme
	pbRS := endpoint.redundancyScheme()

	streamID, err := endpoint.packStreamID(ctx, &pb.SatStreamID{
		Bucket:         req.Bucket,
		EncryptedPath:  req.EncryptedPath,
		Version:        req.Version,
		Redundancy:     pbRS,
		CreationDate:   time.Now(),
		ExpirationDate: req.ExpiresAt,
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	err = endpoint.DeleteObjectPieces(ctx, keyInfo.ProjectID, req.Bucket, req.EncryptedPath)
	if err != nil && !errs2.IsRPC(err, rpcstatus.NotFound) {
		return nil, err
	}

	endpoint.log.Info("Object Upload", zap.Stringer("Project ID", keyInfo.ProjectID), zap.String("operation", "put"), zap.String("type", "object"))
	mon.Meter("req_put_object").Mark(1)

	return &pb.ObjectBeginResponse{
		Bucket:           req.Bucket,
		EncryptedPath:    req.EncryptedPath,
		Version:          req.Version,
		StreamId:         streamID,
		RedundancyScheme: pbRS,
	}, nil
}

// CommitObject commits an object when all its segments have already been
// committed.
func (endpoint *Endpoint) CommitObject(ctx context.Context, req *pb.ObjectCommitRequest) (resp *pb.ObjectCommitResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	streamID := &pb.SatStreamID{}
	err = proto.Unmarshal(req.StreamId, streamID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	err = signing.VerifyStreamID(ctx, endpoint.satellite, streamID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	if streamID.CreationDate.Before(time.Now().Add(-satIDExpiration)) {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, "stream ID expired")
	}

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionWrite,
		Bucket:        streamID.Bucket,
		EncryptedPath: streamID.EncryptedPath,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	streamMeta := pb.StreamMeta{}
	err = proto.Unmarshal(req.EncryptedMetadata, &streamMeta)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, "invalid metadata structure")
	}

	lastSegmentIndex := streamMeta.NumberOfSegments - 1
	lastSegmentPath, err := CreatePath(ctx, keyInfo.ProjectID, lastSegmentIndex, streamID.Bucket, streamID.EncryptedPath)
	if err != nil {
		return nil, rpcstatus.Errorf(rpcstatus.InvalidArgument, "unable to create segment path: %s", err.Error())
	}

	lastSegmentPointerBytes, lastSegmentPointer, err := endpoint.metainfo.GetWithBytes(ctx, lastSegmentPath)
	if err != nil {
		endpoint.log.Error("unable to get pointer", zap.String("segmentPath", lastSegmentPath), zap.Error(err))
		return nil, rpcstatus.Error(rpcstatus.Internal, "unable to commit object")
	}
	if lastSegmentPointer == nil {
		return nil, rpcstatus.Errorf(rpcstatus.NotFound, "unable to find object: %q/%q", streamID.Bucket, streamID.EncryptedPath)
	}

	if lastSegmentPointer.Remote == nil {
		lastSegmentPointer.Remote = &pb.RemoteSegment{}
	}
	// RS is set always for last segment to emulate RS per object
	lastSegmentPointer.Remote.Redundancy = streamID.Redundancy
	lastSegmentPointer.Metadata = req.EncryptedMetadata

	err = endpoint.metainfo.Delete(ctx, lastSegmentPath, lastSegmentPointerBytes)
	if err != nil {
		endpoint.log.Error("unable to delete pointer", zap.String("segmentPath", lastSegmentPath), zap.Error(err))
		return nil, rpcstatus.Error(rpcstatus.Internal, "unable to commit object")
	}

	lastSegmentIndex = -1
	lastSegmentPath, err = CreatePath(ctx, keyInfo.ProjectID, lastSegmentIndex, streamID.Bucket, streamID.EncryptedPath)
	if err != nil {
		endpoint.log.Error("unable to create path", zap.Error(err))
		return nil, rpcstatus.Error(rpcstatus.Internal, "unable to commit object")
	}

	err = endpoint.metainfo.UnsynchronizedPut(ctx, lastSegmentPath, lastSegmentPointer)
	if err != nil {
		endpoint.log.Error("unable to put pointer", zap.Error(err))
		return nil, rpcstatus.Error(rpcstatus.Internal, "unable to commit object")
	}

	return &pb.ObjectCommitResponse{}, nil
}

// GetObject gets single object
func (endpoint *Endpoint) GetObject(ctx context.Context, req *pb.ObjectGetRequest) (resp *pb.ObjectGetResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionRead,
		Bucket:        req.Bucket,
		EncryptedPath: req.EncryptedPath,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(ctx, req.Bucket)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	pointer, _, err := endpoint.getPointer(ctx, keyInfo.ProjectID, -1, req.Bucket, req.EncryptedPath)
	if err != nil {
		return nil, err
	}

	streamMeta := &pb.StreamMeta{}
	err = proto.Unmarshal(pointer.Metadata, streamMeta)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	streamID, err := endpoint.packStreamID(ctx, &pb.SatStreamID{
		Bucket:        req.Bucket,
		EncryptedPath: req.EncryptedPath,
		Version:       req.Version,
		CreationDate:  time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	object := &pb.Object{
		Bucket:            req.Bucket,
		EncryptedPath:     req.EncryptedPath,
		Version:           -1,
		StreamId:          streamID,
		ExpiresAt:         pointer.ExpirationDate,
		CreatedAt:         pointer.CreationDate,
		EncryptedMetadata: pointer.Metadata,
		EncryptionParameters: &pb.EncryptionParameters{
			CipherSuite: pb.CipherSuite(streamMeta.EncryptionType),
			BlockSize:   int64(streamMeta.EncryptionBlockSize),
		},
	}

	if pointer.Remote != nil {
		object.RedundancyScheme = pointer.Remote.Redundancy

		// NumberOfSegments == 0 - pointer with encrypted num of segments
		// NumberOfSegments > 1 - pointer with unencrypted num of segments and multiple segments
	} else if streamMeta.NumberOfSegments == 0 || streamMeta.NumberOfSegments > 1 {
		// workaround
		// The new metainfo API redundancy scheme is on object level (not per segment).
		// Because of that, RS is always taken from the last segment.
		// The old implementation saves RS per segment, and in some cases
		// when the remote file's last segment is an inline segment, we end up
		// missing an RS scheme. This loop will search for RS in segments other than the last one.

		index := int64(0)
		for {
			path, err := CreatePath(ctx, keyInfo.ProjectID, index, req.Bucket, req.EncryptedPath)
			if err != nil {
				endpoint.log.Error("unable to get pointer path", zap.Error(err))
				return nil, rpcstatus.Error(rpcstatus.Internal, "unable to get object")
			}

			pointer, err = endpoint.metainfo.Get(ctx, path)
			if err != nil {
				if storj.ErrObjectNotFound.Has(err) {
					break
				}

				endpoint.log.Error("unable to get pointer", zap.Error(err))
				return nil, rpcstatus.Error(rpcstatus.Internal, "unable to get object")
			}
			if pointer.Remote != nil {
				object.RedundancyScheme = pointer.Remote.Redundancy
				break
			}
			index++
		}
	}
	endpoint.log.Info("Object Download", zap.Stringer("Project ID", keyInfo.ProjectID), zap.String("operation", "get"), zap.String("type", "object"))
	mon.Meter("req_get_object").Mark(1)

	return &pb.ObjectGetResponse{
		Object: object,
	}, nil
}

// ListObjects list objects according to specific parameters
func (endpoint *Endpoint) ListObjects(ctx context.Context, req *pb.ObjectListRequest) (resp *pb.ObjectListResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionList,
		Bucket:        req.Bucket,
		EncryptedPath: req.EncryptedPrefix,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(ctx, req.Bucket)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	prefix, err := CreatePath(ctx, keyInfo.ProjectID, -1, req.Bucket, req.EncryptedPrefix)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	metaflags := meta.All
	// TODO use flags
	segments, more, err := endpoint.metainfo.List(ctx, prefix, string(req.EncryptedCursor), req.Recursive, req.Limit, metaflags)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	items := make([]*pb.ObjectListItem, len(segments))
	for i, segment := range segments {
		items[i] = &pb.ObjectListItem{
			EncryptedPath: []byte(segment.Path),
		}
		if segment.Pointer != nil {
			items[i].EncryptedMetadata = segment.Pointer.Metadata
			items[i].CreatedAt = segment.Pointer.CreationDate
			items[i].ExpiresAt = segment.Pointer.ExpirationDate
		}
	}
	endpoint.log.Info("Object List", zap.Stringer("Project ID", keyInfo.ProjectID), zap.String("operation", "list"), zap.String("type", "object"))
	mon.Meter("req_list_object").Mark(1)

	return &pb.ObjectListResponse{
		Items: items,
		More:  more,
	}, nil
}

// BeginDeleteObject begins object deletion process.
func (endpoint *Endpoint) BeginDeleteObject(ctx context.Context, req *pb.ObjectBeginDeleteRequest) (resp *pb.ObjectBeginDeleteResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionDelete,
		Bucket:        req.Bucket,
		EncryptedPath: req.EncryptedPath,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(ctx, req.Bucket)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	satStreamID := &pb.SatStreamID{
		Bucket:        req.Bucket,
		EncryptedPath: req.EncryptedPath,
		Version:       req.Version,
		CreationDate:  time.Now(),
	}

	satStreamID, err = signing.SignStreamID(ctx, endpoint.satellite, satStreamID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	encodedStreamID, err := proto.Marshal(satStreamID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	streamID, err := storj.StreamIDFromBytes(encodedStreamID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	err = endpoint.DeleteObjectPieces(ctx, keyInfo.ProjectID, satStreamID.Bucket, satStreamID.EncryptedPath)
	if err != nil {
		return nil, err
	}

	endpoint.log.Info("Object Delete", zap.Stringer("Project ID", keyInfo.ProjectID), zap.String("operation", "delete"), zap.String("type", "object"))
	mon.Meter("req_delete_object").Mark(1)

	return &pb.ObjectBeginDeleteResponse{
		StreamId: streamID,
	}, nil
}

// FinishDeleteObject finishes object deletion
func (endpoint *Endpoint) FinishDeleteObject(ctx context.Context, req *pb.ObjectFinishDeleteRequest) (resp *pb.ObjectFinishDeleteResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	streamID := &pb.SatStreamID{}
	err = proto.Unmarshal(req.StreamId, streamID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	err = signing.VerifyStreamID(ctx, endpoint.satellite, streamID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	if streamID.CreationDate.Before(time.Now().Add(-satIDExpiration)) {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, "stream ID expired")
	}

	_, err = endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionDelete,
		Bucket:        streamID.Bucket,
		EncryptedPath: streamID.EncryptedPath,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	// we don't need to do anything for shim implementation

	return &pb.ObjectFinishDeleteResponse{}, nil
}

// BeginSegment begins segment uploading
func (endpoint *Endpoint) BeginSegment(ctx context.Context, req *pb.SegmentBeginRequest) (resp *pb.SegmentBeginResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	streamID, err := endpoint.unmarshalSatStreamID(ctx, req.StreamId)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionWrite,
		Bucket:        streamID.Bucket,
		EncryptedPath: streamID.EncryptedPath,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	// no need to validate streamID fields because it was validated during BeginObject

	if req.Position.Index < 0 {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, "segment index must be greater then 0")
	}

	exceeded, limit, err := endpoint.projectUsage.ExceedsStorageUsage(ctx, keyInfo.ProjectID)
	if err != nil {
		endpoint.log.Error("retrieving project storage totals", zap.Error(err))
	}
	if exceeded {
		endpoint.log.Sugar().Errorf("monthly project limits are %s of storage and bandwidth usage. This limit has been exceeded for storage for projectID %s",
			limit, keyInfo.ProjectID,
		)
		return nil, rpcstatus.Error(rpcstatus.ResourceExhausted, "Exceeded Usage Limit")
	}

	redundancy, err := eestream.NewRedundancyStrategyFromProto(streamID.Redundancy)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	maxPieceSize := eestream.CalcPieceSize(req.MaxOrderLimit, redundancy)

	request := overlay.FindStorageNodesRequest{
		RequestedCount: redundancy.TotalCount(),
		FreeBandwidth:  maxPieceSize,
	}
	nodes, err := endpoint.overlay.FindStorageNodes(ctx, request)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	bucketID := createBucketID(keyInfo.ProjectID, streamID.Bucket)
	rootPieceID, addressedLimits, piecePrivateKey, err := endpoint.orders.CreatePutOrderLimits(ctx, bucketID, nodes, streamID.ExpirationDate, maxPieceSize)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	segmentID, err := endpoint.packSegmentID(ctx, &pb.SatSegmentID{
		StreamId:            streamID,
		Index:               req.Position.Index,
		OriginalOrderLimits: addressedLimits,
		RootPieceId:         rootPieceID,
		CreationDate:        time.Now(),
	})

	endpoint.log.Info("Segment Upload", zap.Stringer("Project ID", keyInfo.ProjectID), zap.String("operation", "put"), zap.String("type", "remote"))
	mon.Meter("req_put_remote").Mark(1)

	return &pb.SegmentBeginResponse{
		SegmentId:       segmentID,
		AddressedLimits: addressedLimits,
		PrivateKey:      piecePrivateKey,
	}, nil
}

// CommitSegment commits segment after uploading
func (endpoint *Endpoint) CommitSegment(ctx context.Context, req *pb.SegmentCommitRequest) (resp *pb.SegmentCommitResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	segmentID, err := endpoint.unmarshalSatSegmentID(ctx, req.SegmentId)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	streamID := segmentID.StreamId

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionWrite,
		Bucket:        streamID.Bucket,
		EncryptedPath: streamID.EncryptedPath,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	if numResults := len(req.UploadResult); numResults < int(streamID.Redundancy.GetSuccessThreshold()) {
		endpoint.log.Debug("the results of uploaded pieces for the segment is below the redundancy optimal threshold",
			zap.Int("upload pieces results", numResults),
			zap.Int32("redundancy optimal threshold", streamID.Redundancy.GetSuccessThreshold()),
			zap.Stringer("Segment ID", req.SegmentId),
		)
		return nil, rpcstatus.Errorf(rpcstatus.InvalidArgument,
			"the number of results of uploaded pieces (%d) is below the optimal threshold (%d)",
			numResults, streamID.Redundancy.GetSuccessThreshold(),
		)
	}

	pieces := make([]*pb.RemotePiece, len(req.UploadResult))
	for i, result := range req.UploadResult {
		pieces[i] = &pb.RemotePiece{
			PieceNum: result.PieceNum,
			NodeId:   result.NodeId,
			Hash:     result.Hash,
		}
	}
	remote := &pb.RemoteSegment{
		Redundancy:   streamID.Redundancy,
		RootPieceId:  segmentID.RootPieceId,
		RemotePieces: pieces,
	}

	metadata, err := proto.Marshal(&pb.SegmentMeta{
		EncryptedKey: req.EncryptedKey,
		KeyNonce:     req.EncryptedKeyNonce.Bytes(),
	})
	if err != nil {
		endpoint.log.Error("unable to marshal segment metadata", zap.Error(err))
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	pointer := &pb.Pointer{
		Type:        pb.Pointer_REMOTE,
		Remote:      remote,
		SegmentSize: req.SizeEncryptedData,

		CreationDate:   streamID.CreationDate,
		ExpirationDate: streamID.ExpirationDate,
		Metadata:       metadata,

		PieceHashesVerified: true,
	}

	orderLimits := make([]*pb.OrderLimit, len(segmentID.OriginalOrderLimits))
	for i, orderLimit := range segmentID.OriginalOrderLimits {
		orderLimits[i] = orderLimit.Limit
	}

	err = endpoint.validatePointer(ctx, pointer, orderLimits)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	err = endpoint.filterValidPieces(ctx, pointer, orderLimits)
	if err != nil {
		return nil, err
	}

	path, err := CreatePath(ctx, keyInfo.ProjectID, int64(segmentID.Index), streamID.Bucket, streamID.EncryptedPath)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	exceeded, limit, err := endpoint.projectUsage.ExceedsStorageUsage(ctx, keyInfo.ProjectID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}
	if exceeded {
		endpoint.log.Error("The project limit of storage and bandwidth has been exceeded",
			zap.Int64("limit", limit.Int64()),
			zap.Stringer("Project ID", keyInfo.ProjectID),
		)
		return nil, rpcstatus.Error(rpcstatus.ResourceExhausted, "Exceeded Usage Limit")
	}

	// clear hashes so we don't store them
	for _, piece := range pointer.GetRemote().GetRemotePieces() {
		piece.Hash = nil
	}

	segmentSize, totalStored := calculateSpaceUsed(pointer)

	// ToDo: Replace with hash & signature validation
	// Ensure neither uplink or storage nodes are cheating on us
	if pointer.Type == pb.Pointer_REMOTE {
		//We cannot have more redundancy than total/min
		if float64(totalStored) > (float64(pointer.SegmentSize)/float64(pointer.Remote.Redundancy.MinReq))*float64(pointer.Remote.Redundancy.Total) {
			endpoint.log.Debug("data size mismatch",
				zap.Int64("segment", pointer.SegmentSize),
				zap.Int64("pieces", totalStored),
				zap.Int32("redundancy minimum requested", pointer.Remote.Redundancy.MinReq),
				zap.Int32("redundancy total", pointer.Remote.Redundancy.Total),
			)
			return nil, rpcstatus.Error(rpcstatus.InvalidArgument, "mismatched segment size and piece usage")
		}
	}

	if err := endpoint.projectUsage.AddProjectStorageUsage(ctx, keyInfo.ProjectID, segmentSize); err != nil {
		endpoint.log.Error("Could not track new storage usage by project",
			zap.Stringer("Project ID", keyInfo.ProjectID),
			zap.Error(err),
		)
		// but continue. it's most likely our own fault that we couldn't track it, and the only thing
		// that will be affected is our per-project bandwidth and storage limits.
	}

	err = endpoint.metainfo.UnsynchronizedPut(ctx, path, pointer)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	return &pb.SegmentCommitResponse{
		SuccessfulPieces: int32(len(pointer.Remote.RemotePieces)),
	}, nil
}

// MakeInlineSegment makes inline segment on satellite
func (endpoint *Endpoint) MakeInlineSegment(ctx context.Context, req *pb.SegmentMakeInlineRequest) (resp *pb.SegmentMakeInlineResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	streamID, err := endpoint.unmarshalSatStreamID(ctx, req.StreamId)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionWrite,
		Bucket:        streamID.Bucket,
		EncryptedPath: streamID.EncryptedPath,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	if req.Position.Index < 0 {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, "segment index must be greater then 0")
	}

	path, err := CreatePath(ctx, keyInfo.ProjectID, int64(req.Position.Index), streamID.Bucket, streamID.EncryptedPath)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	exceeded, limit, err := endpoint.projectUsage.ExceedsStorageUsage(ctx, keyInfo.ProjectID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}
	if exceeded {
		endpoint.log.Sugar().Errorf("monthly project limits are %s of storage and bandwidth usage. This limit has been exceeded for storage for projectID %s.",
			limit, keyInfo.ProjectID,
		)
		return nil, rpcstatus.Error(rpcstatus.ResourceExhausted, "Exceeded Usage Limit")
	}

	inlineUsed := int64(len(req.EncryptedInlineData))

	if err := endpoint.projectUsage.AddProjectStorageUsage(ctx, keyInfo.ProjectID, inlineUsed); err != nil {
		endpoint.log.Sugar().Errorf("Could not track new storage usage by project %v: %v", keyInfo.ProjectID, err)
		// but continue. it's most likely our own fault that we couldn't track it, and the only thing
		// that will be affected is our per-project bandwidth and storage limits.
	}

	metadata, err := proto.Marshal(&pb.SegmentMeta{
		EncryptedKey: req.EncryptedKey,
		KeyNonce:     req.EncryptedKeyNonce.Bytes(),
	})

	pointer := &pb.Pointer{
		Type:           pb.Pointer_INLINE,
		SegmentSize:    inlineUsed,
		CreationDate:   streamID.CreationDate,
		ExpirationDate: streamID.ExpirationDate,
		InlineSegment:  req.EncryptedInlineData,
		Metadata:       metadata,
	}

	err = endpoint.metainfo.UnsynchronizedPut(ctx, path, pointer)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	err = endpoint.orders.UpdatePutInlineOrder(ctx, keyInfo.ProjectID, streamID.Bucket, inlineUsed)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	endpoint.log.Info("Inline Segment Upload", zap.Stringer("Project ID", keyInfo.ProjectID), zap.String("operation", "put"), zap.String("type", "inline"))
	mon.Meter("req_put_inline").Mark(1)

	return &pb.SegmentMakeInlineResponse{}, nil
}

// BeginDeleteSegment begins segment deletion process
func (endpoint *Endpoint) BeginDeleteSegment(ctx context.Context, req *pb.SegmentBeginDeleteRequest) (resp *pb.SegmentBeginDeleteResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	streamID, err := endpoint.unmarshalSatStreamID(ctx, req.StreamId)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionDelete,
		Bucket:        streamID.Bucket,
		EncryptedPath: streamID.EncryptedPath,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	pointer, path, err := endpoint.getPointer(ctx, keyInfo.ProjectID, int64(req.Position.Index), streamID.Bucket, streamID.EncryptedPath)
	if err != nil {
		return nil, err
	}

	var limits []*pb.AddressedOrderLimit
	var privateKey storj.PiecePrivateKey
	if pointer.Type == pb.Pointer_REMOTE && pointer.Remote != nil {
		bucketID := createBucketID(keyInfo.ProjectID, streamID.Bucket)
		limits, privateKey, err = endpoint.orders.CreateDeleteOrderLimits(ctx, bucketID, pointer)
		if err != nil {
			return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
		}
	}

	// moved from FinishDeleteSegment to avoid inconsistency if someone will not
	// call FinishDeleteSegment on uplink side
	err = endpoint.metainfo.UnsynchronizedDelete(ctx, path)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	segmentID, err := endpoint.packSegmentID(ctx, &pb.SatSegmentID{
		StreamId:            streamID,
		OriginalOrderLimits: limits,
		Index:               req.Position.Index,
		CreationDate:        time.Now(),
	})

	endpoint.log.Info("Segment Delete", zap.Stringer("Project ID", keyInfo.ProjectID), zap.String("operation", "delete"), zap.String("type", "segment"))
	mon.Meter("req_delete_segment").Mark(1)

	return &pb.SegmentBeginDeleteResponse{
		SegmentId:       segmentID,
		AddressedLimits: limits,
		PrivateKey:      privateKey,
	}, nil
}

// FinishDeleteSegment finishes segment deletion process
func (endpoint *Endpoint) FinishDeleteSegment(ctx context.Context, req *pb.SegmentFinishDeleteRequest) (resp *pb.SegmentFinishDeleteResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	segmentID, err := endpoint.unmarshalSatSegmentID(ctx, req.SegmentId)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	streamID := segmentID.StreamId

	_, err = endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionDelete,
		Bucket:        streamID.Bucket,
		EncryptedPath: streamID.EncryptedPath,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	// at the moment logic is in BeginDeleteSegment

	return &pb.SegmentFinishDeleteResponse{}, nil
}

// ListSegments list object segments
func (endpoint *Endpoint) ListSegments(ctx context.Context, req *pb.SegmentListRequest) (resp *pb.SegmentListResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	streamID, err := endpoint.unmarshalSatStreamID(ctx, req.StreamId)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionList,
		Bucket:        streamID.Bucket,
		EncryptedPath: streamID.EncryptedPath,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	limit := req.Limit
	if limit == 0 || limit > listLimit {
		limit = listLimit
	}

	pointer, _, err := endpoint.getPointer(ctx, keyInfo.ProjectID, lastSegment, streamID.Bucket, streamID.EncryptedPath)
	if err != nil {
		if rpcstatus.Code(err) == rpcstatus.NotFound {
			return &pb.SegmentListResponse{}, nil
		}
		return nil, err
	}

	streamMeta := &pb.StreamMeta{}
	err = proto.Unmarshal(pointer.Metadata, streamMeta)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	endpoint.log.Info("Segment List", zap.Stringer("Project ID", keyInfo.ProjectID), zap.String("operation", "list"), zap.String("type", "segment"))
	mon.Meter("req_list_segment").Mark(1)

	if streamMeta.NumberOfSegments > 0 {
		// use unencrypted number of segments
		// TODO cleanup int32 vs int64
		return endpoint.listSegmentsFromNumberOfSegments(ctx, int32(streamMeta.NumberOfSegments), req.CursorPosition.Index, limit)
	}

	// list segments by requesting each segment from cursor index to n until n segment is not found
	return endpoint.listSegmentsManually(ctx, keyInfo.ProjectID, streamID, req.CursorPosition.Index, limit)
}

func (endpoint *Endpoint) listSegmentsFromNumberOfSegments(ctx context.Context, numberOfSegments, cursorIndex, limit int32) (resp *pb.SegmentListResponse, err error) {
	if numberOfSegments <= 0 {
		endpoint.log.Error(
			"Invalid number of segments; this function requires the value to be greater than 0",
			zap.Int32("numberOfSegments", numberOfSegments),
		)
		return nil, rpcstatus.Error(rpcstatus.Internal, "unable to list segments")
	}

	if cursorIndex > numberOfSegments {
		endpoint.log.Error(
			"Invalid number cursor index; the index cannot be greater than the total number of segments",
			zap.Int32("numberOfSegments", numberOfSegments),
			zap.Int32("cursorIndex", cursorIndex),
		)
		return nil, rpcstatus.Error(rpcstatus.Internal, "unable to list segments")
	}

	numberOfSegments -= cursorIndex

	var (
		segmentItems = make([]*pb.SegmentListItem, 0)
		more         = false
	)
	if numberOfSegments > 0 {
		segmentItems = make([]*pb.SegmentListItem, 0, int(numberOfSegments))

		if numberOfSegments > limit {
			more = true
			numberOfSegments = limit
		} else {
			// remove last segment to avoid if statements in loop to detect last segment,
			// last segment will be added manually at the end of this block
			numberOfSegments--
		}

		for index := int32(0); index < numberOfSegments; index++ {
			segmentItems = append(segmentItems, &pb.SegmentListItem{
				Position: &pb.SegmentPosition{
					Index: index + cursorIndex,
				},
			})
		}

		if !more {
			// last segment is always the last one
			segmentItems = append(segmentItems, &pb.SegmentListItem{
				Position: &pb.SegmentPosition{
					Index: lastSegment,
				},
			})
		}
	}

	return &pb.SegmentListResponse{
		Items: segmentItems,
		More:  more,
	}, nil
}

// listSegmentManually lists the segments that belongs to projectID and streamID
// from the cursorIndex up to the limit. It stops before the limit when
// cursorIndex + n returns a not found pointer.
//
// limit must be greater than 0 and cursorIndex greater than or equal than 0,
// otherwise an error is returned.
func (endpoint *Endpoint) listSegmentsManually(ctx context.Context, projectID uuid.UUID, streamID *pb.SatStreamID, cursorIndex, limit int32) (resp *pb.SegmentListResponse, err error) {
	if limit <= 0 {
		return nil, rpcstatus.Errorf(
			rpcstatus.InvalidArgument, "invalid limit, cannot be 0 or negative. Got %d", limit,
		)
	}

	index := int64(cursorIndex)
	segmentItems := make([]*pb.SegmentListItem, 0)
	more := false

	for {
		_, _, err := endpoint.getPointer(ctx, projectID, index, streamID.Bucket, streamID.EncryptedPath)
		if err != nil {
			if rpcstatus.Code(err) != rpcstatus.NotFound {
				return nil, err
			}

			break
		}

		if limit == int32(len(segmentItems)) {
			more = true
			break
		}
		segmentItems = append(segmentItems, &pb.SegmentListItem{
			Position: &pb.SegmentPosition{
				Index: int32(index),
			},
		})

		index++
	}

	if limit > int32(len(segmentItems)) {
		segmentItems = append(segmentItems, &pb.SegmentListItem{
			Position: &pb.SegmentPosition{
				Index: lastSegment,
			},
		})
	} else {
		more = true
	}

	return &pb.SegmentListResponse{
		Items: segmentItems,
		More:  more,
	}, nil
}

// DownloadSegment returns data necessary to download segment
func (endpoint *Endpoint) DownloadSegment(ctx context.Context, req *pb.SegmentDownloadRequest) (resp *pb.SegmentDownloadResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	streamID, err := endpoint.unmarshalSatStreamID(ctx, req.StreamId)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	keyInfo, err := endpoint.validateAuth(ctx, req.Header, macaroon.Action{
		Op:            macaroon.ActionRead,
		Bucket:        streamID.Bucket,
		EncryptedPath: streamID.EncryptedPath,
		Time:          time.Now(),
	})
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Unauthenticated, err.Error())
	}

	bucketID := createBucketID(keyInfo.ProjectID, streamID.Bucket)

	exceeded, limit, err := endpoint.projectUsage.ExceedsBandwidthUsage(ctx, keyInfo.ProjectID, bucketID)
	if err != nil {
		endpoint.log.Error("retrieving project bandwidth total", zap.Error(err))
	}
	if exceeded {
		endpoint.log.Sugar().Errorf("monthly project limits are %s of storage and bandwidth usage. This limit has been exceeded for bandwidth for projectID %s.",
			limit, keyInfo.ProjectID,
		)
		return nil, rpcstatus.Error(rpcstatus.ResourceExhausted, "Exceeded Usage Limit")
	}

	pointer, _, err := endpoint.getPointer(ctx, keyInfo.ProjectID, int64(req.CursorPosition.Index), streamID.Bucket, streamID.EncryptedPath)
	if err != nil {
		return nil, err
	}

	segmentID, err := endpoint.packSegmentID(ctx, &pb.SatSegmentID{})

	var encryptedKeyNonce storj.Nonce
	var encryptedKey []byte
	if len(pointer.Metadata) != 0 {
		var segmentMeta *pb.SegmentMeta
		if req.CursorPosition.Index == lastSegment {
			streamMeta := &pb.StreamMeta{}
			err = proto.Unmarshal(pointer.Metadata, streamMeta)
			if err != nil {
				return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
			}
			segmentMeta = streamMeta.LastSegmentMeta
		} else {
			segmentMeta = &pb.SegmentMeta{}
			err = proto.Unmarshal(pointer.Metadata, segmentMeta)
			if err != nil {
				return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
			}
		}
		if segmentMeta != nil {
			encryptedKeyNonce, err = storj.NonceFromBytes(segmentMeta.KeyNonce)
			if err != nil {
				endpoint.log.Error("unable to get encryption key nonce from metadata", zap.Error(err))
				return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
			}

			encryptedKey = segmentMeta.EncryptedKey
		}
	}

	if pointer.Type == pb.Pointer_INLINE {
		err := endpoint.orders.UpdateGetInlineOrder(ctx, keyInfo.ProjectID, streamID.Bucket, int64(len(pointer.InlineSegment)))
		if err != nil {
			return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
		}
		endpoint.log.Info("Inline Segment Download", zap.Stringer("Project ID", keyInfo.ProjectID), zap.String("operation", "get"), zap.String("type", "inline"))
		mon.Meter("req_get_inline").Mark(1)

		return &pb.SegmentDownloadResponse{
			SegmentId:           segmentID,
			SegmentSize:         pointer.SegmentSize,
			EncryptedInlineData: pointer.InlineSegment,

			EncryptedKeyNonce: encryptedKeyNonce,
			EncryptedKey:      encryptedKey,
		}, nil
	} else if pointer.Type == pb.Pointer_REMOTE && pointer.Remote != nil {
		limits, privateKey, err := endpoint.orders.CreateGetOrderLimits(ctx, bucketID, pointer)
		if err != nil {
			if orders.ErrDownloadFailedNotEnoughPieces.Has(err) {
				endpoint.log.Sugar().Errorf("unable to create order limits for project id %s from api key id %s: %v.", keyInfo.ProjectID.String(), keyInfo.ID.String(), zap.Error(err))
			}
			return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
		}

		limits = sortLimits(limits, pointer)

		// workaround to avoid sending nil values on top level
		for i := range limits {
			if limits[i] == nil {
				limits[i] = &pb.AddressedOrderLimit{}
			}
		}

		endpoint.log.Info("Segment Download", zap.Stringer("Project ID", keyInfo.ProjectID), zap.String("operation", "get"), zap.String("type", "remote"))
		mon.Meter("req_get_remote").Mark(1)

		return &pb.SegmentDownloadResponse{
			SegmentId:       segmentID,
			AddressedLimits: limits,
			PrivateKey:      privateKey,
			SegmentSize:     pointer.SegmentSize,

			EncryptedKeyNonce: encryptedKeyNonce,
			EncryptedKey:      encryptedKey,
		}, nil
	}

	return &pb.SegmentDownloadResponse{}, rpcstatus.Error(rpcstatus.Internal, "invalid type of pointer")
}

// getPointer returns the pointer and the segment path projectID, bucket and
// encryptedPath. It returns an error with a specific RPC status.
func (endpoint *Endpoint) getPointer(
	ctx context.Context, projectID uuid.UUID, segmentIndex int64, bucket, encryptedPath []byte,
) (_ *pb.Pointer, _ string, err error) {
	defer mon.Task()(&ctx, projectID.String(), segmentIndex, bucket, encryptedPath)(&err)
	path, err := CreatePath(ctx, projectID, segmentIndex, bucket, encryptedPath)
	if err != nil {
		return nil, "", rpcstatus.Error(rpcstatus.InvalidArgument, err.Error())
	}

	pointer, err := endpoint.metainfo.Get(ctx, path)
	if err != nil {
		if storj.ErrObjectNotFound.Has(err) {
			return nil, "", rpcstatus.Error(rpcstatus.NotFound, err.Error())
		}

		endpoint.log.Error("error getting the pointer from metainfo service", zap.Error(err))
		return nil, "", rpcstatus.Error(rpcstatus.Internal, err.Error())
	}
	return pointer, path, nil
}

// getObjectNumberOfSegments returns the number of segments of the indicated
// object by projectID, bucket and encryptedPath.
//
// It returns 0 if the number is unknown.
func (endpoint *Endpoint) getObjectNumberOfSegments(ctx context.Context, projectID uuid.UUID, bucket, encryptedPath []byte) (_ int64, err error) {
	defer mon.Task()(&ctx, projectID.String(), bucket, encryptedPath)(&err)

	pointer, _, err := endpoint.getPointer(ctx, projectID, lastSegment, bucket, encryptedPath)
	if err != nil {
		return 0, err
	}

	meta := &pb.StreamMeta{}
	err = proto.Unmarshal(pointer.Metadata, meta)
	if err != nil {
		endpoint.log.Error("error unmarshaling pointer metadata", zap.Error(err))
		return 0, rpcstatus.Error(rpcstatus.Internal, "unable to unmarshal metadata")
	}

	return meta.NumberOfSegments, nil
}

// sortLimits sorts order limits and fill missing ones with nil values
func sortLimits(limits []*pb.AddressedOrderLimit, pointer *pb.Pointer) []*pb.AddressedOrderLimit {
	sorted := make([]*pb.AddressedOrderLimit, pointer.GetRemote().GetRedundancy().GetTotal())
	for _, piece := range pointer.GetRemote().GetRemotePieces() {
		sorted[piece.GetPieceNum()] = getLimitByStorageNodeID(limits, piece.NodeId)
	}
	return sorted
}

func getLimitByStorageNodeID(limits []*pb.AddressedOrderLimit, storageNodeID storj.NodeID) *pb.AddressedOrderLimit {
	for _, limit := range limits {
		if limit == nil || limit.GetLimit() == nil {
			continue
		}

		if limit.GetLimit().StorageNodeId == storageNodeID {
			return limit
		}
	}
	return nil
}

func (endpoint *Endpoint) packStreamID(ctx context.Context, satStreamID *pb.SatStreamID) (streamID storj.StreamID, err error) {
	defer mon.Task()(&ctx)(&err)

	signedStreamID, err := signing.SignStreamID(ctx, endpoint.satellite, satStreamID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	encodedStreamID, err := proto.Marshal(signedStreamID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}

	streamID, err = storj.StreamIDFromBytes(encodedStreamID)
	if err != nil {
		return nil, rpcstatus.Error(rpcstatus.Internal, err.Error())
	}
	return streamID, nil
}

func (endpoint *Endpoint) packSegmentID(ctx context.Context, satSegmentID *pb.SatSegmentID) (segmentID storj.SegmentID, err error) {
	defer mon.Task()(&ctx)(&err)

	signedSegmentID, err := signing.SignSegmentID(ctx, endpoint.satellite, satSegmentID)
	if err != nil {
		return nil, err
	}

	encodedSegmentID, err := proto.Marshal(signedSegmentID)
	if err != nil {
		return nil, err
	}

	segmentID, err = storj.SegmentIDFromBytes(encodedSegmentID)
	if err != nil {
		return nil, err
	}
	return segmentID, nil
}

func (endpoint *Endpoint) unmarshalSatStreamID(ctx context.Context, streamID storj.StreamID) (_ *pb.SatStreamID, err error) {
	defer mon.Task()(&ctx)(&err)

	satStreamID := &pb.SatStreamID{}
	err = proto.Unmarshal(streamID, satStreamID)
	if err != nil {
		return nil, err
	}

	err = signing.VerifyStreamID(ctx, endpoint.satellite, satStreamID)
	if err != nil {
		return nil, err
	}

	if satStreamID.CreationDate.Before(time.Now().Add(-satIDExpiration)) {
		return nil, errs.New("stream ID expired")
	}

	return satStreamID, nil
}

func (endpoint *Endpoint) unmarshalSatSegmentID(ctx context.Context, segmentID storj.SegmentID) (_ *pb.SatSegmentID, err error) {
	defer mon.Task()(&ctx)(&err)

	satSegmentID := &pb.SatSegmentID{}
	err = proto.Unmarshal(segmentID, satSegmentID)
	if err != nil {
		return nil, err
	}
	if satSegmentID.StreamId == nil {
		return nil, errs.New("stream ID missing")
	}

	err = signing.VerifySegmentID(ctx, endpoint.satellite, satSegmentID)
	if err != nil {
		return nil, err
	}

	if satSegmentID.CreationDate.Before(time.Now().Add(-satIDExpiration)) {
		return nil, errs.New("segment ID expired")
	}

	return satSegmentID, nil
}

// DeleteObjectPieces deletes all the pieces of the storage nodes that belongs
// to the specified object.
//
// NOTE: this method is exported for being able to individually test it without
// having import cycles.
func (endpoint *Endpoint) DeleteObjectPieces(
	ctx context.Context, projectID uuid.UUID, bucket, encryptedPath []byte,
) (err error) {
	defer mon.Task()(&ctx, projectID.String(), bucket, encryptedPath)(&err)

	// We should ignore client cancelling and always try to delete segments.
	ctx = context2.WithoutCancellation(ctx)

	var (
		lastSegmentNotFound  = false
		prevLastSegmentIndex int64
	)
	{
		numOfSegments, err := endpoint.getObjectNumberOfSegments(ctx, projectID, bucket, encryptedPath)
		if err != nil {
			if !errs2.IsRPC(err, rpcstatus.NotFound) {
				return err
			}

			// Not found is that the last segment doesn't exist, so we proceed deleting
			// in a reverse order the continuous segments starting from index 0
			lastSegmentNotFound = true
			{
				var err error
				prevLastSegmentIndex, err = endpoint.findIndexPreviousLastSegmentWhenNotKnowingNumSegments(
					ctx, projectID, bucket, encryptedPath,
				)
				if err != nil {
					endpoint.log.Error("unexpected error while finding last segment index previous to the last segment",
						zap.Stringer("project_id", projectID),
						zap.ByteString("bucket_name", bucket),
						zap.Binary("encrypted_path", encryptedPath),
						zap.Error(err),
					)

					return err
				}
			}

			// There no last segment and any continuous segment so we return the
			// NotFound error handled in this conditional block
			if prevLastSegmentIndex == -1 {
				return err
			}

		} else {
			prevLastSegmentIndex = numOfSegments - 2 // because of the last segment and because it's an index
		}
	}

	var (
		nodesPieces = make(map[storj.NodeID][]storj.PieceID)
		nodeIDs     storj.NodeIDList
		piecesSize  int64
	)

	if !lastSegmentNotFound {
		// first delete the last segment
		pointer, err := endpoint.deletePointer(ctx, projectID, lastSegment, bucket, encryptedPath)
		if err != nil {
			if storj.ErrObjectNotFound.Has(err) {
				endpoint.log.Warn(
					"unexpected not found error while deleting a pointer, it may have been deleted concurrently",
					zap.String("pointer_path",
						fmt.Sprintf("%s/l/%s/%q", projectID, bucket, encryptedPath),
					),
					zap.String("segment", "l"),
				)
			} else {
				endpoint.log.Error("unexpected error while deleting object pieces",
					zap.Stringer("project_id", projectID),
					zap.ByteString("bucket_name", bucket),
					zap.Binary("encrypted_path", encryptedPath),
					zap.Error(err),
				)
				return rpcstatus.Error(rpcstatus.Internal, err.Error())
			}
		}

		if err == nil && pointer.Type == pb.Pointer_REMOTE {
			rootPieceID := pointer.GetRemote().RootPieceId
			for _, piece := range pointer.GetRemote().GetRemotePieces() {
				pieceID := rootPieceID.Derive(piece.NodeId, piece.PieceNum)
				pieces, ok := nodesPieces[piece.NodeId]
				if !ok {
					nodesPieces[piece.NodeId] = []storj.PieceID{pieceID}
					nodeIDs = append(nodeIDs, piece.NodeId)
					continue
				}

				nodesPieces[piece.NodeId] = append(pieces, pieceID)
			}
			segmentSize, _ := calculateSpaceUsed(pointer)
			piecesSize += segmentSize
		}
	}

	for segmentIdx := prevLastSegmentIndex; segmentIdx >= 0; segmentIdx-- {
		pointer, err := endpoint.deletePointer(ctx, projectID, segmentIdx, bucket, encryptedPath)
		if err != nil {
			segment := "s" + strconv.FormatInt(segmentIdx, 10)
			if storj.ErrObjectNotFound.Has(err) {
				endpoint.log.Warn(
					"unexpected not found error while deleting a pointer, it may have been deleted concurrently",
					zap.String("pointer_path",
						fmt.Sprintf("%s/%s/%s/%q", projectID, segment, bucket, encryptedPath),
					),
					zap.String("segment", segment),
				)
			} else {
				endpoint.log.Warn(
					"unexpected error while deleting a pointer",
					zap.String("pointer_path",
						fmt.Sprintf("%s/%s/%s/%q", projectID, segment, bucket, encryptedPath),
					),
					zap.String("segment", segment),
					zap.Error(err),
				)
			}

			// We continue with the next segment and we leave the pieces of this
			// segment to be deleted by the garbage collector
			continue
		}

		if pointer.Type != pb.Pointer_REMOTE {
			continue
		}

		rootPieceID := pointer.GetRemote().RootPieceId
		for _, piece := range pointer.GetRemote().GetRemotePieces() {
			pieceID := rootPieceID.Derive(piece.NodeId, piece.PieceNum)
			pieces, ok := nodesPieces[piece.NodeId]
			if !ok {
				nodesPieces[piece.NodeId] = []storj.PieceID{pieceID}
				nodeIDs = append(nodeIDs, piece.NodeId)
				continue
			}

			nodesPieces[piece.NodeId] = append(pieces, pieceID)
		}

		segmentSize, _ := calculateSpaceUsed(pointer)
		piecesSize += segmentSize
	}

	if len(nodeIDs) == 0 {
		return
	}

	nodes, err := endpoint.overlay.KnownReliable(ctx, nodeIDs)
	if err != nil {
		endpoint.log.Warn("unable to look up nodes from overlay",
			zap.String("object_path",
				fmt.Sprintf("%s/%s/%q", projectID, bucket, encryptedPath),
			),
			zap.Error(err),
		)
		// Pieces will be collected by garbage collector
		return nil
	}

	var nodesPiecesList NodesPieces
	for _, node := range nodes {
		nodesPiecesList = append(nodesPiecesList, NodePieces{
			Node:   node,
			Pieces: nodesPieces[node.Id],
		})
	}

	err = endpoint.deletePieces.DeletePieces(ctx, nodesPiecesList, deleteObjectPiecesSuccessThreshold)
	if nil != err {
		return err
	}

	if piecesSize != 0 {
		if err := endpoint.projectUsage.AddProjectStorageUsage(ctx, projectID, -piecesSize); err != nil {
			endpoint.log.Error("Could not track new storage usage by project",
				zap.Stringer("Project ID", projectID),
				zap.Error(err),
			)
			// but continue. it's most likely our own fault that we couldn't track it, and the only thing
			// that will be affected is our per-project bandwidth and storage limits.
		}
	}

	return nil
}

// deletePointer deletes a pointer returning the deleted pointer.
//
// If the pointer isn't found when getting or deleting it, it returns
// storj.ErrObjectNotFound error.
func (endpoint *Endpoint) deletePointer(
	ctx context.Context, projectID uuid.UUID, segmentIndex int64, bucket, encryptedPath []byte,
) (_ *pb.Pointer, err error) {
	defer mon.Task()(&ctx, projectID, segmentIndex, bucket, encryptedPath)(&err)

	pointer, path, err := endpoint.getPointer(ctx, projectID, segmentIndex, bucket, encryptedPath)
	if err != nil {
		if errs2.IsRPC(err, rpcstatus.NotFound) {
			return nil, storj.ErrObjectNotFound.New("%s", err.Error())
		}
		return nil, err
	}

	err = endpoint.metainfo.UnsynchronizedDelete(ctx, path)
	if err != nil {
		return nil, err
	}

	return pointer, nil
}

// findIndexPreviousLastSegmentWhenNotKnowingNumSegments returns the index of
// the segment previous to the last segment when there is an unknown number of
// segments.
//
// It returns -1 index if none is found and error if there is some error getting
// the segments' pointers.
func (endpoint *Endpoint) findIndexPreviousLastSegmentWhenNotKnowingNumSegments(
	ctx context.Context, projectID uuid.UUID, bucket, encryptedPath []byte,
) (index int64, err error) {
	defer mon.Task()(&ctx, projectID, bucket, encryptedPath)(&err)

	lastIdxFound := int64(-1)
	for {
		_, _, err := endpoint.getPointer(ctx, projectID, lastIdxFound+1, bucket, encryptedPath)
		if err != nil {
			if errs2.IsRPC(err, rpcstatus.NotFound) {
				break
			}
			return -1, err
		}

		lastIdxFound++
	}

	return lastIdxFound, nil
}

func (endpoint *Endpoint) redundancyScheme() *pb.RedundancyScheme {
	return &pb.RedundancyScheme{
		Type:             pb.RedundancyScheme_RS,
		MinReq:           int32(endpoint.requiredRSConfig.MinThreshold),
		RepairThreshold:  int32(endpoint.requiredRSConfig.RepairThreshold),
		SuccessThreshold: int32(endpoint.requiredRSConfig.SuccessThreshold),
		Total:            int32(endpoint.requiredRSConfig.TotalThreshold),
		ErasureShareSize: endpoint.requiredRSConfig.ErasureShareSize.Int32(),
	}
}
