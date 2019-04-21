// Copyright 2019 The Wuffs Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// ----------------

// Package flatecut produces DEFLATE-formatted data subject to a maximum
// compressed size.
//
// The typical compression problem is to encode all of the given source data in
// some number of bytes. This package's problem is finding a reasonably long
// prefix of the source data that encodes in up to a given number of bytes.
package flatecut

import (
	"errors"
)

var (
	errMaxEncodedLenTooSmall = errors.New("flatecut: maxEncodedLen is too small")

	errInternalNoProgress   = errors.New("flatecut: internal: no progress")
	errInternalSomeProgress = errors.New("flatecut: internal: some progress")

	errInvalidBadBlockLength = errors.New("flatecut: invalid input: bad block length")
	errInvalidBadBlockType   = errors.New("flatecut: invalid input: bad block type")
	errInvalidBadCodeLengths = errors.New("flatecut: invalid input: bad code lengths")
	errInvalidBadHuffmanTree = errors.New("flatecut: invalid input: bad Huffman tree")
	errInvalidBadSymbol      = errors.New("flatecut: invalid input: bad symbol")
	errInvalidNoEndOfBlock   = errors.New("flatecut: invalid input: no end-of-block")
	errInvalidNotEnoughData  = errors.New("flatecut: invalid input: not enough data")
	errInvalidTooManyCodes   = errors.New("flatecut: invalid input: too many codes")
)

var (
	// codeOrder is defined in RFC 1951 section 3.2.7.
	codeOrder = [19]uint32{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}

	// These tables are defined in RFC 1951 section 3.2.5.
	//
	// The l-tables' indexes are biased by 256.
	lBases = [32]int32{
		mostNegativeInt32,
		3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31,
		35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258,
		mostNegativeInt32, mostNegativeInt32,
	}
	lExtras = [32]uint32{
		0,
		0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2,
		3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0,
		0, 0,
	}
	dBases = [32]int32{
		1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193,
		257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577,
		mostNegativeInt32, mostNegativeInt32,
	}
	dExtras = [32]uint32{
		0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6,
		7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13,
		0, 0,
	}
)

const (
	// SmallestValidMaxEncodedLen is the length in bytes of the smallest valid
	// DEFLATE-encoded data.
	SmallestValidMaxEncodedLen = 2

	maxCodeBits = 15
	maxNumCodes = 288

	mostNegativeInt32 = -0x80000000
)

type bitstream struct {
	// bytes[index] is the next byte to load into the 'bits' field.
	bytes []byte
	index int

	// The low nBits bits of the 'bits' field hold the next bits (in LSB-first
	// order).
	bits  uint32
	nBits uint32
}

func (b *bitstream) take(nBits uint32) int32 {
	for b.nBits < nBits {
		if b.index >= len(b.bytes) {
			return mostNegativeInt32
		}
		b.bits |= uint32(b.bytes[b.index]) << b.nBits
		b.nBits += 8
		b.index++
	}

	mask := ((uint32(1)) << nBits) - 1
	ret := b.bits & mask
	b.bits >>= nBits
	b.nBits -= nBits
	return int32(ret)
}

// huffman represents a DEFLATE Huffman tree.
//
// For the "Consider the alphabet ABCDEFGH, with bit lengths (3, 3, 3, 3, 3, 2,
// 4, 4)" example from RFC 1951 section 3.2.2, the huffman struct is
// initialized by calling:
//
// h.construct([]uint32{
//   'A': 3, 'B': 3, 'C': 3, 'D': 3, 'E': 3, 'F': 2, 'G': 4, 'H': 4,
// })
//
// which results in:
//
// huffman{
//   counts: []uint32{
//     2: 1, 3: 5, 4: 2,
//   },
//   symbols: []int32{
//     0: 'F', 1: 'A', 2: 'B', 3: 'C', 4: 'D', 5: 'E', 6: 'G', 7: 'H',
//   },
// }
//
// Continuing the example from the RFC, decoding "1110" from the bitstream will
// produce the 'G' symbol.
type huffman struct {
	counts  [maxCodeBits + 1]uint32
	symbols [maxNumCodes]int32
}

