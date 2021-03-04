// Copyright (C) 2020 Storj Labs, Inc.
// See LICENSE for copying information.

package multinode

import (
	"context"
	"net"

	"github.com/spacemonkeygo/monkit/v3"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"storj.io/common/identity"
	"storj.io/common/peertls/tlsopts"
	"storj.io/common/rpc"
	"storj.io/private/debug"
	"storj.io/storj/multinode/console"
	"storj.io/storj/multinode/console/server"
	"storj.io/storj/multinode/nodes"
	"storj.io/storj/multinode/payouts"
	"storj.io/storj/private/lifecycle"
)

var (
	mon = monkit.Package()
)

// DB is the master database for Multinode Dashboard.
//
// architecture: Master Database
type DB interface {
	// Nodes returns nodes database.
	Nodes() nodes.DB
	// Members returns members database.
	Members() console.Members

	// Close closes the database.
	Close() error
	// CreateSchema creates schema.
	CreateSchema(ctx context.Context) error
}

// Config is all the configuration parameters for a Multinode Dashboard.
type Config struct {
	Identity identity.Config
	Debug    debug.Config

	Console server.Config
}

// Peer is the a Multinode Dashboard application itself.
//
// architecture: Peer
type Peer struct {
	// core dependencies
	Log      *zap.Logger
	Identity *identity.FullIdentity
	DB       DB

	Dialer rpc.Dialer

	// contains logic of nodes domain.
	Nodes struct {
		Service *nodes.Service
	}

	// contains logic of payouts domain.
	Payouts struct {
		Service *payouts.Service
	}

	// Web server with web UI.
	Console struct {
		Listener net.Listener
		Endpoint *server.Server
	}

	Servers *lifecycle.Group
}

// New creates a new instance of Multinode Dashboard application.
func New(log *zap.Logger, full *identity.FullIdentity, config Config, db DB) (_ *Peer, err error) {
	peer := &Peer{
		Log:      log,
		Identity: full,
		DB:       db,
		Servers:  lifecycle.NewGroup(log.Named("servers")),
	}

	tlsConfig := tlsopts.Config{
		UsePeerCAWhitelist: false,
		PeerIDVersions:     "0",
	}

	tlsOptions, err := tlsopts.NewOptions(peer.Identity, tlsConfig, nil)
	if err != nil {
		return nil, err
	}

	peer.Dialer = rpc.NewDefaultDialer(tlsOptions)

	{ // nodes setup
		peer.Nodes.Service = nodes.NewService(
			peer.Log.Named("nodes:service"),
			peer.Dialer,
			peer.DB.Nodes(),
		)
	}

	{ // payouts setup
		peer.Payouts.Service = payouts.NewService(
			peer.Log.Named("payouts:service"),
			peer.Dialer,
			peer.DB.Nodes(),
		)
	}

	{ // console setup
		peer.Console.Listener, err = net.Listen("tcp", config.Console.Address)
		if err != nil {
			return nil, err
		}

		peer.Console.Endpoint, err = server.NewServer(
			peer.Log.Named("console:endpoint"),
			config.Console,
			peer.Nodes.Service,
			peer.Payouts.Service,
			peer.Console.Listener,
		)
		if err != nil {
			return nil, err
		}

		peer.Servers.Add(lifecycle.Item{
			Name:  "console:endpoint",
			Run:   peer.Console.Endpoint.Run,
			Close: peer.Console.Endpoint.Close,
		})
	}

	return peer, nil
}

// Run runs Multinode Dashboard services and servers until it's either closed or it errors.
func (peer *Peer) Run(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)

	group, ctx := errgroup.WithContext(ctx)

	peer.Servers.Run(ctx, group)

	return group.Wait()
}

// Close closes all the resources.
func (peer *Peer) Close() error {
	return peer.Servers.Close()
}
