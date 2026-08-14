//go:build !linux

package ccid

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/ElMostafaIdrassi/goscard"
)

var ErrReaderNotFound = errors.New("reader not found")

const TransportPCSC = "pcsc"

// ReaderInfo is the non-Linux fallback for the USBFS ReaderInfo type.
// On non-Linux platforms the transport is always PC/SC and USB-specific
// fields are zero values.
type ReaderInfo struct {
	Name             string
	BusNumber        uint8
	DeviceAddress    uint8
	ChannelAvailable bool
	USBPath          string
	USBSerial        string
	VendorID         uint16
	ProductID        uint16
	Transport        string
}

// ListReaderInfo enumerates readers through PC/SC on non-Linux platforms.
func ListReaderInfo(ctx context.Context) ([]ReaderInfo, error) {
	names, err := ListReaders(ctx)
	if err != nil {
		return nil, err
	}
	infos := make([]ReaderInfo, 0, len(names))
	for _, name := range names {
		infos = append(infos, ReaderInfo{
			Name:             name,
			ChannelAvailable: true,
			Transport:        TransportPCSC,
		})
	}
	return infos, nil
}

type Reader struct {
	mu     sync.Mutex
	ctx    *goscard.Context
	card   *goscard.Card
	ioSend *goscard.SCardIORequest
	closed bool
}

var pcsc struct {
	mu   sync.Mutex
	refs int
}

func ListReaders(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("listing readers: %w", err)
	}
	pcscCtx, err := newContext()
	if err != nil {
		return nil, err
	}
	defer releasePCSCContext(pcscCtx)

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("listing readers: %w", err)
	}
	readers, _, err := pcscCtx.ListReaders(nil)
	if err != nil {
		return nil, fmt.Errorf("listing readers: %w", err)
	}
	return slices.Clone(readers), nil
}

func Open(ctx context.Context, readerName string) (*Reader, error) {
	readerName = strings.TrimSpace(readerName)
	if readerName == "" {
		return nil, errors.New("opening reader: reader name is empty")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("opening reader: %w", err)
	}

	pcscCtx, err := newContext()
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		releasePCSCContext(pcscCtx)
		return nil, fmt.Errorf("opening reader: %w", err)
	}
	readers, _, err := pcscCtx.ListReaders(nil)
	if err != nil {
		releasePCSCContext(pcscCtx)
		return nil, fmt.Errorf("listing readers: %w", err)
	}
	if !slices.Contains(readers, readerName) {
		releasePCSCContext(pcscCtx)
		return nil, fmt.Errorf("selecting %q: %w", readerName, ErrReaderNotFound)
	}

	if err := ctx.Err(); err != nil {
		releasePCSCContext(pcscCtx)
		return nil, fmt.Errorf("opening reader: %w", err)
	}
	card, _, err := pcscCtx.Connect(readerName, goscard.SCardShareShared, goscard.SCardProtocolAny)
	if err != nil {
		releasePCSCContext(pcscCtx)
		return nil, fmt.Errorf("connecting to %s: %w", readerName, err)
	}
	ioSend, err := ioRequestForProtocol(card.ActiveProtocol())
	if err != nil {
		_, _ = card.Disconnect(goscard.SCardLeaveCard) // Cleanup cannot change the protocol error.
		releasePCSCContext(pcscCtx)
		return nil, err
	}

	return &Reader{
		ctx:    &pcscCtx,
		card:   &card,
		ioSend: ioSend,
	}, nil
}

func newContext() (goscard.Context, error) {
	if err := acquirePCSC(); err != nil {
		return goscard.Context{}, err
	}

	ctx, _, err := goscard.NewContext(goscard.SCardScopeSystem, nil, nil)
	if err != nil {
		releasePCSC()
		return goscard.Context{}, fmt.Errorf("creating pcsc context: %w", err)
	}
	return ctx, nil
}

func releasePCSCContext(pcscCtx goscard.Context) {
	_, _ = pcscCtx.Release() // PC/SC cleanup is best effort; no useful recovery remains.
	releasePCSC()
}

func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}

	var errs []error
	if err := r.disconnectCard(); err != nil {
		errs = append(errs, err)
	}
	if _, err := r.ctx.Release(); err != nil {
		errs = append(errs, fmt.Errorf("releasing pcsc context: %w", err))
	}
	r.ctx = nil
	r.closed = true
	releasePCSC()
	return errors.Join(errs...)
}

func (r *Reader) disconnectCard() error {
	if r.card == nil {
		return nil
	}

	_, err := r.card.Disconnect(goscard.SCardLeaveCard)
	r.card = nil
	r.ioSend = nil
	if err != nil {
		return fmt.Errorf("disconnecting card: %w", err)
	}
	return nil
}

func (r *Reader) Transmit(ctx context.Context, req []byte) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("transmitting APDU: reader is closed")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	recv, _, err := r.card.Transmit(r.ioSend, req, nil)
	if err != nil {
		return nil, fmt.Errorf("transmitting APDU %X: %w", req, err)
	}
	return recv, nil
}

func acquirePCSC() error {
	pcsc.mu.Lock()
	defer pcsc.mu.Unlock()

	if pcsc.refs == 0 {
		if err := goscard.Initialize(goscard.NewDefaultLogger(goscard.LogLevelNone)); err != nil {
			return fmt.Errorf("initializing pcsc: %w", err)
		}
	}
	pcsc.refs++
	return nil
}

func releasePCSC() {
	pcsc.mu.Lock()
	defer pcsc.mu.Unlock()

	if pcsc.refs == 0 {
		return
	}
	pcsc.refs--
	if pcsc.refs == 0 {
		goscard.Finalize()
	}
}

func ioRequestForProtocol(protocol goscard.SCardProtocol) (*goscard.SCardIORequest, error) {
	switch protocol {
	case goscard.SCardProtocolT0:
		return &goscard.SCardIoRequestT0, nil
	case goscard.SCardProtocolT1:
		return &goscard.SCardIoRequestT1, nil
	default:
		return nil, fmt.Errorf("unsupported active PC/SC protocol: %s", protocol.String())
	}
}

// Ping verifies that the reader is still usable on non-Linux platforms.
func (r *Reader) Ping(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.card == nil {
		return errors.New("checking reader: reader is closed")
	}
	return nil
}