func (h *huffman) decode(b *bitstream) int32 {
	code := uint32(0)     // The i bits from the bitstream.
	first := uint32(0)    // The first Huffman code of length i.
	symIndex := uint32(0) // How many elements of h.symbols we've gone past.

	// The "at this point" comments in the loop detail the `"1110" decodes to
	// 'G'` example discussed above in the huffman type's doc comment.
	//
	// Note that, as a loop invariant, code >= first.
	for i := 1; i <= maxCodeBits; i++ {
		if b.nBits == 0 {
			if b.index >= len(b.bytes) {
				return mostNegativeInt32
			}
			b.bits = uint32(b.bytes[b.index])
			b.nBits = 8
			b.index++
		}

		code |= b.bits & 1
		b.bits >>= 1
		b.nBits -= 1

		// At this point:
		//  - When i == 1, code is  1, symIndex is 0, first is  0.
		//  - When i == 2, code is  3, symIndex is 0, first is  0.
		//  - When i == 3, code is  7, symIndex is 1, first is  2.
		//  - When i == 4, code is 14, symIndex is 6, first is 14.

		// Recall that h.counts is: {0, 0, 1, 5, 2, 0, 0, ...}.
		count := h.counts[i]
		if code < (count + first) {
			// At this point:
			//  - When i == 4, code is 14, symIndex is 6, first is 14.
			//
			// h.symbols[6+14-14] is indeed 'G'.
			return h.symbols[symIndex+code-first]
		}

		symIndex += count
		first += count
		first <<= 1
		code <<= 1

		// At this point:
		//  - When i == 1, code is  2, symIndex is 0, first is  0.
		//  - When i == 2, code is  6, symIndex is 1, first is  2.
		//  - When i == 3, code is 14, symIndex is 6, first is 14.
	}
	return mostNegativeInt32
}

func (h *huffman) construct(lengths []uint32) (endCodeBits uint32, endCodeNBits uint32, retErr error) {
	for i := range h.counts {
		h.counts[i] = 0
	}
	for _, x := range lengths {
		h.counts[x]++
	}
	if h.counts[0] == uint32(len(lengths)) {
		return 0, 0, errInvalidBadHuffmanTree
	}

	// Check for an over- or under-subscribed Huffman tree, and for the
	// end-of-block code (with value 256).
	const endCode = 256
	remaining := uint32(1)
	endCodeLength := uint32(0)
	if len(lengths) > endCode {
		endCodeLength = lengths[endCode]
	}
	for i := uint32(1); i <= maxCodeBits; i++ {
		remaining *= 2
		if remaining < h.counts[i] {
			return 0, 0, errInvalidBadHuffmanTree
		}
		remaining -= h.counts[i]

		if i == endCodeLength {
			remainingForEndCode := remaining
			for _, l := range lengths[endCode+1:] {
				if l == endCodeLength {
					remainingForEndCode++
				}
			}
			endCodeBits = ((uint32(1) << endCodeLength) - 1) - remainingForEndCode
			endCodeNBits = endCodeLength
		}
	}
	if remaining != 0 {
		return 0, 0, errInvalidBadHuffmanTree
	}

	offsets := [maxCodeBits + 1]uint32{}
	for i := 1; i < maxCodeBits; i++ {
		offsets[i+1] = offsets[i] + h.counts[i]
	}

	for symbol, length := range lengths {
		if length != 0 {
			h.symbols[offsets[length]] = int32(symbol)
			offsets[length]++
		}
	}
	return endCodeBits, endCodeNBits, nil
}

