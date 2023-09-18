// This file is part of go-algorand
// Copyright (C) 2019-2023 Algorand, Inc.
//
// go-algorand is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// go-algorand is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with go-algorand.  If not, see <https://www.gnu.org/licenses/>.

package data

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aybabtme/uniplot/histogram"
	"github.com/golang/snappy"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/exp/rand"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/bookkeeping"
	"github.com/algorand/go-algorand/data/transactions"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/network"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/test/partitiontest"
	"github.com/algorand/go-codec/codec"
	"github.com/algorand/go-deadlock"
)

func TestAppRateLimiter_Make(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	rate := uint64(10)
	window := 1 * time.Second
	rm := makeAppRateLimiter(10, rate, window)

	require.Equal(t, 1, rm.maxBucketSize)
	require.NotEmpty(t, rm.seed)
	require.NotEmpty(t, rm.salt)
	for i := 0; i < len(rm.buckets); i++ {
		require.NotNil(t, rm.buckets[i].entries)
		require.NotNil(t, rm.buckets[i].lru)
	}
}

func TestAppRateLimiter_NoApps(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	rate := uint64(10)
	window := 1 * time.Second
	rm := makeAppRateLimiter(10, rate, window)

	txns := []transactions.SignedTxn{
		{
			Txn: transactions.Transaction{
				Type: protocol.AssetConfigTx,
			},
		},
		{
			Txn: transactions.Transaction{
				Type: protocol.PaymentTx,
			},
		},
	}
	drop := rm.shouldDrop(txns, nil)
	require.False(t, drop)
}

func getAppTxnGroup(appIdx basics.AppIndex) []transactions.SignedTxn {
	apptxn := transactions.Transaction{
		Type: protocol.ApplicationCallTx,
		ApplicationCallTxnFields: transactions.ApplicationCallTxnFields{
			ApplicationID: appIdx,
		},
	}

	return []transactions.SignedTxn{{Txn: apptxn}}
}

func TestAppRateLimiter_Basics(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	rate := uint64(10)
	window := 1 * time.Second
	rm := makeAppRateLimiter(512, rate, window)

	txns := getAppTxnGroup(1)
	now := time.Now().UnixNano()
	drop := rm.shouldDropAt(txns, nil, now)
	require.False(t, drop)

	for i := len(txns); i < int(rate); i++ {
		drop = rm.shouldDropAt(txns, nil, now)
		require.False(t, drop)
	}

	drop = rm.shouldDropAt(txns, nil, now)
	require.True(t, drop)

	// check a single group with exceed rate is dropped
	apptxn2 := txns[0].Txn
	apptxn2.ApplicationID = 2
	txns = make([]transactions.SignedTxn, 0, rate+1)
	for i := 0; i < int(rate+1); i++ {
		txns = append(txns, transactions.SignedTxn{
			Txn: apptxn2,
		})
	}
	drop = rm.shouldDropAt(txns, nil, now)
	require.True(t, drop)

	drop = rm.shouldDropAt(txns, nil, now+int64(2*window))
	require.True(t, drop)

	// check foreign apps
	apptxn3 := txns[0].Txn
	apptxn3.ApplicationID = 3
	for i := 0; i < int(rate); i++ {
		apptxn3.ForeignApps = append(apptxn3.ForeignApps, 3)
	}
	txns = []transactions.SignedTxn{{Txn: apptxn3}}
	drop = rm.shouldDropAt(txns, nil, now)
	require.True(t, drop)
}

// TestAppRateLimiter_Interval checks prev + cur rate approximation logic
func TestAppRateLimiter_Interval(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	rate := uint64(10)
	window := 10 * time.Second
	perSecondRate := uint64(window) / rate / uint64(time.Second)
	rm := makeAppRateLimiter(512, perSecondRate, window)

	txns := getAppTxnGroup(1)
	now := time.Date(2023, 9, 11, 10, 10, 11, 0, time.UTC).UnixNano() // 11 sec => 1 sec into the interval

	// fill 80% of the current interval
	// switch to the next interval
	// ensure only 30% of the rate is available (8 * 0.9 = 7.2 => 7)
	// 0.9 is calculated as 1 - 0.1 (fraction of the interval elapsed)
	// since the next interval at second 21 would by 1 sec (== 10% == 0.1) after the interval beginning
	for i := 0; i < int(0.8*float64(rate)); i++ {
		drop := rm.shouldDropAt(txns, nil, now)
		require.False(t, drop)
	}

	next := now + int64(window)
	for i := 0; i < int(0.3*float64(rate)); i++ {
		drop := rm.shouldDropAt(txns, nil, next)
		require.False(t, drop)
	}

	drop := rm.shouldDropAt(txns, nil, next)
	require.True(t, drop)
}

