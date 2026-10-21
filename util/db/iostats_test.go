// Copyright (C) 2019-2026 Algorand Foundation Ltd.
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

package db

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/test/partitiontest"
)

const (
	testPageSize  = 4096
	testFrameSize = testPageSize + walFrameHeaderSize
)

// distinctLogPages returns the number of distinct pages among frames
// (from, to] of the log of the database at path, read from the log file.
func distinctLogPages(t *testing.T, path string, from, to uint64) uint64 {
	hdr, err := readWALIndex(path + "-shm")
	require.NoError(t, err)
	pages, err := walFramePages(path+"-wal", hdr.pageSize, hdr.salt, uint32(from), uint32(to), nil)
	require.NoError(t, err)
	return distinctPages(pages, 0, uint32(len(pages)))
}

// logFrames checkpoints the log in PASSIVE mode and returns the frames it
// holds.  The checkpoint copies every frame into the database file, so the
// next write restarts the log: each transaction the tests commit after one
// of these calls is alone in its generation of the log.
func logFrames(t *testing.T, acc Accessor) uint64 {
	_, frames, checkpointed, err := acc.walCheckpoint(context.Background(), "PASSIVE")
	require.NoError(t, err)
	require.Equal(t, frames, checkpointed)
	return uint64(frames)
}

func TestIOStatsAccountsCommits(t *testing.T) {
	partitiontest.PartitionTest(t)
	f := newWALEraseFixture(t)
	s := f.acc.stats
	require.NotNil(t, s)
	require.Equal(t, "erase.sqlite", s.name)

	// the fixture left the log empty, so the first transaction's frames are
	// the whole log and no restart happens
	c0 := s.counters()
	f.insert(40)
	n1 := logFrames(t, f.acc)
	p1 := distinctLogPages(t, f.path, 0, n1)
	require.Equal(t, n1, p1, "a transaction wrote a page twice")
	c1 := s.counters()
	require.Equal(t, c0.commits+1, c1.commits)
	require.Equal(t, n1*testFrameSize, c1.walBytes-c0.walBytes)
	require.Equal(t, c0.backfillBytes, c1.backfillBytes)
	require.Equal(t, c0.restarts, c1.restarts)
	require.Equal(t, c0.unobserved, c1.unobserved)
	require.Equal(t, c0.observeErrors, c1.observeErrors)
	require.Equal(t, c0.txBytesCount+1, c1.txBytesCount)
	require.Equal(t, n1*testFrameSize, c1.txBytesSum-c0.txBytesSum)
	require.Equal(t, uint64(1), c1.txBuckets[txBytesBucket(n1*testFrameSize)]-c0.txBuckets[txBytesBucket(n1*testFrameSize)])
	require.GreaterOrEqual(t, c1.txMicros, c0.txMicros)

	// the checkpoint above copied the log into the file, so this transaction
	// restarts the log: its own frames are charged to the log, the previous
	// generation's pages to the backfill
	f.exec("DELETE FROM t WHERE id=1")
	n2 := logFrames(t, f.acc)
	p2 := distinctLogPages(t, f.path, 0, n2)
	c2 := s.counters()
	require.Equal(t, c1.commits+1, c2.commits)
	require.Equal(t, n2*testFrameSize, c2.walBytes-c1.walBytes)
	require.Equal(t, p1*testPageSize, c2.backfillBytes-c1.backfillBytes)
	require.Equal(t, c1.restarts+1, c2.restarts)
	require.Equal(t, c1.unobserved, c2.unobserved)

	// a rolled-back transaction is counted and appends nothing
	err := f.acc.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO t (v) VALUES (x'00')"); err != nil {
			return err
		}
		return errors.New("abort")
	})
	require.Error(t, err)
	c3 := s.counters()
	require.Equal(t, c2.rollbacks+1, c3.rollbacks)
	require.Equal(t, c2.commits, c3.commits)
	require.Equal(t, c2.walBytes, c3.walBytes)

	// a write outside Atomic is not observed; the next observation sees the
	// change counter skip and charges the frames of both to the log
	_, err = f.acc.Handle.Exec("INSERT INTO t (v) VALUES (x'01')")
	require.NoError(t, err)
	f.exec("DELETE FROM t WHERE id=2")
	n4 := logFrames(t, f.acc)
	c4 := s.counters()
	require.Equal(t, c3.commits+1, c4.commits)
	require.Equal(t, c3.unobserved+1, c4.unobserved)
	require.Equal(t, n4*testFrameSize, c4.walBytes-c3.walBytes)
	require.Equal(t, p2*testPageSize, c4.backfillBytes-c3.backfillBytes)

	// the read-only half of a pair commits read transactions, which are not
	// observed; an in-memory database has no accounting at all
	path := filepath.Join(t.TempDir(), "pair.sqlite")
	p, err := OpenPair(path, false)
	require.NoError(t, err)
	require.Same(t, p.Rdb.stats, p.Wdb.stats)
	require.NoError(t, p.Wdb.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)")
		return err
	}))
	pc := p.Wdb.stats.counters()
	require.NoError(t, p.Rdb.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		var n int64
		return tx.QueryRow("SELECT COUNT(*) FROM t").Scan(&n)
	}))
	require.Equal(t, pc.commits, p.Wdb.stats.counters().commits)
	p.Close()

	mem, err := MakeAccessor("iostats-mem", false, true)
	require.NoError(t, err)
	require.Nil(t, mem.stats)
	require.NoError(t, mem.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)")
		return err
	}))
	mem.Close()
}