// cutEmpty sets encoding[:2] to contain valid DEFLATE-encoded data. Decoding
// that data produces zero bytes.
func cutEmpty(encoded []byte, maxEncodedLen int) (encodedLen int, decodedLen int, retErr error) {
	if maxEncodedLen < SmallestValidMaxEncodedLen {
		panic("unreachable")
	}
	// Set encoded[:2] to hold:
	//  - 1 bit   ...._...._...._...1  finalBlock   = true.
	//  - 2 bits  ...._...._...._.01.  blockType    = 1 (static Huffman).
	//  - 7 bits  ...._..00_0000_0...  litLenSymbol = 256 (end-of-block).
	//  - 6 bits  0000_00.._...._....  padding.
	encoded[0] = 0x03
	encoded[1] = 0x00
	return 2, 0, nil
}

// Cut modifies encoded's contents such that encoded[:encodedLen] is valid
// DEFLATE-compressed data, assuming that encoded starts off containing valid
// DEFLATE-compressed data.
//
// Decompressing that modified, shorter byte slice produces a prefix (of length
// decodedLen) of the decompression of the original, longer byte slice.
//
// It does not necessarily return the largest possible decodedLen.
func Cut(encoded []byte, maxEncodedLen int) (encodedLen int, decodedLen int, retErr error) {
	if maxEncodedLen < SmallestValidMaxEncodedLen {
		return 0, 0, errMaxEncodedLenTooSmall
	}
	const m = 1 << 30
	if uint64(maxEncodedLen) > m {
		maxEncodedLen = m
	}
	if maxEncodedLen > len(encoded) {
		maxEncodedLen = len(encoded)
	}
	if maxEncodedLen < SmallestValidMaxEncodedLen {
		return 0, 0, errInvalidNotEnoughData
	}

	c := cutter{
		bits: bitstream{
			bytes: encoded,
		},
		maxEncodedLen: maxEncodedLen,
	}
	return c.cut()
}

type cutter struct {
	bits bitstream

	maxEncodedLen int
	decodedLen    int32

	endCodeBits  uint32
	endCodeNBits uint32

	lHuff huffman
	dHuff huffman
}

func (c *cutter) cut() (encodedLen int, decodedLen int, retErr error) {
	prevFinalBlockIndex := -1
	prevFinalBlockNBits := uint32(0)

	for {
		finalBlock := c.bits.take(1)
		if finalBlock < 0 {
			return 0, 0, errInvalidNotEnoughData
		}

		finalBlockIndex := c.bits.index
		finalBlockNBits := c.bits.nBits

		blockType := c.bits.take(2)
		if blockType < 0 {
			return 0, 0, errInvalidNotEnoughData
		}

		err := error(nil)
		switch blockType {
		case 0:
			err = c.doStored()
		case 1:
			err = c.doStaticHuffman()
		case 2:
			err = c.doDynamicHuffman()
		case 3:
			return 0, 0, errInvalidBadBlockType
		}

		switch err {
		case nil:
			if finalBlock == 0 {
				prevFinalBlockIndex = finalBlockIndex
				prevFinalBlockNBits = finalBlockNBits
				continue
			}

		case errInternalNoProgress:
			if prevFinalBlockIndex < 0 {
				return cutEmpty(c.bits.bytes, c.maxEncodedLen)
			}

			// Un-read to just before the finalBlock bit.
			c.bits.index = finalBlockIndex
			c.bits.nBits = finalBlockNBits + 1
			if c.bits.nBits == 8 {
				c.bits.index--
				c.bits.nBits = 0
			}

			finalBlockIndex = prevFinalBlockIndex
			finalBlockNBits = prevFinalBlockNBits
			fallthrough

		case errInternalSomeProgress:
			// Set the n'th bit (LSB=0, MSB=7) of
			// c.bits.bytes[finalBlockIndex-1] to be 1.
			n := 7 - finalBlockNBits
			mask := uint32(1) << n
			c.bits.bytes[finalBlockIndex-1] |= uint8(mask)

		default:
			return 0, 0, err

		}
		break
	}

	if c.bits.nBits != 0 {
		// Clear the high c.bits.nBits bits of c.bits.bytes[c.bits.index-1].
		mask := (uint32(1) << (8 - c.bits.nBits)) - 1
		c.bits.bytes[c.bits.index-1] &= uint8(mask)
	}

	return c.bits.index, int(c.decodedLen), nil
}