// TestAppRateLimiter_IntervalFull checks the cur counter accounts only admitted requests
func TestAppRateLimiter_IntervalAdmitted(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	rate := uint64(10)
	window := 10 * time.Second
	perSecondRate := uint64(window) / rate / uint64(time.Second)
	rm := makeAppRateLimiter(512, perSecondRate, window)

	txns := getAppTxnGroup(1)
	bk := txgroupToKeys(getAppTxnGroup(basics.AppIndex(1)), nil, rm.seed, rm.salt, numBuckets)
	require.Equal(t, 1, len(bk.buckets))
	require.Equal(t, 1, len(bk.keys))
	b := bk.buckets[0]
	k := bk.keys[0]
	now := time.Date(2023, 9, 11, 10, 10, 11, 0, time.UTC).UnixNano() // 11 sec => 1 sec into the interval

	// fill a current interval with more than rate requests
	// ensure the counter does not exceed the rate
	for i := 0; i < int(rate); i++ {
		drop := rm.shouldDropAt(txns, nil, now)
		require.False(t, drop)
	}
	drop := rm.shouldDropAt(txns, nil, now)
	require.True(t, drop)

	entry := rm.buckets[b].entries[k]
	require.NotNil(t, entry)
	require.Equal(t, int64(rate), entry.cur.Load())
}

// TestAppRateLimiter_IntervalSkip checks that the rate is reset when no requests within some interval
func TestAppRateLimiter_IntervalSkip(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	rate := uint64(10)
	window := 10 * time.Second
	perSecondRate := uint64(window) / rate / uint64(time.Second)
	rm := makeAppRateLimiter(512, perSecondRate, window)

	txns := getAppTxnGroup(1)
	now := time.Date(2023, 9, 11, 10, 10, 11, 0, time.UTC).UnixNano() // 11 sec => 1 sec into the interval

	// fill 80% of the current interval
	// switch to the next next interval
	// ensure all capacity is available

	for i := 0; i < int(0.8*float64(rate)); i++ {
		drop := rm.shouldDropAt(txns, nil, now)
		require.False(t, drop)
	}

	nextnext := now + int64(2*window)
	for i := 0; i < int(rate); i++ {
		drop := rm.shouldDropAt(txns, nil, nextnext)
		require.False(t, drop)
	}

	drop := rm.shouldDropAt(txns, nil, nextnext)
	require.True(t, drop)
}

func TestAppRateLimiter_IPAddr(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	rate := uint64(10)
	window := 10 * time.Second
	perSecondRate := uint64(window) / rate / uint64(time.Second)
	rm := makeAppRateLimiter(512, perSecondRate, window)

	txns := getAppTxnGroup(1)
	now := time.Now().UnixNano()

	for i := 0; i < int(rate); i++ {
		drop := rm.shouldDropAt(txns, []byte{1}, now)
		require.False(t, drop)
		drop = rm.shouldDropAt(txns, []byte{2}, now)
		require.False(t, drop)
	}

	drop := rm.shouldDropAt(txns, []byte{1}, now)
	require.True(t, drop)
	drop = rm.shouldDropAt(txns, []byte{2}, now)
	require.True(t, drop)
}

// TestAppRateLimiter_MaxSize puts size+1 elements into a single bucket and ensures the total size is capped
func TestAppRateLimiter_MaxSize(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	const bucketSize = 4
	const size = bucketSize * numBuckets
	const rate uint64 = 10
	window := 10 * time.Second
	rm := makeAppRateLimiter(size, rate, window)

	for i := 1; i <= int(size)+1; i++ {
		drop := rm.shouldDrop(getAppTxnGroup(basics.AppIndex(1)), []byte{byte(i)})
		require.False(t, drop)
	}
	bucket := int(memhash64(uint64(1), rm.seed) % numBuckets)
	require.Equal(t, bucketSize, len(rm.buckets[bucket].entries))
	var totalSize int
	for i := 0; i < len(rm.buckets); i++ {
		totalSize += len(rm.buckets[i].entries)
		if i != bucket {
			require.Equal(t, 0, len(rm.buckets[i].entries))
		}
	}
	require.LessOrEqual(t, totalSize, int(size))
}

