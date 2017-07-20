// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package secretbox encrypts and authenticates small messages.

Secretbox uses XSalsa20 and Poly1305 to encrypt and authenticate messages with
secret-key cryptography. The length of messages is not hidden.

It is the caller's responsibility to ensure the uniqueness of nonces—for
example, by using nonce 1 for the first message, nonce 2 for the second
message, etc. Nonces are long enough that randomly generated nonces have
negligible risk of collision.

This package is interoperable with NaCl: https://nacl.cr.yp.to/secretbox.html.
*/
package secretbox // import "github.com/kevinburke/nacl/secretbox"

import (
	"github.com/kevinburke/nacl"
	"github.com/kevinburke/nacl/onetimeauth"
	"golang.org/x/crypto/salsa20/salsa"
)

// Overhead is the number of bytes of overhead when boxing a message.
const Overhead = onetimeauth.Size

// setup produces a sub-key and Salsa20 counter given a nonce and key.
func setup(subKey nacl.Key, counter *[16]byte, nonce nacl.Nonce, key nacl.Key) {
	// We use XSalsa20 for encryption so first we need to generate a
	// key and nonce with HSalsa20.
	var hNonce [16]byte
	copy(hNonce[:], nonce[:])
	salsa.HSalsa20(subKey, &hNonce, key, &salsa.Sigma)

	// The final 8 bytes of the original nonce form the new nonce.
	copy(counter[:], nonce[16:])
}

// sliceForAppend takes a slice and a requested number of bytes. It returns a
// slice with the contents of the given slice followed by that many bytes and a
// second slice that aliases into it and contains only the extra bytes. If the
// original slice has sufficient capacity then no allocation is performed.
func sliceForAppend(in []byte, n int) (head, tail []byte) {
	if total := len(in) + n; cap(in) >= total {
		head = in[:total]
	} else {
		head = make([]byte, total)
		copy(head, in)
	}
	tail = head[len(in):]
	return
}

// Seal appends an encrypted and authenticated copy of message to out, which
// must not overlap message. The key and nonce pair must be unique for each
// distinct message and the output will be Overhead bytes longer than message.
func Seal(out, message []byte, nonce nacl.Nonce, key nacl.Key) []byte {
	var subKey [32]byte
	var counter [16]byte
	setup(&subKey, &counter, nonce, key)

	// The Poly1305 key is generated by encrypting 32 bytes of zeros. Since
	// Salsa20 works with 64-byte blocks, we also generate 32 bytes of
	// keystream as a side effect.
	var firstBlock [64]byte
	salsa.XORKeyStream(firstBlock[:], firstBlock[:], &counter, &subKey)

	var poly1305Key [32]byte
	copy(poly1305Key[:], firstBlock[:])

	ret, out := sliceForAppend(out, len(message)+onetimeauth.Size)

	// We XOR up to 32 bytes of message with the keystream generated from
	// the first block.
	firstMessageBlock := message
	if len(firstMessageBlock) > 32 {
		firstMessageBlock = firstMessageBlock[:32]
	}

	tagOut := out
	out = out[onetimeauth.Size:]
	for i, x := range firstMessageBlock {
		out[i] = firstBlock[32+i] ^ x
	}
	message = message[len(firstMessageBlock):]
	ciphertext := out
	out = out[len(firstMessageBlock):]

	// Now encrypt the rest.
	counter[8] = 1
	salsa.XORKeyStream(out, message, &counter, &subKey)

	tag := onetimeauth.Sum(ciphertext, &poly1305Key)
	copy(tagOut, tag[:])

	return ret
}

// Open authenticates and decrypts a box produced by Seal and appends the
// message to out, which must not overlap box. The output will be Overhead
// bytes smaller than box.
func Open(out []byte, box []byte, nonce nacl.Nonce, key nacl.Key) ([]byte, bool) {
	if len(box) < Overhead {
		return nil, false
	}

	var subKey [32]byte
	var counter [16]byte
	setup(&subKey, &counter, nonce, key)

	// The Poly1305 key is generated by encrypting 32 bytes of zeros. Since
	// Salsa20 works with 64-byte blocks, we also generate 32 bytes of
	// keystream as a side effect.
	var firstBlock [64]byte
	salsa.XORKeyStream(firstBlock[:], firstBlock[:], &counter, &subKey)

	var poly1305Key [32]byte
	copy(poly1305Key[:], firstBlock[:])
	var tag [onetimeauth.Size]byte
	copy(tag[:], box)

	if !onetimeauth.Verify(&tag, box[onetimeauth.Size:], &poly1305Key) {
		return nil, false
	}

	ret, out := sliceForAppend(out, len(box)-Overhead)

	// We XOR up to 32 bytes of box with the keystream generated from
	// the first block.
	box = box[Overhead:]
	firstMessageBlock := box
	if len(firstMessageBlock) > 32 {
		firstMessageBlock = firstMessageBlock[:32]
	}
	for i, x := range firstMessageBlock {
		out[i] = firstBlock[32+i] ^ x
	}

	box = box[len(firstMessageBlock):]
	out = out[len(firstMessageBlock):]

	// Now decrypt the rest.
	counter[8] = 1
	salsa.XORKeyStream(out, box, &counter, &subKey)

	return ret, true
}