func (c *cutter) doStored() error {
	if (c.maxEncodedLen - c.bits.index) < 4 {
		return errInternalNoProgress
	}
	length := uint32(c.bits.bytes[c.bits.index+0]) | uint32(c.bits.bytes[c.bits.index+1])<<8
	invLen := uint32(c.bits.bytes[c.bits.index+2]) | uint32(c.bits.bytes[c.bits.index+3])<<8
	if length+invLen != 0xFFFF {
		return errInvalidBadBlockLength
	}

	// Check for potential overflow.
	if (c.decodedLen + int32(length)) < 0 {
		return errInternalNoProgress
	}

	index := c.bits.index + 4
	if remaining := c.maxEncodedLen - index; remaining >= int(length) {
		c.bits.index = index + int(length)
		c.bits.bits = 0
		c.bits.nBits = 0
		c.decodedLen += int32(length)
		return nil
	} else if remaining == 0 {
		return errInternalNoProgress
	} else {
		length = uint32(remaining)
		invLen = 0xFFFF - length
	}

	c.bits.bytes[c.bits.index+0] = uint8(length >> 0)
	c.bits.bytes[c.bits.index+1] = uint8(length >> 8)
	c.bits.bytes[c.bits.index+2] = uint8(invLen >> 0)
	c.bits.bytes[c.bits.index+3] = uint8(invLen >> 8)
	c.bits.index = index + int(length)
	c.bits.bits = 0
	c.bits.nBits = 0
	c.decodedLen += int32(length)
	return errInternalSomeProgress
}

func (c *cutter) doStaticHuffman() error {
	const (
		numLCodes = 288
		numDCodes = 32
	)

	// Initialize lengths as per RFC 1951 section 3.2.6.
	lengths := make([]uint32, numLCodes+numDCodes)
	i := 0
	for ; i < 144; i++ {
		lengths[i] = 8
	}
	for ; i < 256; i++ {
		lengths[i] = 9
	}
	for ; i < 280; i++ {
		lengths[i] = 7
	}
	for ; i < 288; i++ {
		lengths[i] = 8
	}
	for ; i < 320; i++ {
		lengths[i] = 5
	}

	return c.doHuffman(lengths[:numLCodes], lengths[numLCodes:])
}

func (c *cutter) doDynamicHuffman() error {
	numLCodes := 257 + c.bits.take(5)
	if numLCodes < 0 {
		return errInvalidNotEnoughData
	}

	numDCodes := 1 + c.bits.take(5)
	if numDCodes < 0 {
		return errInvalidNotEnoughData
	}

	numCodeLengths := 4 + c.bits.take(4)
	if numCodeLengths < 0 {
		return errInvalidNotEnoughData
	}

	// The 286 and 30 numbers come from RFC 1951 section 3.2.5.
	if (numLCodes > 286) || (numDCodes > 30) {
		return errInvalidTooManyCodes
	}

	lengths := make([]uint32, numLCodes+numDCodes)
	for i := int32(0); i < numCodeLengths; i++ {
		x := c.bits.take(3)
		if x < 0 {
			return errInvalidNotEnoughData
		}
		lengths[codeOrder[i]] = uint32(x)
	}

	if _, _, err := c.lHuff.construct(lengths); err != nil {
		return err
	}

	for i := int32(0); i < (numLCodes + numDCodes); {
		symbol := c.lHuff.decode(&c.bits)
		if symbol < 0 {
			return errInvalidBadCodeLengths
		}
		value, count := uint32(0), int32(0)

		switch symbol {
		default:
			lengths[i] = uint32(symbol)
			i++
			continue

		case 16:
			if i == 0 {
				return errInvalidBadCodeLengths
			}
			value = lengths[i-1]
			count = 3 + c.bits.take(2)

		case 17:
			count = 3 + c.bits.take(3)

		case 18:
			count = 11 + c.bits.take(7)
		}
		if count < 0 {
			return errInvalidNotEnoughData
		}

		if (i + count) > (numLCodes + numDCodes) {
			return errInvalidBadCodeLengths
		}
		for ; count > 0; count-- {
			lengths[i] = value
			i++
		}
	}

	return c.doHuffman(lengths[:numLCodes], lengths[numLCodes:])
}