// TestAppRateLimiter_EvictOrder ensures that the least recent used is evicted
func TestAppRateLimiter_EvictOrder(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	const bucketSize = 4
	const size = bucketSize * numBuckets
	const rate uint64 = 10
	window := 10 * time.Second
	rm := makeAppRateLimiter(size, rate, window)

	keys := make([]keyType, 0, int(bucketSize)+1)
	bucket := int(memhash64(uint64(1), rm.seed) % numBuckets)
	for i := 0; i < bucketSize; i++ {
		bk := txgroupToKeys(getAppTxnGroup(basics.AppIndex(1)), []byte{byte(i)}, rm.seed, rm.salt, numBuckets)
		require.Equal(t, 1, len(bk.buckets))
		require.Equal(t, 1, len(bk.keys))
		require.Equal(t, bucket, bk.buckets[0])
		keys = append(keys, bk.keys[0])
		drop := rm.shouldDrop(getAppTxnGroup(basics.AppIndex(1)), []byte{byte(i)})
		require.False(t, drop)
	}
	require.Equal(t, bucketSize, len(rm.buckets[bucket].entries))

	// add one more and expect the first evicted
	bk := txgroupToKeys(getAppTxnGroup(basics.AppIndex(1)), []byte{byte(bucketSize)}, rm.seed, rm.salt, numBuckets)
	require.Equal(t, 1, len(bk.buckets))
	require.Equal(t, 1, len(bk.keys))
	require.Equal(t, bucket, bk.buckets[0])
	drop := rm.shouldDrop(getAppTxnGroup(basics.AppIndex(1)), []byte{byte(bucketSize)})
	require.False(t, drop)

	require.Equal(t, bucketSize, len(rm.buckets[bucket].entries))
	require.NotContains(t, rm.buckets[bucket].entries, keys[0])
	for i := 1; i < len(keys); i++ {
		require.Contains(t, rm.buckets[bucket].entries, keys[i])
	}

	var totalSize int
	for i := 0; i < len(rm.buckets); i++ {
		totalSize += len(rm.buckets[i].entries)
		if i != bucket {
			require.Equal(t, 0, len(rm.buckets[i].entries))
		}
	}
	require.LessOrEqual(t, totalSize, int(size))
}

func BenchmarkBlake2(b *testing.B) {
	var salt [16]byte
	crypto.RandBytes(salt[:])
	origin := make([]byte, 4)

	var buf [8 + 16 + 16]byte // uint64 + 16 bytes of salt + up to 16 bytes of address

	b.Run("blake2b-sum256", func(b *testing.B) {
		total := 0
		for i := 0; i < b.N; i++ {
			binary.LittleEndian.PutUint64(buf[:8], rand.Uint64())
			copy(buf[8:], salt[:])
			copied := copy(buf[8+16:], origin)
			h := blake2b.Sum256(buf[:8+16+copied])
			total += len(h[:])
		}
		b.Logf("total1: %d", total) // to prevent optimizing out the loop
	})

	b.Run("blake2b-sum8", func(b *testing.B) {
		total := 0
		for i := 0; i < b.N; i++ {
			d, err := blake2b.New(8, nil)
			require.NoError(b, err)

			binary.LittleEndian.PutUint64(buf[:8], rand.Uint64())
			copy(buf[8:], salt[:])
			copied := copy(buf[8+16:], origin)

			_, err = d.Write(buf[:8+16+copied])
			require.NoError(b, err)
			h := d.Sum([]byte{})
			total += len(h[:])
		}
		b.Logf("total2: %d", total)
	})
}

