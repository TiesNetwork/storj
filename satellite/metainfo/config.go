// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package metainfo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"storj.io/common/memory"
	"storj.io/common/pb"
	"storj.io/common/storj"
	"storj.io/common/uuid"
	"storj.io/storj/private/dbutil"
	"storj.io/storj/satellite/metainfo/metabase"
	"storj.io/storj/satellite/metainfo/objectdeletion"
	"storj.io/storj/satellite/metainfo/piecedeletion"
	"storj.io/storj/storage"
	"storj.io/storj/storage/cockroachkv"
	"storj.io/storj/storage/postgreskv"
	"storj.io/uplink/private/storage/meta"
)

const (
	// BoltPointerBucket is the string representing the bucket used for `PointerEntries` in BoltDB.
	BoltPointerBucket = "pointers"
)

// RSConfig is a configuration struct that keeps details about default
// redundancy strategy information.
//
// Can be used as a flag.
type RSConfig struct {
	ErasureShareSize memory.Size
	Min              int
	Repair           int
	Success          int
	Total            int
}

// Type implements pflag.Value.
func (RSConfig) Type() string { return "metainfo.RSConfig" }

// String is required for pflag.Value.
func (rs *RSConfig) String() string {
	return fmt.Sprintf("%d/%d/%d/%d-%s",
		rs.Min,
		rs.Repair,
		rs.Success,
		rs.Total,
		rs.ErasureShareSize.String())
}

// Set sets the value from a string in the format k/m/o/n-size (min/repair/optimal/total-erasuresharesize).
func (rs *RSConfig) Set(s string) error {
	// Split on dash. Expect two items. First item is RS numbers. Second item is memory.Size.
	info := strings.Split(s, "-")
	if len(info) != 2 {
		return Error.New("Invalid default RS config (expect format k/m/o/n-ShareSize, got %s)", s)
	}
	rsNumbersString := info[0]
	shareSizeString := info[1]

	// Attempt to parse "-size" part of config.
	shareSizeInt, err := memory.ParseString(shareSizeString)
	if err != nil {
		return Error.New("Invalid share size in RS config: '%s', %w", shareSizeString, err)
	}
	shareSize := memory.Size(shareSizeInt)

	// Split on forward slash. Expect exactly four positive non-decreasing integers.
	rsNumbers := strings.Split(rsNumbersString, "/")
	if len(rsNumbers) != 4 {
		return Error.New("Invalid default RS numbers (wrong size, expect 4): %s", rsNumbersString)
	}

	minValue := 1
	values := []int{}
	for _, nextValueString := range rsNumbers {
		nextValue, err := strconv.Atoi(nextValueString)
		if err != nil {
			return Error.New("Invalid default RS numbers (should all be valid integers): %s, %w", rsNumbersString, err)
		}
		if nextValue < minValue {
			return Error.New("Invalid default RS numbers (should be non-decreasing): %s", rsNumbersString)
		}
		values = append(values, nextValue)
		minValue = nextValue
	}

	rs.ErasureShareSize = shareSize
	rs.Min = values[0]
	rs.Repair = values[1]
	rs.Success = values[2]
	rs.Total = values[3]

	return nil
}

// RateLimiterConfig is a configuration struct for endpoint rate limiting.
type RateLimiterConfig struct {
	Enabled         bool          `help:"whether rate limiting is enabled." releaseDefault:"true" devDefault:"true"`
	Rate            float64       `help:"request rate per project per second." releaseDefault:"1000" devDefault:"100"`
	CacheCapacity   int           `help:"number of projects to cache." releaseDefault:"10000" devDefault:"10"`
	CacheExpiration time.Duration `help:"how long to cache the projects limiter." releaseDefault:"10m" devDefault:"10s"`
}

// ProjectLimitConfig is a configuration struct for default project limits.
type ProjectLimitConfig struct {
	MaxBuckets          int         `help:"max bucket count for a project." default:"100"`
	DefaultMaxUsage     memory.Size `help:"the default storage usage limit" default:"500.00GB"`
	DefaultMaxBandwidth memory.Size `help:"the default bandwidth usage limit" default:"500.00GB"`
}

