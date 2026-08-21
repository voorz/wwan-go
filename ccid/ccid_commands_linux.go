//go:build linux

package ccid

import (
	"context"
	"fmt"
)

// This file implements additional CCID commands that are not part of the
// core T=0/T=1 transmission path but are defined by the USB CCID spec.
//
// These commands are used for:
//   - Reader-level control (Abort, Mechanical, IccClock)
//   - Parameter management (GetParameters, ResetParameters)
//   - Data rate configuration (SetDataRateAndClockFrequency)
//   - T=0 specific APDU handling (T0APDU)
//   - Event notification (NotifySlotChange, HardwareError)

// CCID command and response message types for additional commands.
const (
	ccidGetParameters    uint8 = 0x6C // PC_to_RDR_GetParameters
	ccidResetParameters  uint8 = 0x6D // PC_to_RDR_ResetParameters
	ccidIccClock         uint8 = 0x6E // PC_to_RDR_IccClock
	ccidT0APDU           uint8 = 0x6A // PC_to_RDR_T0APDU
	ccidMechanical       uint8 = 0x71 // PC_to_RDR_Mechanical
	ccidAbort            uint8 = 0x72 // PC_to_RDR_Abort
	ccidSetDataRate      uint8 = 0x73 // PC_to_RDR_SetDataRateAndClockFrequency
	ccidNotifySlotChange uint8 = 0x50 // RDR_to_PC_NotifySlotChange (interrupt endpoint)
	ccidHardwareError    uint8 = 0x51 // RDR_to_PC_HardwareError (interrupt endpoint)
	ccidEscapeResponse   uint8 = 0x83 // RDR_to_PC_Escape
	ccidDataRateResponse uint8 = 0x84 // RDR_to_PC_DataRateAndClockFrequency
)

// --- Abort (0x72) ---

// abort sends a PC_to_RDR_Abort command to cancel a pending TransferBlock.
// The sequence number identifies which command to abort.
// This is rarely needed since timeouts handle cancellation, but is provided
// for CCID spec completeness.
func (r *usbfsReader) abort(ctx context.Context, sequence uint8) error {
	resp, err := r.command(ctx, ccidAbort, sequence, 0, 0, nil, ccidSlotStatus)
	if err != nil {
		return fmt.Errorf("abort: %w", err)
	}
	_ = resp
	return nil
}

// --- Mechanical (0x71) ---

// mechanical sends a PC_to_RDR_Mechanical command for reader-specific
// mechanical operations (e.g., eject card, capture card).
// The function code is reader-specific.
func (r *usbfsReader) mechanical(ctx context.Context, function uint8) error {
	resp, err := r.command(ctx, ccidMechanical, function, 0, 0, nil, ccidSlotStatus)
	if err != nil {
		return fmt.Errorf("mechanical: %w", err)
	}
	_ = resp
	return nil
}

// --- IccClock (0x6E) ---

// IccClockAction defines clock operations.
const (
	ccidClockStop    uint8 = 0x00 // Stop clock
	ccidClockStart   uint8 = 0x01 // Start clock
	ccidClockRestart uint8 = 0x02 // Restart clock with specific frequency
)

// iccClock sends a PC_to_RDR_IccClock command to control the card clock.
// action: 0=stop, 1=start, 2=restart with frequency in parameter1.
func (r *usbfsReader) iccClock(ctx context.Context, action uint8, frequency uint8) error {
	resp, err := r.command(ctx, ccidIccClock, action, frequency, 0, nil, ccidSlotStatus)
	if err != nil {
		return fmt.Errorf("iccClock: %w", err)
	}
	_ = resp
	return nil
}

// --- SetDataRateAndClockFrequency (0x73) ---

// setDataRate sends a PC_to_RDR_SetDataRateAndClockFrequency command to
// explicitly set the reader's data rate and clock frequency.
// Returns the actual data rate and clock frequency set by the reader.
func (r *usbfsReader) setDataRate(ctx context.Context, dataRate uint32, clockFrequency uint32) (uint32, uint32, error) {
	// Data: dwDataRate (4 bytes LE) + dwClockFrequency (4 bytes LE)
	data := make([]byte, 8)
	data[0] = byte(dataRate)
	data[1] = byte(dataRate >> 8)
	data[2] = byte(dataRate >> 16)
	data[3] = byte(dataRate >> 24)
	data[4] = byte(clockFrequency)
	data[5] = byte(clockFrequency >> 8)
	data[6] = byte(clockFrequency >> 16)
	data[7] = byte(clockFrequency >> 24)

	resp, err := r.command(ctx, ccidSetDataRate, 0, 0, 0, data, ccidDataRateResponse)
	if err != nil {
		return 0, 0, fmt.Errorf("setDataRate: %w", err)
	}
	if len(resp.data) < 8 {
		return 0, 0, fmt.Errorf("setDataRate: response too short: %d bytes", len(resp.data))
	}
	actualRate := uint32(resp.data[0]) | uint32(resp.data[1])<<8 | uint32(resp.data[2])<<16 | uint32(resp.data[3])<<24
	actualClock := uint32(resp.data[4]) | uint32(resp.data[5])<<8 | uint32(resp.data[6])<<16 | uint32(resp.data[7])<<24
	return actualRate, actualClock, nil
}

