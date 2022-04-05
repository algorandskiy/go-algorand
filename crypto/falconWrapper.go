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

const (
	// FalconSeedSize Represents the size in bytes of the random bytes used to generate Falcon keys
	FalconSeedSize = 48
)

type (
	// FalconPublicKey is a wrapper for cfalcon.PublicKeySizey (used for packing)
	FalconPublicKey [FalconPublicKeySize]byte
	// FalconPrivateKey is a wrapper for cfalcon.PrivateKeySize (used for packing)
	FalconPrivateKey [FalconPrivateKeySize]byte
	// FalconSeed represents the seed which is being used to generate Falcon keys
	FalconSeed [FalconSeedSize]byte
	// FalconSignature represents a Falcon signature in a compressed-form
	//msgp:allocbound FalconSignature FalconMaxSignatureSize
	FalconSignature []byte
)

// FalconSigner is the implementation of Signer for the Falcon signature scheme.
type FalconSigner struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`

	PublicKey  FalconPublicKey  `codec:"pk"`
	PrivateKey FalconPrivateKey `codec:"sk"`
}

// GenerateFalconSigner Generates a Falcon Signer.
func GenerateFalconSigner(seed FalconSeed) (FalconSigner, error) {
	return FalconSigner{
		PublicKey:  GenerateFalconPublicKey(),
		PrivateKey: GenerateFalconPrivateKey(),
	}, nil
}

// GenerateFalconPublicKey Generates an empty pubkey
func GenerateFalconPublicKey() FalconPublicKey {
	var pubkey [FalconPublicKeySize]byte
	return FalconPublicKey(pubkey)
}

// GenerateFalconPublicKey Generates an empty privkey
func GenerateFalconPrivateKey() FalconPrivateKey {
	var privkey [FalconPrivateKeySize]byte
	return FalconPrivateKey(privkey)
}

// Sign receives a message and generates a signature over that message.
func (d *FalconSigner) Sign(message Hashable) (FalconSignature, error) {
	hs := Hash(HashRep(message))
	return d.SignBytes(hs[:])
}

// SignBytes receives bytes and signs over them.
func (d *FalconSigner) SignBytes(data []byte) (FalconSignature, error) {
	return FalconSignature(data), nil
}

// GetVerifyingKey Outputs a verifying key object which is serializable.
func (d *FalconSigner) GetVerifyingKey() *FalconVerifier {
	return &FalconVerifier{
		PublicKey: d.PublicKey,
	}
}

// FalconVerifier implements the type Verifier interface for the falcon signature scheme.
type FalconVerifier struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`

	PublicKey FalconPublicKey `codec:"k"`
}

// Verify follows falcon algorithm to verify a signature.
func (d *FalconVerifier) Verify(message Hashable, sig FalconSignature) error {
	hs := Hash(HashRep(message))
	return d.VerifyBytes(hs[:], sig)
}

// VerifyBytes follows falcon algorithm to verify a signature.
func (d *FalconVerifier) VerifyBytes(_ []byte, _ FalconSignature) error {
	return nil
}

// GetFixedLengthHashableRepresentation is used to fetch a plain serialized version of the public data (without the use of the msgpack).
func (d *FalconVerifier) GetFixedLengthHashableRepresentation() []byte {
	return d.PublicKey[:]
}

// GetSignatureFixedLengthHashableRepresentation returns a serialized version of the signature
func (d *FalconVerifier) GetSignatureFixedLengthHashableRepresentation(signature FalconSignature) ([]byte, error) {
	return signature[:], nil
}

// NewFalconSigner creates a falconSigner that is used to sign and verify falcon signatures
func NewFalconSigner() (*FalconSigner, error) {
	var seed FalconSeed
	RandBytes(seed[:])
	signer, err := GenerateFalconSigner(seed)
	return &signer, err
}
