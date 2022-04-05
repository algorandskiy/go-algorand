//+build !wasm

package crypto

import "github.com/algorand/go-deadlock"

const masterDerivationKeyLenBytes = 32

/* Classical signatures */
type ed25519Signature [64]byte
type ed25519PublicKey [32]byte
type ed25519PrivateKey [64]byte
type ed25519Seed [32]byte

// PrivateKey is an exported ed25519PrivateKey
type PrivateKey ed25519PrivateKey

// PublicKey is an exported ed25519PublicKey
type PublicKey ed25519PublicKey

// MasterDerivationKey is used to derive ed25519 keys for use in wallets
type MasterDerivationKey [masterDerivationKeyLenBytes]byte

// A Signature is a cryptographic signature. It proves that a message was
// produced by a holder of a cryptographic secret.
type Signature ed25519Signature

// A Seed holds the entropy needed to generate cryptographic keys.
type Seed ed25519Seed

// A SignatureVerifier is used to identify the holder of SignatureSecrets
// and verify the authenticity of Signatures.
type SignatureVerifier = PublicKey

// SignatureSecrets are used by an entity to produce unforgeable signatures over
// a message.
type SignatureSecrets struct {
	_struct struct{} `codec:""`

	SignatureVerifier
	SK ed25519PrivateKey
}

// TODO: Go arrays are copied by value, so any call to e.g. VrfPrivkey.Prove() makes a copy of the secret key that lingers in memory.
// To avoid this, should we instead allocate memory for secret keys here (maybe even in the C heap) and pass around pointers?
// e.g., allocate a privkey with sodium_malloc and have VrfPrivkey be of type unsafe.Pointer?
type (
	// A VrfPrivkey is a private key used for producing VRF proofs.
	// Specifically, we use a 64-byte ed25519 private key (the latter 32-bytes are the precomputed public key)
	VrfPrivkey [64]byte
	// A VrfPubkey is a public key that can be used to verify VRF proofs.
	VrfPubkey [32]byte
	// A VrfProof for a message can be generated with a secret key and verified against a public key, like a signature.
	// Proofs are malleable, however, for a given message and public key, the VRF output that can be computed from a proof is unique.
	VrfProof [80]byte
	// VrfOutput is a 64-byte pseudorandom value that can be computed from a VrfProof.
	// The VRF scheme guarantees that such output will be unique
	VrfOutput [64]byte
)

// VRFSecrets is a wrapper for a VRF keypair. Use *VrfPrivkey instead
type VRFSecrets struct {
	_struct struct{} `codec:""`

	PK VrfPubkey
	SK VrfPrivkey
}

// A OneTimeSignature is a cryptographic signature that is produced a limited
// number of times and provides forward integrity.
//
// Specifically, a OneTimeSignature is generated from an ephemeral secret. After
// some number of messages is signed under a given OneTimeSignatureIdentifier
// identifier, the corresponding secret is deleted. This prevents the
// secret-holder from signing a contradictory message in the future in the event
// of a secret-key compromise.
type OneTimeSignature struct {
	// Unfortunately we forgot to mark this struct as omitempty at
	// one point, and now it's hard to recover from that if we want
	// to preserve encodings..
	_struct struct{} `codec:""`

	// Sig is a signature of msg under the key PK.
	Sig ed25519Signature `codec:"s"`
	PK  ed25519PublicKey `codec:"p"`

	// Old-style signature that does not use proper domain separation.
	// PKSigOld is unused; however, unfortunately we forgot to mark it
	// `codec:omitempty` and so it appears (with zero value) in certs.
	// This means we can't delete the field without breaking catchup.
	PKSigOld ed25519Signature `codec:"ps"`

	// Used to verify a new-style two-level ephemeral signature.
	// PK1Sig is a signature of OneTimeSignatureSubkeyOffsetID(PK, Batch, Offset) under the key PK2.
	// PK2Sig is a signature of OneTimeSignatureSubkeyBatchID(PK2, Batch) under the master key (OneTimeSignatureVerifier).
	PK2    ed25519PublicKey `codec:"p2"`
	PK1Sig ed25519Signature `codec:"p1s"`
	PK2Sig ed25519Signature `codec:"p2s"`
}

