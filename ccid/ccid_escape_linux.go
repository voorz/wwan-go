//go:build linux

package ccid

import (
	"context"
	"fmt"
)

// CCID Escape command message types (USB CCID spec 1.10).
const (
	ccidEscapeCmd  = 0x6B // PC_to_RDR_Escape
	ccidEscapeResp = 0x83 // RDR_to_PC_Escape
)

// escape sends a CCID Escape command to the reader (not the card).
//
// The Escape command is used to communicate with the reader itself rather than
// the card. Typical uses include:
//   - Querying reader firmware features (Gemalto ESC 0x6A)
//   - Setting reader-specific modes (e.g. ISO/APDU mode on GemPC)
//   - Firmware upgrade operations
//
// The data format is reader-specific and not defined by the CCID specification.
// Returns the raw response data from the reader.
func (r *usbfsReader) escape(ctx context.Context, data []byte) ([]byte, error) {
	if r.closed || r.file == nil {
		return nil, fmt.Errorf("escape: reader is closed")
	}
	maxPayload := int(r.device.descriptor.maxMessageLength) - ccidHeaderLength
	if len(data) > maxPayload {
		return nil, fmt.Errorf("escape: data length %d exceeds reader limit %d", len(data), maxPayload)
	}
	response, err := r.command(ctx, ccidEscapeCmd, 0, 0, 0, data, ccidEscapeResp)
	if err != nil {
		return nil, fmt.Errorf("escape command: %w", err)
	}
	return response.data, nil
}
