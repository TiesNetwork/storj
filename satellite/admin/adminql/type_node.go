package adminql

import (
	"errors"
	"time"

	"github.com/graphql-go/graphql"
	"storj.io/storj/satellite/admin/service"
)

const (
	// StorageNodeType is a graphql type for storage node
	StorageNodeType = "StorageNode"
	// StorageNodeUsageType is a graphql type for storage node usage
	StorageNodeUsageType = "NodeUsage"

	// FieldNodeID is a field name for storage node id
	FieldNodeID = "nodeID"
	// FieldNodeWallet is a field name for node wallet address
	FieldNodeWallet = "wallet"
	// FieldNodeAddress is a field name for storage node address
	FieldNodeAddress = "address"
	// FieldNodeLastNet is a field name for storage node lastNet
	FieldNodeLastNet = "lastNet"
	// FieldNodeEmail is a field name for storage node email
	FieldNodeEmail = "email"
	// FieldNodeFreeBandwidth is a field name for storage node freeBandwidth
	FieldNodeFreeBandwidth = "freeBandwidth"
	// FieldNodeFreeDisk is a field name for storage node freeDisk
	FieldNodeFreeDisk = "freeDisk"
	// FieldNodePieceCount is a field name for storage node pieceCount
	FieldNodePieceCount = "pieceCount"
	// FieldNodeTimestamp is a field name for storage node timestamp
	FieldNodeTimestamp = "timestamp"
	// FieldNodeRelease is a field name for storage node release
	FieldNodeRelease = "release"
	// FieldNodeAuditSuccessCount is a field name for storage node auditSuccessCount
	FieldNodeAuditSuccessCount = "auditSuccessCount"
	// FieldNodeTotalAuditCount is a field name for storage node totalAuditCount
	FieldNodeTotalAuditCount = "totalAuditCount"
	// FieldNodeUptimeSuccessCount is a field name for storage node uptimeSuccessCount
	FieldNodeUptimeSuccessCount = "uptimeSuccessCount"
	// FieldNodeTotalUptimeCount is a field name for storage node totalUptimeCount
	FieldNodeTotalUptimeCount = "totalUptimeCount"
	// FieldNodeCreatedAt is a field name for storage node createdAt
	FieldNodeCreatedAt = "createdAt"
	// FieldNodeUpdatedAt is a field name for storage node updatedAt
	FieldNodeUpdatedAt = "updatedAt"
	// FieldNodeLastContactSuccessAt is a field name for storage node lastContactSuccessAt
	FieldNodeLastContactSuccessAt = "lastContactSuccessAt"
	// FieldNodeLastContactFailureAt is a field name for storage node lastContactFailureAt
	FieldNodeLastContactFailureAt = "lastContactFailureAt"
	// FieldNodeContained is a field name for storage node contained
	FieldNodeContained = "contained"
	// FieldNodeDisqualifiedAt is a field name for storage node disqualifiedAt
	FieldNodeDisqualifiedAt = "disqualifiedAt"
	// FieldNodeDisqualified is a field name for storage node disqualified
	FieldNodeDisqualified = "disqualified"
	// FieldNodeAuditReputationAlpha is a field name for storage node auditReputationAlpha
	FieldNodeAuditReputationAlpha = "auditReputationAlpha"
	// FieldNodeAuditReputationBeta is a field name for storage node auditReputationBeta
	FieldNodeAuditReputationBeta = "auditReputationBeta"
	// FieldNodeUptimeReputationAlpha is a field name for storage node uptimeReputationAlpha
	FieldNodeUptimeReputationAlpha = "uptimeReputationAlpha"
	// FieldNodeUptimeReputationBeta is a field name for storage node uptimeReputationBeta
	FieldNodeUptimeReputationBeta = "uptimeReputationBeta"
	// FieldNodeExitInitiatedAt is a field name for storage node exitInitiatedAt
	FieldNodeExitInitiatedAt = "exitInitiatedAt"
	// FieldNodeExitLoopCompletedAt is a field name for storage node exitLoopCompletedAt
	FieldNodeExitLoopCompletedAt = "exitLoopCompletedAt"
	// FieldNodeExitFinishedAt is a field name for storage node exitFinishedAt
	FieldNodeExitFinishedAt = "exitFinishedAt"
	// FieldNodeExitSuccess is a field name for storage node exitSuccess
	FieldNodeExitSuccess = "exitSuccess"
	// FieldNodeUsage is a field name for for storage node usage
	FieldNodeUsage = "usage"
	// FieldNodePutTotal is a field name for for storage node putTotal
	FieldNodePutTotal = "putTotal"
	// FieldNodeGetTotal is a field name for for storage node getTotal
	FieldNodeGetTotal = "getTotal"
	// FieldNodeGetAuditTotal is a field name for for storage node getAuditTotal
	FieldNodeGetAuditTotal = "getAuditTotal"
	// FieldNodeGetRepairTotal is a field name for for storage node getRepairTotal
	FieldNodeGetRepairTotal = "getRepairTotal"
	// FieldNodePutRepairTotal is a field name for for storage node putRepairTotal
	FieldNodePutRepairTotal = "putRepairTotal"
	// FieldNodeAtRestTotal is a field name for for storage node atRestTotal
	FieldNodeAtRestTotal = "atRestTotal"
)

