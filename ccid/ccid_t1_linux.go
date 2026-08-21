//go:build linux

package ccid

import (
	"context"
	"errors"
	"fmt"
)

// This file implements T=1 protocol support for the built-in USBFS CCID driver.
//
// T=1 is a block-oriented transmission protocol defined in ISO 7816-3.
// Unlike T=0 (which is byte/character oriented), T=1 transmits data in
// structured blocks with error detection (LRC or CRC) and retransmission.
//
// Block format: [NAD] [PCB] [LEN] [INF...] [EDC]
//   - NAD: Node Address (source/destination)
//   - PCB: Protocol Control Byte (block type + sequence)
//   - LEN: Information field length (0-254)
//   - INF: Information field (data)
//   - EDC: Error Detection Code (LRC: 1 byte, CRC: 2 bytes)
//
// Block types:
//   - I-block: Information block (carries data, has sequence number)
//   - R-block: Receive-ready block (acknowledgment, has sequence number)
//   - S-block: Supervisory block (control: RESYNC, IFS, ABORT, WTX)
//
// This implementation follows the structure of libccid's openct/proto-t1.c
// but is currently a placeholder — full T=1 support requires:
//   - T=1 state machine (SENDING/RECEIVING/RESYNCH/DEAD states)
//   - Block building and parsing (I/R/S blocks)
//   - Sequence number management (ns/nr)
//   - IFSC/IFSD negotiation (S-block IFS exchange)
//   - LRC and CRC checksum computation
//   - Retransmission with ISO 7816-3 Rules 7.x
//   - BWT (Block Waiting Time) and CWT (Character Waiting Time) computation
//   - RESYNC mechanism (max 3 resyncs before giving up)
//
// TODO: Implement full T=1 protocol when a T=1 card needs to be supported.
//       Current VoHive use case is eSIM (T=0 only), so T=1 is deferred.

// T=1 block type constants.
const (
	t1BlockI uint8 = 0x00 // I-block: bit 7=0
	t1BlockR uint8 = 0x80 // R-block: bit 7=1, bit 6=0
	t1BlockS uint8 = 0xC0 // S-block: bit 7=1, bit 6=1
)

// T=1 S-block types.
const (
	t1SResync uint8 = 0x00
	t1SIFS    uint8 = 0x01
	t1SAbort  uint8 = 0x02
	t1SWTX    uint8 = 0x03
)

// T=1 S-block response flag.
const t1SResponse uint8 = 0x20

// T=1 state machine states.
type t1State int

const (
	t1StateSending t1State = iota
	t1StateReceiving
	t1StateResync
	t1StateDead
)

// t1Protocol encapsulates the T=1 protocol state.
type t1Protocol struct {
	state   t1State
	ns      uint8 // send sequence number (0 or 1)
	nr      uint8 // receive sequence number (0 or 1)
	ifsc    uint8 // information field size (card → host)
	ifsd    uint8 // information field size (host → card)
	wtx     uint8 // waiting time extension multiplier
	retries int
	rcBytes int  // checksum bytes: 1 (LRC) or 2 (CRC)
	useCRC  bool // true: CRC, false: LRC
	nad     uint8
}

// newT1Protocol creates a T=1 protocol handler with default parameters.
// Defaults aligned with libccid: IFSC=32, IFSD=32, retries=3, LRC checksum.
func newT1Protocol() *t1Protocol {
	return &t1Protocol{
		state:   t1StateSending,
		ifsc:    32,
		ifsd:    32,
		retries: 3,
		rcBytes: 1, // LRC by default
		nad:     0,
	}
}

// ErrT1NotImplemented is returned when T=1 protocol is requested but not yet implemented.
var ErrT1NotImplemented = errors.New("T=1 protocol support is not yet implemented in the built-in CCID driver")

// transmitT1 would perform a T=1 APDU exchange.
// This is a placeholder — the full T=1 state machine is not yet implemented.
func (r *usbfsReader) transmitT1(ctx context.Context, request []byte) ([]byte, error) {
	_ = ctx
	_ = request
	return nil, ErrT1NotImplemented
}

// t1ComputeLRC computes the LRC (Longitudinal Redundancy Check) for a T=1 block.
// LRC is the XOR of all bytes from NAD to the last INF byte.
func t1ComputeLRC(block []byte) byte {
	var lrc byte
	for _, b := range block {
		lrc ^= b
	}
	return lrc
}

