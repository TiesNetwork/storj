// Copyright (C) 2020 Storj Labs, Inc.
// See LICENSE for copying information.

package satellite

import (
	"context"
	"errors"
	"net"

	"github.com/spacemonkeygo/monkit/v3"
	"github.com/zeebo/errs"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"storj.io/common/identity"
	"storj.io/common/peertls/extensions"
	"storj.io/common/storj"
	"storj.io/storj/pkg/debug"
	"storj.io/storj/private/lifecycle"
	"storj.io/storj/private/version"
	"storj.io/storj/private/version/checker"
	"storj.io/storj/satellite/accounting"
	"storj.io/storj/satellite/admin"
	"storj.io/storj/satellite/metainfo"
)

// Admin is the satellite core process that runs chores
//
// architecture: Peer
type Admin struct {
	// core dependencies
	Log      *zap.Logger
	Identity *identity.FullIdentity

	Servers  *lifecycle.Group
	Services *lifecycle.Group

	Debug struct {
		Listener net.Listener
		Server   *debug.Server
	}

	Version *checker.Service

	Admin struct {
		Listener net.Listener
		Server   *admin.Server
	}
}

// NewAdmin creates a new satellite admin peer.
func NewAdmin(log *zap.Logger, full *identity.FullIdentity, db DB,
	pointerDB metainfo.PointerDB,
	revocationDB extensions.RevocationDB,
	accountingCache accounting.Cache,
	versionInfo version.Info, config *Config) (*Admin, error) {
	peer := &Admin{
		Log:      log,
		Identity: full,

		Servers:  lifecycle.NewGroup(log.Named("servers")),
		Services: lifecycle.NewGroup(log.Named("services")),
	}

	{ // setup debug
		var err error
		if config.Debug.Address != "" {
			peer.Debug.Listener, err = net.Listen("tcp", config.Debug.Address)
			if err != nil {
				withoutStack := errors.New(err.Error())
				peer.Log.Debug("failed to start debug endpoints", zap.Error(withoutStack))
				err = nil
			}
		}
		debugConfig := config.Debug
		debugConfig.ControlTitle = "Admin"
		peer.Debug.Server = debug.NewServer(log.Named("debug"), peer.Debug.Listener, monkit.Default, debugConfig)
		peer.Servers.Add(lifecycle.Item{
			Name:  "debug",
			Run:   peer.Debug.Server.Run,
			Close: peer.Debug.Server.Close,
		})
	}

	{
		if !versionInfo.IsZero() {
			peer.Log.Sugar().Debugf("Binary Version: %s with CommitHash %s, built at %s as Release %v",
				versionInfo.Version.String(), versionInfo.CommitHash, versionInfo.Timestamp.String(), versionInfo.Release)
		}
		peer.Version = checker.NewService(log.Named("version"), config.Version, versionInfo, "Satellite")

		peer.Services.Add(lifecycle.Item{
			Name: "version",
			Run:  peer.Version.Run,
		})
	}

	{ // setup admin
		var err error
		peer.Admin.Listener, err = net.Listen("tcp", config.Admin.Address)
		if err != nil {
			return nil, err
		}
		liveAccounting := accounting.NewService(
			db.ProjectAccounting(),
			accountingCache,
			config.Rollup.MaxAlphaUsage,
		)
		service := admin.NewService(db.Console(), db.ProjectAccounting(), liveAccounting, &admin.ServiceConfig{
			SatelliteNodeID:  &peer.Identity.ID,
			SatelliteAddress: config.Server.Address,
		})
		peer.Admin.Server = admin.NewServer(log.Named("admin"), peer.Admin.Listener, config.Admin, service)
		peer.Servers.Add(lifecycle.Item{
			Name:  "admin",
			Run:   peer.Admin.Server.Run,
			Close: peer.Admin.Server.Close,
		})
	}

	return peer, nil
}

// Run runs satellite until it's either closed or it errors.
func (peer *Admin) Run(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)

	group, ctx := errgroup.WithContext(ctx)

	peer.Servers.Run(ctx, group)
	peer.Services.Run(ctx, group)

	return group.Wait()
}

// Close closes all the resources.
func (peer *Admin) Close() error {
	return errs.Combine(
		peer.Servers.Close(),
		peer.Services.Close(),
	)
}

// ID returns the peer ID.
func (peer *Admin) ID() storj.NodeID { return peer.Identity.ID }