// Config is a configuration struct that is everything you need to start a metainfo.
type Config struct {
	DatabaseURL          string                `help:"the database connection string to use" default:"postgres://"`
	MinRemoteSegmentSize memory.Size           `default:"1240" help:"minimum remote segment size"`
	MaxInlineSegmentSize memory.Size           `default:"4KiB" help:"maximum inline segment size"`
	MaxSegmentSize       memory.Size           `default:"64MiB" help:"maximum segment size"`
	MaxMetadataSize      memory.Size           `default:"2KiB" help:"maximum segment metadata size"`
	MaxCommitInterval    time.Duration         `default:"48h" help:"maximum time allowed to pass between creating and committing a segment"`
	Overlay              bool                  `default:"true" help:"toggle flag if overlay is enabled"`
	RS                   RSConfig              `releaseDefault:"29/35/80/110-256B" devDefault:"4/6/8/10-256B" help:"redundancy scheme configuration in the format k/m/o/n-sharesize"`
	Loop                 LoopConfig            `help:"loop configuration"`
	RateLimiter          RateLimiterConfig     `help:"rate limiter configuration"`
	ProjectLimits        ProjectLimitConfig    `help:"project limit configuration"`
	PieceDeletion        piecedeletion.Config  `help:"piece deletion configuration"`
	ObjectDeletion       objectdeletion.Config `help:"object deletion configuration"`
}

// PointerDB stores pointers.
//
// architecture: Database
type PointerDB interface {
	// MigrateToLatest migrates to latest schema version.
	MigrateToLatest(ctx context.Context) error

	storage.KeyValueStore
}

// OpenStore returns database for storing pointer data.
func OpenStore(ctx context.Context, logger *zap.Logger, dbURLString string, app string) (db PointerDB, err error) {
	_, source, implementation, err := dbutil.SplitConnStr(dbURLString)
	if err != nil {
		return nil, err
	}

	switch implementation {
	case dbutil.Postgres:
		db, err = postgreskv.Open(ctx, source, app)
	case dbutil.Cockroach:
		db, err = cockroachkv.Open(ctx, source, app)
	default:
		err = Error.New("unsupported db implementation: %s", dbURLString)
	}

	if err != nil {
		return nil, err
	}

	logger.Debug("Connected to:", zap.String("db source", source))
	return db, nil
}

// PointerDBMetabase this is wrapper struct that translates pointerDB to metabase.
// Use only for testing purposes.
type PointerDBMetabase struct {
	metainfo *Service
}

// NewPointerDBMetabase creates new NewPointerDBMetabase instance.
func NewPointerDBMetabase(service *Service) *PointerDBMetabase {
	return &PointerDBMetabase{
		metainfo: service,
	}
}

// TestingAllCommittedObjects gets all committed objects from bucket. Use only for testing purposes.
func (m *PointerDBMetabase) TestingAllCommittedObjects(ctx context.Context, projectID uuid.UUID, bucketName string) (objects []metabase.ObjectEntry, err error) {
	location, err := CreatePath(ctx, projectID, -1, []byte(bucketName), []byte{})
	if err != nil {
		return nil, err
	}
	items, _, err := m.metainfo.List(ctx, location.Encode(), "", true, -1, meta.All)
	if err != nil {
		return nil, err
	}
	entries := make([]metabase.ObjectEntry, len(items))
	for i, item := range items {
		entries[i] = metabase.ObjectEntry{
			ObjectKey: metabase.ObjectKey(item.Path),
		}
	}
	return entries, nil
}

// TestingAllObjectSegments gets all segments for given object. Use only for testing purposes.
func (m *PointerDBMetabase) TestingAllObjectSegments(ctx context.Context, objectLocation metabase.ObjectLocation) (segments []metabase.Segment, err error) {
	location, err := CreatePath(ctx, objectLocation.ProjectID, -1, []byte(objectLocation.BucketName), []byte(objectLocation.ObjectKey))
	if err != nil {
		return nil, err
	}

	pointer, err := m.metainfo.Get(ctx, location.Encode())
	if err != nil {
		return nil, err
	}

	streamMeta := &pb.StreamMeta{}
	err = pb.Unmarshal(pointer.Metadata, streamMeta)
	if err != nil {
		return nil, err
	}

	segments = make([]metabase.Segment, 0)
	for i := int64(0); i < streamMeta.NumberOfSegments-1; i++ {
		location.Index = i
		_, err = m.metainfo.Get(ctx, location.Encode())
		if err != nil {
			if storj.ErrObjectNotFound.Has(err) {
				continue
			}
			return nil, err
		}
		segments = append(segments, metabase.Segment{
			Position: metabase.SegmentPosition{
				Index: uint32(i),
			},
		})
	}

	segments = append(segments, metabase.Segment{
		Position: metabase.SegmentPosition{
			Index: uint32(streamMeta.NumberOfSegments - 1),
		},
	})
	return segments, nil
}
