// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information

package testplanet

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/zeebo/errs"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"storj.io/common/errs2"
	"storj.io/common/identity"
	"storj.io/common/memory"
	"storj.io/common/peertls/extensions"
	"storj.io/common/peertls/tlsopts"
	"storj.io/common/rpc"
	"storj.io/common/storj"
	"storj.io/storj/pkg/debug"
	"storj.io/storj/pkg/revocation"
	"storj.io/storj/pkg/server"
	"storj.io/storj/private/version"
	versionchecker "storj.io/storj/private/version/checker"
	"storj.io/storj/satellite"
	"storj.io/storj/satellite/accounting"
	"storj.io/storj/satellite/accounting/live"
	"storj.io/storj/satellite/accounting/reportedrollup"
	"storj.io/storj/satellite/accounting/rollup"
	"storj.io/storj/satellite/accounting/tally"
	"storj.io/storj/satellite/admin"
	"storj.io/storj/satellite/audit"
	"storj.io/storj/satellite/console"
	"storj.io/storj/satellite/console/consoleweb"
	"storj.io/storj/satellite/contact"
	"storj.io/storj/satellite/dbcleanup"
	"storj.io/storj/satellite/downtime"
	"storj.io/storj/satellite/gc"
	"storj.io/storj/satellite/gracefulexit"
	"storj.io/storj/satellite/inspector"
	"storj.io/storj/satellite/mailservice"
	"storj.io/storj/satellite/marketingweb"
	"storj.io/storj/satellite/metainfo"
	"storj.io/storj/satellite/metrics"
	"storj.io/storj/satellite/nodestats"
	"storj.io/storj/satellite/orders"
	"storj.io/storj/satellite/overlay"
	"storj.io/storj/satellite/repair/checker"
	"storj.io/storj/satellite/repair/irreparable"
	"storj.io/storj/satellite/repair/repairer"
	"storj.io/storj/satellite/vouchers"
	"storj.io/storj/storage/redis/redisserver"
)

// SatelliteSystem contains all the processes needed to run a full Satellite setup
type SatelliteSystem struct {
	Core     *satellite.Core
	API      *satellite.API
	Repairer *satellite.Repairer
	Admin    *satellite.Admin

	Log      *zap.Logger
	Identity *identity.FullIdentity
	DB       satellite.DB

	Dialer rpc.Dialer

	Server *server.Server

	Version *versionchecker.Service

	Contact struct {
		Service  *contact.Service
		Endpoint *contact.Endpoint
	}

	Overlay struct {
		DB        overlay.DB
		Service   *overlay.Service
		Inspector *overlay.Inspector
	}

	Metainfo struct {
		Database  metainfo.PointerDB
		Service   *metainfo.Service
		Endpoint2 *metainfo.Endpoint
		Loop      *metainfo.Loop
	}

	Inspector struct {
		Endpoint *inspector.Endpoint
	}

	Orders struct {
		DB       orders.DB
		Endpoint *orders.Endpoint
		Service  *orders.Service
		Chore    *orders.Chore
	}

	Repair struct {
		Checker   *checker.Checker
		Repairer  *repairer.Service
		Inspector *irreparable.Inspector
	}
	Audit struct {
		Queue    *audit.Queue
		Worker   *audit.Worker
		Chore    *audit.Chore
		Verifier *audit.Verifier
		Reporter *audit.Reporter
	}

	GarbageCollection struct {
		Service *gc.Service
	}

	DBCleanup struct {
		Chore *dbcleanup.Chore
	}

	Accounting struct {
		Tally          *tally.Service
		Rollup         *rollup.Service
		ProjectUsage   *accounting.Service
		ReportedRollup *reportedrollup.Chore
	}

	LiveAccounting struct {
		Cache accounting.Cache
	}

	Mail struct {
		Service *mailservice.Service
	}

	Vouchers struct {
		Endpoint *vouchers.Endpoint
	}

	Console struct {
		Listener net.Listener
		Service  *console.Service
		Endpoint *consoleweb.Server
	}

	Marketing struct {
		Listener net.Listener
		Endpoint *marketingweb.Server
	}

	NodeStats struct {
		Endpoint *nodestats.Endpoint
	}

	GracefulExit struct {
		Chore    *gracefulexit.Chore
		Endpoint *gracefulexit.Endpoint
	}

	Metrics struct {
		Chore *metrics.Chore
	}

	DowntimeTracking struct {
		DetectionChore  *downtime.DetectionChore
		EstimationChore *downtime.EstimationChore
		Service         *downtime.Service
	}
}

