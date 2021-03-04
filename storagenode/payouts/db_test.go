// Copyright (C) 2020 Storj Labs, Inc.
// See LICENSE for copying information.

package payouts_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"storj.io/common/storj"
	"storj.io/common/testcontext"
	"storj.io/storj/storagenode"
	"storj.io/storj/storagenode/payouts"
	"storj.io/storj/storagenode/storagenodedb/storagenodedbtest"
)

func TestHeldAmountDB(t *testing.T) {
	storagenodedbtest.Run(t, func(ctx *testcontext.Context, t *testing.T, db storagenode.DB) {
		payout := db.Payout()
		satelliteID := storj.NodeID{}
		period := "2020-01"
		paystub := payouts.PayStub{
			SatelliteID:    satelliteID,
			Period:         "2020-01",
			Created:        time.Now().UTC(),
			Codes:          "1",
			UsageAtRest:    1,
			UsageGet:       2,
			UsagePut:       3,
			UsageGetRepair: 4,
			UsagePutRepair: 5,
			UsageGetAudit:  6,
			CompAtRest:     7,
			CompGet:        8,
			CompPut:        9,
			CompGetRepair:  10,
			CompPutRepair:  11,
			CompGetAudit:   12,
			SurgePercent:   13,
			Held:           14,
			Owed:           15,
			Disposed:       16,
		}
		paystub2 := paystub
		paystub2.Period = "2020-02"
		paystub2.Created = paystub.Created.Add(time.Hour * 24 * 30)

		t.Run("Test StorePayStub", func(t *testing.T) {
			err := payout.StorePayStub(ctx, paystub)
			assert.NoError(t, err)
		})

		payment := payouts.Payment{
			SatelliteID: satelliteID,
			Period:      period,
			Receipt:     "test",
		}

		t.Run("Test GetPayStub", func(t *testing.T) {
			err := payout.StorePayment(ctx, payment)
			assert.NoError(t, err)

			stub, err := payout.GetPayStub(ctx, satelliteID, period)
			assert.NoError(t, err)
			receipt, err := payout.GetReceipt(ctx, satelliteID, period)
			assert.NoError(t, err)
			assert.Equal(t, stub.Period, paystub.Period)
			assert.Equal(t, stub.Created, paystub.Created)
			assert.Equal(t, stub.Codes, paystub.Codes)
			assert.Equal(t, stub.CompAtRest, paystub.CompAtRest)
			assert.Equal(t, stub.CompGet, paystub.CompGet)
			assert.Equal(t, stub.CompGetAudit, paystub.CompGetAudit)
			assert.Equal(t, stub.CompGetRepair, paystub.CompGetRepair)
			assert.Equal(t, stub.CompPut, paystub.CompPut)
			assert.Equal(t, stub.CompPutRepair, paystub.CompPutRepair)
			assert.Equal(t, stub.Disposed, paystub.Disposed)
			assert.Equal(t, stub.Held, paystub.Held)
			assert.Equal(t, stub.Owed, paystub.Owed)
			assert.Equal(t, stub.Paid, paystub.Paid)
			assert.Equal(t, stub.SatelliteID, paystub.SatelliteID)
			assert.Equal(t, stub.SurgePercent, paystub.SurgePercent)
			assert.Equal(t, stub.UsageAtRest, paystub.UsageAtRest)
			assert.Equal(t, stub.UsageGet, paystub.UsageGet)
			assert.Equal(t, stub.UsageGetAudit, paystub.UsageGetAudit)
			assert.Equal(t, stub.UsageGetRepair, paystub.UsageGetRepair)
			assert.Equal(t, stub.UsagePut, paystub.UsagePut)
			assert.Equal(t, stub.UsagePutRepair, paystub.UsagePutRepair)
			assert.Equal(t, receipt, payment.Receipt)

			stub, err = payout.GetPayStub(ctx, satelliteID, "")
			assert.Error(t, err)
			assert.Equal(t, true, payouts.ErrNoPayStubForPeriod.Has(err))
			assert.Nil(t, stub)
			assert.NotNil(t, receipt)
			receipt, err = payout.GetReceipt(ctx, satelliteID, "")
			assert.Error(t, err)
			assert.Equal(t, true, payouts.ErrNoPayStubForPeriod.Has(err))

			stub, err = payout.GetPayStub(ctx, storj.NodeID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, period)
			assert.Error(t, err)
			assert.Equal(t, true, payouts.ErrNoPayStubForPeriod.Has(err))
			assert.Nil(t, stub)
			assert.NotNil(t, receipt)
		})

		t.Run("Test AllPayStubs", func(t *testing.T) {
			stubs, err := payout.AllPayStubs(ctx, period)
			assert.NoError(t, err)
			assert.NotNil(t, stubs)
			assert.Equal(t, 1, len(stubs))
			assert.Equal(t, stubs[0].Period, paystub.Period)
			assert.Equal(t, stubs[0].Created, paystub.Created)
			assert.Equal(t, stubs[0].Codes, paystub.Codes)
			assert.Equal(t, stubs[0].CompAtRest, paystub.CompAtRest)
			assert.Equal(t, stubs[0].CompGet, paystub.CompGet)
			assert.Equal(t, stubs[0].CompGetAudit, paystub.CompGetAudit)
			assert.Equal(t, stubs[0].CompGetRepair, paystub.CompGetRepair)
			assert.Equal(t, stubs[0].CompPut, paystub.CompPut)
			assert.Equal(t, stubs[0].CompPutRepair, paystub.CompPutRepair)
			assert.Equal(t, stubs[0].Disposed, paystub.Disposed)
			assert.Equal(t, stubs[0].Held, paystub.Held)
			assert.Equal(t, stubs[0].Owed, paystub.Owed)
			assert.Equal(t, stubs[0].Paid, paystub.Paid)
			assert.Equal(t, stubs[0].SatelliteID, paystub.SatelliteID)
			assert.Equal(t, stubs[0].SurgePercent, paystub.SurgePercent)
			assert.Equal(t, stubs[0].UsageAtRest, paystub.UsageAtRest)
			assert.Equal(t, stubs[0].UsageGet, paystub.UsageGet)
			assert.Equal(t, stubs[0].UsageGetAudit, paystub.UsageGetAudit)
			assert.Equal(t, stubs[0].UsageGetRepair, paystub.UsageGetRepair)
			assert.Equal(t, stubs[0].UsagePut, paystub.UsagePut)
			assert.Equal(t, stubs[0].UsagePutRepair, paystub.UsagePutRepair)

			stubs, err = payout.AllPayStubs(ctx, "")
			assert.Equal(t, len(stubs), 0)
			assert.NoError(t, err)
		})

		payment = payouts.Payment{
			ID:          1,
			Created:     time.Now().UTC(),
			SatelliteID: satelliteID,
			Period:      period,
			Amount:      228,
			Receipt:     "receipt",
			Notes:       "notes",
		}

		t.Run("Test StorePayment", func(t *testing.T) {
			err := payout.StorePayment(ctx, payment)
			assert.NoError(t, err)
		})

		t.Run("Test SatellitesHeldbackHistory", func(t *testing.T) {
			heldback, err := payout.SatellitesHeldbackHistory(ctx, satelliteID)
			assert.NoError(t, err)
			assert.Equal(t, heldback[0].Amount, paystub.Held)
			assert.Equal(t, heldback[0].Period, paystub.Period)
		})

		t.Run("Test SatellitePeriods", func(t *testing.T) {
			periods, err := payout.SatellitePeriods(ctx, paystub.SatelliteID)
			assert.NoError(t, err)
			assert.NotNil(t, periods)
			assert.Equal(t, 1, len(periods))
			assert.Equal(t, paystub.Period, periods[0])

			err = payout.StorePayStub(ctx, paystub2)
			require.NoError(t, err)

			periods, err = payout.SatellitePeriods(ctx, paystub.SatelliteID)
			assert.NoError(t, err)
			assert.NotNil(t, periods)
			assert.Equal(t, 2, len(periods))
			assert.Equal(t, paystub.Period, periods[0])
			assert.Equal(t, paystub2.Period, periods[1])
		})

		t.Run("Test AllPeriods", func(t *testing.T) {
			periods, err := payout.AllPeriods(ctx)
			assert.NoError(t, err)
			assert.NotNil(t, periods)
			assert.Equal(t, 2, len(periods))
			assert.Equal(t, paystub.Period, periods[0])
			assert.Equal(t, paystub2.Period, periods[1])

			paystub3 := paystub2
			paystub3.SatelliteID = storj.NodeID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
			paystub3.Period = "2020-03"
			paystub3.Created = paystub2.Created.Add(time.Hour * 24 * 30)

			err = payout.StorePayStub(ctx, paystub3)
			require.NoError(t, err)

			periods, err = payout.AllPeriods(ctx)
			assert.NoError(t, err)
			assert.NotNil(t, periods)
			assert.Equal(t, 3, len(periods))
			assert.Equal(t, paystub.Period, periods[0])
			assert.Equal(t, paystub2.Period, periods[1])
			assert.Equal(t, paystub3.Period, periods[2])
		})
	})
}