// --- GetParameters (0x6C) ---

// getParameters sends a PC_to_RDR_GetParameters command to read the
// current protocol parameters from the reader.
// protocol: 0 = T=0, 1 = T=1
// Returns the raw parameter data structure.
func (r *usbfsReader) getParameters(ctx context.Context, protocol uint8) ([]byte, error) {
	resp, err := r.command(ctx, ccidGetParameters, protocol, 0, 0, nil, ccidParameters)
	if err != nil {
		return nil, fmt.Errorf("getParameters: %w", err)
	}
	return resp.data, nil
}

// --- ResetParameters (0x6D) ---

// resetParameters sends a PC_to_RDR_ResetParameters command to restore
// the reader's protocol parameters to their default values.
// protocol: 0 = T=0, 1 = T=1
// Returns the reset parameter data structure.
func (r *usbfsReader) resetParameters(ctx context.Context, protocol uint8) ([]byte, error) {
	resp, err := r.command(ctx, ccidResetParameters, protocol, 0, 0, nil, ccidParameters)
	if err != nil {
		return nil, fmt.Errorf("resetParameters: %w", err)
	}
	return resp.data, nil
}

// --- T0APDU (0x6A) ---

// t0APDU sends a PC_to_RDR_T0APDU command for T=0 specific APDU handling
// at the TPDU exchange level. This is an alternative to TransferBlock for
// T=0, where the reader firmware handles procedure bytes.
// mrbwi: Maximum retries and BWI (byte 7)
// expectedLength: expected response length (bytes 8-9, wLevelParameter)
func (r *usbfsReader) t0APDU(ctx context.Context, data []byte, mrbwi uint8, expectedLength uint16) ([]byte, error) {
	resp, err := r.command(ctx, ccidT0APDU, mrbwi, uint8(expectedLength&0xFF), uint8((expectedLength>>8)&0xFF), data, ccidDataBlock)
	if err != nil {
		return nil, fmt.Errorf("t0APDU: %w", err)
	}
	return resp.data, nil
}

// --- NotifySlotChange (0x50) ---

// parseNotifySlotChange parses a RDR_to_PC_NotifySlotChange interrupt message.
// Each slot uses 2 bits: bit0 = card present (changed), bit1 = card present.
// For a 1-slot reader, the first byte contains:
//
//	bit 0: slot 0 card present changed
//	bit 1: slot 0 card present
//	bits 2-7: additional slots (if any)
//
// This is received via the interrupt IN endpoint, not the bulk IN endpoint.
// VoHive currently uses ping() (GetSlotStatus) polling instead, but this
// parser is provided for future interrupt-driven card event handling.
func parseNotifySlotChange(data []byte, slotCount int) []bool {
	if len(data) < 1 {
		return nil
	}
	changed := make([]bool, slotCount)
	for i := 0; i < slotCount && i < 4; i++ {
		// Each slot uses 2 bits. First slot in bits 0-1, second in bits 2-3, etc.
		changed[i] = data[0]&(1<<(uint(i)*2)) != 0
	}
	// Additional slots are in subsequent bytes.
	for i := 4; i < slotCount && i/4 < len(data); i++ {
		byteIdx := i / 4
		bitIdx := uint(i%4) * 2
		changed[i] = data[byteIdx]&(1<<bitIdx) != 0
	}
	return changed
}

// --- HardwareError (0x51) ---

// HardwareErrorInfo describes a hardware error notification from the reader.
type HardwareErrorInfo struct {
	Slot     uint8 // bSlot
	ErrorSeq uint8 // bSeq
	Error    uint8 // bHardwareErrorCode
}

// parseHardwareError parses a RDR_to_PC_HardwareError interrupt message.
// Format: [bMessage type=0x51] [bSlot] [bSeq] [bHardwareErrorCode] [abRFU 3 bytes]
func parseHardwareError(data []byte) (*HardwareErrorInfo, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("hardware error message too short: %d bytes", len(data))
	}
	return &HardwareErrorInfo{
		Slot:     data[1],
		ErrorSeq: data[2],
		Error:    data[3],
	}, nil
}
