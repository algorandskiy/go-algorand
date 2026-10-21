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
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/algorand/go-deadlock"

	"github.com/algorand/go-algorand/util/metrics"
)

// Write accounting of the SQLite stores.
//
// Every page a transaction modifies is appended to the write-ahead log as a
// frame (a 24-byte header and the page image), and a checkpoint later copies
// the frames into the database file, so a committed page costs about two
// page writes.  SQLite keeps the state of the log in the WAL index, the
// memory-mapped <database>-shm file (https://www.sqlite.org/walformat.html,
// "WAL-Index Format"): the number of frames in the log (mxFrame), the salts
// that identify the current generation of the log (they change whenever the
// log restarts from its beginning), a counter of committed transactions
// (iChange), and the number of frames a checkpoint has copied into the
// database file (nBackfill).  The writer publishes the header before it
// releases the write lock, and an automatic checkpoint runs before COMMIT
// returns, so reading the header right after a commit sees that commit and
// any checkpoint it triggered.
//
// One ioStats per database file reads the header after every transaction
// AtomicContext commits and after each step of EraseWAL, and turns the
// differences into counters: frames the transaction appended, pages a
// checkpoint copied since the previous observation, log restarts, and
// commits nobody observed (iChange advanced by more than one: a write
// outside Atomic, such as Vacuum).  A checkpoint copies each distinct page
// among the frames it consumes once, its newest image, however many frames
// hold the page: a store that rewrites the same row on every transaction
// checkpoints a few pages for thousands of frames.  So the page number of
// every frame is read from the frame's header in the log file as the frames
// are appended, and a checkpoint is charged with the distinct pages among
// the frames it consumed.  Frames a transaction spilled to the log before
// rolling back are the one thing not counted: they are in the file but
// never in mxFrame.  IOMetrics reports the counters of every open file.
const (
	walIndexHeaderSize = 48  // one copy of the header
	walIndexReadSize   = 100 // both copies and the nBackfill field after them
	walIndexVersion    = 3007000
)

// walIndexHeader is the part of the WAL index header the accounting uses.
type walIndexHeader struct {
	change   uint32  // iChange: committed transactions, never reset while the index exists
	pageSize uint32  // szPage
	mxFrame  uint32  // frames in the log
	salt     [8]byte // aSalt: the generation of the log
	backfill uint32  // nBackfill: frames copied into the database file
}

var errWALIndexTorn = errors.New("the two copies of the write-ahead log index header differ")

// decodeWALIndex decodes the header of a WAL index.  The two copies of the
// header must match: the writer updates them one after the other.
func decodeWALIndex(buf []byte) (hdr walIndexHeader, err error) {
	if len(buf) < walIndexReadSize {
		return hdr, fmt.Errorf("write-ahead log index is %d bytes, shorter than its header", len(buf))
	}
	if !bytes.Equal(buf[:walIndexHeaderSize], buf[walIndexHeaderSize:2*walIndexHeaderSize]) {
		return hdr, errWALIndexTorn
	}
	ne := binary.NativeEndian // the index is memory-mapped, so it is in the host's byte order
	if v := ne.Uint32(buf[0:4]); v != walIndexVersion {
		return hdr, fmt.Errorf("write-ahead log index version %d, expected %d", v, walIndexVersion)
	}
	if buf[12] != 1 {
		return hdr, errors.New("write-ahead log index is not initialized")
	}
	hdr.change = ne.Uint32(buf[8:12])
	szPage := uint32(ne.Uint16(buf[14:16]))
	hdr.pageSize = (szPage & 0xfe00) + ((szPage & 1) << 16) // 1 stands for 65536
	hdr.mxFrame = ne.Uint32(buf[16:20])
	copy(hdr.salt[:], buf[32:40])
	hdr.backfill = ne.Uint32(buf[96:100])
	return hdr, nil
}

// readWALIndex reads the header of the WAL index at path.  A read that lands
// between the writer's updates of the two copies is retried.
func readWALIndex(path string) (hdr walIndexHeader, err error) {
	f, err := os.Open(path)
	if err != nil {
		return hdr, err
	}
	defer f.Close()
	var buf [walIndexReadSize]byte
	for attempt := 0; ; attempt++ {
		n, readErr := f.ReadAt(buf[:], 0)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return hdr, readErr
		}
		hdr, err = decodeWALIndex(buf[:n])
		if !errors.Is(err, errWALIndexTorn) || attempt == 3 {
			return hdr, err
		}
	}
}

