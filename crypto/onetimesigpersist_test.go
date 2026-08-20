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

package crypto

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/test/partitiontest"
)

// reassemble runs a secrets through PersistentParts/FromParts and returns the result.
func reassemble(t *testing.T, s *OneTimeSignatureSecrets) *OneTimeSignatureSecrets {
	t.Helper()
	scalars, batches, offsets := s.PersistentParts()
	restored, err := OneTimeSignatureSecretsFromParts(scalars, batches, offsets)
	require.NoError(t, err)
	return restored
}

// requireSameSecrets compares two secrets by their canonical msgpack encoding.
func requireSameSecrets(t *testing.T, expected, actual *OneTimeSignatureSecrets) {
	t.Helper()
	e := expected.Snapshot()
	a := actual.Snapshot()
	require.Equal(t, protocol.Encode(&e), protocol.Encode(&a))
}

// TestOneTimeSignatureSecretsBlobRoundTrip covers the legacy whole-blob
// encoding path: encode/decode fidelity of a full secrets struct.
func TestOneTimeSignatureSecretsBlobRoundTrip(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	s := GenerateOneTimeSignatureSecrets(0, 100)
	s.DeleteBeforeFineGrained(OneTimeSignatureIdentifier{Batch: 3, Offset: 5}, 256)

	snap := s.Snapshot()
	blob := protocol.Encode(&snap)
	var restored OneTimeSignatureSecrets
	require.NoError(t, protocol.Decode(blob, &restored))
	requireSameSecrets(t, s, &restored)
}

func TestPersistentPartsRoundTrip(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	const numKeysPerBatch = 16

	// fresh: batches only, no offsets
	fresh := GenerateOneTimeSignatureSecrets(0, 10)
	requireSameSecrets(t, fresh, reassemble(t, fresh))

	// immediately after a batch rollover: offsets freshly expanded
	rolled := GenerateOneTimeSignatureSecrets(0, 10)
	rolled.DeleteBeforeFineGrained(OneTimeSignatureIdentifier{Batch: 1, Offset: 0}, numKeysPerBatch)
	require.NotEmpty(t, rolled.Offsets)
	requireSameSecrets(t, rolled, reassemble(t, rolled))

	// mid-batch: offsets partially consumed, FirstOffset > 0
	mid := GenerateOneTimeSignatureSecrets(0, 10)
	mid.DeleteBeforeFineGrained(OneTimeSignatureIdentifier{Batch: 2, Offset: 7}, numKeysPerBatch)
	require.NotZero(t, mid.FirstOffset)
	requireSameSecrets(t, mid, reassemble(t, mid))

	// exhausted: everything deleted
	spent := GenerateOneTimeSignatureSecrets(0, 3)
	spent.DeleteBeforeFineGrained(OneTimeSignatureIdentifier{Batch: 100, Offset: 0}, numKeysPerBatch)
	require.Empty(t, spent.Batches)
	require.Empty(t, spent.Offsets)
	requireSameSecrets(t, spent, reassemble(t, spent))
}

// TestFromPartsNilBatchesEdge verifies that zero rows reassemble to nil
// slices, so an exhausted key's FirstBatch is not spuriously bumped by a
// later far-future DeleteBeforeFineGrained (which skips the bump only when
// Batches is nil).
func TestFromPartsNilBatchesEdge(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	const numKeysPerBatch = 16

	spent := GenerateOneTimeSignatureSecrets(0, 3)
	spent.DeleteBeforeFineGrained(OneTimeSignatureIdentifier{Batch: 50, Offset: 0}, numKeysPerBatch)
	require.Nil(t, spent.Batches)
	firstBatchAfterExhaustion := spent.FirstBatch

	restored := reassemble(t, spent)
	require.Nil(t, restored.Batches)
	require.Nil(t, restored.Offsets)

	restored.DeleteBeforeFineGrained(OneTimeSignatureIdentifier{Batch: 200, Offset: 0}, numKeysPerBatch)
	require.Equal(t, firstBatchAfterExhaustion, restored.FirstBatch)
}