func graphqlStorageNode(s *service.Service, types *TypeCreator) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: StorageNodeType,
		Fields: graphql.Fields{
			FieldNodeID: &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
			},
			FieldNodeAddress: &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			FieldNodeLastNet: &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			FieldNodeEmail: &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			FieldNodeWallet: &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			FieldNodeFreeBandwidth: &graphql.Field{
				Type: graphql.NewNonNull(bigInt),
			},
			FieldNodeFreeDisk: &graphql.Field{
				Type: graphql.NewNonNull(bigInt),
			},
			FieldNodePieceCount: &graphql.Field{
				Type: graphql.NewNonNull(bigInt),
			},
			FieldNodeTimestamp: &graphql.Field{
				Type: graphql.NewNonNull(graphql.DateTime),
			},
			FieldNodeRelease: &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			FieldNodeAuditSuccessCount: &graphql.Field{
				Type: graphql.NewNonNull(bigInt),
			},
			FieldNodeTotalAuditCount: &graphql.Field{
				Type: graphql.NewNonNull(bigInt),
			},
			FieldNodeUptimeSuccessCount: &graphql.Field{
				Type: graphql.NewNonNull(bigInt),
			},
			FieldNodeTotalUptimeCount: &graphql.Field{
				Type: graphql.NewNonNull(bigInt),
			},
			FieldNodeCreatedAt: &graphql.Field{
				Type: graphql.NewNonNull(graphql.DateTime),
			},
			FieldNodeUpdatedAt: &graphql.Field{
				Type: graphql.NewNonNull(graphql.DateTime),
			},
			FieldNodeLastContactSuccessAt: &graphql.Field{
				Type: graphql.NewNonNull(graphql.DateTime),
			},
			FieldNodeLastContactFailureAt: &graphql.Field{
				Type: graphql.NewNonNull(graphql.DateTime),
			},
			FieldNodeContained: &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			FieldNodeDisqualifiedAt: &graphql.Field{
				Type: graphql.DateTime,
			},
			FieldNodeAuditReputationAlpha: &graphql.Field{
				Type: graphql.NewNonNull(graphql.Float),
			},
			FieldNodeAuditReputationBeta: &graphql.Field{
				Type: graphql.NewNonNull(graphql.Float),
			},
			FieldNodeUptimeReputationAlpha: &graphql.Field{
				Type: graphql.NewNonNull(graphql.Float),
			},
			FieldNodeUptimeReputationBeta: &graphql.Field{
				Type: graphql.NewNonNull(graphql.Float),
			},
			FieldNodeExitInitiatedAt: &graphql.Field{
				Type: graphql.DateTime,
			},
			FieldNodeExitLoopCompletedAt: &graphql.Field{
				Type: graphql.DateTime,
			},
			FieldNodeExitFinishedAt: &graphql.Field{
				Type: graphql.DateTime,
			},
			FieldNodeExitSuccess: &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			FieldNodeDisqualified: &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					node, ok := p.Source.(*service.Node)
					if !ok {
						return nil, errors.New("Source object is not a " + StorageNodeType)
					}
					return nil != node.DisqualifiedAt, nil
				},
			},
			FieldNodeUsage: &graphql.Field{
				Type:    graphql.NewNonNull(types.storageNodeUsage),
				Args:    graphqlStorageNodeInnerStorageNodeUsageQueryArgs(),
				Resolve: graphqlStorageNodeInnerStorageNodeUsageQueryResolve(s),
			},
		},
	})
}

func graphqlStorageNodeQueryArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldNodeWallet: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.String),
		},
	}
}

func graphqlStorageNodeQueryResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		walletAddr, _ := p.Args[FieldNodeWallet].(string)
		return s.GetNodesByWallet(p.Context, walletAddr)
	}
}

func graphqlStorageNodeUsage() *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: StorageNodeUsageType,
		Fields: graphql.Fields{
			FieldNodeID: &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
			},
			FieldStartTime: &graphql.Field{
				Type: graphql.NewNonNull(graphql.DateTime),
			},
			FieldEndTime: &graphql.Field{
				Type: graphql.NewNonNull(graphql.DateTime),
			},
			FieldNodePutTotal: &graphql.Field{
				Type: graphql.NewNonNull(bigInt),
			},
			FieldNodeGetTotal: &graphql.Field{
				Type: graphql.NewNonNull(bigInt),
			},
			FieldNodeGetAuditTotal: &graphql.Field{
				Type: graphql.NewNonNull(bigInt),
			},
			FieldNodePutRepairTotal: &graphql.Field{
				Type: graphql.NewNonNull(bigInt),
			},
			FieldNodeGetRepairTotal: &graphql.Field{
				Type: graphql.NewNonNull(bigInt),
			},
			FieldNodeAtRestTotal: &graphql.Field{
				Type: graphql.NewNonNull(graphql.Float),
			},
		},
	})
}

func graphqlStorageNodeUsageQueryArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldNodeID: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.ID),
		},
		FieldStartTime: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.DateTime),
		},
		FieldEndTime: &graphql.ArgumentConfig{
			Type: graphql.DateTime,
		},
	}
}

func graphqlStorageNodeUsageQueryResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		nodeIDString, _ := p.Args[FieldNodeID].(string)
		return storageNodeUsageQueryResolveCommon(s, p, nodeIDString)
	}
}

func graphqlStorageNodeInnerStorageNodeUsageQueryArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		FieldStartTime: &graphql.ArgumentConfig{
			Type: graphql.NewNonNull(graphql.DateTime),
		},
		FieldEndTime: &graphql.ArgumentConfig{
			Type: graphql.DateTime,
		},
	}
}

func graphqlStorageNodeInnerStorageNodeUsageQueryResolve(s *service.Service) func(graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		node := p.Source.(*service.Node)
		return storageNodeUsageQueryResolveCommon(s, p, node.ID.String())
	}
}

func storageNodeUsageQueryResolveCommon(s *service.Service, p graphql.ResolveParams, nodeIDString string) (interface{}, error) {
	start, _ := p.Args[FieldStartTime].(time.Time)
	end, _ := p.Args[FieldEndTime].(time.Time)
	if end.IsZero() {
		end = time.Now()
	}
	if start.IsZero() {
		start = end
	}
	return s.GetNodeUsage(p.Context, nodeIDString, start, end)
}
