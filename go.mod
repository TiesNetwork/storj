module storj.io/storj

go 1.13

require (
	github.com/alessio/shellescape v1.2.2
	github.com/alicebob/miniredis/v2 v2.13.3
	github.com/btcsuite/btcutil v1.0.3-0.20201208143702-a53e38424cce
	github.com/calebcase/tmpfile v1.0.2
	github.com/cheggaaa/pb/v3 v3.0.5
	github.com/fatih/color v1.9.0
	github.com/go-redis/redis v6.15.9+incompatible
	github.com/gogo/protobuf v1.3.2
	github.com/golang-migrate/migrate/v4 v4.7.0
	github.com/google/go-cmp v0.5.2
	github.com/google/pprof v0.0.0-20200229191704-1ebb73c60ed3 // indirect
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/schema v1.2.0
	github.com/graphql-go/graphql v0.7.9
	github.com/jackc/pgconn v1.8.0
	github.com/jackc/pgtype v1.6.2
	github.com/jackc/pgx/v4 v4.10.1
	github.com/jtolds/monkit-hw/v2 v2.0.0-20191108235325-141a0da276b3
	github.com/lucas-clemente/quic-go v0.7.1-0.20210131023823-622ca23d4eb4
	github.com/mattn/go-sqlite3 v2.0.3+incompatible
	github.com/nsf/jsondiff v0.0.0-20200515183724-f29ed568f4ce
	github.com/nsf/termbox-go v0.0.0-20200418040025-38ba6e5628f1
	github.com/shopspring/decimal v1.2.0
	github.com/spacemonkeygo/monkit/v3 v3.0.7
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.1
	github.com/stretchr/testify v1.6.1
	github.com/stripe/stripe-go v70.15.0+incompatible
	github.com/vivint/infectious v0.0.0-20200605153912-25a574ae18a3
	github.com/zeebo/assert v1.1.0
	github.com/jtolds/tracetagger/v2 v2.0.0-rc2
	github.com/klauspost/cpuid v0.0.0-20180405133222-e7e905edc00e // indirect
	github.com/klauspost/reedsolomon v0.0.0-20180704173009-925cb01d6510 // indirect
	github.com/lib/pq v1.3.0
	github.com/loov/hrtime v0.0.0-20181214195526-37a208e8344e
	github.com/loov/plot v0.0.0-20180510142208-e59891ae1271
	github.com/mattn/go-isatty v0.0.9 // indirect
	github.com/mattn/go-sqlite3 v2.0.2+incompatible
	github.com/minio/cli v1.3.0
	github.com/minio/dsync v0.0.0-20180124070302-439a0961af70 // indirect
	github.com/minio/highwayhash v0.0.0-20180501080913-85fc8a2dacad // indirect
	github.com/minio/lsync v0.0.0-20180328070428-f332c3883f63 // indirect
	github.com/minio/mc v0.0.0-20180926130011-a215fbb71884 // indirect
	github.com/minio/minio-go v6.0.3+incompatible
	github.com/minio/sio v0.0.0-20180327104954-6a41828a60f0 // indirect
	github.com/nats-io/gnatsd v1.3.0 // indirect
	github.com/nats-io/go-nats v1.6.0 // indirect
	github.com/nats-io/go-nats-streaming v0.4.2 // indirect
	github.com/nats-io/nats v1.6.0 // indirect
	github.com/nats-io/nats-streaming-server v0.12.2 // indirect
	github.com/nats-io/nuid v1.0.0 // indirect
	github.com/nsf/jsondiff v0.0.0-20160203110537-7de28ed2b6e3
	github.com/nsf/termbox-go v0.0.0-20190121233118-02980233997d
	github.com/pascaldekloe/goe v0.0.0-20180627143212-57f6aae5913c // indirect
	github.com/pkg/profile v1.2.1 // indirect
	github.com/prometheus/procfs v0.0.0-20190517135640-51af30a78b0e // indirect
	github.com/rs/cors v1.5.0 // indirect
	github.com/shopspring/decimal v0.0.0-20200105231215-408a2507e114
	github.com/skyrings/skyring-common v0.0.0-20160929130248-d1c0bb1cbd5e
	github.com/smartystreets/assertions v0.0.0-20180820201707-7c9eb446e3cf // indirect
	github.com/smartystreets/goconvey v0.0.0-20180222194500-ef6db91d284a // indirect
	github.com/spacemonkeygo/monkit/v3 v3.0.1
	github.com/spf13/cast v1.3.0
	github.com/spf13/cobra v0.0.5
	github.com/spf13/pflag v1.0.3
	github.com/spf13/viper v1.4.0
	github.com/streadway/amqp v0.0.0-20180806233856-70e15c650864 // indirect
	github.com/stretchr/testify v1.4.0
	github.com/stripe/stripe-go v63.1.1+incompatible
	github.com/tidwall/gjson v1.1.3 // indirect
	github.com/tidwall/match v0.0.0-20171002075945-1731857f09b1 // indirect
	github.com/vivint/infectious v0.0.0-20190108171102-2455b059135b
	github.com/zeebo/admission/v2 v2.0.0
	github.com/zeebo/errs v1.2.2
	go.etcd.io/bbolt v1.3.5
	go.uber.org/zap v1.16.0
	golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad
	golang.org/x/sync v0.0.0-20201207232520-09787c993a3a
	golang.org/x/sys v0.0.0-20210124154548-22da62e12c0c
	golang.org/x/term v0.0.0-20201117132131-f5c789dd3221
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e
	google.golang.org/api v0.20.0 // indirect
	google.golang.org/protobuf v1.25.0 // indirect
	storj.io/common v0.0.0-20210217105242-970e119468ed
	storj.io/drpc v0.0.16
	storj.io/monkit-jaeger v0.0.0-20210205021559-85f08034688c
	storj.io/private v0.0.0-20210203200143-9d2ec06f0d3c
	storj.io/uplink v1.4.6-0.20210212112107-f7f8a3c8321a
)