func TestSatellitePayStubPeriodCached(t *testing.T) {
	storagenodedbtest.Run(t, func(ctx *testcontext.Context, t *testing.T, db storagenode.DB) {
		heldAmountDB := db.Payout()
		reputationDB := db.Reputation()
		satellitesDB := db.Satellites()
		service, err := payouts.NewService(nil, heldAmountDB, reputationDB, satellitesDB, nil)
		require.NoError(t, err)

		payStub := payouts.PayStub{
			SatelliteID:    storj.NodeID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			Created:        time.Now().UTC(),
			Codes:          "code",
			UsageAtRest:    1,
			UsageGet:       2,
			UsagePut:       3,
			UsageGetRepair: 4,
			UsagePutRepair: 5,
			UsageGetAudit:  6,
			CompAtRest:     7,
			CompGet:        8,
			CompPut:        9,
			CompGetRepair:  10,
			CompPutRepair:  11,
			CompGetAudit:   12,
			SurgePercent:   13,
			Held:           14,
			Owed:           15,
			Disposed:       16,
			Paid:           17,
		}

		for i := 1; i < 4; i++ {
			payStub.Period = fmt.Sprintf("2020-0%d", i)
			err := heldAmountDB.StorePayStub(ctx, payStub)
			require.NoError(t, err)
		}

		payStubs, err := service.SatellitePayStubPeriod(ctx, payStub.SatelliteID, "2020-01", "2020-03")
		require.NoError(t, err)
		require.Equal(t, 3, len(payStubs))

		payStubs, err = service.SatellitePayStubPeriod(ctx, payStub.SatelliteID, "2019-01", "2021-03")
		require.NoError(t, err)
		require.Equal(t, 3, len(payStubs))

		payStubs, err = service.SatellitePayStubPeriod(ctx, payStub.SatelliteID, "2019-01", "2020-01")
		require.NoError(t, err)
		require.Equal(t, 1, len(payStubs))
	})
}