// ID returns the ID of the Satellite system.
func (system *SatelliteSystem) ID() storj.NodeID { return system.API.Identity.ID }

// Local returns the peer local node info from the Satellite system API.
func (system *SatelliteSystem) Local() overlay.NodeDossier { return system.API.Contact.Service.Local() }

// Addr returns the public address from the Satellite system API.
func (system *SatelliteSystem) Addr() string { return system.API.Server.Addr().String() }

// URL returns the storj.NodeURL from the Satellite system API.
func (system *SatelliteSystem) URL() storj.NodeURL {
	return storj.NodeURL{ID: system.API.ID(), Address: system.API.Addr()}
}

// Close closes all the subsystems in the Satellite system
func (system *SatelliteSystem) Close() error {
	return errs.Combine(
		system.API.Close(),
		system.Core.Close(),
		system.Repairer.Close(),
		system.Admin.Close(),
	)
}

// Run runs all the subsystems in the Satellite system
func (system *SatelliteSystem) Run(ctx context.Context) (err error) {
	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() error {
		return errs2.IgnoreCanceled(system.Core.Run(ctx))
	})
	group.Go(func() error {
		return errs2.IgnoreCanceled(system.API.Run(ctx))
	})
	group.Go(func() error {
		return errs2.IgnoreCanceled(system.Repairer.Run(ctx))
	})
	group.Go(func() error {
		return errs2.IgnoreCanceled(system.Admin.Run(ctx))
	})
	return group.Wait()
}

// PrivateAddr returns the private address from the Satellite system API.
func (system *SatelliteSystem) PrivateAddr() string { return system.API.Server.PrivateAddr().String() }

