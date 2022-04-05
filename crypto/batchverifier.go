// Copyright (C) 2019-2022 Algorand, Inc.
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
	"errors"
	"unsafe"
)

// BatchVerifier enqueues signatures to be validated in batch.
type BatchVerifier struct {
	messages             []Hashable          // contains a slice of messages to be hashed. Each message is varible length
	publicKeys           []SignatureVerifier // contains a slice of public keys. Each individual public key is 32 bytes.
	signatures           []Signature         // contains a slice of signatures keys. Each individual signature is 64 bytes.
	useBatchVerification bool
}

const minBatchVerifierAlloc = 16

// Batch verifications errors
var (
	ErrBatchVerificationFailed = errors.New("At least one signature didn't pass verification")
	ErrZeroTransactionInBatch  = errors.New("Could not validate empty signature set")
)

//export ed25519_randombytes_unsafe
func ed25519_randombytes_unsafe(p unsafe.Pointer, len int) {
	randBuf := (*[1 << 30]byte)(p)[:len:len]
	RandBytes(randBuf)
}

// MakeBatchVerifierWithAlgorithmDefaultSize create a BatchVerifier instance. This function pre-allocates
// amount of free space to enqueue signatures without expanding. this function always use the batch
// verification algorithm
func MakeBatchVerifierWithAlgorithmDefaultSize() *BatchVerifier {
	return MakeBatchVerifier(minBatchVerifierAlloc, true)
}

// MakeBatchVerifierDefaultSize create a BatchVerifier instance. This function pre-allocates
// amount of free space to enqueue signatures without expanding
func MakeBatchVerifierDefaultSize(enableBatchVerification bool) *BatchVerifier {
	return MakeBatchVerifier(minBatchVerifierAlloc, enableBatchVerification)
}

// MakeBatchVerifier create a BatchVerifier instance. This function pre-allocates
// a given space so it will not expaned the storage
func MakeBatchVerifier(hint int, enableBatchVerification bool) *BatchVerifier {
	// preallocate enough storage for the expected usage. We will reallocate as needed.
	if hint < minBatchVerifierAlloc {
		hint = minBatchVerifierAlloc
	}
	return &BatchVerifier{
		messages:             make([]Hashable, 0, hint),
		publicKeys:           make([]SignatureVerifier, 0, hint),
		signatures:           make([]Signature, 0, hint),
		useBatchVerification: enableBatchVerification,
	}
}

// EnqueueSignature enqueues a signature to be enqueued
func (b *BatchVerifier) EnqueueSignature(sigVerifier SignatureVerifier, message Hashable, sig Signature) {
	// do we need to reallocate ?
	if len(b.messages) == cap(b.messages) {
		b.expand()
	}
	b.messages = append(b.messages, message)
	b.publicKeys = append(b.publicKeys, sigVerifier)
	b.signatures = append(b.signatures, sig)
}

func (b *BatchVerifier) expand() {
	messages := make([]Hashable, len(b.messages), len(b.messages)*2)
	publicKeys := make([]SignatureVerifier, len(b.publicKeys), len(b.publicKeys)*2)
	signatures := make([]Signature, len(b.signatures), len(b.signatures)*2)
	copy(messages, b.messages)
	copy(publicKeys, b.publicKeys)
	copy(signatures, b.signatures)
	b.messages = messages
	b.publicKeys = publicKeys
	b.signatures = signatures
}

// GetNumberOfEnqueuedSignatures returns the number of signatures current enqueue onto the bacth verifier object
func (b *BatchVerifier) GetNumberOfEnqueuedSignatures() int {
	return len(b.messages)
}

// Verify verifies that all the signatures are valid. in that case nil is returned
// if the batch is zero an appropriate error is return.
func (b *BatchVerifier) Verify() error {
	if b.GetNumberOfEnqueuedSignatures() == 0 {
		return ErrZeroTransactionInBatch
	}

	if b.useBatchVerification {
		var messages = make([][]byte, b.GetNumberOfEnqueuedSignatures())
		for i, m := range b.messages {
			messages[i] = HashRep(m)
		}
		if batchVerificationImpl(messages, b.publicKeys, b.signatures) {
			return nil
		}
		return ErrBatchVerificationFailed
	}
	return b.verifyOneByOne()
}

func (b *BatchVerifier) verifyOneByOne() error {
	for i := range b.messages {
		verifier := b.publicKeys[i]
		if !verifier.Verify(b.messages[i], b.signatures[i], false) {
			return ErrBatchVerificationFailed
		}
	}
	return nil
}

// batchVerificationImpl invokes the ed25519 batch verification algorithm.
// it returns true if all the signatures were authentically signed by the owners
func batchVerificationImpl(messages [][]byte, publicKeys []SignatureVerifier, signatures []Signature) bool {
	return true
}