func BenchmarkAppRateLimiter(b *testing.B) {
	cfg := config.GetDefaultLocal()

	b.Run("multi bucket no evict", func(b *testing.B) {
		rm := makeAppRateLimiter(
			cfg.TxBacklogAppTxRateLimiterMaxSize,
			uint64(cfg.TxBacklogAppTxPerSecondRate),
			time.Duration(cfg.TxBacklogServiceRateWindowSeconds)*time.Second,
		)
		dropped := 0
		for i := 0; i < b.N; i++ {
			if rm.shouldDrop(getAppTxnGroup(basics.AppIndex(i%512)), []byte{byte(i), byte(i % 256)}) {
				dropped++
			}
		}
		b.ReportMetric(float64(dropped)/float64(b.N), "%_drop")
		if rm.evictions > 0 {
			b.Logf("# evictions %d, time %d us", rm.evictions, rm.evictionTime/uint64(time.Microsecond))
		}
	})

	b.Run("single bucket no evict", func(b *testing.B) {
		rm := makeAppRateLimiter(
			cfg.TxBacklogAppTxRateLimiterMaxSize,
			uint64(cfg.TxBacklogAppTxPerSecondRate),
			time.Duration(cfg.TxBacklogServiceRateWindowSeconds)*time.Second,
		)
		dropped := 0
		for i := 0; i < b.N; i++ {
			if rm.shouldDrop(getAppTxnGroup(basics.AppIndex(1)), []byte{byte(i), byte(i % 256)}) {
				dropped++
			}
		}
		b.ReportMetric(float64(dropped)/float64(b.N), "%_drop")
		if rm.evictions > 0 {
			b.Logf("# evictions %d, time %d us", rm.evictions, rm.evictionTime/uint64(time.Microsecond))
		}
	})

	b.Run("single bucket w evict", func(b *testing.B) {
		rm := makeAppRateLimiter(
			cfg.TxBacklogAppTxRateLimiterMaxSize,
			uint64(cfg.TxBacklogAppTxPerSecondRate),
			time.Duration(cfg.TxBacklogServiceRateWindowSeconds)*time.Second,
		)
		dropped := 0
		for i := 0; i < b.N; i++ {
			if rm.shouldDrop(getAppTxnGroup(basics.AppIndex(1)), []byte{byte(i), byte(i / 256), byte(i % 256)}) {
				dropped++
			}
		}
		b.ReportMetric(float64(dropped)/float64(b.N), "%_drop")
		if rm.evictions > 0 {
			b.Logf("# evictions %d, time %d us", rm.evictions, rm.evictionTime/uint64(time.Microsecond))
		}
	})
}

type headerRow struct {
	Ts   time.Time
	IP   [4]byte
	Port int
}

type decoderV2 struct{}

func (decoderV2) decodeHeader(r *snappy.Reader) (*headerRow, int, error) {
	headerBytes := make([]byte, 18)
	n, err := io.ReadFull(r, headerBytes)
	if err != nil {
		return nil, 0, err
	} else if n != 18 {
		return nil, 0, errors.New("incomplete v2 header")
	}
	ts := int64(binary.BigEndian.Uint64(headerBytes))
	tsTime := time.Unix(0, ts)
	var ip [4]byte
	copy(ip[:], headerBytes[8:12])
	port := binary.BigEndian.Uint16(headerBytes[12:14])
	lenMsg := binary.BigEndian.Uint32(headerBytes[14:])
	return &headerRow{Ts: tsTime, IP: ip, Port: int(port)}, int(lenMsg), nil
}

type txGroupItem struct {
	err     error
	path    string
	ts      time.Time
	ip      [4]byte
	port    int
	txgroup []transactions.SignedTxn
	data    []byte
}

