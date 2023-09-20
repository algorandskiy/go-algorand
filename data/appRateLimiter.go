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
	"sync/atomic"
	"time"

	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/transactions"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/util"
	"golang.org/x/crypto/blake2b"

	"github.com/algorand/go-deadlock"
)

const numBuckets = 128

type keyType [8]byte

// appRateLimiter implements a sliding window counter rate limiter for applications.
// It is a shared map with numBuckets of maps each protected by its own mutex.
// Bucket is selected by hashing the application index with a seed (see memhash64).
type appRateLimiter struct {
	maxBucketSize        int
	serviceRatePerWindow uint64
	serviceRateWindow    time.Duration

	// seed for hashing application index to bucket
	seed uint64
	// salt for hashing application index + origin address
	salt [16]byte

	buckets [numBuckets]map[keyType]*appRateLimiterEntry
	mus     [numBuckets]deadlock.RWMutex
	lrus    [numBuckets]*util.List[keyType]

	// evictions
	// TODO: delete
	evictions    uint64
	evictionTime uint64
}

type appRateLimiterEntry struct {
	prev       atomic.Uint64
	cur        atomic.Uint64
	interval   atomic.Int64 // numeric representation of the current interval value
	lruElement *util.ListNode[keyType]
}

// makeAppRateLimiter creates a new appRateLimiter from the parameters:
// maxCacheSize is the maximum number of entries to keep in the cache to keep it memory bounded
// maxAppPeerRate is the maximum number of admitted apps per peer per second
// serviceRateWindow is the service window
func makeAppRateLimiter(maxCacheSize int, maxAppPeerRate uint64, serviceRateWindow time.Duration) *appRateLimiter {
	// convert target per app rate to per window service rate
	serviceRatePerWindow := maxAppPeerRate * uint64(serviceRateWindow/time.Second)
	maxBucketSize := maxCacheSize / numBuckets
	if maxBucketSize == 0 {
		// got the max size less then buckets, use maps of 1
		maxBucketSize = 1
	}
	r := &appRateLimiter{
		maxBucketSize:        maxBucketSize,
		serviceRatePerWindow: serviceRatePerWindow,
		serviceRateWindow:    serviceRateWindow,
		seed:                 crypto.RandUint64(),
	}
	crypto.RandBytes(r.salt[:])

	for i := 0; i < numBuckets; i++ {
		r.buckets[i] = make(map[keyType]*appRateLimiterEntry)
		r.lrus[i] = util.NewList[keyType]().AllocateFreeNodes(maxBucketSize)
	}
	return r
}

// newEntryLocked adds a new entry, must be called with appropriate bucket lock held
func (r *appRateLimiter) newEntryLocked(b int, key keyType, curInt int64) {
	if len(r.buckets[b]) >= r.maxBucketSize {
		// evict the oldest entry
		start := time.Now()
		el := r.lrus[b].Back()
		delete(r.buckets[b], el.Value)
		r.lrus[b].Remove(el)

		// some stats for benchmarks/tests, remove?
		atomic.AddUint64(&r.evictions, 1)
		atomic.AddUint64(&r.evictionTime, uint64(time.Since(start)))
	}
	el := r.lrus[b].PushFront(key)
	entry := &appRateLimiterEntry{lruElement: el}
	entry.cur.Store(1)
	entry.interval.Store(curInt)
	r.buckets[b][key] = entry
}

// interval calculates the interval numeric representation based on the given time
func (r *appRateLimiter) interval(now time.Time) int64 {
	return now.UnixNano() / int64(r.serviceRateWindow)
}

// fraction calculates the fraction of the interval that is elapsed since the given time
func (r *appRateLimiter) fraction(now time.Time) float64 {
	return float64(now.UnixNano()%int64(r.serviceRateWindow)) / float64(r.serviceRateWindow)
}

// shouldDrop returns true if the given transaction group should be dropped based on the
// on the rate for the applications in the group: the entire group is dropped if a single application
// exceeds the rate.
func (r *appRateLimiter) shouldDrop(txgroup []transactions.SignedTxn, origin []byte) bool {
	return r.shouldDropInner(txgroup, origin, time.Now())
}

// shouldDropInner is the same as shouldDrop but accepts the current time as a parameter
// in order to make it testable
func (r *appRateLimiter) shouldDropInner(txgroup []transactions.SignedTxn, origin []byte, now time.Time) bool {
	buckets, keys := txgroupToKeys(txgroup, origin, r.seed, r.salt, numBuckets)
	if len(keys) == 0 {
		return false
	}
	return r.shouldDropKeys(buckets, keys, now)
}