// OneTimeSignatureSecrets are used to produced unforgeable signatures over a
// message.
//
// When the method OneTimeSignatureSecrets.DeleteBefore(ID) is called, ephemeral
// secrets corresponding to OneTimeSignatureIdentifiers preceding ID are
// deleted. Thereafter, an entity can no longer sign different messages with old
// OneTimeSignatureIdentifiers, protecting the integrity of the messages signed
// under those identifiers.
type OneTimeSignatureSecrets struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`

	OneTimeSignatureSecretsPersistent

	// We keep track of an RNG, used to generate additional randomness.
	// This is used purely for testing (fuzzing, specifically).  Except
	// for testing, the RNG is SystemRNG.
	rng RNG

	// We use a read-write lock to guard against concurrent invocations,
	// such as Sign() concurrently running with DeleteBefore*().
	mu deadlock.RWMutex
}

// OneTimeSignatureSecretsPersistent denotes the fields of a OneTimeSignatureSecrets
// that get stored to persistent storage (through reflection on exported fields).
type OneTimeSignatureSecretsPersistent struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`

	OneTimeSignatureVerifier

	// FirstBatch denotes the first batch whose subkey appears in Batches.
	// The odd `codec:` name is for backwards compatibility with previous
	// stored keys where we failed to give any explicit `codec:` name.
	FirstBatch uint64            `codec:"First"`
	Batches    []ephemeralSubkey `codec:"Sub,allocbound=-"`

	// FirstOffset denotes the first offset whose subkey appears in Offsets.
	// These subkeys correspond to batch FirstBatch-1.
	FirstOffset uint64            `codec:"firstoff"`
	Offsets     []ephemeralSubkey `codec:"offkeys,allocbound=-"` // the bound is keyDilution

	// When Offsets is non-empty, OffsetsPK2 is the intermediate-level public
	// key that can be used to verify signatures on the subkeys in Offsets, and
	// OffsetsPK2Sig is the signature from the master key (OneTimeSignatureVerifier)
	// on OneTimeSignatureSubkeyBatchID(OffsetsPK2, FirstBatch-1).
	OffsetsPK2    ed25519PublicKey `codec:"offpk2"`
	OffsetsPK2Sig ed25519Signature `codec:"offpk2sig"`
}

// A OneTimeSignatureVerifier is used to identify the holder of
// OneTimeSignatureSecrets and prove the authenticity of OneTimeSignatures
// against some OneTimeSignatureIdentifier.
type OneTimeSignatureVerifier ed25519PublicKey

// An ephemeralSubkey produces OneTimeSignatures for messages and is deleted
// after use.
type ephemeralSubkey struct {
	// Unfortunately we forgot to mark this struct as omitempty at
	// one point, and now it's hard to recover from that if we want
	// to preserve encodings..
	_struct struct{} `codec:""`

	PK ed25519PublicKey
	SK ed25519PrivateKey

	// PKSigOld is the signature that authenticates PK.  It is the
	// signature of the PK together with the batch number, using an
	// old style of signatures that we support for backwards
	// compatibility (thus the odd `codec:` name).
	PKSigOld ed25519Signature `codec:"PKSig"`

	// PKSigNew is the signature that authenticates PK, signed using the
	// Hashable interface for domain separation (the Hashable object is either
	// OneTimeSignatureSubkeyBatchID or OneTimeSignatureSubkeyOffsetID).
	PKSigNew ed25519Signature `codec:"sig2"`
}

// A OneTimeSignatureSubkeyBatchID identifies an ephemeralSubkey of a batch
// for the purposes of signing it with the top-level master key.
type OneTimeSignatureSubkeyBatchID struct {
	// Unfortunately we forgot to mark this struct as omitempty at
	// one point, and now it's hard to recover from that if we want
	// to preserve encodings..
	_struct struct{} `codec:""`

	SubKeyPK ed25519PublicKey `codec:"pk"`
	Batch    uint64           `codec:"batch"`
}

// A OneTimeSignatureSubkeyOffsetID identifies an ephemeralSubkey of a specific
// offset within a batch, for the purposes of signing it with the batch subkey.
type OneTimeSignatureSubkeyOffsetID struct {
	// Unfortunately we forgot to mark this struct as omitempty at
	// one point, and now it's hard to recover from that if we want
	// to preserve encodings..
	_struct struct{} `codec:""`

	SubKeyPK ed25519PublicKey `codec:"pk"`
	Batch    uint64           `codec:"batch"`
	Offset   uint64           `codec:"off"`
}

// SecretKey is casted from SignatureSecrets
type SecretKey = SignatureSecrets

// MultisigSubsig is a struct that holds a pair of public key and signatures
// signatures may be empty
type MultisigSubsig struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`

	Key PublicKey `codec:"pk"` // all public keys that are possible signers for this address
	Sig Signature `codec:"s"`  // may be either empty or a signature
}

// MultisigSig is the structure that holds multiple Subsigs
type MultisigSig struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`

	Version   uint8            `codec:"v"`
	Threshold uint8            `codec:"thr"`
	Subsigs   []MultisigSubsig `codec:"subsig,allocbound=maxMultisig"`
}

const multiSigString = "MultisigAddr"
const maxMultisig = 255

// VRFVerifier is a deprecated name for VrfPubkey
type VRFVerifier = VrfPubkey

// A OneTimeSignatureIdentifier is an identifier under which a OneTimeSignature is
// produced on a given message.  This identifier is represented using a two-level
// structure, which corresponds to two levels of our ephemeral key tree.
type OneTimeSignatureIdentifier struct {
	// Batch represents the most-significant part of the identifier.
	Batch uint64

	// Offset represents the least-significant part of the identifier.
	// When moving to a new Batch, the Offset values restart from 0.
	Offset uint64
}