// walFramePages reads the page numbers of frames (from, to] of the
// write-ahead log at path, whose frames hold pages of pageSize bytes and
// carry salt, and appends them to pages, one per frame.  A frame whose salt
// differs belongs to another generation of the log: the log restarted since
// the WAL index was read.
func walFramePages(path string, pageSize uint32, salt [8]byte, from, to uint32, pages []uint32) ([]uint32, error) {
	if to <= from {
		return pages, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return pages, err
	}
	defer f.Close()
	frameSize := int64(walFrameHeaderSize) + int64(pageSize)
	var hdr [16]byte // page number, commit size, salts
	for i := from; i < to; i++ {
		if _, err := f.ReadAt(hdr[:], walHeaderSize+int64(i)*frameSize); err != nil {
			return pages, fmt.Errorf("frame %d of %s: %w", i+1, path, err)
		}
		if [8]byte(hdr[8:16]) != salt {
			return pages, fmt.Errorf("frame %d of %s belongs to another generation of the log", i+1, path)
		}
		pages = append(pages, binary.BigEndian.Uint32(hdr[0:4]))
	}
	return pages, nil
}

// distinctPages counts the distinct pages among frames (from, to] of a log
// whose frames' pages are known up to len(pages); a frame beyond that counts
// as a page of its own.
func distinctPages(pages []uint32, from, to uint32) uint64 {
	if to <= from {
		return 0
	}
	known := min(to, uint32(len(pages)))
	var n uint64
	if from < known {
		seen := make(map[uint32]struct{}, known-from)
		for _, p := range pages[from:known] {
			seen[p] = struct{}{}
		}
		n = uint64(len(seen))
	}
	if to > known {
		n += uint64(to - max(from, known))
	}
	return n
}

// txBytesBuckets are the upper bounds of the histogram of write-ahead log
// bytes per transaction; one frame of a 4 KiB page is 4120 bytes.
var txBytesBuckets = [...]uint64{8 << 10, 32 << 10, 128 << 10, 512 << 10, 2 << 20, 8 << 20}

// ioCounters are the counters of the write accounting of one file.
type ioCounters struct {
	commits       uint64
	rollbacks     uint64
	unobserved    uint64 // commits that bypassed AtomicContext
	restarts      uint64
	observeErrors uint64
	txMicros      uint64
	walBytes      uint64 // frames appended by committed transactions
	backfillBytes uint64 // distinct pages checkpoints copied into the database file
	eraseBytes    uint64 // zero pages EraseWAL wrote and checkpointed
	txBuckets     [len(txBytesBuckets) + 1]uint64
	txBytesSum    uint64
	txBytesCount  uint64
}

// ioStats is the write accounting of one database file, shared by every
// Accessor open on it.
type ioStats struct {
	path string
	name string // the label of the file in the metrics: its base name
	refs int    // accessors open on the file; guarded by ioRegistry.mu

	mu     deadlock.Mutex
	seeded bool // last holds an observation to diff against
	last   walIndexHeader
	// pages holds the page number of each frame of the current generation of
	// the log, frame i at index i-1, as far as the frames have been read
	pages []uint32
	ioCounters
}

// ioRegistry holds the accounting of every open database file.
var ioRegistry = struct {
	mu    deadlock.Mutex
	stats map[string]*ioStats
}{stats: make(map[string]*ioStats)}

// acquireIOStats returns the accounting of the database file at path, which
// stays registered until every accessor that acquired it is closed.
func acquireIOStats(path string) *ioStats {
	ioRegistry.mu.Lock()
	defer ioRegistry.mu.Unlock()
	s := ioRegistry.stats[path]
	if s == nil {
		s = &ioStats{path: path, name: filepath.Base(path)}
		// A file with a log from before has its state recorded now, so that
		// the first commit is not charged with it.  A file without one yet is
		// seeded by its first commit.
		if hdr, err := readWALIndex(path + "-shm"); err == nil {
			s.seed(hdr)
		}
		ioRegistry.stats[path] = s
	}
	s.refs++
	return s
}

// release drops one accessor's reference; the last one unregisters the file.
func (s *ioStats) release() {
	if s == nil {
		return
	}
	ioRegistry.mu.Lock()
	defer ioRegistry.mu.Unlock()
	s.refs--
	if s.refs <= 0 {
		delete(ioRegistry.stats, s.path)
	}
}

type observation uint8

const (
	observedCommit     observation = iota // a transaction AtomicContext committed
	observedCheckpoint                    // a checkpoint of EraseWAL: backfill only
	observedErase                         // zero pages of EraseWAL: frames and backfill
)

func (s *ioStats) observeCommit(started time.Time) {
	if s == nil {
		return
	}
	s.observe(observedCommit, uint64(time.Since(started).Microseconds()))
}

func (s *ioStats) observeCheckpoint() { s.observe(observedCheckpoint, 0) }

func (s *ioStats) observeErase() { s.observe(observedErase, 0) }

