//go:build linux

package ccid

import (
	"context"
	"errors"
	"fmt"
)

// ccidExchangeCharacter is the CHARACTER exchange level (dwFeatures bits 16-18 = 0).
// At this level the reader does not handle T=0 procedure bytes; the host must
// drive the protocol byte-by-byte via TransferBlock.
const ccidExchangeCharacter uint32 = 0x00000000

// transmitT0Character performs T=0 APDU exchange at the CHARACTER exchange level.
//
// This is the Go equivalent of libccid's CmdXfrBlockCHAR_T0 (commands.c).
// At this level the reader firmware does NOT handle procedure bytes (NULL/ACK/SW1).
// The host must:
//  1. Send the 5-byte command header via TransferBlock.
//  2. Receive one byte (procedure byte).
//  3. Process the procedure byte:
//     - 0x60 (NULL): retry (send empty block, receive next procedure byte).
//     - INS or INS^0x01 (ACK): transfer all remaining data.
//     - INS^0xFF or INS^0xFE (ACK 1 byte): transfer one byte at a time.
//     - 0x6X/0x9X (SW1): read SW2, finish.
//
// This is only used when the reader advertises CCID_CLASS_CHARACTER (exchange
// level = 0). TPDU/ShortAPDU/ExtendedAPDU level readers handle procedure bytes
// in firmware and use the regular transmit() path.
func (r *usbfsReader) transmitT0Character(ctx context.Context, request []byte) ([]byte, error) {
	if len(request) < 4 {
		return nil, fmt.Errorf("T=0 CHARACTER: APDU too short: %d bytes", len(request))
	}

	// Parse the 5-byte command header.
	var cmd [5]byte
	var data []byte
	if len(request) == 4 {
		copy(cmd[:4], request)
		cmd[4] = 0
		data = nil
	} else {
		copy(cmd[:5], request)
		data = request[5:]
	}

	ins := cmd[1]
	// INS must not be 0x6X or 0x9X (ISO 7816-3 8.3.2).
	if (ins&0xF0) == 0x60 || (ins&0xF0) == 0x90 {
		return nil, fmt.Errorf("T=0 CHARACTER: invalid INS 0x%02X", ins)
	}

	// Determine expected response length from Le field.
	expLen := 0
	isReceive := len(request) == 4 || (len(request) == 5)
	if len(request) == 5 {
		expLen = int(request[4]) // Le
	} else if len(request) > 5 {
		// If data present, Lc is at index 4, Le (if present) is at the end.
		lc := int(request[4])
		if len(request) > 5+lc {
			expLen = int(request[5+lc]) // Le field present
		}
	}

	// Send the 5-byte command header.
	if err := r.t0CharTransmit(ctx, cmd[:], 1); err != nil {
		return nil, fmt.Errorf("T=0 CHARACTER: sending command header: %w", err)
	}

	// In receive mode (no data to send), we expect to receive data.
	isRcv := isReceive || (len(data) == 0 && expLen > 0)

	var recvBuf []byte
	var inBuf []byte // leftover data from a previous receive

	for {
		// If we have no buffered procedure byte, receive one.
		if len(inBuf) == 0 {
			resp, err := r.t0CharReceive(ctx, 1)
			if err != nil {
				return nil, fmt.Errorf("T=0 CHARACTER: receiving procedure byte: %w", err)
			}
			inBuf = resp
		}

		if len(inBuf) == 0 {
			return nil, errors.New("T=0 CHARACTER: received empty procedure byte")
		}

		procByte := inBuf[0]

		// NULL byte: wait for next procedure byte.
		if procByte == 0x60 {
			inBuf = nil
			// Send an empty block to request the next procedure byte.
			if err := r.t0CharTransmit(ctx, nil, 1); err != nil {
				return nil, fmt.Errorf("T=0 CHARACTER: sending NULL retry: %w", err)
			}
			continue
		}

		// ACK (all remaining data): INS or INS^0x01.
		if procByte == ins || procByte == (ins^0x01) {
			inBuf = inBuf[1:]
			remain := 0
			if isRcv {
				// Receiving mode: we expect expLen bytes total.
				remain = expLen - len(recvBuf)
			} else {
				// Sending mode: send all remaining data.
				remain = len(data)
			}
			if remain > 0x200 {
				return nil, fmt.Errorf("T=0 CHARACTER: proc_len %d exceeds limit", remain)
			}

			if isRcv {
				// First, drain any leftover bytes from inBuf.
				if len(inBuf) > 0 && len(inBuf) >= remain {
					recvBuf = append(recvBuf, inBuf[:remain]...)
					inBuf = inBuf[remain:]
					// All expected data received, now wait for SW.
					return r.t0CharReceiveSW(ctx, inBuf, recvBuf)
				}
				// Drain leftover, then receive the rest.
				fromBuf := len(inBuf)
				if fromBuf > 0 {
					recvBuf = append(recvBuf, inBuf[:fromBuf]...)
					inBuf = inBuf[fromBuf:]
				}
				need := remain - fromBuf
				if need > 0 {
					if err := r.t0CharTransmit(ctx, nil, uint8(need)); err != nil {
						return nil, fmt.Errorf("T=0 CHARACTER: ACK receive transmit: %w", err)
					}
					resp, err := r.t0CharReceive(ctx, need)
					if err != nil {
						return nil, fmt.Errorf("T=0 CHARACTER: ACK receive: %w", err)
					}
					recvBuf = append(recvBuf, resp...)
				}
				if len(recvBuf) == expLen {
					// All expected data received, now wait for SW.
					return r.t0CharReceiveSW(ctx, inBuf, recvBuf)
				}
			} else {
				// Sending mode: send remaining data.
				if err := r.t0CharTransmit(ctx, data, 1); err != nil {
					return nil, fmt.Errorf("T=0 CHARACTER: ACK send: %w", err)
				}
				data = nil
			}
			continue
		}

		// ACK 1 byte: INS^0xFF or INS^0xFE.
		if procByte == (ins^0xFF) || procByte == (ins^0xFE) {
			inBuf = inBuf[1:]
			if isRcv {
				// Receive one byte.
				need := 1
				if len(inBuf) > 0 {
					recvBuf = append(recvBuf, inBuf[0])
					inBuf = inBuf[1:]
				} else {
					if err := r.t0CharTransmit(ctx, nil, 1); err != nil {
						return nil, fmt.Errorf("T=0 CHARACTER: ACK1 receive transmit: %w", err)
					}
					resp, err := r.t0CharReceive(ctx, need)
					if err != nil {
						return nil, fmt.Errorf("T=0 CHARACTER: ACK1 receive: %w", err)
					}
					recvBuf = append(recvBuf, resp...)
				}
				if len(recvBuf) == expLen {
					return r.t0CharReceiveSW(ctx, inBuf, recvBuf)
				}
			} else {
				// Send one byte.
				if len(data) == 0 {
					return nil, errors.New("T=0 CHARACTER: ACK1 but no data to send")
				}
				if err := r.t0CharTransmit(ctx, data[:1], 1); err != nil {
					return nil, fmt.Errorf("T=0 CHARACTER: ACK1 send: %w", err)
				}
				data = data[1:]
			}
			continue
		}

		// SW1: 0x6X or 0x9X.
		if (procByte&0xF0) == 0x60 || (procByte&0xF0) == 0x90 {
			return r.t0CharProcessSW1(ctx, inBuf, recvBuf, procByte)
		}

		// Unrecognized procedure byte.
		return nil, fmt.Errorf("T=0 CHARACTER: unrecognized procedure byte 0x%02X", procByte)
	}
}

