//go:build linux

package ccid

import (
	"context"
	"errors"
	"fmt"
)

// This file implements the T=1 protocol for the built-in USBFS CCID driver.
//
// T=1 is a block-oriented transmission protocol defined in ISO 7816-3.
// Unlike T=0 (byte/character oriented), T=1 transmits data in structured
// blocks with error detection (LRC or CRC) and retransmission.
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
// This implementation follows the structure of libccid's openct/proto-t1.c,
// including the full state machine with retransmission (ISO 7816-3 Rule 7.x),
// RESYNC mechanism (Rule 6.x), IFSD negotiation, and WTX handling.

// --- Block type constants ---

const (
	t1BlockI uint8 = 0x00 // I-block: bit 7=0
	t1BlockR uint8 = 0x80 // R-block: bit 7=1, bit 6=0
	t1BlockS uint8 = 0xC0 // S-block: bit 7=1, bit 6=1
)

// I-block PCB bits.
const (
	t1MoreBlocks uint8 = 0x20 // M-bit: more data follows
	t1ISeqShift        = 6    // sequence number in bit 6
)

// R-block PCB bits.
const (
	t1REdcError   uint8 = 0x01 // EDC error
	t1ROtherError uint8 = 0x02 // other error
	t1RSeqShift         = 4    // sequence number in bit 4
)

// S-block types.
const (
	t1SResync uint8 = 0x00
	t1SIFS    uint8 = 0x01
	t1SAbort  uint8 = 0x02
	t1SWTX    uint8 = 0x03
)

// S-block response flag.
const t1SResponse uint8 = 0x20

// --- State machine ---

type t1State int

const (
	t1StateSending t1State = iota
	t1StateReceiving
	t1StateResync
	t1StateDead
)