func (s *ioStats) rolledBack() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.rollbacks++
	s.mu.Unlock()
}

// observe reads the WAL index and charges what changed since the previous
// observation to the counters of kind.  The read and the update happen
// under the mutex so that transactions committing concurrently on the file
// are counted exactly once in total, each charged to whichever of them
// observes first.
func (s *ioStats) observe(kind observation, micros uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind == observedCommit {
		s.commits++
		s.txMicros += micros
	}
	hdr, err := readWALIndex(s.path + "-shm")
	if err != nil {
		s.observeErrors++
		return
	}
	if !s.seeded {
		s.seed(hdr)
		return
	}

	var frames, backfill uint64
	restarted := hdr.salt != s.last.salt || hdr.mxFrame < s.last.mxFrame
	if restarted {
		// the log restarted, after a checkpoint had copied all of it
		frames = uint64(hdr.mxFrame)
		backfill = distinctPages(s.pages, s.last.backfill, s.last.mxFrame)
		s.pages = s.pages[:0]
		s.restarts++
	} else {
		frames = uint64(hdr.mxFrame - s.last.mxFrame)
	}
	s.readPages(hdr)
	if restarted {
		backfill += distinctPages(s.pages, 0, hdr.backfill)
	} else {
		backfill = distinctPages(s.pages, s.last.backfill, hdr.backfill)
	}
	changes := hdr.change - s.last.change
	s.last = hdr

	pageSize := uint64(hdr.pageSize)
	frameBytes := frames * (pageSize + walFrameHeaderSize)
	backfillBytes := backfill * pageSize
	switch kind {
	case observedCommit:
		if changes > 1 {
			s.unobserved += uint64(changes - 1)
		}
		s.walBytes += frameBytes
		s.backfillBytes += backfillBytes
		s.txBytesSum += frameBytes
		s.txBytesCount++
		s.txBuckets[txBytesBucket(frameBytes)]++
	case observedCheckpoint:
		s.walBytes += frameBytes // none expected: a checkpoint appends nothing
		s.backfillBytes += backfillBytes
	case observedErase:
		s.eraseBytes += frameBytes + backfillBytes
	}
}

// seed records the first observation of the log, which nothing is charged
// with.
func (s *ioStats) seed(hdr walIndexHeader) {
	s.last, s.seeded = hdr, true
	s.pages = s.pages[:0]
	s.readPages(hdr)
}

// readPages reads the pages of the frames appended to the log since its
// pages were last read.  A frame it cannot read counts as a page of its own
// when checkpointed; the next observation reads it again, unless the log
// has restarted by then.
func (s *ioStats) readPages(hdr walIndexHeader) {
	if uint32(len(s.pages)) >= hdr.mxFrame {
		return
	}
	var err error
	s.pages, err = walFramePages(s.path+"-wal", hdr.pageSize, hdr.salt, uint32(len(s.pages)), hdr.mxFrame, s.pages)
	if err != nil {
		s.observeErrors++
	}
}

// counters returns a copy of the counters.
func (s *ioStats) counters() ioCounters {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ioCounters
}

func txBytesBucket(n uint64) int {
	for i, le := range txBytesBuckets {
		if n <= le {
			return i
		}
	}
	return len(txBytesBuckets)
}

// ioSample is a copy of the counters of one file, with the sizes of the
// file and its log at the time of the sample.
type ioSample struct {
	name  string
	stats ioCounters
	file  uint64
	wal   uint64
}

// sampleIOStats copies the accounting of every open file, ordered by name.
// Files sharing a base name (tests, mostly) are told apart by their
// directory.
func sampleIOStats() []ioSample {
	ioRegistry.mu.Lock()
	all := make([]*ioStats, 0, len(ioRegistry.stats))
	for _, s := range ioRegistry.stats {
		all = append(all, s)
	}
	ioRegistry.mu.Unlock()

	byName := make(map[string]int, len(all))
	for _, s := range all {
		byName[s.name]++
	}
	samples := make([]ioSample, 0, len(all))
	for _, s := range all {
		smp := ioSample{name: s.name}
		if byName[s.name] > 1 {
			smp.name = filepath.Join(filepath.Base(filepath.Dir(s.path)), s.name)
		}
		smp.stats = s.counters()
		smp.file = fileSize(s.path)
		smp.wal = fileSize(s.path + "-wal")
		samples = append(samples, smp)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].name < samples[j].name })
	return samples
}

func fileSize(path string) uint64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return uint64(info.Size())
}

// IOMetrics reports the write accounting of every open database file in
// the Prometheus exposition format, one series per file labeled db="<base
// name>"; register it with metrics.DefaultRegistry().
var IOMetrics metrics.Metric = ioGatherer{}

