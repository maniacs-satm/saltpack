// Copyright 2015 Keybase, Inc. All rights reserved. Use of
// this source code is governed by the included BSD license.

package saltpack

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"fmt"

	"github.com/keybase/go-codec/codec"
	"golang.org/x/crypto/poly1305"
)

// encryptionBlockNumber describes which block number we're at in the sequence
// of encrypted blocks. Each encrypted block of course fits into a packet.
type encryptionBlockNumber uint64

func codecHandle() *codec.MsgpackHandle {
	var mh codec.MsgpackHandle
	mh.WriteExt = true
	return &mh
}

func randomFill(b []byte) (err error) {
	l := len(b)
	n, err := rand.Read(b)
	if err != nil {
		return err
	}
	if n != l {
		return ErrInsufficientRandomness
	}
	return nil
}

func (e encryptionBlockNumber) check() error {
	if e >= encryptionBlockNumber(0xffffffffffffffff) {
		return ErrPacketOverflow
	}
	return nil
}

func assertEndOfStream(stream *msgpackStream) error {
	var i interface{}
	_, err := stream.Read(&i)
	if err == nil {
		err = ErrTrailingGarbage
	}
	return err
}

type headerHash [sha512.Size]byte

func attachedSignatureInput(headerHash headerHash, block *signatureBlock) []byte {
	hasher := sha512.New()
	hasher.Write(headerHash[:])
	binary.Write(hasher, binary.BigEndian, block.seqno)
	hasher.Write(block.PayloadChunk)

	var buf bytes.Buffer
	buf.Write([]byte(signatureAttachedString))
	buf.Write(hasher.Sum(nil))

	return buf.Bytes()
}

func detachedSignatureInput(headerHash headerHash, plaintext []byte) []byte {
	hasher := sha512.New()
	hasher.Write(headerHash[:])
	hasher.Write(plaintext)

	return detachedSignatureInputFromHash(hasher.Sum(nil))
}

func detachedSignatureInputFromHash(plaintextAndHeaderHash []byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte(signatureDetachedString))
	buf.Write(plaintextAndHeaderHash)

	return buf.Bytes()
}

// TODO: Use this in more places.
func copyEqualSize(out, in []byte) {
	if len(out) != len(in) {
		panic(fmt.Sprintf("len(out)=%d != len(in)=%d", len(out), len(in)))
	}
	copy(out, in)
}

type payloadHash [sha512.Size]byte

type payloadAuthenticator [cryptoAuthBytes]byte

func (pa payloadAuthenticator) Equal(other payloadAuthenticator) bool {
	return hmac.Equal(pa[:], other[:])
}

func authenticatePayload(macKey macKey, payloadHash payloadHash) payloadAuthenticator {
	// Equivalent to crypto_auth, but using Go's builtin HMAC. Truncates
	// SHA512, instead of calling SHA512/256, which has different IVs.
	authenticatorDigest := hmac.New(sha512.New, macKey[:])
	authenticatorDigest.Write(payloadHash[:])
	fullMAC := authenticatorDigest.Sum(nil)
	var auth payloadAuthenticator
	copyEqualSize(auth[:], fullMAC[:cryptoAuthBytes])
	return auth
}

type macKey [cryptoAuthKeyBytes]byte

func computeMACKey(secret BoxSecretKey, public BoxPublicKey, headerHash headerHash) macKey {
	nonce := nonceForMACKeyBox(headerHash)
	macKeyBox := secret.Box(public, nonce, make([]byte, cryptoAuthKeyBytes))
	var macKey macKey
	copyEqualSize(macKey[:], macKeyBox[poly1305.TagSize:poly1305.TagSize+cryptoAuthKeyBytes])
	return macKey
}

func computePayloadHash(headerHash headerHash, nonce *Nonce, payloadCiphertext []byte) payloadHash {
	payloadDigest := sha512.New()
	payloadDigest.Write(headerHash[:])
	payloadDigest.Write(nonce[:])
	payloadDigest.Write(payloadCiphertext)
	h := payloadDigest.Sum(nil)
	var payloadHash payloadHash
	copyEqualSize(payloadHash[:], h)
	return payloadHash
}

func hashHeader(headerBytes []byte) headerHash {
	return sha512.Sum512(headerBytes)
}
