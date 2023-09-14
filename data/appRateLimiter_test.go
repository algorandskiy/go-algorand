// Copyright (C) 2019-2023 Algorand, Inc.
// This file is part of go-algorand
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
	"encoding/binary"
	"testing"
	"time"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/transactions"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/test/partitiontest"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/exp/rand"
)

func TestAppRateLimiter_Make(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	rate := uint64(10)
	window := 1 * time.Second
	rm := makeAppRateLimiter(10, rate, window)

	require.NotEmpty(t, rm.seed)
	require.NotEmpty(t, rm.salt)
	require.NotEmpty(t, rm.buckets)
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
	now := time.Now()
	drop := rm.shouldDropInner(txns, nil, now)
	require.False(t, drop)

	for i := len(txns); i < int(rate); i++ {
		drop = rm.shouldDropInner(txns, nil, now)
		require.False(t, drop)
	}

	drop = rm.shouldDropInner(txns, nil, now)
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
	drop = rm.shouldDropInner(txns, nil, now)
	require.True(t, drop)

	drop = rm.shouldDropInner(txns, nil, now.Add(2*window))
	require.True(t, drop)

	// check foreign apps
	apptxn3 := txns[0].Txn
	apptxn3.ApplicationID = 3
	for i := 0; i < int(rate); i++ {
		apptxn3.ForeignApps = append(apptxn3.ForeignApps, 3)
	}
	txns = []transactions.SignedTxn{{Txn: apptxn3}}
	drop = rm.shouldDropInner(txns, nil, now)
	require.True(t, drop)
}

func TestAppRateLimiter_Interval(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	rate := uint64(10)
	window := 10 * time.Second
	perSecondRate := uint64(window) / rate / uint64(time.Second)
	rm := makeAppRateLimiter(512, perSecondRate, window)

	txns := getAppTxnGroup(1)
	now := time.Date(2023, 9, 11, 10, 10, 11, 0, time.UTC) // 11 sec => 1 sec into the interval

	// fill 80% of the current interval
	// switch to the next interval
	// ensure only 30% of the rate is available (8 * 0.9 = 7.2 => 7)
	// 0.9 is calculated as 1 - 0.1 (fraction of the interval elapsed)
	// since the next interval at second 21 would by 1 sec (== 10% == 0.1) after the interval beginning
	for i := 0; i < int(0.8*float64(rate)); i++ {
		drop := rm.shouldDropInner(txns, nil, now)
		require.False(t, drop)
	}

	next := now.Add(window)
	for i := 0; i < int(0.3*float64(rate)); i++ {
		drop := rm.shouldDropInner(txns, nil, next)
		require.False(t, drop)
	}

	drop := rm.shouldDropInner(txns, nil, next)
	require.True(t, drop)
}

func TestAppRateLimiter_IntervalSkip(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	rate := uint64(10)
	window := 10 * time.Second
	perSecondRate := uint64(window) / rate / uint64(time.Second)
	rm := makeAppRateLimiter(512, perSecondRate, window)

	txns := getAppTxnGroup(1)
	now := time.Date(2023, 9, 11, 10, 10, 11, 0, time.UTC) // 11 sec => 1 sec into the interval

	// fill 80% of the current interval
	// switch to the next next interval
	// ensure all capacity is available

	for i := 0; i < int(0.8*float64(rate)); i++ {
		drop := rm.shouldDropInner(txns, nil, now)
		require.False(t, drop)
	}

	nextnext := now.Add(2 * window)
	for i := 0; i < int(rate); i++ {
		drop := rm.shouldDropInner(txns, nil, nextnext)
		require.False(t, drop)
	}

	drop := rm.shouldDropInner(txns, nil, nextnext)
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
	now := time.Now()

	for i := 0; i < int(rate); i++ {
		drop := rm.shouldDropInner(txns, []byte{1}, now)
		require.False(t, drop)
		drop = rm.shouldDropInner(txns, []byte{2}, now)
		require.False(t, drop)
	}

	drop := rm.shouldDropInner(txns, []byte{1}, now)
	require.True(t, drop)
	drop = rm.shouldDropInner(txns, []byte{2}, now)
	require.True(t, drop)
}

// TestAppRateLimiter_MaxSize puts size+1 elements into a single bucket and ensures the total size is capped
func TestAppRateLimiter_MaxSize(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	const size uint64 = 512
	const rate uint64 = 10
	const bucketSize = int(size / numBuckets)
	window := 10 * time.Second
	rm := makeAppRateLimiter(size, rate, window)

	for i := 1; i <= int(size)+1; i++ {
		drop := rm.shouldDrop(getAppTxnGroup(basics.AppIndex(1)), []byte{byte(i)})
		require.False(t, drop)
	}
	bucket := int(memhash64(uint64(1), rm.seed) % uint64(len(rm.buckets)))
	require.Equal(t, bucketSize, len(rm.buckets[bucket]))
	var totalSize int
	for i := 0; i < len(rm.buckets); i++ {
		totalSize += len(rm.buckets[i])
		if i != bucket {
			require.Equal(t, 0, len(rm.buckets[i]))
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

	b.Run("multi bucket", func(b *testing.B) {
		rm := makeAppRateLimiter(
			uint64(cfg.TxBacklogTxRateLimiterMaxSize),
			uint64(cfg.TxBacklogTxRate),
			time.Duration(cfg.TxBacklogServiceRateWindowSeconds)*time.Second,
		)
		dropped := 0
		for i := 0; i < b.N; i++ {
			if rm.shouldDrop(getAppTxnGroup(basics.AppIndex(i%512)), []byte{byte(i), byte(i % 256)}) {
				dropped++
			}
		}
		b.ReportMetric(float64(dropped)/float64(b.N), "%_drop")
	})

	b.Run("single bucket no evict", func(b *testing.B) {
		rm := makeAppRateLimiter(
			uint64(cfg.TxBacklogTxRateLimiterMaxSize),
			uint64(cfg.TxBacklogTxRate),
			time.Duration(cfg.TxBacklogServiceRateWindowSeconds)*time.Second,
		)
		dropped := 0
		for i := 0; i < b.N; i++ {
			if rm.shouldDrop(getAppTxnGroup(basics.AppIndex(1)), []byte{byte(i), byte(i % 256)}) {
				dropped++
			}
		}
		b.ReportMetric(float64(dropped)/float64(b.N), "%_drop")
		b.Logf("# evictions %d, time %d us", rm.evictions, rm.evictionTime/uint64(time.Microsecond))
	})

	b.Run("single bucket w evict", func(b *testing.B) {
		rm := makeAppRateLimiter(
			uint64(cfg.TxBacklogTxRateLimiterMaxSize),
			uint64(cfg.TxBacklogTxRate),
			time.Duration(cfg.TxBacklogServiceRateWindowSeconds)*time.Second,
		)
		dropped := 0
		for i := 0; i < b.N; i++ {
			if rm.shouldDrop(getAppTxnGroup(basics.AppIndex(1)), []byte{byte(i), byte(i / 256), byte(i % 256)}) {
				dropped++
			}
		}
		b.ReportMetric(float64(dropped)/float64(b.N), "%_drop")
		b.Logf("# evictions %d, time %d us", rm.evictions, rm.evictionTime/uint64(time.Microsecond))
	})
}