func transcribeSnappyLog(filePath string, decode bool, output chan txGroupItem, wg *sync.WaitGroup) {
	if wg != nil {
		defer wg.Done()
	}
	file, err := os.OpenFile(filePath, os.O_RDONLY, 0644)
	if err != nil {
		output <- txGroupItem{err: err}
		return
	}
	defer file.Close()

	decoder := decoderV2{}
	snappyReader := snappy.NewReader(file)
	var n int

	for {
		headers, lenMsg, err := decoder.decodeHeader(snappyReader)
		if err == io.EOF {
			break
		} else if err != nil {
			output <- txGroupItem{err: err}
			return
		}

		msgBuff := make([]byte, lenMsg)
		n, err = io.ReadFull(snappyReader, msgBuff)
		if err == io.EOF {
			output <- txGroupItem{err: fmt.Errorf("missing body in %s", filePath)}
			return
		}
		if n != int(lenMsg) {
			output <- txGroupItem{err: fmt.Errorf("incomplete message body in %s", filePath)}
			return
		}

		var txgroup []transactions.SignedTxn
		if decode {
			dec := codec.NewDecoderBytes(msgBuff, new(codec.MsgpackHandle))
			for {
				var stx transactions.SignedTxn
				err := dec.Decode(&stx)
				if err == io.EOF {
					break
				} else if err != nil {
					output <- txGroupItem{err: err}
					return
				}
				txgroup = append(txgroup, stx)
			}
		}
		output <- txGroupItem{ts: headers.Ts, ip: headers.IP, port: headers.Port, txgroup: txgroup, data: msgBuff, path: filePath}
	}
}

func logsReaderWorkers(logsDir string, files []fs.DirEntry, decode bool, output chan txGroupItem, numWorkers int, wg *sync.WaitGroup) {
	input := make(chan string, len(files))
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		path := filepath.Join(logsDir, file.Name())
		input <- path
	}
	close(input)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range input {
				transcribeSnappyLog(path, decode, output, nil)
			}
		}()
	}
}

func logsReaderConcurrent(logsDir string, files []fs.DirEntry, decode bool, output chan txGroupItem, _ int, wg *sync.WaitGroup) {
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		path := filepath.Join(logsDir, file.Name())
		wg.Add(1)
		go transcribeSnappyLog(path, decode, output, wg)
	}
}

type txlogstats struct {
	keys         int
	apps         int
	appAddrPairs int
	ips          int
	peers        int
	mints        time.Time
	maxts        time.Time
}

type benchstats struct {
	count      uint64
	drops      uint64
	dur        time.Duration
	admissions []int64 // nanoseconds
	txlogstats
}

func consumerWorkers(input chan txGroupItem, output chan benchstats, rm *appRateLimiter, numWorkers int, wg *sync.WaitGroup, useCurTime bool) {
	var count atomic.Uint64
	var drops atomic.Uint64
	var dur time.Duration
	var admissions []int64 = make([]int64, 0, 600_000)
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			for item := range input {
				if rm != nil {
					t := item.ts.UnixNano()
					if useCurTime {
						t = time.Now().UnixNano()
					}
					dropped := rm.shouldDropAt(item.txgroup, item.ip[:], t)
					if dropped {
						drops.Add(1)
					} else {
						admissions = append(admissions, t)
					}
				}
				count.Add(1)
			}
			dur = time.Since(start)
			output <- benchstats{count.Load(), drops.Load(), dur, admissions, txlogstats{}}
		}()
	}
}

func txgroupStats(input chan txGroupItem, output chan benchstats, _ *appRateLimiter, _ int, wg *sync.WaitGroup, _ bool) {
	var count uint64
	uniqueKeys := make(map[keyType]struct{})
	uniqueApps := make(map[basics.AppIndex]struct{})
	uniqueAppAddrPairs := make(map[[12]byte]struct{})
	ips := make(map[[4]byte]struct{})
	peers := make(map[[6]byte]struct{})
	var mints time.Time
	var maxts time.Time
	wg.Add(1)
	go func() {
		defer wg.Done()
		for item := range input {
			if item.ts.Before(mints) || mints.IsZero() {
				mints = item.ts
			}
			if item.ts.After(maxts) || maxts.IsZero() {
				maxts = item.ts
			}
			kb := txgroupToKeys(item.txgroup, item.ip[:], 0, [16]byte{}, numBuckets)
			for _, key := range kb.keys {
				uniqueKeys[key] = struct{}{}
			}
			putAppKeyBuf(kb)
			ips[item.ip] = struct{}{}
			var peer [6]byte
			copy(peer[:], item.ip[:])
			binary.BigEndian.PutUint16(peer[4:], uint16(item.port))
			peers[peer] = struct{}{}

			var buf [8 + 4]byte
			for i := range item.txgroup {
				if item.txgroup[i].Txn.Type == protocol.ApplicationCallTx {
					appIdx := item.txgroup[i].Txn.ApplicationID
					uniqueApps[appIdx] = struct{}{}
					binary.LittleEndian.PutUint64(buf[:8], uint64(appIdx))
					copy(buf[8:], item.ip[:])
					uniqueAppAddrPairs[buf] = struct{}{}

					if len(item.txgroup[i].Txn.ForeignApps) > 0 {
						for _, appIdx := range item.txgroup[i].Txn.ForeignApps {
							uniqueApps[appIdx] = struct{}{}
							binary.LittleEndian.PutUint64(buf[:8], uint64(appIdx))
							copy(buf[8:], item.ip[:])
							uniqueAppAddrPairs[buf] = struct{}{}
						}
					}
				}
			}
			count++
		}
		output <- benchstats{count, 0, 0, nil, txlogstats{len(uniqueKeys), len(uniqueApps), len(uniqueAppAddrPairs), len(ips), len(peers), mints, maxts}}
	}()
}