func TestFromPartsValidation(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	const numKeysPerBatch = 16
	s := GenerateOneTimeSignatureSecrets(0, 10)
	s.DeleteBeforeFineGrained(OneTimeSignatureIdentifier{Batch: 2, Offset: 3}, numKeysPerBatch)
	scalars, batches, offsets := s.PersistentParts()
	require.NotEmpty(t, batches)
	require.NotEmpty(t, offsets)

	// good baseline
	_, err := OneTimeSignatureSecretsFromParts(scalars, batches, offsets)
	require.NoError(t, err)

	// scalars carrying subkeys
	badScalars := s.Snapshot().OneTimeSignatureSecretsPersistent
	require.NotEmpty(t, badScalars.Batches)
	_, err = OneTimeSignatureSecretsFromParts(badScalars, batches, offsets)
	require.ErrorContains(t, err, "expected none")

	// gap in batch rows
	gapped := append([]KeyedSubkey{}, batches...)
	gapped[1].Index++
	_, err = OneTimeSignatureSecretsFromParts(scalars, gapped, offsets)
	require.ErrorContains(t, err, "batch row")

	// wrong anchor for offset rows
	shifted := append([]KeyedSubkey{}, offsets...)
	for i := range shifted {
		shifted[i].Index++
	}
	_, err = OneTimeSignatureSecretsFromParts(scalars, batches, shifted)
	require.ErrorContains(t, err, "offset row")

	// corrupt row bytes
	corrupt := append([]KeyedSubkey{}, batches...)
	corrupt[0].Key = []byte{0xff, 0x00, 0x01}
	_, err = OneTimeSignatureSecretsFromParts(scalars, corrupt, offsets)
	require.ErrorContains(t, err, "failed to decode")
}

// TestPersistentPartsSignAfterReassembly walks a simulated sequence of rounds
// (crossing several batch boundaries), reassembling from parts at every step
// and checking the restored secrets still produce verifiable signatures.
func TestPersistentPartsSignAfterReassembly(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	const numKeysPerBatch = 8
	s := GenerateOneTimeSignatureSecrets(0, 6)
	pub := s.OneTimeSignatureVerifier

	msg := randString()
	for round := uint64(0); round < 4*numKeysPerBatch; round += 3 {
		id := OneTimeSignatureIdentifier{Batch: round / numKeysPerBatch, Offset: round % numKeysPerBatch}
		s.DeleteBeforeFineGrained(id, numKeysPerBatch)

		restored := reassemble(t, s)
		require.Equal(t, pub, restored.OneTimeSignatureVerifier)
		sig := restored.Sign(id, msg)
		require.True(t, pub.Verify(id, msg, sig), "restored secrets failed to sign round %d", round)
		requireSameSecrets(t, s, restored)
	}
}

// TestPersistentPartsConcurrentDelete exercises PersistentParts racing
// DeleteBeforeFineGrained under -race; it verifies every captured snapshot is
// internally consistent (reassembles without contiguity errors).
func TestPersistentPartsConcurrentDelete(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	const numKeysPerBatch = 8
	s := GenerateOneTimeSignatureSecrets(0, 64)
	msg := randString()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for round := uint64(0); round < 32*numKeysPerBatch; round++ {
			id := OneTimeSignatureIdentifier{Batch: round / numKeysPerBatch, Offset: round % numKeysPerBatch}
			s.DeleteBeforeFineGrained(id, numKeysPerBatch)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			scalars, batches, offsets := s.PersistentParts()
			_, err := OneTimeSignatureSecretsFromParts(scalars, batches, offsets)
			require.NoError(t, err)
		}
	}()
	go func() {
		defer wg.Done()
		// signing (as agreement does) must be safe against concurrent
		// deletion and persistence snapshots
		for round := uint64(0); round < 16*numKeysPerBatch; round++ {
			id := OneTimeSignatureIdentifier{Batch: round / numKeysPerBatch, Offset: round % numKeysPerBatch}
			s.Sign(id, msg)
		}
	}()
	wg.Wait()
}