// newSatellites initializes satellites
func (planet *Planet) newSatellites(count int) ([]*SatelliteSystem, error) {
	var xs []*SatelliteSystem
	defer func() {
		for _, x := range xs {
			planet.peers = append(planet.peers, newClosablePeer(x))
		}
	}()

	for i := 0; i < count; i++ {
		prefix := "satellite" + strconv.Itoa(i)
		log := planet.log.Named(prefix)

		storageDir := filepath.Join(planet.directory, prefix)
		if err := os.MkdirAll(storageDir, 0700); err != nil {
			return nil, err
		}

		identity, err := planet.NewIdentity()
		if err != nil {
			return nil, err
		}

		var db satellite.DB
		if planet.config.Reconfigure.NewSatelliteDB != nil {
			db, err = planet.config.Reconfigure.NewSatelliteDB(log.Named("db"), i)
		} else {
			return nil, errs.New("NewSatelliteDB not defined")
		}
		if err != nil {
			return nil, err
		}
		planet.databases = append(planet.databases, db)

		var pointerDB metainfo.PointerDB
		if planet.config.Reconfigure.NewSatellitePointerDB != nil {
			pointerDB, err = planet.config.Reconfigure.NewSatellitePointerDB(log.Named("pointerdb"), i)
		} else {
			return nil, errs.New("NewSatellitePointerDB not defined")
		}
		if err != nil {
			return nil, err
		}
		planet.databases = append(planet.databases, pointerDB)

		redis, err := redisserver.Mini()
		if err != nil {
			return nil, err
		}

		planet.databases = append(planet.databases, redis)

		config := satellite.Config{
			Server: server.Config{
				Address:        "127.0.0.1:0",
				PrivateAddress: "127.0.0.1:0",

				Config: tlsopts.Config{
					RevocationDBURL:     "bolt://" + filepath.Join(storageDir, "revocation.db"),
					UsePeerCAWhitelist:  true,
					PeerCAWhitelistPath: planet.whitelistPath,
					PeerIDVersions:      "latest",
					Extensions: extensions.Config{
						Revocation:          false,
						WhitelistSignedLeaf: false,
					},
				},
			},
			Debug: debug.Config{
				Address: "",
			},
			Admin: admin.Config{
				Address: "127.0.0.1:0",
			},
			Overlay: overlay.Config{
				Node: overlay.NodeSelectionConfig{
					UptimeCount:       0,
					AuditCount:        0,
					NewNodePercentage: 0,
					OnlineWindow:      time.Minute,
					DistinctIP:        false,
					MinimumDiskSpace:  100 * memory.MB,

					AuditReputationRepairWeight: 1,
					AuditReputationUplinkWeight: 1,
					AuditReputationAlpha0:       1,
					AuditReputationBeta0:        0,
					AuditReputationLambda:       0.95,
					AuditReputationWeight:       1,
					AuditReputationDQ:           0.6,
				},
				UpdateStatsBatchSize: 100,
			},
			Metainfo: metainfo.Config{
				DatabaseURL:          "", // not used
				MinRemoteSegmentSize: 0,  // TODO: fix tests to work with 1024
				MaxInlineSegmentSize: 8000,
				MaxCommitInterval:    1 * time.Hour,
				Overlay:              true,
				RS: metainfo.RSConfig{
					MaxSegmentSize:   64 * memory.MiB,
					MaxBufferMem:     memory.Size(256),
					ErasureShareSize: memory.Size(256),
					MinThreshold:     atLeastOne(planet.config.StorageNodeCount * 1 / 5),
					RepairThreshold:  atLeastOne(planet.config.StorageNodeCount * 2 / 5),
					SuccessThreshold: atLeastOne(planet.config.StorageNodeCount * 3 / 5),
					TotalThreshold:   atLeastOne(planet.config.StorageNodeCount * 4 / 5),

					MinTotalThreshold: (planet.config.StorageNodeCount * 4 / 5),
					MaxTotalThreshold: (planet.config.StorageNodeCount * 4 / 5),
					Validate:          false,
				},
				Loop: metainfo.LoopConfig{
					CoalesceDuration: 1 * time.Second,
				},
				RateLimiter: metainfo.RateLimiterConfig{
					Enabled:         true,
					Rate:            1000,
					CacheCapacity:   100,
					CacheExpiration: 10 * time.Second,
				},
			},
			Orders: orders.Config{
				Expiration:          7 * 24 * time.Hour,
				SettlementBatchSize: 10,
				FlushBatchSize:      10,
				FlushInterval:       defaultInterval,
				NodeStatusLogging:   true,
			},
			Checker: checker.Config{
				Interval:                  defaultInterval,
				IrreparableInterval:       defaultInterval,
				ReliabilityCacheStaleness: 1 * time.Minute,
			},
			Repairer: repairer.Config{
				MaxRepair:                     10,
				Interval:                      defaultInterval,
				Timeout:                       1 * time.Minute, // Repairs can take up to 10 seconds. Leaving room for outliers
				DownloadTimeout:               1 * time.Minute,
				MaxBufferMem:                  4 * memory.MiB,
				MaxExcessRateOptimalThreshold: 0.05,
			},
			Audit: audit.Config{
				MaxRetriesStatDB:   0,
				MinBytesPerSecond:  1 * memory.KB,
				MinDownloadTimeout: 5 * time.Second,
				MaxReverifyCount:   3,
				ChoreInterval:      defaultInterval,
				QueueInterval:      defaultInterval,
				Slots:              3,
				WorkerConcurrency:  1,
			},
			GarbageCollection: gc.Config{
				Interval:          defaultInterval,
				Enabled:           true,
				InitialPieces:     10,
				FalsePositiveRate: 0.1,
				ConcurrentSends:   1,
			},
			DBCleanup: dbcleanup.Config{
				SerialsInterval: defaultInterval,
			},
			Tally: tally.Config{
				Interval: defaultInterval,
			},
			Rollup: rollup.Config{
				Interval:      defaultInterval,
				MaxAlphaUsage: 25 * memory.GB,
				DeleteTallies: false,
			},
			ReportedRollup: reportedrollup.Config{
				Interval: defaultInterval,
			},
			LiveAccounting: live.Config{
				StorageBackend: "redis://" + redis.Addr() + "?db=0",
			},
			Mail: mailservice.Config{
				SMTPServerAddress: "smtp.mail.test:587",
				From:              "Labs <storj@mail.test>",
				AuthType:          "simulate",
				TemplatePath:      filepath.Join(developmentRoot, "web/satellite/static/emails"),
			},
			Console: consoleweb.Config{
				Address:         "127.0.0.1:0",
				StaticDir:       filepath.Join(developmentRoot, "web/satellite"),
				PasswordCost:    console.TestPasswordCost,
				AuthTokenSecret: "my-suppa-secret-key",
			},
			Marketing: marketingweb.Config{
				Address:   "127.0.0.1:0",
				StaticDir: filepath.Join(developmentRoot, "web/marketing"),
			},
			Version: planet.NewVersionConfig(),
			GracefulExit: gracefulexit.Config{
				Enabled: true,

				ChoreBatchSize: 10,
				ChoreInterval:  defaultInterval,

				EndpointBatchSize:            100,
				MaxFailuresPerPiece:          5,
				MaxInactiveTimeFrame:         time.Second * 10,
				OverallMaxFailuresPercentage: 10,
				RecvTimeout:                  time.Minute * 1,
				MaxOrderLimitSendCount:       3,
				NodeMinAgeInMonths:           0,
			},
			Metrics: metrics.Config{
				ChoreInterval: defaultInterval,
			},
			Downtime: downtime.Config{
				DetectionInterval:   defaultInterval,
				EstimationInterval:  defaultInterval,
				EstimationBatchSize: 0,
			},
		}

		if planet.ReferralManager != nil {
			config.Referrals.ReferralManagerURL = storj.NodeURL{
				ID:      planet.ReferralManager.Identity().ID,
				Address: planet.ReferralManager.Addr().String(),
			}
		}
		if planet.config.Reconfigure.Satellite != nil {
			planet.config.Reconfigure.Satellite(log, i, &config)
		}

		versionInfo := planet.NewVersionInfo()

		revocationDB, err := revocation.NewDBFromCfg(config.Server.Config)
		if err != nil {
			return xs, errs.Wrap(err)
		}

		planet.databases = append(planet.databases, revocationDB)

		liveAccounting, err := live.NewCache(log.Named("live-accounting"), config.LiveAccounting)
		if err != nil {
			return xs, errs.Wrap(err)
		}
		planet.databases = append(planet.databases, liveAccounting)

		rollupsWriteCache := orders.NewRollupsWriteCache(log.Named("orders-write-cache"), db.Orders(), config.Orders.FlushBatchSize)
		planet.databases = append(planet.databases, rollupsWriteCacheCloser{rollupsWriteCache})

		peer, err := satellite.New(log, identity, db, pointerDB, revocationDB, liveAccounting, rollupsWriteCache, versionInfo, &config)
		if err != nil {
			return xs, err
		}

		err = db.TestingCreateTables(context.TODO())
		if err != nil {
			return nil, err
		}

		api, err := planet.newAPI(i, identity, db, pointerDB, config, versionInfo)
		if err != nil {
			return xs, err
		}

		adminPeer, err := planet.newAdmin(i, identity, db, pointerDB, config, versionInfo)
		if err != nil {
			return xs, err
		}

		repairerPeer, err := planet.newRepairer(i, identity, db, pointerDB, config, versionInfo)
		if err != nil {
			return xs, err
		}

		log.Debug("id=" + peer.ID().String() + " addr=" + api.Addr())

		system := createNewSystem(log, peer, api, repairerPeer, adminPeer)
		xs = append(xs, system)
	}
	return xs, nil
}

