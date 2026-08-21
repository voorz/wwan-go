//go:build linux

package ccid

import (
	"context"
	"errors"
	"fmt"
)

// This file implements Extended APDU chaining for the built-in USBFS CCID driver.
//
// Extended APDU allows sending/receiving APDUs larger than the reader's
// maxMessageLength by chaining multiple CCID TransferBlock commands.
// The chaining is controlled by the wLevelParameter field (bytes 8-9 of the
// CCID message header):
//
//   0x00 = single block (APDU begins and ends in this block)
//   0x01 = first block (APDU begins here, continues in next block)
//   0x02 = last block (APDU ends here)
//   0x03 = middle block (APDU continues, more blocks follow)
//   0x10 = empty block (request continuation of response APDU)
//
// This is the Go equivalent of libccid's CmdXfrBlockAPDU_extended (commands.c).
// It is used when the reader advertises CCID_CLASS_EXTENDED_APDU (0x00040000)
// and the APDU length exceeds the reader's maxMessageLength - CCID_HEADER_SIZE.

// CCID chain parameter constants (wLevelParameter).
const (
	ccidChainSingle  uint8 = 0x00 // APDU begins and ends
	ccidChainFirst   uint8 = 0x01 // APDU begins, continues
	ccidChainLast    uint8 = 0x02 // APDU ends
	ccidChainMiddle  uint8 = 0x03 // APDU continues, more follows
	ccidChainRequest uint8 = 0x10 // Empty block, request response continuation
)

// transmitExtendedAPDU sends an APDU that may exceed the reader's maxMessageLength
// by chaining multiple TransferBlock commands.
//
// Send flow:
//  1. If APDU fits in one block: send with chain=0x00, receive response.
//  2. If APDU is larger:
//     a. Send first chunk with chain=0x01
//     b. Receive a null block (acknowledgment)
//     c. Send middle chunks with chain=0x03
//     d. Send last chunk with chain=0x02
//  3. Receive response (may also be chained).
//
// Receive flow:
//  1. Receive first response block (chain=0x00 or 0x01 or 0x02 or 0x03)
//  2. If chain=0x00 or 0x02: done
//  3. If chain=0x01 or 0x03: send empty block (chain=0x10) to request next
//  4. Receive next response block, repeat
func (r *usbfsReader) transmitExtendedAPDU(ctx context.Context, request []byte) ([]byte, error) {
	if r.closed || r.file == nil {
		return nil, errors.New("built-in CCID reader is closed")
	}
	if !r.active {
		return nil, errors.New("built-in CCID card is inactive")
	}
	if len(request) == 0 {
		return nil, errors.New("APDU request is empty")
	}

	maxPayload := int(r.device.descriptor.maxMessageLength) - ccidHeaderLength
	if maxPayload < 1 {
		return nil, errors.New("reader maxMessageLength is too small")
	}

	// --- Send phase ---
	sent := 0
	chainParam := ccidChainSingle

	// Determine if we need chaining.
	remaining := len(request)
	if remaining > maxPayload {
		chainParam = ccidChainFirst
	}

	for {
		chunkLen := remaining
		if chainParam == ccidChainFirst || chainParam == ccidChainMiddle {
			if chunkLen > maxPayload {
				chunkLen = maxPayload
				if chainParam == ccidChainFirst {
					chainParam = ccidChainFirst // stays first, next will be middle/last
				}
			} else {
				chainParam = ccidChainLast
			}
		}

		// Determine the actual chain parameter for this iteration.
		var actualChain uint8
		if sent == 0 && remaining <= maxPayload {
			// Single block.
			actualChain = ccidChainSingle
			chunkLen = remaining
		} else if sent == 0 {
			// First block.
			actualChain = ccidChainFirst
			chunkLen = maxPayload
		} else if remaining-chunkLen > 0 && chunkLen >= maxPayload {
			// Middle block.
			actualChain = ccidChainMiddle
			chunkLen = maxPayload
		} else {
			// Last block.
			actualChain = ccidChainLast
			chunkLen = remaining
		}

		// Send the chunk.
		chunk := request[sent : sent+chunkLen]
		resp, err := r.command(ctx, ccidTransferBlock, 0, actualChain, 0, chunk, ccidDataBlock)
		if err != nil {
			if errors.Is(err, ErrCardNotPresent) {
				r.active = false
			}
			return nil, fmt.Errorf("extended APDU send (chain=0x%02X): %w", actualChain, err)
		}

		sent += chunkLen
		remaining -= chunkLen

		// If this was the last or single block, process the response.
		if actualChain == ccidChainLast || actualChain == ccidChainSingle {
			// The response may itself be chained.
			return r.receiveExtendedResponse(ctx, resp)
		}

		// We just sent a first or middle block. The reader should respond
		// with a null block (acknowledgment). We already received it in the
		// command() call above — check that it's empty.
		if len(resp.data) != 0 {
			// Non-empty response to a non-terminal chain block is unexpected.
			return nil, fmt.Errorf("extended APDU: unexpected non-empty response to chain block (len=%d)", len(resp.data))
		}

		// Continue sending the next chunk.
	}

}

// receiveExtendedResponse handles the potentially chained response from the reader.
// The first response block has already been received in the send phase.
func (r *usbfsReader) receiveExtendedResponse(ctx context.Context, firstResp ccidResponse) ([]byte, error) {
	var result []byte
	result = append(result, firstResp.data...)

	// Check the chain parameter of the first response.
	chain := firstResp.chain

	for chain == ccidChainFirst || chain == ccidChainMiddle || chain == ccidChainRequest {
		// Send an empty block to request the next response chunk.
		resp, err := r.command(ctx, ccidTransferBlock, 0, ccidChainRequest, 0, nil, ccidDataBlock)
		if err != nil {
			return nil, fmt.Errorf("extended APDU receive continuation: %w", err)
		}
		result = append(result, resp.data...)
		chain = resp.chain
	}

	// chain should be 0x00 (single) or 0x02 (last) at this point.
	if chain != ccidChainSingle && chain != ccidChainLast {
		return nil, fmt.Errorf("extended APDU: unexpected final chain parameter 0x%02X", chain)
	}

	return result, nil
}