type ioGatherer struct{}

// ioCounterFamilies are the counter families IOMetrics reports, with the
// counter of a sample each one reads.
var ioCounterFamilies = []struct {
	name  metrics.MetricName
	value func(*ioCounters) uint64
}{
	{metrics.DBCommitsTotal, func(c *ioCounters) uint64 { return c.commits }},
	{metrics.DBRollbacksTotal, func(c *ioCounters) uint64 { return c.rollbacks }},
	{metrics.DBUnobservedCommitsTotal, func(c *ioCounters) uint64 { return c.unobserved }},
	{metrics.DBTxMicrosTotal, func(c *ioCounters) uint64 { return c.txMicros }},
	{metrics.DBWALBytesTotal, func(c *ioCounters) uint64 { return c.walBytes }},
	{metrics.DBBackfillBytesTotal, func(c *ioCounters) uint64 { return c.backfillBytes }},
	{metrics.DBWALEraseBytesTotal, func(c *ioCounters) uint64 { return c.eraseBytes }},
	{metrics.DBWALRestartsTotal, func(c *ioCounters) uint64 { return c.restarts }},
	{metrics.DBObserveErrorsTotal, func(c *ioCounters) uint64 { return c.observeErrors }},
}

// WriteMetric implements metrics.Metric.
func (ioGatherer) WriteMetric(buf *strings.Builder, parentLabels string) {
	samples := sampleIOStats()
	if len(samples) == 0 {
		return
	}
	labels := func(smp ioSample, extra string) string {
		l := `db="` + escapeLabelValue(smp.name) + `"`
		if extra != "" {
			l += "," + extra
		}
		if parentLabels != "" {
			l += "," + parentLabels
		}
		return l
	}
	line := func(name, labels string, value uint64) {
		buf.WriteString(name)
		buf.WriteString("{")
		buf.WriteString(labels)
		buf.WriteString("} ")
		buf.WriteString(strconv.FormatUint(value, 10))
		buf.WriteString("\n")
	}

	for _, c := range ioCounterFamilies {
		writeMetricHeader(buf, c.name, "counter")
		for i := range samples {
			line(c.name.Name, labels(samples[i], ""), c.value(&samples[i].stats))
		}
	}

	writeMetricHeader(buf, metrics.DBTxBytes, "histogram")
	for _, smp := range samples {
		var cumulative uint64
		for i, le := range txBytesBuckets {
			cumulative += smp.stats.txBuckets[i]
			line(metrics.DBTxBytes.Name+"_bucket", labels(smp, `le="`+strconv.FormatUint(le, 10)+`"`), cumulative)
		}
		cumulative += smp.stats.txBuckets[len(txBytesBuckets)]
		line(metrics.DBTxBytes.Name+"_bucket", labels(smp, `le="+Inf"`), cumulative)
		line(metrics.DBTxBytes.Name+"_sum", labels(smp, ""), smp.stats.txBytesSum)
		line(metrics.DBTxBytes.Name+"_count", labels(smp, ""), smp.stats.txBytesCount)
	}

	writeMetricHeader(buf, metrics.DBFileBytes, "gauge")
	for _, smp := range samples {
		line(metrics.DBFileBytes.Name, labels(smp, `file="main"`), smp.file)
		line(metrics.DBFileBytes.Name, labels(smp, `file="wal"`), smp.wal)
	}
}

// AddMetric implements metrics.Metric: the counters and the histogram's sum
// and count, keyed like a labeled counter is for telemetry.
func (ioGatherer) AddMetric(values map[string]float64) {
	samples := sampleIOStats()
	for i := range samples {
		smp := &samples[i]
		key := func(name string) string {
			return metrics.SanitizeTelemetryName(name + `:db="` + smp.name + `"`)
		}
		for _, c := range ioCounterFamilies {
			values[key(c.name.Name)] = float64(c.value(&smp.stats))
		}
		values[key(metrics.DBTxBytes.Name+"_sum")] = float64(smp.stats.txBytesSum)
		values[key(metrics.DBTxBytes.Name+"_count")] = float64(smp.stats.txBytesCount)
	}
}

func writeMetricHeader(buf *strings.Builder, name metrics.MetricName, kind string) {
	buf.WriteString("# HELP ")
	buf.WriteString(name.Name)
	buf.WriteString(" ")
	buf.WriteString(name.Description)
	buf.WriteString("\n# TYPE ")
	buf.WriteString(name.Name)
	buf.WriteString(" ")
	buf.WriteString(kind)
	buf.WriteString("\n")
}

var labelValueEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func escapeLabelValue(v string) string {
	return labelValueEscaper.Replace(v)
}