func (c *cutter) doHuffman(lLengths []uint32, dLengths []uint32) error {
	err := error(nil)
	if c.endCodeBits, c.endCodeNBits, err = c.lHuff.construct(lLengths); err != nil {
		return err
	}
	if c.endCodeNBits == 0 {
		return errInvalidNoEndOfBlock
	}
	if _, _, err := c.dHuff.construct(dLengths); err != nil {
		return err
	}

	if c.bits.index > c.maxEncodedLen {
		return errInternalNoProgress
	}

	checkpointIndex := -1
	checkpointNBits := uint32(0)
	decodedLen := c.decodedLen

	for {
		lSymbol := c.lHuff.decode(&c.bits)
		if lSymbol < 0 {
			return errInvalidBadSymbol

		} else if lSymbol < 256 {
			// It's a literal byte.
			decodedLen++

		} else if lSymbol > 256 {
			// It's a length/distance copy.
			length := lBases[lSymbol-256] + c.bits.take(lExtras[lSymbol-256])
			if length < 0 {
				if lBases[lSymbol-256] < 0 {
					return errInvalidBadSymbol
				}
				return errInvalidNotEnoughData
			}

			dSymbol := c.dHuff.decode(&c.bits)
			if dSymbol < 0 {
				return errInvalidBadSymbol
			}
			distance := dBases[dSymbol] + c.bits.take(dExtras[dSymbol])
			if distance < 0 {
				if dBases[dSymbol] < 0 {
					return errInvalidBadSymbol
				}
				return errInvalidNotEnoughData
			}

			decodedLen += length

		} else {
			// It's the end-of-block.
			return nil
		}

		// Check for overflow.
		if decodedLen < 0 {
			break
		}

		// Check the maxEncodedLen budget, considering that we might still need
		// to write an end-of-block code.
		encodedBits := 8*uint64(c.bits.index) - uint64(c.bits.nBits)
		maxEncodedBits := 8 * uint64(c.maxEncodedLen)
		if encodedBits+uint64(c.endCodeNBits) > maxEncodedBits {
			break
		}

		checkpointIndex = c.bits.index
		checkpointNBits = c.bits.nBits
		c.decodedLen = decodedLen
	}

	if checkpointIndex < 0 {
		return errInternalNoProgress
	}
	c.bits.index = checkpointIndex
	c.bits.nBits = checkpointNBits
	c.writeEndCode()
	return errInternalSomeProgress
}

func (c *cutter) writeEndCode() {
	// Change c.bits.bytes[c.bits.index-1:]'s bits to have the end-of-block
	// code. That code's bits are given MSB-to-LSB but the wire format reads
	// LSB-to-MSB.
	for j := c.endCodeNBits; j > 0; j-- {
		if c.bits.nBits == 0 {
			c.bits.index++
			c.bits.nBits = 8
		}
		c.bits.nBits--

		// Set the n'th bit (LSB=0, MSB=7) of c.bits.bytes[c.bits.index-1] to
		// be b.
		n := 7 - c.bits.nBits
		b := (c.endCodeBits >> (j - 1)) & 1
		mask := uint32(1) << n
		c.bits.bytes[c.bits.index-1] &^= uint8(mask)
		c.bits.bytes[c.bits.index-1] |= uint8(mask * b)
	}
}