// t1Protocol encapsulates the T=1 protocol state.
type t1Protocol struct {
	state     t1State
	ns        uint8   // send sequence number (0 or 1)
	nr        uint8   // receive sequence number (0 or 1)
	ifsc      uint8   // information field size (card → host)
	ifsd      uint8   // information field size (host → card)
	wtx       uint8   // waiting time extension multiplier
	retries   int     // retransmission retries before RESYNC
	rcBytes   int     // checksum bytes: 1 (LRC) or 2 (CRC)
	useCRC    bool    // true: CRC, false: LRC
	nad       uint8   // node address
	more      bool    // more data to send (I-block M-bit)
	prevBlock [4]byte // last sent block (for retransmission)
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

// --- Block type helpers ---

func t1BlockType(pcb uint8) uint8 {
	switch pcb & 0xC0 {
	case t1BlockR:
		return t1BlockR
	case t1BlockS:
		return t1BlockS
	default:
		return t1BlockI
	}
}

func t1Seq(pcb uint8) uint8 {
	switch pcb & 0xC0 {
	case t1BlockR:
		return (pcb >> t1RSeqShift) & 1
	case t1BlockS:
		return 0
	default:
		return (pcb >> t1ISeqShift) & 1
	}
}

func t1IsSError(pcb uint8) uint8 {
	return pcb & 0x0F
}

func t1SIsResponse(pcb uint8) bool {
	return pcb&t1SResponse != 0
}

func t1SType(pcb uint8) uint8 {
	return pcb & 0x0F
}

func swapNibbles(x uint8) uint8 {
	return (x >> 4) | ((x & 0xF) << 4)
}

// --- Checksum ---

func t1ComputeLRC(block []byte) byte {
	var lrc byte
	for _, b := range block {
		lrc ^= b
	}
	return lrc
}

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

func (t1 *t1Protocol) computeChecksum(block []byte, offset int) int {
	if t1.useCRC {
		crc := t1ComputeCRC(block[:offset])
		block[offset] = byte(crc & 0xFF)
		block[offset+1] = byte((crc >> 8) & 0xFF)
		return offset + 2
	}
	block[offset] = t1ComputeLRC(block[:offset])
	return offset + 1
}

func (t1 *t1Protocol) verifyChecksum(block []byte, length int) bool {
	m := length - t1.rcBytes
	if m < 0 {
		return false
	}
	if t1.useCRC {
		crc := t1ComputeCRC(block[:m])
		expected := uint16(block[m]) | (uint16(block[m+1]) << 8)
		return crc == expected
	}
	lrc := t1ComputeLRC(block[:m])
	return lrc == block[m]
}

// --- Block building ---

// t1Build builds a T=1 block and returns the complete block including EDC.
// If bp has more data than ifsc, the M-bit is set and only ifsc bytes are sent.
// For R-blocks, bp should be nil (no data).
// For S-blocks, bp contains the S-block data (0 or 1 byte).
// lastSend returns how many bytes were consumed from bp.
func (t1 *t1Protocol) buildBlock(dad uint8, pcb byte, data []byte) ([]byte, int) {
	length := len(data)
	more := false
	if t1BlockType(pcb) == t1BlockI && length > int(t1.ifsc) {
		pcb |= t1MoreBlocks
		length = int(t1.ifsc)
		more = true
	}

	// Add sequence number.
	switch t1BlockType(pcb) {
	case t1BlockR:
		pcb |= t1.nr << t1RSeqShift
	case t1BlockI:
		pcb |= t1.ns << t1ISeqShift
		t1.more = more
	}

	block := make([]byte, 3+length+t1.rcBytes)
	block[0] = dad
	block[1] = pcb
	block[2] = byte(length)
	if length > 0 {
		copy(block[3:], data[:length])
	}

	totalLen := t1.computeChecksum(block, 3+length)
	block = block[:totalLen]

	// Memorize the last sent block (only first 4 bytes for R-block retransmission).
	copy(t1.prevBlock[:4], block)

	return block, length
}

// rebuildBlock rebuilds the last sent R-block for retransmission (ISO 7816-3 Rule 7.2).
func (t1 *t1Protocol) rebuildBlock() []byte {
	pcb := t1.prevBlock[1]
	if t1BlockType(pcb) != t1BlockR {
		return nil
	}
	block := make([]byte, 3+t1.rcBytes)
	copy(block[:4], t1.prevBlock[:4])
	if t1.rcBytes == 2 {
		// Recompute CRC
		block = block[:3]
		block = append(block, 0, 0)
		t1.computeChecksum(block, 3)
	} else {
		block[3] = t1ComputeLRC(block[:3])
	}
	return block
}

// --- Transmit/Receive (CCID level) ---

// t1Xcv sends a block and receives the response via CCID TransferBlock.
// Returns the received block and its length.
// Returns error (-2 equivalent: parity error) if the CCID response indicates
// a parity/EDC error.
func (r *usbfsReader) t1Xcv(ctx context.Context, t1 *t1Protocol, block []byte) ([]byte, error) {
	resp, err := r.command(ctx, ccidTransferBlock, t1.wtx, 0, 0, block, ccidDataBlock)
	if err != nil {
		// Check for parity error (CCID status indicates ICC parity error)
		if cmdErr, ok := err.(*ccidCommandError); ok {
			if cmdErr.errorCode == 0x0B { // ICC parity error
				return nil, errParity
			}
		}
		return nil, err
	}
	return resp.data, nil
}

var errParity = errors.New("parity error")

// --- Transceive (full state machine) ---

// transmitT1 performs a complete T=1 APDU exchange.
// This is the Go equivalent of libccid's t1_transceive.
func (r *usbfsReader) transmitT1(ctx context.Context, request []byte) ([]byte, error) {
	if len(request) == 0 {
		return nil, errors.New("T=1: empty APDU")
	}

	t1 := r.t1
	if t1 == nil || t1.state == t1StateDead {
		return nil, errors.New("T=1: state machine is DEAD, reset the card first")
	}

	dad := swapNibbles(t1.nad)
	t1.state = t1StateSending
	retries := t1.retries
	resyncs := 3

	// Send/receive buffers.
	sndBuf := request
	var rcvBuf []byte
	lastSend := 0 // bytes consumed from sndBuf in last I-block

	// Send the first I-block.
	var data []byte
	if len(sndBuf) > int(t1.ifsc) {
		data = sndBuf[:t1.ifsc]
	} else {
		data = sndBuf
	}
	block, consumed := t1.buildBlock(dad, t1BlockI, data)
	lastSend = consumed

	for {
		retries--

		recvBlock, err := r.t1Xcv(ctx, t1, block)
		if err != nil {
			if errors.Is(err, errParity) {
				// ISO 7816-3 Rule 7.4.2
				if retries <= 0 {
					goto resync
				}
				// ISO 7816-3 Rule 7.2
				if t1BlockType(t1.prevBlock[1]) == t1BlockR {
					block = t1.rebuildBlock()
					continue
				}
				block, _ = t1.buildBlock(dad, t1BlockR|t1REdcError, nil)
				continue
			}
			// Fatal transmit/receive error.
			t1.state = t1StateDead
			return nil, fmt.Errorf("T=1: transmit/receive failed: %w", err)
		}

		// Validate NAD and LEN.
		if len(recvBlock) < 4 { // minimum: NAD+PCB+LEN+EDC(LRC)
			goto invalidBlock
		}
		recvNAD := recvBlock[0]
		recvLEN := recvBlock[2]
		if recvNAD != swapNibbles(dad) || recvLEN == 0xFF {
			// Wrong NAD or illegal length.
			if retries <= 0 {
				goto resync
			}
			if t1BlockType(t1.prevBlock[1]) == t1BlockR {
				block = t1.rebuildBlock()
				continue
			}
			block, _ = t1.buildBlock(dad, t1BlockR|t1ROtherError, nil)
			continue
		}

		// Verify checksum.
		if !t1.verifyChecksum(recvBlock, len(recvBlock)) {
			if retries <= 0 {
				goto resync
			}
			if t1BlockType(t1.prevBlock[1]) == t1BlockR {
				block = t1.rebuildBlock()
				continue
			}
			block, _ = t1.buildBlock(dad, t1BlockR|t1REdcError, nil)
			continue
		}

		pcb := recvBlock[1]
		switch t1BlockType(pcb) {

		case t1BlockR:
			// Validate R-block.
			if recvBlock[2] != 0x00 || (pcb&0x20) != 0 {
				// LEN != 0 or b6 set → illegal.
				if retries <= 0 {
					goto resync
				}
				if t1BlockType(t1.prevBlock[1]) == t1BlockR {
					block = t1.rebuildBlock()
					continue
				}
				block, _ = t1.buildBlock(dad, t1BlockR|t1ROtherError, nil)
				continue
			}

			// Check sequence number.
			if t1Seq(pcb) != t1.ns && !t1.more {
				// Wrong sequence number and no more data.
				if retries <= 0 {
					goto resync
				}
				if t1BlockType(t1.prevBlock[1]) == t1BlockR {
					block = t1.rebuildBlock()
					continue
				}
				block, _ = t1.buildBlock(dad, t1BlockR|t1ROtherError, nil)
				continue
			}

			if t1.state == t1StateReceiving {
				// We were receiving, but got an R-block (unexpected).
				if t1BlockType(t1.prevBlock[1]) == t1BlockR {
					if retries <= 0 {
						goto resync
					}
					block = t1.rebuildBlock()
					continue
				}
				// Send a plain R-block ACK.
				block, _ = t1.buildBlock(dad, t1BlockR, nil)
				continue // back to while loop
			}

			// In SENDING state: R-block acknowledges our I-block.
			// If the card requests the next sequence number, our previous
			// block was received successfully.
			if t1Seq(pcb) != t1.ns {
				// Advance send buffer past acknowledged data.
				sndBuf = sndBuf[lastSend:]
				lastSend = 0
				t1.ns ^= 1
			}

			// If there's no more data to send, RESYNC.
			if len(sndBuf) == 0 {
				goto resync
			}

			// Send next I-block with remaining data.
			var data []byte
			if len(sndBuf) > int(t1.ifsc) {
				data = sndBuf[:t1.ifsc]
			} else {
				data = sndBuf
			}
			block, lastSend = t1.buildBlock(dad, t1BlockI, data)

		case t1BlockI:
			// First I-block from card means our last block was received.
			if t1.state == t1StateSending {
				sndBuf = sndBuf[lastSend:]
				lastSend = 0
				t1.ns ^= 1
			}
			t1.state = t1StateReceiving

			// Check expected receive sequence.
			if t1Seq(pcb) != t1.nr {
				if retries <= 0 {
					goto resync
				}
				block, _ = t1.buildBlock(dad, t1BlockR|t1ROtherError, nil)
				continue
			}
			t1.nr ^= 1

			// Append received data.
			dataLen := int(recvBlock[2])
			rcvBuf = append(rcvBuf, recvBlock[3:3+dataLen]...)

			// If no more-data bit, we're done.
			if pcb&t1MoreBlocks == 0 {
				return rcvBuf, nil
			}

			// Send R-block to request next I-block.
			block, _ = t1.buildBlock(dad, t1BlockR, nil)

		case t1BlockS:
			// Handle S-block.
			if t1SIsResponse(pcb) && t1.state == t1StateResync {
				// RESYNC response received (ISO 7816-3 Rule 6.2).
				t1.state = t1StateSending
				lastSend = 0
				resyncs = 3
				retries = t1.retries
				rcvBuf = nil
				// Restart: send first I-block.
				var data []byte
				if len(request) > int(t1.ifsc) {
					data = request[:t1.ifsc]
				} else {
					data = request
				}
				block, lastSend = t1.buildBlock(dad, t1BlockI, data)
				sndBuf = request
				continue
			}

			if t1SIsResponse(pcb) {
				// Unexpected S-block response.
				if retries <= 0 {
					goto resync
				}
				if t1BlockType(t1.prevBlock[1]) == t1BlockR {
					block = t1.rebuildBlock()
					continue
				}
				block, _ = t1.buildBlock(dad, t1BlockR|t1ROtherError, nil)
				continue
			}

			// S-block request from card.
			var sData []byte
			switch t1SType(pcb) {

			case t1SResync:
				// Card should not send RESYNC.
				if recvBlock[2] != 0 {
					block, _ = t1.buildBlock(dad, t1BlockR|t1ROtherError, nil)
					continue
				}
				goto resync

			case t1SAbort:
				if recvBlock[2] != 0 {
					block, _ = t1.buildBlock(dad, t1BlockR|t1ROtherError, nil)
					continue
				}
				// ISO 7816-3 Rule 9: abort.
				return nil, errors.New("T=1: card requested abort")

			case t1SIFS:
				// Card requests new IFSC.
				if recvBlock[2] != 1 {
					block, _ = t1.buildBlock(dad, t1BlockR|t1ROtherError, nil)
					continue
				}
				newIFSC := recvBlock[3]
				if newIFSC == 0 || newIFSC == 255 {
					goto resync
				}
				t1.ifsc = newIFSC
				sData = []byte{newIFSC}

			case t1SWTX:
				// Card requests waiting time extension.
				if recvBlock[2] != 1 {
					block, _ = t1.buildBlock(dad, t1BlockR|t1ROtherError, nil)
					continue
				}
				t1.wtx = recvBlock[3]
				sData = []byte{recvBlock[3]}

			default:
				goto resync
			}

			// Send S-block response.
			block, _ = t1.buildBlock(dad, t1BlockS|t1SResponse|t1SType(pcb), sData)
		}

		// Everything went well, reset retries.
		retries = t1.retries
		continue

	resync:
		// ISO 7816-3 Rule 6.4: max 3 resyncs.
		if resyncs == 0 {
			t1.state = t1StateDead
			return nil, errors.New("T=1: RESYNC limit exceeded")
		}
		resyncs--
		t1.ns = 0
		t1.nr = 0
		t1.state = t1StateResync
		t1.more = false
		retries = 1
		block, _ = t1.buildBlock(dad, t1BlockS|t1SResync, nil)
		continue

	invalidBlock:
		if retries <= 0 {
			goto resync
		}
		if t1BlockType(t1.prevBlock[1]) == t1BlockR {
			block = t1.rebuildBlock()
			continue
		}
		block, _ = t1.buildBlock(dad, t1BlockR|t1ROtherError, nil)
		continue
	}
}

// --- IFSD Negotiation ---

// negotiateIFSD performs an S-block IFS exchange to negotiate the
// host-to-card information field size (IFSD).
// This is the Go equivalent of libccid's t1_negotiate_ifsd.
func (r *usbfsReader) negotiateIFSD(ctx context.Context, t1 *t1Protocol, ifsd uint8) error {
	dad := swapNibbles(t1.nad)
	retries := t1.retries

	sndBuf := []byte{ifsd}

	for {
		retries--
		if retries < 0 {
			t1.state = t1StateDead
			return errors.New("T=1: IFSD negotiation retries exhausted")
		}

		block, _ := t1.buildBlock(dad, t1BlockS|t1SIFS, sndBuf)
		recvBlock, err := r.t1Xcv(ctx, t1, block)
		if err != nil {
			continue // retry on transmit error
		}

		// Validate response.
		if len(recvBlock) < 4+int(t1.rcBytes) {
			continue
		}
		if recvBlock[0] != swapNibbles(dad) {
			continue
		}
		if !t1.verifyChecksum(recvBlock, len(recvBlock)) {
			continue
		}
		if recvBlock[1] != (t1BlockS | t1SResponse | t1SIFS) {
			continue
		}
		if recvBlock[2] != 1 {
			continue
		}
		if recvBlock[3] != ifsd {
			continue
		}

		// Success.
		t1.ifsd = ifsd
		return nil
	}
}

// --- BWT/CWT Timeout Calculation ---

// t1CardTimeout computes the T=1 Block Waiting Time (BWT) in milliseconds.
// BWT = (2^BWI × 960 + 11) × 372 / clock (kHz)
// This is the Go equivalent of libccid's T1_card_timeout.
func (r *usbfsReader) t1CardTimeout(f, d float64, bwi, cwi uint8) uint32 {
	clock := r.device.descriptor.defaultClock
	if clock == 0 {
		clock = 4000 // default 4 MHz
	}
	if f == 0 {
		f = 372 // default
	}

	// BWT = (2^BWI × 960 + 11) × 372 / clock
	// Convert to milliseconds: BWT (ms) = BWT (cycles) / clock (kHz)
	bwtCycles := (float64(uint32(1)<<bwi)*960.0 + 11.0) * 372.0
	bwtMs := bwtCycles / float64(clock)
	if bwtMs < 1000 {
		bwtMs = 1000
	}
	if bwtMs > 60000 {
		bwtMs = 60000
	}

	// Also account for CWT (Character Waiting Time) for receiving.
	// CWT = (2^CWI + 11) × (372 / clock)
	// CWT is per-character, so for a block of IFSC chars: CWT_total = CWT × IFSC
	cwtCycles := (float64(uint32(1)<<cwi) + 11.0) * f
	cwtMs := cwtCycles / float64(clock)
	if cwtMs < 100 {
		cwtMs = 100
	}

	// Total timeout = BWT + CWT × IFSC (rough estimate)
	total := uint32(bwtMs + cwtMs*float64(t1_ifsc_default))
	if total < 5000 {
		total = 5000
	}
	if total > 60000 {
		total = 60000
	}
	return total
}

const t1_ifsc_default = 32

// --- Initialize T=1 on activate ---

// initT1Protocol initializes the T=1 protocol handler from ATR parameters.
// Called from activate() when the card's ATR indicates T=1.
func (r *usbfsReader) initT1Protocol(parsed ccidATR) {
	t1 := newT1Protocol()

	// Check if CRC is required (TA(2) indicates specific mode with CRC).
	// For now, default to LRC (most common).
	// libccid checks the card's ATR for the checksum type.

	// Set IFSC from ATR (TAi of T=1, i>2).
	// Default IFSC = 32 (from libccid t1_set_defaults).
	ifsc := uint8(32) // default
	// TODO: parse IFSC from ATR's TA(i) bytes when T=1 is the protocol.
	// This requires the ATR parser to extract TA(i) for i>2 when T=1.
	t1.ifsc = ifsc

	r.t1 = t1
}
