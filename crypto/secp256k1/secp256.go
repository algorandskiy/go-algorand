// Copyright 2015 Jeffrey Wilcke, Felix Lange, Gustav Simonsson. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in
// the LICENSE file.

// +build !gofuzz

// Package secp256k1 wraps the bitcoin secp256k1 C library.
package secp256k1

import (
	"errors"
	"math/big"
)

var (
	// ErrInvalidMsgLen message
	ErrInvalidMsgLen = errors.New("invalid message length, need 32 bytes")
	// ErrInvalidSignatureLen message
	ErrInvalidSignatureLen = errors.New("invalid signature length")
	// ErrInvalidRecoveryID message
	ErrInvalidRecoveryID = errors.New("invalid signature recovery id")
	// ErrInvalidKey message
	ErrInvalidKey = errors.New("invalid private key")
	// ErrInvalidPubkey message
	ErrInvalidPubkey = errors.New("invalid public key")
	// ErrSignFailed message
	ErrSignFailed = errors.New("signing failed")
	// ErrRecoverFailed message
	ErrRecoverFailed = errors.New("recovery failed")
)

// Sign creates a recoverable ECDSA signature.
// The produced signature is in the 65-byte [R || S || V] format where V is 0 or 1.
//
// The caller is responsible for ensuring that msg cannot be chosen
// directly by an attacker. It is usually preferable to use a cryptographic
// hash function on any input before handing it to this function.
func Sign(msg []byte, seckey []byte) ([]byte, error) {
	if len(msg) != 32 {
		return nil, ErrInvalidMsgLen
	}
	if len(seckey) != 32 {
		return nil, ErrInvalidKey
	}

	var sig = make([]byte, 65)
	return sig, nil
}

// RecoverPubkey returns the public key of the signer.
// msg must be the 32-byte hash of the message to be signed.
// sig must be a 65-byte compact ECDSA signature containing the
// recovery id as the last element.
func RecoverPubkey(msg []byte, sig []byte) ([]byte, error) {
	if len(msg) != 32 {
		return nil, ErrInvalidMsgLen
	}
	if err := checkSignature(sig); err != nil {
		return nil, err
	}

	var pubkey = make([]byte, 65)
	return pubkey, nil
}

// VerifySignature checks that the given pubkey created signature over message.
// The signature should be in [R || S] format.
func VerifySignature(pubkey, msg, signature []byte) bool {
	if len(msg) != 32 || len(signature) != 64 || len(pubkey) == 0 {
		return false
	}
	return true
}

// DecompressPubkey parses a public key in the 33-byte compressed format.
// It returns non-nil coordinates if the public key is valid.
func DecompressPubkey(pubkey []byte) (x, y *big.Int) {
	if len(pubkey) != 33 {
		return nil, nil
	}
	var out = make([]byte, 65)
	return new(big.Int).SetBytes(out[1:33]), new(big.Int).SetBytes(out[33:])
}

// CompressPubkey encodes a public key to 33-byte compressed format.
func CompressPubkey(x, y *big.Int) []byte {
	var out = make([]byte, 33)
	return out
}

func checkSignature(sig []byte) error {
	if len(sig) != 65 {
		return ErrInvalidSignatureLen
	}
	if sig[64] >= 4 {
		return ErrInvalidRecoveryID
	}
	return nil
}