func TestIOStatsAccountsErase(t *testing.T) {
	partitiontest.PartitionTest(t)
	ctx := context.Background()
	f := newWALEraseFixture(t)
	s := f.acc.stats

	// two transactions in one generation of the log; the delete rewrites a
	// leaf page of the insert, so the log holds more frames than pages
	f.insert(40)
	f.exec("DELETE FROM t WHERE id=1")
	s.mu.Lock()
	m := uint64(s.last.mxFrame)
	s.mu.Unlock()
	require.NotZero(t, m)
	pm := distinctLogPages(t, f.path, 0, m)
	require.Less(t, pm, m, "the log holds no page twice; the test proves nothing")
	c0 := s.counters()

	var zeroFrames, zeroPages uint64
	f.acc.SetEraseWALHook(func() {
		hdr, err := readWALIndex(f.path + "-shm")
		require.NoError(t, err)
		zeroFrames = uint64(hdr.mxFrame)
		zeroPages = distinctLogPages(t, f.path, 0, zeroFrames)
	})
	require.NoError(t, f.acc.EraseWAL(ctx, true))
	f.acc.SetEraseWALHook(nil)
	require.NotZero(t, zeroFrames)
	c1 := s.counters()
	// the RESTART checkpoint copied the transactions' pages into the file,
	// each once
	require.Equal(t, pm*testPageSize, c1.backfillBytes-c0.backfillBytes)
	require.Equal(t, c0.walBytes, c1.walBytes)
	// the zero pages were appended, then copied into the file
	require.Equal(t, zeroFrames*testFrameSize+zeroPages*testPageSize, c1.eraseBytes-c0.eraseBytes)
	// the overwrite restarted the log and the truncation reset it
	require.Equal(t, c0.restarts+2, c1.restarts)
	require.Equal(t, c0.commits, c1.commits)
	require.Equal(t, c0.unobserved, c1.unobserved)
	require.Equal(t, c0.observeErrors, c1.observeErrors)

	// the next transaction is accounted on its own, in the empty log
	f.insert(1)
	n := logFrames(t, f.acc)
	c2 := s.counters()
	require.Equal(t, n*testFrameSize, c2.walBytes-c1.walBytes)
	require.Equal(t, c1.backfillBytes, c2.backfillBytes)
	require.Equal(t, c1.restarts, c2.restarts)
	require.Equal(t, c1.unobserved, c2.unobserved)
}

// TestIOStatsCheckpointCountsPages checks that a checkpoint is charged with
// the distinct pages among the frames it consumed: a row replaced on every
// transaction fills the log with frames of the same few pages, which the
// checkpoint copies once each.
func TestIOStatsCheckpointCountsPages(t *testing.T) {
	partitiontest.PartitionTest(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "replace.sqlite")
	acc, err := MakeAccessor(path, false, false)
	require.NoError(t, err)
	defer acc.Close()
	_, err = acc.Handle.ExecContext(ctx, "PRAGMA wal_autocheckpoint=0")
	require.NoError(t, err)
	require.NoError(t, acc.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TABLE Service (data BLOB)")
		return err
	}))
	_, _, _, err = acc.walCheckpoint(ctx, "TRUNCATE")
	require.NoError(t, err)
	s := acc.stats
	s.observeCheckpoint() // charges the checkpoint of the CREATE TABLE
	c0 := s.counters()

	// replace one 64 KiB row 20 times: 20 transactions of about 17 frames
	// over the same two sets of pages
	const replaces = 20
	for i := 0; i < replaces; i++ {
		blob := make([]byte, 64<<10)
		blob[0] = byte(i)
		require.NoError(t, acc.Atomic(func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.Exec("INSERT OR REPLACE INTO Service (rowid, data) VALUES (1, ?)", blob)
			return err
		}))
	}
	frames := logFrames(t, acc) // checkpoints the log
	pages := distinctLogPages(t, path, 0, frames)
	require.Greater(t, frames, 10*pages, "the log does not rewrite its pages; the test proves nothing")
	s.mu.Lock()
	require.Len(t, s.pages, int(frames))
	s.mu.Unlock()

	// the checkpoint is observed at the next commit, which restarts the log
	require.NoError(t, acc.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec("INSERT OR REPLACE INTO Service (rowid, data) VALUES (1, x'00')")
		return err
	}))
	c1 := s.counters()
	require.Equal(t, c0.commits+replaces+1, c1.commits)
	require.GreaterOrEqual(t, c1.walBytes-c0.walBytes, frames*testFrameSize)
	require.Equal(t, pages*testPageSize, c1.backfillBytes-c0.backfillBytes)
	require.Equal(t, c0.restarts+1, c1.restarts)
	require.Equal(t, c0.observeErrors, c1.observeErrors)
}