// run with
// export ALGOD_TXLOGS_DIR=mytxlogs
// go test ./data -run ^$ -bench BenchmarkAppRateLimiterLogs -benchmem -benchtime=1x -v
func BenchmarkAppRateLimiterLogs(b *testing.B) {
	deadlockDisable := deadlock.Opts.Disable
	deadlock.Opts.Disable = true
	defer func() {
		deadlock.Opts.Disable = deadlockDisable
	}()

	logsDir := os.Getenv("ALGOD_TXLOGS_DIR")
	if len(logsDir) == 0 {
		b.Log("ALGOD_TXLOGS_DIR not set")
		b.FailNow()
	}

	// set to admissions.log or something to get timestamps of admitted events
	admissionsLog := os.Getenv("ALGOD_TXLOGS_ADMISSIONS_LOG")

	var numFiles uint64
	numFilesStr := os.Getenv("ALGOD_TXLOGS_NUM_FILES")
	if len(numFilesStr) > 0 {
		var err error
		numFiles, err = strconv.ParseUint(numFilesStr, 10, 64)
		require.NoError(b, err)
	}

	files, err := os.ReadDir(logsDir)
	if err != nil {
		b.Log(err)
		b.FailNow()
	}
	if numFiles > uint64(len(files)) || numFiles == 0 {
		numFiles = uint64(len(files))
	}

	files = files[:numFiles]

	benchmarks := []struct {
		name         string
		readerFunc   func(logsDir string, files []fs.DirEntry, decode bool, output chan txGroupItem, numWorkers int, wg *sync.WaitGroup)
		numReaders   int
		consumerFunc func(input chan txGroupItem, output chan benchstats, _ *appRateLimiter, _ int, wg *sync.WaitGroup, useCurTime bool)
		numConsumers int
		noop         bool
		useCurTime   bool
	}{
		// {"stats", logsReaderConcurrent, 0, txgroupStats, 1, true, false},
		// {"max_reader_1_consumer_noop", logsReaderConcurrent, 0, consumerWorkers, 1, true, false},
		// {"4_reader_4_consumer_noop", logsReaderWorkers, 4, consumerWorkers, 4, true, false},
		// {"max_reader_1_consumer", logsReaderConcurrent, 0, consumerWorkers, 1, false, false},
		// {"4_reader_4_consumer", logsReaderWorkers, 4, consumerWorkers, 4, false, false},
		// {"max_reader_20_consumer", logsReaderConcurrent, 0, consumerWorkers, 20, false, false},
		// {"one_reader_1_consumer_noop", logsReaderWorkers, 1, consumerWorkers, 1, true, false},
		{"one_reader_20_consumer", logsReaderWorkers, 1, consumerWorkers, 20, false, false},
	}

	for _, bench := range benchmarks {
		b.Run(bench.name, func(b *testing.B) {
			cfg := config.GetDefaultLocal()

			txgroupStream := make(chan txGroupItem)
			var wg sync.WaitGroup
			const decodeTxn = true
			bench.readerFunc(logsDir, files, decodeTxn, txgroupStream, bench.numReaders, &wg)

			var rm *appRateLimiter
			if !bench.noop {
				rm = makeAppRateLimiter(
					cfg.TxBacklogAppTxRateLimiterMaxSize,
					uint64(cfg.TxBacklogAppTxPerSecondRate),
					time.Duration(cfg.TxBacklogServiceRateWindowSeconds)*time.Second,
				)
			}

			var dur time.Duration
			var wg2 sync.WaitGroup
			start := time.Now()
			result := make(chan benchstats, bench.numConsumers)
			bench.consumerFunc(txgroupStream, result, rm, bench.numConsumers, &wg2, bench.useCurTime)

			wg.Wait()
			close(txgroupStream)
			wg2.Wait()
			dur = time.Since(start)

			// write out some report info
			if rm != nil {
				stat := <-result
				count := int(stat.count)
				drops := int(stat.drops)
				b.Logf("processed %d txgroups in %s (%d dropped, %d evictions)", count, dur, drops, rm.evictions)
				// print bucket sizes by count
				// write rm.buckets size histogram code sorted by bucket size
				bucketSizes := make([]float64, len(rm.buckets))
				for i := range rm.buckets {
					bucketSizes[i] = float64(len(rm.buckets[i].entries))
				}
				hist := histogram.Hist(10, bucketSizes)
				err := histogram.Fprint(os.Stdout, hist, histogram.Linear(5))
				require.NoError(b, err)

				if len(stat.admissions) > 0 && len(admissionsLog) > 0 {
					file, err := os.OpenFile(admissionsLog, os.O_CREATE|os.O_WRONLY, 0644)
					require.NoError(b, err)
					defer file.Close()
					datawriter := bufio.NewWriter(file)
					defer datawriter.Flush()
					for _, t := range stat.admissions {
						us := t / 1000 // nano to microseconds
						require.NoError(b, err)
						_, err := datawriter.WriteString(strconv.FormatInt(us, 10) + "\n")
						require.NoError(b, err)
					}
				}
			} else {
				stat := <-result
				count := int(stat.count)
				b.Logf("processed %d txgroups in %s", count, dur)
				if bench.name == "stats" {
					b.Logf("unique keys %d, apps %d, ips %d, peers %d, app-addr pairs %d\n", stat.keys, stat.apps, stat.ips, stat.peers, stat.appAddrPairs)
					b.Logf("mint %s, maxt %s, duration %d\n", stat.mints, stat.maxts, int(stat.maxts.Sub(stat.mints).Seconds()))
				}
			}
		})
	}
}