func TestAllPayStubPeriodCached(t *testing.T) {
	storagenodedbtest.Run(t, func(ctx *testcontext.Context, t *testing.T, db storagenode.DB) {
		heldAmountDB := db.Payout()
		reputationDB := db.Reputation()
		satellitesDB := db.Satellites()
		service, err := payouts.NewService(nil, heldAmountDB, reputationDB, satellitesDB, nil)
		require.NoError(t, err)

		payStub := payouts.PayStub{
			SatelliteID:    storj.NodeID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			Created:        time.Now().UTC(),
			Codes:          "code",
			UsageAtRest:    1,
			UsageGet:       2,
			UsagePut:       3,
			UsageGetRepair: 4,
			UsagePutRepair: 5,
			UsageGetAudit:  6,
			CompAtRest:     7,
			CompGet:        8,
			CompPut:        9,
			CompGetRepair:  10,
			CompPutRepair:  11,
			CompGetAudit:   12,
			SurgePercent:   13,
			Held:           14,
			Owed:           15,
			Disposed:       16,
			Paid:           17,
		}

		for i := 1; i < 4; i++ {
			payStub.SatelliteID[0] += byte(i)
			for j := 1; j < 4; j++ {
				payStub.Period = fmt.Sprintf("2020-0%d", j)
				err := heldAmountDB.StorePayStub(ctx, payStub)
				require.NoError(t, err)
			}
		}

		payStubs, err := service.AllPayStubsPeriod(ctx, "2020-01", "2020-03")
		require.NoError(t, err)
		require.Equal(t, 9, len(payStubs))

		payStubs, err = service.AllPayStubsPeriod(ctx, "2019-01", "2021-03")
		require.NoError(t, err)
		require.Equal(t, 9, len(payStubs))

		payStubs, err = service.AllPayStubsPeriod(ctx, "2019-01", "2020-01")
		require.NoError(t, err)
		require.Equal(t, 3, len(payStubs))

		payStubs, err = service.AllPayStubsPeriod(ctx, "2019-01", "2019-01")
		require.NoError(t, err)
		require.Equal(t, 0, len(payStubs))
	})
}

