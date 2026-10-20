module yoru

go 1.26.5

// Releases build with the patched standard library (net/http, crypto/tls fixes up to 1.26.9).
toolchain go1.26.9

require (
	filippo.io/edwards25519 v1.2.0
	github.com/BurntSushi/toml v1.6.0
	github.com/Microsoft/go-winio v0.6.4
	github.com/RoaringBitmap/roaring/v2 v2.29.0
	github.com/anacrolix/btree v0.1.1
	github.com/anacrolix/chansync v0.8.0
	github.com/anacrolix/envpprof v1.6.0
	github.com/anacrolix/generics v0.2.0
	github.com/anacrolix/go-libutp v1.5.1
	github.com/anacrolix/log v0.17.1-0.20251118025802-918f1157b7bb
	github.com/anacrolix/missinggo v1.3.0
	github.com/anacrolix/missinggo/v2 v2.11.0
	github.com/anacrolix/multiless v0.4.0
	github.com/anacrolix/stm v0.6.0
	github.com/anacrolix/sync v0.6.0
	github.com/anacrolix/upnp v0.1.4
	github.com/anacrolix/utp v0.2.0
	github.com/bahlo/generic-list-go v0.2.0
	github.com/benbjohnson/immutable v0.4.3
	github.com/bodgit/sevenzip v1.6.5
	github.com/bradfitz/iter v0.0.0-20191230175014-e8f45d346db8
	github.com/caddyserver/certmagic v0.25.6
	github.com/cespare/xxhash v1.1.0
	github.com/davecgh/go-spew v1.1.1
	github.com/dustin/go-humanize v1.1.0
	github.com/edsrzf/mmap-go v1.2.0
	github.com/frankban/quicktest v1.14.6
	github.com/go-llsqlite/adapter v0.2.0
	github.com/go-quicktest/qt v1.102.0
	github.com/google/btree v1.1.3
	github.com/google/go-cmp v0.7.0
	github.com/klauspost/compress v1.20.3
	github.com/libdns/duckdns v0.3.0
	github.com/multiformats/go-multihash v0.2.3
	github.com/pressly/goose/v3 v3.29.0
	github.com/protolambda/ctxlock v0.2.0
	github.com/rs/dnscache v0.0.0-20230804202142-fc85eb664529
	github.com/stretchr/testify v1.12.1
	github.com/tidwall/btree v1.8.2
	github.com/yeqown/go-qrcode/v2 v2.3.0
	go.etcd.io/bbolt v1.5.0
	go.uber.org/zap v1.28.0
	golang.org/x/exp v0.0.0-20261014143641-f0251eb4aa3e
	golang.org/x/net v0.61.0
	golang.org/x/sync v0.24.0
	golang.org/x/sys v0.49.0
	golang.org/x/text v0.43.0
	golang.org/x/time v0.16.0
	modernc.org/sqlite v1.60.1
)

require (
	github.com/anacrolix/lsan v0.1.0 // indirect
	github.com/anacrolix/missinggo/perf v1.0.0 // indirect
	github.com/anacrolix/mmsg v1.1.1 // indirect
	github.com/andybalholm/brotli v1.2.6 // indirect
	github.com/bits-and-blooms/bitset v1.26.0 // indirect
	github.com/bodgit/plumbing v1.3.0 // indirect
	github.com/bodgit/windows v1.0.1 // indirect
	github.com/caddyserver/zerossl v0.1.6 // indirect
	github.com/felixge/fgprof v0.9.5 // indirect
	github.com/go-llsqlite/crawshaw v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20261016150608-1facf8f91b50 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/huandu/xstrings v1.6.2 // indirect
	github.com/klauspost/cpuid/v2 v2.4.1 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/libdns/libdns v1.1.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/mholt/acmez/v3 v3.1.7 // indirect
	github.com/miekg/dns v1.1.73 // indirect
	github.com/minio/sha256-simd v1.0.1 // indirect
	github.com/mr-tron/base58 v1.3.0 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/multiformats/go-varint v0.1.0 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rogpeppe/go-internal v1.16.0 // indirect
	github.com/ryszard/goskiplist v0.0.0-20150312221310-2dfbae5fcf46 // indirect
	github.com/sethvargo/go-retry v0.5.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/stangelandcl/ppmd v0.1.1 // indirect
	github.com/ulikunitz/xz v0.5.17 // indirect
	github.com/yeqown/reedsolomon v1.0.0 // indirect
	github.com/zeebo/blake3 v0.2.4 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap/exp v0.3.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	go4.org v0.0.0-20260112195520-a5071408f32f // indirect
	golang.org/x/crypto v0.58.0 // indirect
	lukechampine.com/blake3 v1.4.1 // indirect
	modernc.org/libc v1.79.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	zombiezen.com/go/sqlite v1.4.2 // indirect
)
