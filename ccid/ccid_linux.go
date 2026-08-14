//go:build linux

// Package ccid contains the built-in Linux CCID transport used by celmux.
//
// The implementation does not link a host PC/SC client: readers are discovered
// and driven directly through USBFS, which keeps the final Go binary
// self-contained and statically linkable with musl.
package ccid

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

var ErrReaderNotFound = errors.New("reader not found")

// OpenOptions controls reader connection behaviour. On Linux (USBFS transport)
// these fields are ignored because the USBFS transport is inherently exclusive.
// On non-Linux platforms they map to the corresponding PC/SC parameters.
type OpenOptions struct {
	// ShareMode is the PC/SC share mode. Ignored on Linux.
	ShareMode ShareMode
	// Protocol is the preferred PC/SC protocol. Ignored on Linux.
	Protocol Protocol
	// SendTerminalCapabilities controls whether the terminal capabilities APDU
	// is sent after connecting. Ignored on Linux.
	SendTerminalCapabilities bool
}

// ShareMode controls how a reader connection is shared among callers.
type ShareMode int

const (
	ShareExclusive ShareMode = iota
	ShareShared
)

// Protocol selects the card protocol to use after connecting.
type Protocol int

const (
	ProtocolAny Protocol = iota
	ProtocolT0
	ProtocolT1
)

// ReaderInfo identifies the USB endpoint currently backing a CCID reader.
// BusNumber and DeviceAddress are runtime values; callers should translate
// them to a stable sysfs topology path before persisting an identity.
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

const TransportUSBFS = "usbfs"

type Reader struct {
	mu     sync.Mutex
	direct *sharedUSBFSReader
	closed bool
}

// sharedUSBFSReader owns one USBDEVFS claim and is reference counted by
// logical Reader handles. A CCID interface cannot be claimed twice, while
// the backend and the eUICC LPA legitimately need independent handles in the
// same process.
type sharedUSBFSReader struct {
	mu     sync.Mutex
	name   string
	reader *usbfsReader
	refs   int
}

var directReaders = struct {
	sync.Mutex
	byName map[string]*sharedUSBFSReader
}{byName: make(map[string]*sharedUSBFSReader)}

func normalizedContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func ListReaders(ctx context.Context) ([]string, error) {
	infos, err := ListReaderInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing readers: %w", err)
	}
	readers := make([]string, 0, len(infos))
	for _, info := range infos {
		readers = append(readers, info.Name)
	}
	return readers, nil
}

// ListReaderInfo enumerates standard CCID interfaces through the built-in
// USBFS transport. It does not consult pcscd or any host shared library.
func ListReaderInfo(ctx context.Context) ([]ReaderInfo, error) {
	ctx = normalizedContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("listing reader information: %w", err)
	}
	return listUSBFSReaderInfo(ctx)
}

func Open(ctx context.Context, readerName string) (*Reader, error) {
	return OpenWithOptions(ctx, readerName, OpenOptions{})
}

// OpenWithOptions opens a reader with the given options. On Linux the options
// are ignored because USBFS connections are inherently exclusive.
func OpenWithOptions(ctx context.Context, readerName string, opts OpenOptions) (*Reader, error) {
	ctx = normalizedContext(ctx)
	readerName = strings.TrimSpace(readerName)
	if readerName == "" {
		return nil, errors.New("opening reader: reader name is empty")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("opening reader: %w", err)
	}
	direct, err := openSharedUSBFSReader(ctx, readerName)
	if err == nil {
		return &Reader{direct: direct}, nil
	}
	if errors.Is(err, ErrReaderNotFound) {
		return nil, fmt.Errorf("selecting %q: %w", readerName, ErrReaderNotFound)
	}
	return nil, fmt.Errorf("opening built-in CCID reader %q: %w", readerName, err)
}

func openSharedUSBFSReader(ctx context.Context, readerName string) (*sharedUSBFSReader, error) {
	readerName = strings.TrimSpace(readerName)
	directReaders.Lock()
	defer directReaders.Unlock()
	if shared := directReaders.byName[readerName]; shared != nil {
		shared.refs++
		return shared, nil
	}
	reader, err := openUSBFSReader(ctx, readerName)
	if err != nil {
		return nil, err
	}
	shared := &sharedUSBFSReader{name: readerName, reader: reader, refs: 1}
	directReaders.byName[readerName] = shared
	return shared, nil
}

func (s *sharedUSBFSReader) release() error {
	if s == nil {
		return nil
	}
	directReaders.Lock()
	defer directReaders.Unlock()
	if s.refs > 1 {
		s.refs--
		return nil
	}
	if current := directReaders.byName[s.name]; current == s {
		delete(directReaders.byName, s.name)
	}
	s.refs = 0
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reader.close()
}

func (s *sharedUSBFSReader) transmit(ctx context.Context, request []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reader.transmit(ctx, request)
}

func (s *sharedUSBFSReader) ping(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reader.ping(ctx)
}

func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	var err error
	if r.direct != nil {
		err = r.direct.release()
		r.direct = nil
	}
	r.closed = true
	return err
}

func (r *Reader) Transmit(ctx context.Context, request []byte) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.direct == nil {
		return nil, errors.New("transmitting APDU: reader is closed")
	}
	ctx = normalizedContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response, err := r.direct.transmit(ctx, request)
	if err != nil {
		cla, ins := byte(0), byte(0)
		if len(request) > 0 {
			cla = request[0]
		}
		if len(request) > 1 {
			ins = request[1]
		}
		return nil, fmt.Errorf("transmitting APDU (CLA=%02X INS=%02X length=%d): %w", cla, ins, len(request), err)
	}
	return response, nil
}

// Ping verifies that the built-in reader is still usable without changing
// the selected UICC application or file.
func (r *Reader) Ping(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.direct == nil {
		return errors.New("checking reader: reader is closed")
	}
	ctx = normalizedContext(ctx)
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("checking reader: %w", err)
	}
	return r.direct.ping(ctx)
}
