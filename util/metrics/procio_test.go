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
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/test/partitiontest"
)

func TestParseProcIO(t *testing.T) {
	partitiontest.PartitionTest(t)
	const sample = "rchar: 1\nwchar: 2\nsyscr: 3\nsyscw: 4\nread_bytes: 5\nwrite_bytes: 6\ncancelled_write_bytes: 7\n"
	stats, err := parseProcIO([]byte(sample))
	require.NoError(t, err)
	require.Equal(t, procIOStats{wchar: 2, writeBytes: 6, cancelledWriteBytes: 7}, stats)

	_, err = parseProcIO([]byte("wchar: many\n"))
	require.Error(t, err)
}

func TestProcessIOMetrics(t *testing.T) {
	partitiontest.PartitionTest(t)
	var buf strings.Builder
	ProcessIOMetrics.WriteMetric(&buf, `host="h"`)
	values := make(map[string]float64)
	ProcessIOMetrics.AddMetric(values)
	if runtime.GOOS != "linux" {
		require.Empty(t, buf.String())
		require.Empty(t, values)
		return
	}
	require.Contains(t, buf.String(), "# TYPE algod_process_wchar_bytes_total counter\n")
	require.Regexp(t, `algod_process_write_bytes_total\{host="h"\} [0-9]+\n`, buf.String())
	require.Contains(t, values, "algod_process_wchar_bytes_total")
}