func TestPayouts(t *testing.T) {
	storagenodedbtest.Run(t, func(ctx *testcontext.Context, t *testing.T, db storagenode.DB) {
		payout := db.Payout()
		t.Run("Test SatelliteIDs", func(t *testing.T) {
			id1 := storj.NodeID{1, 2, 3}
			id2 := storj.NodeID{2, 3, 4}
			id3 := storj.NodeID{3, 3, 3}
			err := payout.StorePayStub(ctx, payouts.PayStub{
				SatelliteID: id1,
			})
			require.NoError(t, err)
			err = payout.StorePayStub(ctx, payouts.PayStub{
				SatelliteID: id1,
			})
			require.NoError(t, err)
			err = payout.StorePayStub(ctx, payouts.PayStub{
				SatelliteID: id2,
			})
			require.NoError(t, err)
			err = payout.StorePayStub(ctx, payouts.PayStub{
				SatelliteID: id3,
			})
			require.NoError(t, err)
			err = payout.StorePayStub(ctx, payouts.PayStub{
				SatelliteID: id3,
			})
			require.NoError(t, err)
			err = payout.StorePayStub(ctx, payouts.PayStub{
				SatelliteID: id2,
			})
			require.NoError(t, err)
			listIDs, err := payout.GetPayingSatellitesIDs(ctx)
			require.Equal(t, len(listIDs), 3)
			require.NoError(t, err)
		})
		t.Run("Test GetSatelliteEarned", func(t *testing.T) {
			id1 := storj.NodeID{1, 2, 3}
			id2 := storj.NodeID{2, 3, 4}
			id3 := storj.NodeID{3, 3, 3}
			err := payout.StorePayStub(ctx, payouts.PayStub{
				Period:      "2020-11",
				SatelliteID: id1,
				CompGet:     11,
				CompAtRest:  11,
			})
			require.NoError(t, err)
			err = payout.StorePayStub(ctx, payouts.PayStub{
				Period:      "2020-12",
				SatelliteID: id1,
				CompGet:     22,
				CompAtRest:  22,
			})
			require.NoError(t, err)
			err = payout.StorePayStub(ctx, payouts.PayStub{
				Period:      "2020-11",
				SatelliteID: id2,
				CompGet:     33,
				CompAtRest:  33,
			})
			require.NoError(t, err)
			err = payout.StorePayStub(ctx, payouts.PayStub{
				Period:      "2020-10",
				SatelliteID: id3,
				CompGet:     44,
				CompAtRest:  44,
			})
			require.NoError(t, err)
			err = payout.StorePayStub(ctx, payouts.PayStub{
				Period:      "2020-11",
				SatelliteID: id3,
				CompGet:     55,
				CompAtRest:  55,
			})
			require.NoError(t, err)
			err = payout.StorePayStub(ctx, payouts.PayStub{
				Period:      "2020-10",
				SatelliteID: id2,
				CompGet:     66,
				CompAtRest:  66,
			})
			require.NoError(t, err)
			satellite1Earned, err := payout.GetEarnedAtSatellite(ctx, id1)
			require.Equal(t, int(satellite1Earned), 66)
			require.NoError(t, err)
			satellite2Earned, err := payout.GetEarnedAtSatellite(ctx, id2)
			require.Equal(t, int(satellite2Earned), 198)
			require.NoError(t, err)
			satellite3Earned, err := payout.GetEarnedAtSatellite(ctx, id3)
			require.Equal(t, int(satellite3Earned), 198)
			require.NoError(t, err)
		})
	})
}