// txlogSender is used to implement OnClose, since TXHandlers expect to use Senders and ERL Clients
type txlogSender struct {
	ip   [4]byte
	port int
}

func (m *txlogSender) OnClose(func()) {}

func (m *txlogSender) IPAddr() []byte      { return m.ip[:] }
func (m *txlogSender) RoutingAddr() []byte { return m.ip[:] }

// run with
// export ALGOD_TXLOGS_DIR=mytxlogs
// go test ./data -run ^$ -bench BenchmarkAppRateLimiterTxHandlerLogs -benchmem -benchtime=1x -v
func BenchmarkAppRateLimiterTxHandlerLogs(b *testing.B) {
	logsDir := os.Getenv("ALGOD_TXLOGS_DIR")
	if len(logsDir) == 0 {
		b.Log("ALGOD_TXLOGS_DIR not set")
		b.FailNow()
	}

	var numFiles uint64
	numFilesStr := os.Getenv("ALGOD_TXLOGS_NUM_FILES")
	if len(numFilesStr) > 0 {
		var err error
		numFiles, err = strconv.ParseUint(numFilesStr, 10, 64)
		require.NoError(b, err)
	}

	files, err := os.ReadDir(logsDir)
	if err != nil {
		b.Log(err)
		b.FailNow()
	}
	if numFiles > uint64(len(files)) || numFiles == 0 {
		numFiles = uint64(len(files))
	}

	files = files[:numFiles]

	cfg := config.GetDefaultLocal()

	benchmarks := []struct {
		name           string
		setupCfg       func(c config.Local) config.Local
		setupTxHandler func(h *TxHandler)
	}{
		// {"unlimited", func(c config.Local) config.Local {
		// 	c.EnableTxBacklogRateLimiting = false
		// 	c.TxBacklogAppTxRateLimiterMaxSize = 0
		// 	return c
		// }, func(h *TxHandler) {}},
		// {"erl_only", func(c config.Local) config.Local {
		// 	c.EnableTxBacklogRateLimiting = true
		// 	c.TxBacklogTxRateLimiterMaxSize = 0
		// 	return c
		// }, func(h *TxHandler) {}},
		{"app_only", func(c config.Local) config.Local {
			c.EnableTxBacklogRateLimiting = true
			c.TxBacklogAppTxRateLimiterMaxSize = config.GetDefaultLocal().TxBacklogAppTxRateLimiterMaxSize
			return c
		}, func(h *TxHandler) { h.erl = nil }},
		// {"all_limited", func(c config.Local) config.Local {
		// 	c.EnableTxBacklogRateLimiting = true
		// 	c.TxBacklogTxRateLimiterMaxSize = config.GetDefaultLocal().TxBacklogTxRateLimiterMaxSize
		// 	return c
		// }, func(h *TxHandler) {}},
	}

	for _, bench := range benchmarks {
		b.Run(bench.name, func(b *testing.B) {
			transactionMessagesDroppedFromBacklogStart := transactionMessagesDroppedFromBacklog.GetUint64Value()
			txBacklogDroppedCongestionManagementStart := txBacklogDroppedCongestionManagement.GetUint64Value()
			transactionMessagesAppLimiterDropStart := transactionMessagesAppLimiterDrop.GetUint64Value()

			txgroupStream := make(chan txGroupItem)
			var wgReaders sync.WaitGroup
			const decodeTxn = false
			// logsReaderConcurrent(logsDir, files, decodeTxn, txgroupStream, 0, &wgReaders)
			logsReaderWorkers(logsDir, files, decodeTxn, txgroupStream, 1, &wgReaders)

			_, _, genesis := makeTestGenesisAccounts(b, 10)
			genBal := bookkeeping.MakeGenesisBalances(genesis, sinkAddr, poolAddr)
			conf := bench.setupCfg(cfg)

			log := logging.TestingLog(b)
			log.SetLevel(logging.Warn)
			const inMem = true
			ledgerName := fmt.Sprintf("%s-mem-%d", b.Name(), b.N)
			l, err := LoadLedger(log, ledgerName, inMem, protocol.ConsensusCurrentVersion, genBal, genesisID, genesisHash, nil, conf)
			require.NoError(b, err)
			defer l.Close()

			txHandler, err := makeTestTxHandler(l, conf)
			require.NoError(b, err)
			defer txHandler.txVerificationPool.Shutdown()
			defer close(txHandler.streamVerifierDropped)

			bench.setupTxHandler(txHandler)

			var wgBgWorker sync.WaitGroup
			wgBgWorker.Add(1)
			var backlogMsgs uint64
			go func() {
				// drain backlog and count
				defer wgBgWorker.Done()
				for range txHandler.backlogQueue {
					backlogMsgs++
					// sleep 100us => 1 sec / (100 * 10^-6) = 10,000 rate
					time.Sleep(100 * time.Microsecond)
				}
			}()

			const numWorkers = 20
			var wgWorkers sync.WaitGroup
			start := time.Now()
			for i := 0; i < numWorkers; i++ {
				wgWorkers.Add(1)
				go func() {
					defer wgWorkers.Done()
					for item := range txgroupStream {
						msg := network.IncomingMessage{
							Data:     item.data,
							Sender:   &txlogSender{ip: item.ip, port: item.port},
							Received: item.ts.UnixNano(),
						}
						txHandler.processIncomingTxn(msg)
					}
				}()
			}
			wgReaders.Wait()
			close(txgroupStream)
			wgWorkers.Wait()
			dur := time.Since(start)

			close(txHandler.backlogQueue)
			wgBgWorker.Wait()
			b.Logf("backlog: admitted %d (%d dropped) in %s", backlogMsgs, transactionMessagesDroppedFromBacklog.GetUint64Value()-transactionMessagesDroppedFromBacklogStart, dur)
			b.Logf("erl dropped %d, app limiter dropped %d", txBacklogDroppedCongestionManagement.GetUint64Value()-txBacklogDroppedCongestionManagementStart, transactionMessagesAppLimiterDrop.GetUint64Value()-transactionMessagesAppLimiterDropStart)
		})
	}
}
