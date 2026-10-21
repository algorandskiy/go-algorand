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
package logging

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/test/partitiontest"
)

func TestEntryCountHook(t *testing.T) {
	partitiontest.PartitionTest(t)
	log := NewLogger()
	log.SetOutput(io.Discard)
	log.SetLevel(Info)
	log.AddHook(EntryCountHook())

	info := logEntryLabels[logrus.InfoLevel]
	debug := logEntryLabels[logrus.DebugLevel]
	warn := logEntryLabels[logrus.WarnLevel]
	infoBefore := logEntries.GetUint64ValueForLabels(info)
	debugBefore := logEntries.GetUint64ValueForLabels(debug)
	warnBefore := logEntries.GetUint64ValueForLabels(warn)

	log.Info("one")
	log.Infof("two %d", 2)
	log.Debug("not emitted at Info level")
	log.Warn("three")

	require.Equal(t, infoBefore+2, logEntries.GetUint64ValueForLabels(info))
	require.Equal(t, debugBefore, logEntries.GetUint64ValueForLabels(debug))
	require.Equal(t, warnBefore+1, logEntries.GetUint64ValueForLabels(warn))
}

func TestCyclicFileWriterCountsBytes(t *testing.T) {
	partitiontest.PartitionTest(t)
	dir := t.TempDir()
	w := MakeCyclicFileWriter(filepath.Join(dir, "node.log"), filepath.Join(dir, "node.archive.log"), 1<<20, 0)
	defer w.writer.Close()

	before := logBytesWritten.GetUint64Value()
	n, err := w.Write([]byte("hello\n"))
	require.NoError(t, err)
	require.Equal(t, 6, n)
	require.Equal(t, before+6, logBytesWritten.GetUint64Value())
}
