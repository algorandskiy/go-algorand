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

package main

import (
	"database/sql"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/algorand/go-algorand/data/bookkeeping"
	"github.com/algorand/go-algorand/protocol"
)

var blockDBfile = flag.String("blockdb", "", "Block DB filename")
var numBlocks = flag.Int("numblocks", 10000, "Randomly sample this many blocks for training")
var startRound = flag.Int("start", 0, "Sample blocks starting at this round (inclusive)")
var endRound = flag.Int("end", 0, "Sample blocks ending at this round (inclusive)")
var outDir = flag.String("outdir", ".", "Write blocks to this directory")
var randSeed = flag.Int("seed", 0, "Random seed, otherwise will use time")

func getBlockToFile(db *sql.DB, rnd int64) error {
	var buf []byte
	err := db.QueryRow("SELECT blkdata FROM blocks WHERE rnd=?", rnd).Scan(&buf)
	if err != nil {
		return err
	}
	return os.WriteFile(fmt.Sprintf("%s/%d.block", *outDir, rnd), buf, 0644)
}

func usage() {
	flag.Usage()
	os.Exit(1)
}

func dumpBlocks(db *sql.DB, rnd, spLookback, deltas int64) {
	var buf []byte
	err := db.QueryRow("SELECT hdrdata FROM blocks WHERE rnd=?", rnd).Scan(&buf)
	if err != nil {
		fmt.Println("Error getting block", rnd, err)
		return
	}
	var hdr bookkeeping.BlockHeader
	err = protocol.Decode(buf, &hdr)
	if err != nil {
		fmt.Println("Error decoding block", rnd, err)
		return
	}
	var votersRnd = rnd + spLookback + deltas
	if hdr.ParticipationUpdates.ExpiredParticipationAccounts != nil {
		fmt.Printf("expired at rnd %d = %d - (%d + %d)\n", rnd, votersRnd, spLookback, deltas)
		for _, addr := range hdr.ParticipationUpdates.ExpiredParticipationAccounts {
			fmt.Printf("%s\n", addr.String())
		}
	}
}

func main() {
	flag.Parse()
	if *blockDBfile == "" {
		fmt.Println("-blockdb=file required")
		usage()
	}
	uri := fmt.Sprintf("file:%s?_journal_mode=wal", *blockDBfile)
	fmt.Println("Opening", uri)
	db, err := sql.Open("sqlite3", uri)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		panic(err)
	}

	seed := int64(*randSeed)
	if seed == 0 {
		seed = time.Now().UnixMicro()
	}
	rand.Seed(seed)

	var minRound, maxRound int64
	if *startRound != 0 {
		minRound = int64(*startRound)
	}
	if *endRound != 0 {
		maxRound = int64(*endRound)
	}
	if maxRound == 0 {
		err = db.QueryRow("SELECT MAX(rnd) FROM blocks").Scan(&maxRound)
		if err != nil {
			panic(err)
		}
	}
	if minRound == 0 {
		err := db.QueryRow("SELECT MIN(rnd) FROM blocks").Scan(&minRound)
		if err != nil {
			panic(err)
		}
	}

	// just analyze all blocks from minRound to maxRound
	const spLookback = 16
	const maxDelta = 16
	fmt.Printf("Processing all blocks between round %d and %d\n", minRound, maxRound)
	for i := minRound; i <= maxRound; i++ {
		for d := int64(0); d <= maxDelta; d++ {
			if (i+spLookback+d)%256 == 0 {
				dumpBlocks(db, i, spLookback, d)
			}
		}
	}
}