// createNewSystem makes a new Satellite System and exposes the same interface from
// before we split out the API. In the short term this will help keep all the tests passing
// without much modification needed. However long term, we probably want to rework this
// so it represents how the satellite will run when it is made up of many prrocesses.
func createNewSystem(log *zap.Logger, peer *satellite.Core, api *satellite.API, repairerPeer *satellite.Repairer, adminPeer *satellite.Admin) *SatelliteSystem {
	system := &SatelliteSystem{
		Core:     peer,
		API:      api,
		Repairer: repairerPeer,
		Admin:    adminPeer,
	}
	system.Log = log
	system.Identity = peer.Identity
	system.DB = api.DB

	system.Dialer = api.Dialer

	system.Contact.Service = api.Contact.Service
	system.Contact.Endpoint = api.Contact.Endpoint

	system.Overlay.DB = api.Overlay.DB
	system.Overlay.Service = api.Overlay.Service
	system.Overlay.Inspector = api.Overlay.Inspector

	system.Metainfo.Database = api.Metainfo.Database
	system.Metainfo.Service = peer.Metainfo.Service
	system.Metainfo.Endpoint2 = api.Metainfo.Endpoint2
	system.Metainfo.Loop = peer.Metainfo.Loop

	system.Inspector.Endpoint = api.Inspector.Endpoint

	system.Orders.DB = api.Orders.DB
	system.Orders.Endpoint = api.Orders.Endpoint
	system.Orders.Service = peer.Orders.Service
	system.Orders.Chore = api.Orders.Chore

	system.Repair.Checker = peer.Repair.Checker
	system.Repair.Repairer = repairerPeer.Repairer
	system.Repair.Inspector = api.Repair.Inspector

	system.Audit.Queue = peer.Audit.Queue
	system.Audit.Worker = peer.Audit.Worker
	system.Audit.Chore = peer.Audit.Chore
	system.Audit.Verifier = peer.Audit.Verifier
	system.Audit.Reporter = peer.Audit.Reporter

	system.GarbageCollection.Service = peer.GarbageCollection.Service

	system.DBCleanup.Chore = peer.DBCleanup.Chore

	system.Accounting.Tally = peer.Accounting.Tally
	system.Accounting.Rollup = peer.Accounting.Rollup
	system.Accounting.ProjectUsage = peer.Accounting.ProjectUsage
	system.Accounting.ReportedRollup = peer.Accounting.ReportedRollupChore

	system.LiveAccounting = peer.LiveAccounting

	system.Marketing.Listener = api.Marketing.Listener
	system.Marketing.Endpoint = api.Marketing.Endpoint

	system.GracefulExit.Chore = peer.GracefulExit.Chore
	system.GracefulExit.Endpoint = api.GracefulExit.Endpoint

	system.Metrics.Chore = peer.Metrics.Chore

	system.DowntimeTracking.DetectionChore = peer.DowntimeTracking.DetectionChore
	system.DowntimeTracking.EstimationChore = peer.DowntimeTracking.EstimationChore
	system.DowntimeTracking.Service = peer.DowntimeTracking.Service

	return system
}

