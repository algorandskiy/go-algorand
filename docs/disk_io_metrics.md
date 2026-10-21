# Disk write accounting

`algod` exposes, on the REST API's `/metrics` endpoint (Prometheus exposition
format, no token needed), counters that attribute its disk writes to each
writer. They are always on; the cost is one small read of a memory-mapped
file per committed database transaction.

## What writes to disk on a participation node

Every SQLite store of the node (`ledger.block.sqlite`, `ledger.tracker.sqlite`,
`crash.sqlite`, `partregistry.sqlite`, `stateproof.sqlite`, and `.partkey`
files during the run that imports them) runs in write-ahead log mode with
`synchronous=FULL`. A transaction appends the new image of every page it
changes to the log, as a frame of the page size plus 24 bytes, and a later
checkpoint copies those pages into the database file. So a committed page
is written about twice, and every commit syncs the log. Besides the stores,
the node appends to `node.log`, one write per record, and SQLite may spill
sort results to temporary files in the system temp directory.

## Metrics

Per database file, labeled `db="<file name>"` (counters unless noted):

| Metric | Meaning |
|---|---|
| `algod_db_commits_total` | transactions committed through `db.Accessor` |
| `algod_db_rollbacks_total` | transactions rolled back |
| `algod_db_unobserved_commits_total` | transactions committed outside `Accessor.Atomic` (seen through the log's change counter); expect 0 on a participation node |
| `algod_db_tx_micros_total` | microseconds from begin to commit |
| `algod_db_wal_bytes_total` | bytes appended to the write-ahead log by committed transactions |
| `algod_db_backfill_bytes_total` | bytes of pages checkpoints copied from the log into the database file, each distinct page once per checkpoint |
| `algod_db_wal_erase_bytes_total` | bytes of zero pages the participation registry's `EraseWAL` wrote and checkpointed |
| `algod_db_wal_restarts_total` | times the log restarted from its beginning |
| `algod_db_observe_errors_total` | commits whose log index or frame headers could not be read; expect 0 |
| `algod_db_tx_bytes` (histogram) | log bytes per committed transaction, buckets 8 KiB to 8 MiB; approximate when transactions commit concurrently |
| `algod_db_file_bytes{file="main"\|"wal"}` (gauge) | size of the database file and of its log |

Other writers:

| Metric | Meaning |
|---|---|
| `algod_log_bytes_total` | bytes written to `node.log` |
| `algod_log_entries_total{level}` | log records written, by level |
| `algod_agreement_persist_total`, `algod_agreement_persist_bytes_total` | snapshots of the agreement state written to `crash.sqlite`, and their encoded size |
| `algod_ledger_blockput_bytes_total{part="hdr"\|"blk"\|"cert"}` | encoded bytes stored per block in `ledger.block.sqlite` |
| `algod_process_wchar_bytes_total`, `algod_process_write_bytes_total`, `algod_process_cancelled_write_bytes_total` | the process's totals from `/proc/self/io` (Linux only): bytes passed to write calls, bytes sent to the block layer, bytes truncated before reaching it |

The frames counted in `algod_db_wal_bytes_total` come from SQLite's WAL
index (the `-shm` file): the number of frames in the log, the salts that
change when the log restarts, the transaction counter and the backfill
position are read after each commit and diffed against the previous read.
A checkpoint copies each distinct page among the frames it consumes once,
however many frames hold it (a store that rewrites one row on every
transaction checkpoints a few pages for thousands of frames), so the page
number of every frame is read from its header in the log file as frames
are appended, and `algod_db_backfill_bytes_total` counts the distinct pages
among the frames each checkpoint consumed. Frames a transaction spilled to
the log before rolling back are not counted.

## Reading the numbers

Bytes per second written on behalf of each store (log plus checkpoint plus
erase):

```
sum by (db) (rate(algod_db_wal_bytes_total[5m]) + rate(algod_db_backfill_bytes_total[5m]) + rate(algod_db_wal_erase_bytes_total[5m]))
```

Bytes per round per store (`algod_ledger_round` is a gauge):

```
sum by (db) (increase(algod_db_wal_bytes_total[30m]) + increase(algod_db_backfill_bytes_total[30m]) + increase(algod_db_wal_erase_bytes_total[30m]))
  / scalar(delta(algod_ledger_round[30m]))
```

The log, and how much of it is which level:

```
rate(algod_log_bytes_total[5m])
sum by (level) (rate(algod_log_entries_total[5m]))
```

How much of the crash database's traffic is the agreement snapshot itself,
as opposed to b-tree and log overhead:

```
rate(algod_agreement_persist_bytes_total[5m]) / rate(algod_db_wal_bytes_total{db="crash.sqlite"}[5m])
```

What the per-file counters do not explain (Linux): temporary files, the
`-shm` files and anything else:

```
rate(algod_process_wchar_bytes_total[5m])
  - sum(rate(algod_db_wal_bytes_total[5m]) + rate(algod_db_backfill_bytes_total[5m]) + rate(algod_db_wal_erase_bytes_total[5m]))
  - rate(algod_log_bytes_total[5m])
```

Transactions larger than 512 KiB in the last hour, by store (the bucket
bound is matched by regex because Prometheus 3 formats `le` values as
floats, `524288.0`):

```
increase(algod_db_tx_bytes_count[1h]) - ignoring (le) increase(algod_db_tx_bytes_bucket{le=~"524288(\\.0)?"}[1h])
```

Without Prometheus, two snapshots of `/metrics` some minutes apart, diffed
and divided by the change in `algod_ledger_round`, give the same table.

## Grafana dashboard

`docs/dashboards/algod-disk-writes.json` is a dashboard with all of the
above: an overview of bytes per round by writer, the per-store detail
(log, checkpoint and erase components, commits, transaction sizes and
bursts, restarts, durations, file sizes), and the log, agreement, block and
process panels. Import it in Grafana with *Dashboards > New > Import*,
upload or paste the file, and pick the Prometheus data source that scrapes
the node. The *Node* variable selects an `instance`, the *Store* variable
filters the `db` label, and *Rate window* is the range of the `rate()`
queries: it must be at least twice the scrape interval (the default, 5m,
suits scrape intervals up to 2m). The per-round and per-range figures use
the dashboard's time range instead.

Prometheus scrapes the node's REST address (`algod.net` in the data
directory) at `/metrics`; the endpoint needs no token:

```yaml
scrape_configs:
  - job_name: algod
    scrape_interval: 15s
    static_configs:
      - targets: ["127.0.0.1:8080"]
```

The dashboard also works with the metrics pushed through `node_exporter`
(`EnableMetricReporting`), where `instance` is the exporter's.