func (r *appRateLimiter) shouldDropKeys(buckets []int, keys []keyType, now time.Time) bool {
	curInt := r.interval(now)

	for i := range keys {
		key := keys[i]
		b := buckets[i]
		r.mus[b].Lock()
		entry, has := r.buckets[b][key]
		if !has {
			// add a new entry and prune the oldest if needed
			r.newEntryLocked(b, key, curInt)
			r.mus[b].Unlock()
		} else {
			r.mus[b].Unlock()
			// optimistic lock-free rate check
			// expecting that buckets are large enough to avoid contention and frequent evictions

			interval := entry.interval.Load()
			// copy cur and prev values to check limits
			// apply limits only after admission
			cur := entry.cur.Load()
			prev := entry.prev.Load()
			thisInterval := true
			var newPrev uint64 = 0
			if interval != curInt {
				thisInterval = false
				if interval == curInt-1 {
					// there are continuous intervals, use the previous value
					newPrev = cur
				}
				prev = newPrev
				cur = 1
			} else {
				cur++
			}

			curFraction := r.fraction(now)
			rate := uint64(float64(prev)*(1-curFraction)) + cur

			if rate > r.serviceRatePerWindow {
				return true
			}

			// admitted, update the entry
			if thisInterval {
				entry.cur.Add(1)
			} else {
				entry.prev.Store(newPrev)
				entry.cur.Store(1)
				entry.interval.Store(curInt)
			}

			r.mus[b].Lock()
			el := entry.lruElement
			// it is possible that the was removed from the list while executing
			// the lock-free code above, so check again
			if el.Value == (keyType{}) {
				// moved to free list, add a new entry
				el := r.lrus[b].PushFront(key)
				entry.lruElement = el
			} else if el.Value != key {
				// this means the list element was reused for something else
				// well, admitted, ignore
			} else {
				// normal case, move to front
				r.lrus[b].MoveToFront(el)
			}
			r.mus[b].Unlock()
		}
	}

	return false
}

// txgroupToKeys converts txgroup data to keys
func txgroupToKeys(txgroup []transactions.SignedTxn, origin []byte, seed uint64, salt [16]byte, numBuckets int) ([]int, []keyType) {
	// there are max 16 * 8 = 128 apps (buckets, keys) per txgroup
	// TODO: consider sync.Pool

	var keys []keyType
	var buckets []int
	// since blake2 is a crypto hash function it seems OK to shrink 32 bytes digest down to 8.
	// Rationale: we expect thousands of apps sent from thousands of peers,
	// so required millions of unique pairs => 8 bytes should be enough.
	// The 16 bytes salt makes it harder to find collisions if an adversary attempts to censor
	// some app by finding a collision with some app and flood a network with such transactions:
	// h(app + relay_ip) = h(app2 + relay_ip).
	var buf [8 + 16 + 16]byte // uint64 + 16 bytes of salt + up to 16 bytes of address
	txnToDigest := func(appIdx basics.AppIndex) keyType {
		binary.LittleEndian.PutUint64(buf[:8], uint64(appIdx))
		copy(buf[8:], salt[:])
		copied := copy(buf[8+16:], origin)

		h := blake2b.Sum256(buf[:8+16+copied])
		var key keyType
		copy(key[:], h[:len(key)])
		return key
	}
	txnToBucket := func(appIdx basics.AppIndex) int {
		return int(memhash64(uint64(appIdx), seed) % uint64(numBuckets))
	}
	for i := range txgroup {
		if txgroup[i].Txn.Type == protocol.ApplicationCallTx {
			appIdx := txgroup[i].Txn.ApplicationID
			// hash appIdx into a bucket, do not use modulo since it could
			// assign two vanilla (and presumable, popular) apps to the same bucket.
			buckets = append(buckets, txnToBucket(appIdx))
			keys = append(keys, txnToDigest(appIdx))
			if len(txgroup[i].Txn.ForeignApps) > 0 {
				for _, appIdx := range txgroup[i].Txn.ForeignApps {
					buckets = append(buckets, txnToBucket(appIdx))
					keys = append(keys, txnToDigest(appIdx))
				}
			}
		}
	}
	return buckets, keys
}

const (
	// Constants for multiplication: four random odd 64-bit numbers.
	m1 = 16877499708836156737
	m2 = 2820277070424839065
	m3 = 9497967016996688599
	m4 = 15839092249703872147
)

// memhash64 is uint64 hash function from go runtime
// https://go-review.googlesource.com/c/go/+/59352/4/src/runtime/hash64.go#96
func memhash64(val uint64, seed uint64) uint64 {
	h := seed
	h ^= val
	h = rotl31(h*m1) * m2
	h ^= h >> 29
	h *= m3
	h ^= h >> 32
	return h
}

func rotl31(x uint64) uint64 {
	return (x << 31) | (x >> (64 - 31))
}