// TestWALFramePages checks the reading and counting of the pages of the
// log's frames.
func TestWALFramePages(t *testing.T) {
	partitiontest.PartitionTest(t)
	f := newWALEraseFixture(t)
	f.insert(40)
	f.exec("DELETE FROM t WHERE id=1")
	hdr, err := readWALIndex(f.path + "-shm")
	require.NoError(t, err)
	require.NotZero(t, hdr.mxFrame)

	pages, err := walFramePages(f.path+"-wal", hdr.pageSize, hdr.salt, 0, hdr.mxFrame, nil)
	require.NoError(t, err)
	require.Len(t, pages, int(hdr.mxFrame))
	// the delete's frame is of a page the insert wrote
	require.Less(t, distinctPages(pages, 0, hdr.mxFrame), uint64(hdr.mxFrame))
	require.Equal(t, uint64(hdr.mxFrame)-1, distinctPages(pages, 0, hdr.mxFrame-1), "the insert wrote a page twice")
	// a range beyond the frames read counts each unknown frame as a page
	require.Equal(t, distinctPages(pages, 0, hdr.mxFrame)+3, distinctPages(pages, 0, hdr.mxFrame+3))
	require.Equal(t, uint64(2), distinctPages(pages[:1], 2, 4))
	require.Zero(t, distinctPages(pages, 5, 5))
	// reading (from, to] appends to what is given
	more, err := walFramePages(f.path+"-wal", hdr.pageSize, hdr.salt, 2, hdr.mxFrame, pages[:2])
	require.NoError(t, err)
	require.Equal(t, pages, more)
	// a salt of another generation is rejected
	var other [8]byte
	copy(other[:], hdr.salt[:])
	other[0]++
	_, err = walFramePages(f.path+"-wal", hdr.pageSize, other, 0, hdr.mxFrame, nil)
	require.ErrorContains(t, err, "another generation")
	// so is a frame beyond the file
	_, err = walFramePages(f.path+"-wal", hdr.pageSize, hdr.salt, hdr.mxFrame, hdr.mxFrame+1, nil)
	require.Error(t, err)
}

func TestIOStatsConcurrentCommits(t *testing.T) {
	partitiontest.PartitionTest(t)
	f := newWALEraseFixture(t)
	s := f.acc.stats
	c0 := s.counters()

	const writers, each = 4, 10
	errs := make(chan error, writers*each)
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				errs <- f.acc.Atomic(func(ctx context.Context, tx *sql.Tx) error {
					_, err := tx.Exec("INSERT INTO t (v) VALUES (x'00')")
					return err
				})
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	// all of them in one generation of the log: the total is exact even
	// though a transaction may have been observed by another's commit
	n := logFrames(t, f.acc)
	c1 := s.counters()
	require.Equal(t, c0.commits+writers*each, c1.commits)
	require.Equal(t, c0.txBytesCount+writers*each, c1.txBytesCount)
	require.Equal(t, c0.unobserved, c1.unobserved)
	require.Equal(t, c0.restarts, c1.restarts)
	require.Equal(t, n*testFrameSize, c1.walBytes-c0.walBytes)
	require.Equal(t, n*testFrameSize, c1.txBytesSum-c0.txBytesSum)
}

func TestIOStatsRegistry(t *testing.T) {
	partitiontest.PartitionTest(t)
	path := filepath.Join(t.TempDir(), "refs.sqlite")
	lookup := func() *ioStats {
		ioRegistry.mu.Lock()
		defer ioRegistry.mu.Unlock()
		return ioRegistry.stats[path]
	}
	refs := func() int {
		ioRegistry.mu.Lock()
		defer ioRegistry.mu.Unlock()
		return ioRegistry.stats[path].refs
	}

	p, err := OpenPair(path, false)
	require.NoError(t, err)
	require.Same(t, p.Rdb.stats, lookup())
	require.Equal(t, 2, refs())
	p.Rdb.Close()
	require.Equal(t, 1, refs())
	p.Wdb.Close()
	require.Nil(t, lookup())
	require.Nil(t, p.Wdb.stats)
}

