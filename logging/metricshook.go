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
	"github.com/sirupsen/logrus"

	"github.com/algorand/go-algorand/util/metrics"
)

// logBytesWritten counts the bytes CyclicFileWriter wrote to the live log.
var logBytesWritten = metrics.MakeCounter(metrics.MetricName{Name: "algod_log_bytes_total", Description: "Bytes written to the log file"})

// logEntries counts the records the logger emitted, by level.
var logEntries = metrics.MakeCounter(metrics.MetricName{Name: "algod_log_entries_total", Description: "Log records emitted, by level"})

// logEntryLabels are the labels of logEntries, one set per level, so that
// counting a record allocates nothing.
var logEntryLabels = func() map[logrus.Level]map[string]string {
	labels := make(map[logrus.Level]map[string]string, len(logrus.AllLevels))
	for _, level := range logrus.AllLevels {
		labels[level] = map[string]string{"level": level.String()}
	}
	return labels
}()

// entryCountHook counts the records of the logger it is added to.
type entryCountHook struct{}

func (entryCountHook) Levels() []logrus.Level { return logrus.AllLevels }

func (entryCountHook) Fire(entry *logrus.Entry) error {
	logEntries.AddUint64(1, logEntryLabels[entry.Level])
	return nil
}

// EntryCountHook returns a hook that counts the records of the logger it is
// added to in algod_log_entries_total, by level.  Hooks fire only for the
// records the logger's level lets through, so the count is of records
// written.
func EntryCountHook() logrus.Hook {
	return entryCountHook{}
}
