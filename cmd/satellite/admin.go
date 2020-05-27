// Copyright (C) 2020 Storj Labs, Inc.
// See LICENSE for copying information.

package main

import (
	"github.com/spf13/cobra"
	"github.com/zeebo/errs"
	"go.uber.org/zap"

	"storj.io/storj/pkg/process"
	"storj.io/storj/pkg/revocation"
	"storj.io/storj/private/version"
	"storj.io/storj/satellite"
	"storj.io/storj/satellite/accounting/live"
	"storj.io/storj/satellite/metainfo"
	"storj.io/storj/satellite/satellitedb"
)

func cmdAdminRun(cmd *cobra.Command, args []string) (err error) {
	ctx, _ := process.Ctx(cmd)
	log := zap.L()

	runCfg.Debug.Address = *process.DebugAddrFlag

	identity, err := runCfg.Identity.Load()
	if err != nil {
		zap.S().Fatal(err)
	}

	db, err := satellitedb.New(log.Named("db"), runCfg.Database, satellitedb.Options{
		APIKeysLRUOptions: runCfg.APIKeysLRUOptions(),
	})
	if err != nil {
		return errs.New("Error starting master database on satellite api: %+v", err)
	}
	defer func() {
		err = errs.Combine(err, db.Close())
	}()

	pointerDB, err := metainfo.NewStore(log.Named("pointerdb"), runCfg.Config.Metainfo.DatabaseURL)
	if err != nil {
		return errs.New("Error creating metainfo database on satellite api: %+v", err)
	}
	defer func() {
		err = errs.Combine(err, pointerDB.Close())
	}()

	revocationDB, err := revocation.NewDBFromCfg(runCfg.Config.Server.Config)
	if err != nil {
		return errs.New("Error creating revocation database on satellite api: %+v", err)
	}
	defer func() {
		err = errs.Combine(err, revocationDB.Close())
	}()

	accountingCache, err := live.NewCache(log.Named("live-accounting"), runCfg.LiveAccounting)
	if err != nil {
		return errs.New("Error creating live accounting cache on satellite api: %+v", err)
	}
	defer func() {
		err = errs.Combine(err, accountingCache.Close())
	}()

	peer, err := satellite.NewAdmin(log, identity, db, pointerDB, revocationDB, accountingCache, version.Build, &runCfg.Config)
	if err != nil {
		return err
	}

	err = peer.Version.CheckVersion(ctx)
	if err != nil {
		return err
	}

	if err := process.InitMetricsWithCertPath(ctx, log, nil, runCfg.Identity.CertPath); err != nil {
		zap.S().Warn("Failed to initialize telemetry batcher on satellite api: ", err)
	}

	err = db.CheckVersion(ctx)
	if err != nil {
		zap.S().Fatal("failed satellite database version check: ", err)
		return errs.New("Error checking version for satellitedb: %+v", err)
	}

	runError := peer.Run(ctx)
	closeError := peer.Close()
	return errs.Combine(runError, closeError)
}