// t1BuildIBlock builds an I-block for sending.
// Returns the complete block including NAD, PCB, LEN, INF, and EDC.
func t1BuildIBlock(t1 *t1Protocol, data []byte, more bool) []byte {
	// NAD
	nad := t1.nad
	// PCB: I-block, sequence number, more-data flag
	pcb := byte(t1BlockI)
	pcb |= (t1.ns << 6) // sequence number in bit 6
	if more {
		pcb |= 0x20 // M-bit (more data)
	}
	// LEN
	length := byte(len(data))
	// Build block without EDC
	block := []byte{nad, pcb, length}
	block = append(block, data...)
	// Append EDC (LRC or CRC)
	if t1.useCRC {
		crc := t1ComputeCRC(block)
		block = append(block, byte(crc&0xFF), byte((crc>>8)&0xFF))
	} else {
		block = append(block, t1ComputeLRC(block))
	}
	return block
}

// t1ComputeCRC computes the CRC-32 checksum for a T=1 block.
// CRC uses the polynomial 0x8408 (reversed CRC-CCITT).
func t1ComputeCRC(block []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range block {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&0x0001 != 0 {
				crc = (crc >> 1) ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

// t1ParseBlock parses a T=1 block and returns its type, sequence number, and data.
func t1ParseBlock(block []byte) (blockType uint8, seq uint8, data []byte, err error) {
	if len(block) < 4 { // minimum: NAD + PCB + LEN + EDC (LRC)
		return 0, 0, nil, fmt.Errorf("T=1 block too short: %d bytes", len(block))
	}
	pcb := block[1]
	length := int(block[2])
	if len(block) < 3+length+1 { // NAD+PCB+LEN+data+EDC(LRC)
		return 0, 0, nil, fmt.Errorf("T=1 block truncated: declared %d data bytes but only %d available", length, len(block)-4)
	}
	data = block[3 : 3+length]
	// Determine block type
	switch {
	case pcb&0x80 == 0:
		blockType = t1BlockI
		seq = (pcb >> 6) & 0x01
	case pcb&0xC0 == 0x80:
		blockType = t1BlockR
		seq = (pcb >> 4) & 0x01
	default:
		blockType = t1BlockS
	}
	return blockType, seq, data, nil
}

// t1BuildRBlock builds an R-block for acknowledgment.
// seq is the expected I-block sequence number.
func t1BuildRBlock(t1 *t1Protocol, seq uint8, errorType uint8) []byte {
	nad := t1.nad
	pcb := byte(t1BlockR)
	pcb |= (seq << 4)               // sequence number in bit 4
	pcb |= errorType                // error bits (0 = OK, 1 = EDC error, 2 = other error)
	block := []byte{nad, pcb, 0x00} // LEN=0 for R-block
	// Append EDC
	if t1.useCRC {
		crc := t1ComputeCRC(block)
		block = append(block, byte(crc&0xFF), byte((crc>>8)&0xFF))
	} else {
		block = append(block, t1ComputeLRC(block))
	}
	return block
}

// t1BuildSBlock builds an S-block for supervision.
// sType is one of t1SResync/t1SIFS/t1SAbort/t1SWTX.
// data is the S-block data (e.g., new IFS value for IFS, WTX multiplier for WTX).
func t1BuildSBlock(t1 *t1Protocol, sType uint8, data []byte, isResponse bool) []byte {
	nad := t1.nad
	pcb := byte(t1BlockS)
	pcb |= sType
	if isResponse {
		pcb |= t1SResponse
	}
	length := byte(len(data))
	block := []byte{nad, pcb, length}
	block = append(block, data...)
	if t1.useCRC {
		crc := t1ComputeCRC(block)
		block = append(block, byte(crc&0xFF), byte((crc>>8)&0xFF))
	} else {
		block = append(block, t1ComputeLRC(block))
	}
	return block
}

// t1VerifyChecksum verifies the checksum of a T=1 block.
func t1VerifyChecksum(t1 *t1Protocol, block []byte) bool {
	if t1.useCRC {
		if len(block) < 2 {
			return false
		}
		data := block[:len(block)-2]
		crc := t1ComputeCRC(data)
		expected := uint16(block[len(block)-2]) | (uint16(block[len(block)-1]) << 8)
		return crc == expected
	}
	// LRC
	if len(block) < 1 {
		return false
	}
	lrc := t1ComputeLRC(block[:len(block)-1])
	return lrc == block[len(block)-1]
}