// t0CharReceiveSW receives SW1/SW2 after all expected data has been received.
// inBuf may contain leftover bytes (including SW1) from a previous receive.
func (r *usbfsReader) t0CharReceiveSW(ctx context.Context, inBuf, recvBuf []byte) ([]byte, error) {
	if len(inBuf) >= 2 {
		// SW1 and SW2 both available.
		sw1 := inBuf[0]
		sw2 := inBuf[1]
		recvBuf = append(recvBuf, sw1, sw2)
		return recvBuf, nil
	}
	if len(inBuf) == 1 {
		// SW1 available, need SW2.
		sw1 := inBuf[0]
		if err := r.t0CharTransmit(ctx, nil, 1); err != nil {
			return nil, fmt.Errorf("T=0 CHARACTER: SW2 transmit: %w", err)
		}
		resp, err := r.t0CharReceive(ctx, 1)
		if err != nil {
			return nil, fmt.Errorf("T=0 CHARACTER: SW2 receive: %w", err)
		}
		if len(resp) == 0 {
			return nil, errors.New("T=0 CHARACTER: empty SW2 response")
		}
		recvBuf = append(recvBuf, sw1, resp[0])
		return recvBuf, nil
	}
	// No buffered bytes; receive SW1 first.
	if err := r.t0CharTransmit(ctx, nil, 1); err != nil {
		return nil, fmt.Errorf("T=0 CHARACTER: SW1 transmit: %w", err)
	}
	resp, err := r.t0CharReceive(ctx, 1)
	if err != nil {
		return nil, fmt.Errorf("T=0 CHARACTER: SW1 receive: %w", err)
	}
	if len(resp) == 0 {
		return nil, errors.New("T=0 CHARACTER: empty SW1 response")
	}
	sw1 := resp[0]
	// Now receive SW2.
	resp2, err := r.t0CharReceive(ctx, 1)
	if err != nil {
		return nil, fmt.Errorf("T=0 CHARACTER: SW2 receive: %w", err)
	}
	if len(resp2) == 0 {
		return nil, errors.New("T=0 CHARACTER: empty SW2 response")
	}
	recvBuf = append(recvBuf, sw1, resp2[0])
	return recvBuf, nil
}