func walIndexBytes(change uint32, szPage uint16, mxFrame uint32, salt [8]byte, backfill uint32) []byte {
	buf := make([]byte, walIndexReadSize)
	ne := binary.NativeEndian
	ne.PutUint32(buf[0:4], walIndexVersion)
	ne.PutUint32(buf[8:12], change)
	buf[12] = 1
	ne.PutUint16(buf[14:16], szPage)
	ne.PutUint32(buf[16:20], mxFrame)
	copy(buf[32:40], salt[:])
	copy(buf[walIndexHeaderSize:2*walIndexHeaderSize], buf[:walIndexHeaderSize])
	ne.PutUint32(buf[96:100], backfill)
	return buf
}

func TestDecodeWALIndex(t *testing.T) {
	partitiontest.PartitionTest(t)
	salt := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}

	hdr, err := decodeWALIndex(walIndexBytes(7, 4096, 12, salt, 5))
	require.NoError(t, err)
	require.Equal(t, walIndexHeader{change: 7, pageSize: 4096, mxFrame: 12, salt: salt, backfill: 5}, hdr)

	hdr, err = decodeWALIndex(walIndexBytes(1, 1, 0, salt, 0)) // 1 stands for 65536
	require.NoError(t, err)
	require.Equal(t, uint32(65536), hdr.pageSize)

	torn := walIndexBytes(7, 4096, 12, salt, 5)
	torn[walIndexHeaderSize+16]++
	_, err = decodeWALIndex(torn)
	require.ErrorIs(t, err, errWALIndexTorn)

	wrongVersion := walIndexBytes(7, 4096, 12, salt, 5)
	binary.NativeEndian.PutUint32(wrongVersion[0:4], walIndexVersion+1)
	copy(wrongVersion[walIndexHeaderSize:], wrongVersion[:walIndexHeaderSize])
	_, err = decodeWALIndex(wrongVersion)
	require.ErrorContains(t, err, "version")

	uninitialized := walIndexBytes(7, 4096, 12, salt, 5)
	uninitialized[12] = 0
	copy(uninitialized[walIndexHeaderSize:], uninitialized[:walIndexHeaderSize])
	_, err = decodeWALIndex(uninitialized)
	require.ErrorContains(t, err, "not initialized")

	_, err = decodeWALIndex(walIndexBytes(7, 4096, 12, salt, 5)[:walIndexReadSize-1])
	require.ErrorContains(t, err, "shorter")
}

func TestIOMetricsOutput(t *testing.T) {
	partitiontest.PartitionTest(t)
	f := newWALEraseFixture(t)
	f.insert(1)

	var buf strings.Builder
	IOMetrics.WriteMetric(&buf, `host="h"`)
	out := buf.String()
	require.Contains(t, out, "# TYPE algod_db_commits_total counter\n")
	require.Regexp(t, `algod_db_commits_total\{db="erase.sqlite",host="h"\} [1-9]`, out)
	require.Regexp(t, `algod_db_wal_bytes_total\{db="erase.sqlite",host="h"\} [1-9]`, out)
	require.Contains(t, out, "# TYPE algod_db_tx_bytes histogram\n")
	require.Regexp(t, `algod_db_tx_bytes_bucket\{db="erase.sqlite",le="8192",host="h"\} [0-9]+\n`, out)
	require.Regexp(t, `algod_db_tx_bytes_bucket\{db="erase.sqlite",le="\+Inf",host="h"\} ([0-9]+)\n`, out)
	require.Regexp(t, `algod_db_tx_bytes_sum\{db="erase.sqlite",host="h"\} [1-9]`, out)
	require.Regexp(t, `algod_db_tx_bytes_count\{db="erase.sqlite",host="h"\} [1-9]`, out)
	require.Contains(t, out, "# TYPE algod_db_file_bytes gauge\n")
	require.Regexp(t, `algod_db_file_bytes\{db="erase.sqlite",file="main",host="h"\} [1-9]`, out)
	require.Regexp(t, `algod_db_file_bytes\{db="erase.sqlite",file="wal",host="h"\} [1-9]`, out)
	// the +Inf bucket is the count
	c := f.acc.stats.counters()
	require.Contains(t, out, `algod_db_tx_bytes_bucket{db="erase.sqlite",le="+Inf",host="h"} `+strconv.FormatUint(c.txBytesCount, 10)+"\n")

	values := make(map[string]float64)
	IOMetrics.AddMetric(values)
	require.Equal(t, float64(c.commits), values["algod_db_commits_total_db__erase_sqlite_"])
	require.Equal(t, float64(c.txBytesCount), values["algod_db_tx_bytes_count_db__erase_sqlite_"])
}
