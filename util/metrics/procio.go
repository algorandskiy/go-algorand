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
package metrics

import (
	"fmt"
	"strconv"
	"strings"
)

// procIOStats are the write counters of the process from /proc/self/io.
type procIOStats struct {
	wchar               uint64 // bytes passed to write system calls
	writeBytes          uint64 // bytes sent to the block layer
	cancelledWriteBytes uint64 // bytes written and truncated before reaching it
}

// ProcessIOMetrics reports the process's write counters from /proc/self/io
// (Linux only; it reports nothing elsewhere), implementing the Metric
// interface.  The difference between the sum of the per-file counters and
// these totals is what the per-file accounting cannot see, such as SQLite's
// temporary files.
var ProcessIOMetrics = procIOGatherer{}

type procIOGatherer struct{}

var procIOCounters = []struct {
	name  MetricName
	value func(procIOStats) uint64
}{
	{ProcessWcharBytesTotal, func(s procIOStats) uint64 { return s.wchar }},
	{ProcessWriteBytesTotal, func(s procIOStats) uint64 { return s.writeBytes }},
	{ProcessCancelledWriteBytesTotal, func(s procIOStats) uint64 { return s.cancelledWriteBytes }},
}

// WriteMetric implements Metric.
func (procIOGatherer) WriteMetric(buf *strings.Builder, parentLabels string) {
	stats, err := getProcIOStats()
	if err != nil {
		return
	}
	for _, c := range procIOCounters {
		writeUint64MetricCounterHeader(buf, c.name.Name, c.name.Description)
		writeUint64MetricValue(buf, c.name.Name, parentLabels, c.value(stats))
	}
}

// AddMetric implements Metric.
func (procIOGatherer) AddMetric(values map[string]float64) {
	stats, err := getProcIOStats()
	if err != nil {
		return
	}
	for _, c := range procIOCounters {
		values[sanitizeTelemetryName(c.name.Name)] = float64(c.value(stats))
	}
}

// parseProcIO parses the contents of /proc/self/io: one "name: value" line
// per counter.
func parseProcIO(data []byte) (stats procIOStats, err error) {
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, perr := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		if perr != nil {
			return stats, fmt.Errorf("procio: cannot parse %q: %w", line, perr)
		}
		switch name {
		case "wchar":
			stats.wchar = n
		case "write_bytes":
			stats.writeBytes = n
		case "cancelled_write_bytes":
			stats.cancelledWriteBytes = n
		}
	}
	return stats, nil
}