func (planet *Planet) newAPI(count int, identity *identity.FullIdentity, db satellite.DB, pointerDB metainfo.PointerDB, config satellite.Config, versionInfo version.Info) (*satellite.API, error) {
	prefix := "satellite-api" + strconv.Itoa(count)
	log := planet.log.Named(prefix)
	var err error

	revocationDB, err := revocation.NewDBFromCfg(config.Server.Config)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	planet.databases = append(planet.databases, revocationDB)

	liveAccounting, err := live.NewCache(log.Named("live-accounting"), config.LiveAccounting)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	planet.databases = append(planet.databases, liveAccounting)

	rollupsWriteCache := orders.NewRollupsWriteCache(log.Named("orders-write-cache"), db.Orders(), config.Orders.FlushBatchSize)
	planet.databases = append(planet.databases, rollupsWriteCacheCloser{rollupsWriteCache})

	return satellite.NewAPI(log, identity, db, pointerDB, revocationDB, liveAccounting, rollupsWriteCache, &config, versionInfo)
}

func (planet *Planet) newAdmin(count int, identity *identity.FullIdentity, db satellite.DB, pointerDB metainfo.PointerDB, config satellite.Config, versionInfo version.Info) (*satellite.Admin, error) {
	prefix := "satellite-admin" + strconv.Itoa(count)
	log := planet.log.Named(prefix)
	var err error

	revocationDB, err := revocation.NewDBFromCfg(config.Server.Config)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	planet.databases = append(planet.databases, revocationDB)

	liveAccounting, err := live.NewCache(log.Named("live-accounting"), config.LiveAccounting)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	planet.databases = append(planet.databases, liveAccounting)

	return satellite.NewAdmin(log, identity, db, pointerDB, revocationDB, liveAccounting, versionInfo, &config)
}

func (planet *Planet) newRepairer(count int, identity *identity.FullIdentity, db satellite.DB, pointerDB metainfo.PointerDB, config satellite.Config, versionInfo version.Info) (*satellite.Repairer, error) {
	prefix := "satellite-repairer" + strconv.Itoa(count)
	log := planet.log.Named(prefix)

	revocationDB, err := revocation.NewDBFromCfg(config.Server.Config)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	planet.databases = append(planet.databases, revocationDB)

	rollupsWriteCache := orders.NewRollupsWriteCache(log.Named("orders-write-cache"), db.Orders(), config.Orders.FlushBatchSize)
	planet.databases = append(planet.databases, rollupsWriteCacheCloser{rollupsWriteCache})

	return satellite.NewRepairer(log, identity, pointerDB, revocationDB, db.RepairQueue(), db.Buckets(), db.OverlayCache(), db.Orders(), rollupsWriteCache, versionInfo, &config)
}

type rollupsWriteCacheCloser struct {
	*orders.RollupsWriteCache
}

func (cache rollupsWriteCacheCloser) Close() error {
	return cache.RollupsWriteCache.CloseAndFlush(context.TODO())
}