// t0CharProcessSW1 handles a SW1 byte found in the procedure byte position.
func (r *usbfsReader) t0CharProcessSW1(ctx context.Context, inBuf, recvBuf []byte, sw1 byte) ([]byte, error) {
	// SW1 is already consumed from inBuf (it was the procedure byte).
	// Check if SW2 is in the leftover buffer.
	if len(inBuf) >= 2 {
		// inBuf[0] was the procedure byte (SW1), inBuf[1] is SW2.
		sw2 := inBuf[1]
		recvBuf = append(recvBuf, sw1, sw2)
		return recvBuf, nil
	}
	if len(inBuf) == 1 {
		// Only SW1 was in the buffer (the procedure byte itself).
		// Need to receive SW2.
		if err := r.t0CharTransmit(ctx, nil, 1); err != nil {
			return nil, fmt.Errorf("T=0 CHARACTER: SW2 transmit: %w", err)
		}
		resp, err := r.t0CharReceive(ctx, 1)
		if err != nil {
			return nil, fmt.Errorf("T=0 CHARACTER: SW2 receive: %w", err)
		}
		if len(resp) == 0 {
			return nil, errors.New("T=0 CHARACTER: empty SW2 response")
		}
		recvBuf = append(recvBuf, sw1, resp[0])
		return recvBuf, nil
	}
	// inBuf was exhausted; we need to receive SW2.
	if err := r.t0CharTransmit(ctx, nil, 1); err != nil {
		return nil, fmt.Errorf("T=0 CHARACTER: SW2 transmit: %w", err)
	}
	resp, err := r.t0CharReceive(ctx, 1)
	if err != nil {
		return nil, fmt.Errorf("T=0 CHARACTER: SW2 receive: %w", err)
	}
	if len(resp) == 0 {
		return nil, errors.New("T=0 CHARACTER: empty SW2 response")
	}
	recvBuf = append(recvBuf, sw1, resp[0])
	return recvBuf, nil
}

// t0CharTransmit sends data via CCID TransferBlock with the BWI parameter.
// In CHARACTER mode, parameter0 (BWI) extends the block waiting timeout.
// rxLen is the expected receive length (encoded in parameter1/parameter2).
func (r *usbfsReader) t0CharTransmit(ctx context.Context, data []byte, bwi uint8) error {
	_, err := r.command(ctx, ccidTransferBlock, bwi, 0, 0, data, ccidDataBlock)
	return err
}

// t0CharReceive receives expectedLen bytes from the card via CCID TransferBlock.
// In CHARACTER mode, we send an empty block (or wait) and read the response.
func (r *usbfsReader) t0CharReceive(ctx context.Context, expectedLen int) ([]byte, error) {
	// In CHARACTER mode, receiving is done by sending a TransferBlock with
	// the expected length in parameter1/parameter2 (wlent) and no data,
	// then reading the response.
	// However, in our USBFS implementation, the reader's TransferBlock
	// response already contains the data. We use the existing command()
	// method which handles the full request-response cycle.
	resp, err := r.command(ctx, ccidTransferBlock, 0, uint8(expectedLen&0xFF), uint8((expectedLen>>8)&0xFF), nil, ccidDataBlock)
	if err != nil {
		return nil, err
	}
	return resp.data, nil
}
