//go:build linux

package ccid

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	ccidClass                    = 0x0b
	ccidDescriptorType           = 0x21
	ccidHeaderLength             = 10
	ccidPowerOn                  = 0x62
	ccidPowerOff                 = 0x63
	ccidGetSlotStatus            = 0x65
	ccidTransferBlock            = 0x6f
	ccidSetParameters            = 0x61
	ccidDataBlock                = 0x80
	ccidSlotStatus               = 0x81
	ccidParameters               = 0x82
	ccidCommandFailed            = 0x40
	ccidTimeExtension            = 0x80
	ccidCommandStatusMask        = 0xc0
	ccidICCStatusMask            = 0x03
	ccidICCActive                = 0x00
	ccidICCInactive              = 0x01
	ccidICCAbsent                = 0x02
	ccidAutoActivation           = 0x00000004
	ccidAutoVoltage              = 0x00000008
	ccidAutoConfATR              = 0x00000002 // CCID_CLASS_AUTO_CONF_ATR: reader auto-configures ATR (firmware-handled)
	ccidAutoBaud                 = 0x00000020 // CCID_CLASS_AUTO_BAUD: reader auto-detects baud rate (overlaps with PPS)
	ccidAutoParameterNegotiation = 0x00000040 // CCID_CLASS_AUTO_PPS_PROP: reader auto-handles PPS + SetParameters
	ccidAutoPPSCurrent           = 0x00000080 // CCID_CLASS_AUTO_PPS_CUR: reader auto-handles PPS exchange only
	ccidExchangeMask             = 0x00070000
	ccidExchangeTPDU             = 0x00010000
	ccidExchangeShortAPDU        = 0x00020000
	ccidExchangeExtendedAPDU     = 0x00040000
	defaultCCIDMessageLength     = 271
	maximumCCIDMessageLength     = 1 << 20
	defaultUSBTimeout            = 10 * time.Second
	maximumTimeExtensions        = 32
)

var ErrCardNotPresent = errors.New("card not present")

var (
	usbFSSysfsRoot  = "/sys/bus/usb/devices"
	usbFSDeviceRoot = "/dev/bus/usb"
)

type usbfsCCIDDescriptor struct {
	maxSlotIndex     uint8
	voltageSupport   uint8
	protocols        uint32
	features         uint32
	maxMessageLength uint32
	defaultClock     uint32 // dwDefaultClock (kHz), offset 10 in CCID class descriptor
	maxDataRate      uint32 // dwMaxDataRate (bps), offset 23 in CCID class descriptor
}

type usbfsDevice struct {
	info             ReaderInfo
	deviceNode       string
	interfaceNumber  uint8
	alternateSetting uint8
	bulkIn           uint8
	bulkOut          uint8
	slot             uint8
	descriptor       usbfsCCIDDescriptor
}

type usbfsReader struct {
	file           *os.File
	device         usbfsDevice
	sequence       uint8
	active         bool
	closed         bool
	cardTimeout    uint32      // T=0 communication timeout in ms (computed from ATR + reader clock)
	characterLevel bool        // true if reader is at CHARACTER exchange level (host drives T=0 procedure bytes)
	protocol       uint8       // active card protocol (0 = T=0, 1 = T=1)
	t1             *t1Protocol // T=1 protocol state machine (nil when T=0)
}

type usbfsBulkTransfer struct {
	Endpoint uint32
	Length   uint32
	Timeout  uint32
	Data     unsafe.Pointer
}

type usbfsSetInterface struct {
	Interface  uint32
	AltSetting uint32
}

type ccidResponse struct {
	messageType uint8
	slot        uint8
	sequence    uint8
	status      uint8
	errorCode   uint8
	chain       uint8
	data        []byte
}

type ccidCommandError struct {
	messageType uint8
	status      uint8
	errorCode   uint8
}

func (e *ccidCommandError) Error() string {
	return fmt.Sprintf("CCID command 0x%02X failed (status=0x%02X error=0x%02X)", e.messageType, e.status, e.errorCode)
}

func (e *ccidCommandError) Unwrap() error {
	if e.status&ccidICCStatusMask == ccidICCAbsent {
		return ErrCardNotPresent
	}
	return nil
}

func ListUSBFSReaders(ctx context.Context) ([]ReaderInfo, error) {
	return listUSBFSReaderInfo(ctx)
}

func listUSBFSReaderInfo(ctx context.Context) ([]ReaderInfo, error) {
	devices, err := discoverUSBFSDevices(ctx)
	if err != nil {
		return nil, err
	}
	infos := make([]ReaderInfo, 0, len(devices))
	for _, device := range devices {
		infos = append(infos, device.info)
	}
	return infos, nil
}

func discoverUSBFSDevices(ctx context.Context) ([]usbfsDevice, error) {
	entries, err := os.ReadDir(usbFSSysfsRoot)
	if err != nil {
		return nil, fmt.Errorf("reading USB sysfs at %s: %w", usbFSSysfsRoot, err)
	}
	devices := make([]usbfsDevice, 0)
	var malformed []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("discovering built-in CCID readers: %w", err)
		}
		usbPath := entry.Name()
		if strings.HasPrefix(usbPath, "usb") || strings.Contains(usbPath, ":") {
			continue
		}
		devicePath := filepath.Join(usbFSSysfsRoot, usbPath)
		bus, busOK := readUSBFSUint(devicePath, "busnum", 10, 8)
		address, addressOK := readUSBFSUint(devicePath, "devnum", 10, 8)
		if !busOK || !addressOK || bus == 0 || address == 0 {
			continue
		}
		interfaces, _ := filepath.Glob(filepath.Join(usbFSSysfsRoot, usbPath+":*"))
		sort.Strings(interfaces)
		for _, interfacePath := range interfaces {
			class, ok := readUSBFSUint(interfacePath, "bInterfaceClass", 16, 8)
			if !ok || class != ccidClass {
				continue
			}
			device, buildErr := buildUSBFSDevice(devicePath, interfacePath, usbPath, uint8(bus), uint8(address))
			if buildErr != nil {
				malformed = append(malformed, buildErr)
				continue
			}
			for slot := uint8(0); slot <= device.descriptor.maxSlotIndex; slot++ {
				copy := device
				copy.slot = slot
				copy.info.Name = formatUSBFSReaderName(devicePath, usbPath, copy.interfaceNumber, slot)
				devices = append(devices, copy)
				if slot == 0xff {
					break
				}
			}
		}
	}
	if len(devices) == 0 && len(malformed) > 0 {
		return nil, fmt.Errorf("standard CCID interfaces were found but could not be used: %w", errors.Join(malformed...))
	}
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].info.USBPath != devices[j].info.USBPath {
			return devices[i].info.USBPath < devices[j].info.USBPath
		}
		if devices[i].interfaceNumber != devices[j].interfaceNumber {
			return devices[i].interfaceNumber < devices[j].interfaceNumber
		}
		return devices[i].slot < devices[j].slot
	})
	return devices, nil
}

func buildUSBFSDevice(devicePath, interfacePath, usbPath string, bus, address uint8) (usbfsDevice, error) {
	interfaceNumber, ok := readUSBFSUint(interfacePath, "bInterfaceNumber", 16, 8)
	if !ok {
		return usbfsDevice{}, fmt.Errorf("CCID interface %s has no interface number", filepath.Base(interfacePath))
	}
	alternateSetting, _ := readUSBFSUint(interfacePath, "bAlternateSetting", 10, 8)
	interfaceProtocol, _ := readUSBFSUint(interfacePath, "bInterfaceProtocol", 16, 8)
	if interfaceProtocol != 0 {
		return usbfsDevice{}, fmt.Errorf("CCID interface %s uses unsupported ICCD protocol %d", filepath.Base(interfacePath), interfaceProtocol)
	}
	bulkIn, bulkOut, err := findUSBFSBulkEndpoints(interfacePath)
	if err != nil {
		return usbfsDevice{}, err
	}
	rawDescriptors, err := os.ReadFile(filepath.Join(devicePath, "descriptors"))
	if err != nil {
		return usbfsDevice{}, fmt.Errorf("reading descriptors for USB reader %s: %w", usbPath, err)
	}
	descriptor, err := parseUSBFSCCIDDescriptor(rawDescriptors, uint8(interfaceNumber), uint8(alternateSetting))
	if err != nil {
		return usbfsDevice{}, fmt.Errorf("parsing CCID descriptor for USB reader %s: %w", usbPath, err)
	}
	if descriptor.maxMessageLength < ccidHeaderLength {
		descriptor.maxMessageLength = defaultCCIDMessageLength
	}
	if descriptor.maxMessageLength > maximumCCIDMessageLength {
		return usbfsDevice{}, fmt.Errorf("USB reader %s advertises unreasonable CCID message length %d", usbPath, descriptor.maxMessageLength)
	}
	vendor, _ := readUSBFSUint(devicePath, "idVendor", 16, 16)
	product, _ := readUSBFSUint(devicePath, "idProduct", 16, 16)
	return usbfsDevice{
		info: ReaderInfo{
			BusNumber:        bus,
			DeviceAddress:    address,
			ChannelAvailable: true,
			USBPath:          usbPath,
			USBSerial:        readUSBFSString(devicePath, "serial"),
			VendorID:         uint16(vendor),
			ProductID:        uint16(product),
			Transport:        TransportUSBFS,
		},
		deviceNode:       filepath.Join(usbFSDeviceRoot, fmt.Sprintf("%03d", bus), fmt.Sprintf("%03d", address)),
		interfaceNumber:  uint8(interfaceNumber),
		alternateSetting: uint8(alternateSetting),
		bulkIn:           bulkIn,
		bulkOut:          bulkOut,
		descriptor:       descriptor,
	}, nil
}

func findUSBFSBulkEndpoints(interfacePath string) (uint8, uint8, error) {
	entries, err := os.ReadDir(interfacePath)
	if err != nil {
		return 0, 0, fmt.Errorf("reading endpoints for %s: %w", filepath.Base(interfacePath), err)
	}
	var bulkIn, bulkOut uint8
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "ep_") {
			continue
		}
		endpointPath := filepath.Join(interfacePath, entry.Name())
		attributes, attrOK := readUSBFSUint(endpointPath, "bmAttributes", 16, 8)
		address, addressOK := readUSBFSUint(endpointPath, "bEndpointAddress", 16, 8)
		if !attrOK || !addressOK || attributes&0x03 != 0x02 {
			continue
		}
		if address&0x80 != 0 {
			bulkIn = uint8(address)
		} else {
			bulkOut = uint8(address)
		}
	}
	if bulkIn == 0 || bulkOut == 0 {
		return 0, 0, fmt.Errorf("CCID interface %s is missing bulk IN/OUT endpoints", filepath.Base(interfacePath))
	}
	return bulkIn, bulkOut, nil
}

func parseUSBFSCCIDDescriptor(raw []byte, interfaceNumber, alternateSetting uint8) (usbfsCCIDDescriptor, error) {
	currentInterface, currentAlternate := -1, -1
	for offset := 0; offset+2 <= len(raw); {
		length := int(raw[offset])
		if length < 2 || offset+length > len(raw) {
			return usbfsCCIDDescriptor{}, fmt.Errorf("malformed USB descriptor at offset %d", offset)
		}
		descriptor := raw[offset : offset+length]
		switch descriptor[1] {
		case 0x04:
			if len(descriptor) >= 9 {
				currentInterface = int(descriptor[2])
				currentAlternate = int(descriptor[3])
			}
		case ccidDescriptorType:
			if currentInterface == int(interfaceNumber) && currentAlternate == int(alternateSetting) {
				if len(descriptor) < 54 {
					return usbfsCCIDDescriptor{}, fmt.Errorf("CCID class descriptor is only %d bytes", len(descriptor))
				}
				return usbfsCCIDDescriptor{
					maxSlotIndex:     descriptor[4],
					voltageSupport:   descriptor[5],
					protocols:        binary.LittleEndian.Uint32(descriptor[6:10]),
					defaultClock:     binary.LittleEndian.Uint32(descriptor[10:14]),
					maxDataRate:      binary.LittleEndian.Uint32(descriptor[23:27]),
					features:         binary.LittleEndian.Uint32(descriptor[40:44]),
					maxMessageLength: binary.LittleEndian.Uint32(descriptor[44:48]),
				}, nil
			}
		}
		offset += length
	}
	return usbfsCCIDDescriptor{}, errors.New("CCID class descriptor was not found")
}

func formatUSBFSReaderName(devicePath, usbPath string, interfaceNumber, slot uint8) string {
	name := readUSBFSString(devicePath, "product")
	if name == "" {
		name = readUSBFSString(devicePath, "manufacturer")
	}
	if name == "" {
		name = "USB CCID Reader"
	}
	identity := readUSBFSString(devicePath, "serial")
	if identity == "" {
		identity = usbPath
	}
	return fmt.Sprintf("%s (%s) %02d %02d", name, identity, interfaceNumber, slot)
}

func readUSBFSString(path, name string) string {
	value, err := os.ReadFile(filepath.Join(path, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(value))
}

func readUSBFSUint(path, name string, base, bits int) (uint64, bool) {
	value := readUSBFSString(path, name)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value, base, bits)
	return parsed, err == nil
}

// extractSerialFromReaderName 从读卡器名称中提取括号内的序列号。
// 例如 "ESTKme-RED (2051315E5056) 00 00" → "2051315E5056"
// 用于跨模式（pcscd/USBFS）回退匹配，因为两种模式下读卡器名称不同但序列号一致。
var readerSerialRe = regexp.MustCompile(`\(([^)]+)\)`)

func extractSerialFromReaderName(name string) string {
	m := readerSerialRe.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func openUSBFSReader(ctx context.Context, readerName string) (*usbfsReader, error) {
	devices, err := discoverUSBFSDevices(ctx)
	if err != nil {
		return nil, err
	}
	var selected *usbfsDevice
	for i := range devices {
		if devices[i].info.Name == readerName {
			selected = &devices[i]
			break
		}
	}
	// 全名匹配失败时回退到序列号匹配：
	// pcscd 和 USBFS 两种模式下读卡器名称可能不同（如 "ESTKme-RED (SN) 00 00" vs "Generic Smart Card Reader Interface (SN) 00 00"），
	// 但序列号始终一致。
	if selected == nil {
		targetSerial := extractSerialFromReaderName(readerName)
		if targetSerial != "" {
			for i := range devices {
				if devices[i].info.USBSerial == targetSerial {
					selected = &devices[i]
					break
				}
			}
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("selecting %q in built-in CCID driver: %w", readerName, ErrReaderNotFound)
	}
	file, err := os.OpenFile(selected.deviceNode, os.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("opening %s: USB write permission is required: %w", selected.deviceNode, err)
		}
		return nil, fmt.Errorf("opening CCID USB device %s: %w", selected.deviceNode, err)
	}
	reader := &usbfsReader{file: file, device: *selected}
	if err := reader.claim(); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := reader.activate(ctx); err != nil {
		_ = reader.release()
		_ = file.Close()
		return nil, fmt.Errorf("activating built-in CCID reader %s: %w", selected.info.Name, err)
	}
	return reader, nil
}

func (r *usbfsReader) claim() error {
	interfaceNumber := uint32(r.device.interfaceNumber)
	if err := usbFSIoctl(r.file.Fd(), usbFSClaimInterfaceRequest(), unsafe.Pointer(&interfaceNumber)); err != nil {
		if errors.Is(err, unix.EBUSY) {
			return fmt.Errorf("claiming CCID interface %d: reader is already owned by pcscd or another process: %w", interfaceNumber, err)
		}
		if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) {
			return fmt.Errorf("claiming CCID interface %d: USB write permission is required: %w", interfaceNumber, err)
		}
		return fmt.Errorf("claiming CCID interface %d: %w", interfaceNumber, err)
	}
	if r.device.alternateSetting != 0 {
		setting := usbfsSetInterface{Interface: interfaceNumber, AltSetting: uint32(r.device.alternateSetting)}
		if err := usbFSIoctl(r.file.Fd(), usbFSSetInterfaceRequest(), unsafe.Pointer(&setting)); err != nil {
			_ = r.release()
			return fmt.Errorf("selecting CCID alternate setting %d: %w", setting.AltSetting, err)
		}
	}
	return nil
}

func (r *usbfsReader) release() error {
	if r.file == nil {
		return nil
	}
	interfaceNumber := uint32(r.device.interfaceNumber)
	if err := usbFSIoctl(r.file.Fd(), usbFSReleaseInterfaceRequest(), unsafe.Pointer(&interfaceNumber)); err != nil && !errors.Is(err, unix.ENODEV) {
		return fmt.Errorf("releasing CCID interface %d: %w", interfaceNumber, err)
	}
	return nil
}

func (r *usbfsReader) activate(ctx context.Context) error {
	exchange := r.device.descriptor.features & ccidExchangeMask
	if exchange != ccidExchangeCharacter && exchange != ccidExchangeTPDU && exchange != ccidExchangeShortAPDU && exchange != ccidExchangeExtendedAPDU {
		return fmt.Errorf("unsupported CCID exchange level 0x%08X", exchange)
	}
	r.characterLevel = exchange == ccidExchangeCharacter
	atr, err := r.powerOn(ctx)
	if err != nil {
		return err
	}
	parsed, err := parseCCIDATR(atr)
	if err != nil {
		return fmt.Errorf("parsing card ATR: %w", err)
	}
	if parsed.protocol != 0 {
		// T=1 protocol: initialize the T=1 state machine.
		if r.device.descriptor.protocols&0x02 != 0 {
			r.protocol = parsed.protocol
			r.initT1Protocol(parsed)
			r.active = true
			return nil
		}
		return fmt.Errorf("built-in CCID: card selected T=1 but reader does not support it")
	}
	if r.device.descriptor.protocols&0x01 == 0 {
		return errors.New("reader does not advertise T=0 support")
	}

	// --- PPS negotiation & SetParameters (aligned with libccid IFDHSetProtocolParameters) ---

	// If the reader fully auto-handles PPS + SetParameters (CCID_CLASS_AUTO_PPS_PROP = 0x40),
	// skip everything and let the reader firmware do it.
	if r.device.descriptor.features&ccidAutoParameterNegotiation != 0 {
		r.active = true
		return nil
	}

	// Determine whether PPS exchange is needed.
	// libccid: skip PPS if CCID_CLASS_AUTO_PPS_CUR (0x80) is set, or if TA2 is present (specific mode).
	needsPPS := r.device.descriptor.features&ccidAutoPPSCurrent == 0 && !parsed.ta2Present

	// Compute the PPS1 (TA1) value to negotiate, potentially adapting the baud rate.
	pps1 := parsed.fiDi
	if needsPPS {
		adapted, ok := r.adaptBaudRate(parsed)
		if ok {
			pps1 = adapted
		}
	}

	// Execute PPS exchange if the requested protocol differs from the default,
	// or if PPS1 is present (TA1 is non-default 0x11).
	// libccid: if (((pps[1] & 0x0F) != default_protocol) || (PPS_HAS_PPS1(pps)))
	if needsPPS {
		defaultProtocol := parsed.defaultProtocol
		if parsed.protocol != defaultProtocol || pps1 != 0x11 {
			if err := r.ppsExchange(ctx, parsed.protocol, pps1); err != nil {
				return fmt.Errorf("PPS exchange failed: %w", err)
			}
		}
	}

	// Always set T=0 parameters explicitly (libccid does this even when PPS was auto-handled by
	// the reader via CCID_CLASS_AUTO_PPS_CUR, but not when CCID_CLASS_AUTO_PPS_PROP is set).
	if err := r.setT0Parameters(ctx, parsed, pps1); err != nil {
		return err
	}
	// Compute a card-specific T=0 communication timeout (libccid: T0_card_timeout).
	r.cardTimeout = r.t0CardTimeout(parsed, pps1)

	r.active = true
	return nil
}

func (r *usbfsReader) powerOn(ctx context.Context) ([]byte, error) {
	selections := powerSelections(r.device.descriptor.voltageSupport, r.device.descriptor.features)
	var failures []error
	for _, voltage := range selections {
		response, err := r.command(ctx, ccidPowerOn, voltage, 0, 0, nil, ccidDataBlock)
		if err == nil {
			if len(response.data) == 0 {
				return nil, errors.New("reader returned an empty ATR")
			}
			return response.data, nil
		}
		if errors.Is(err, ErrCardNotPresent) {
			return nil, err
		}
		failures = append(failures, fmt.Errorf("voltage selector %d: %w", voltage, err))
	}
	return nil, fmt.Errorf("powering on card: %w", errors.Join(failures...))
}

func powerSelections(voltageSupport uint8, features uint32) []uint8 {
	if features&(ccidAutoActivation|ccidAutoVoltage) != 0 || voltageSupport == 0 {
		return []uint8{0}
	}
	selections := make([]uint8, 0, 3)
	for _, item := range []struct {
		mask, selector uint8
	}{{0x02, 2}, {0x04, 3}, {0x01, 1}} {
		if voltageSupport&item.mask != 0 {
			selections = append(selections, item.selector)
		}
	}
	return selections
}

type ccidATR struct {
	protocol          uint8
	inverseConvention bool
	fiDi              uint8 // TA1: Fi/Di
	ta2Present        bool  // TA2 present → specific mode
	tc1               uint8 // TC1: extra guard time
	tc2               uint8 // TC2: waiting integer (T=0)
	specificMode      bool
	implicitParams    bool
	defaultProtocol   uint8 // protocol indicated by first TD
}

func parseCCIDATR(atr []byte) (ccidATR, error) {
	if len(atr) < 2 {
		return ccidATR{}, errors.New("ATR is too short")
	}
	result := ccidATR{fiDi: 0x11, tc2: 0x0a}
	switch atr[0] {
	case 0x3b:
	case 0x3f:
		result.inverseConvention = true
	default:
		return ccidATR{}, fmt.Errorf("unsupported initial character 0x%02X", atr[0])
	}
	y := atr[1] >> 4
	historicalLength := int(atr[1] & 0x0f)
	offset := 2
	group := 1
	protocolSeen := false
	for {
		var ta, tc *uint8
		if y&0x01 != 0 {
			if offset >= len(atr) {
				return ccidATR{}, errors.New("ATR ends inside TA interface byte")
			}
			value := atr[offset]
			ta = &value
			offset++
		}
		if y&0x02 != 0 {
			if offset >= len(atr) {
				return ccidATR{}, errors.New("ATR ends inside TB interface byte")
			}
			offset++
		}
		if y&0x04 != 0 {
			if offset >= len(atr) {
				return ccidATR{}, errors.New("ATR ends inside TC interface byte")
			}
			value := atr[offset]
			tc = &value
			offset++
		}
		if group == 1 {
			if ta != nil {
				result.fiDi = *ta
			}
			if tc != nil {
				result.tc1 = *tc
			}
		}
		if group == 2 {
			if ta != nil {
				result.ta2Present = true
				result.specificMode = true
				result.protocol = *ta & 0x0f
				result.implicitParams = *ta&0x10 != 0
			}
			if tc != nil {
				result.tc2 = *tc
			}
		}
		if y&0x08 == 0 {
			break
		}
		if offset >= len(atr) {
			return ccidATR{}, errors.New("ATR ends inside TD interface byte")
		}
		td := atr[offset]
		offset++
		if !protocolSeen {
			result.defaultProtocol = td & 0x0f
			if !result.specificMode {
				result.protocol = td & 0x0f
			}
			protocolSeen = true
		}
		y = td >> 4
		group++
		if group > 16 {
			return ccidATR{}, errors.New("ATR contains too many interface groups")
		}
	}
	if offset+historicalLength > len(atr) {
		return ccidATR{}, errors.New("ATR historical bytes are truncated")
	}
	if result.implicitParams {
		return ccidATR{}, errors.New("ATR requests unsupported implicit specific-mode parameters")
	}
	return result, nil
}

// setT0Parameters sends CCID SetParameters for T=0 protocol, aligned with libccid.
// pps1 is the negotiated TA1 value (may differ from ATR's TA1 after baud rate adaptation).
func (r *usbfsReader) setT0Parameters(ctx context.Context, atr ccidATR, pps1 uint8) error {
	fiDi := uint8(0x11)
	if atr.specificMode || pps1 != 0x11 {
		fiDi = pps1
	}
	tccks := uint8(0)
	if atr.inverseConvention {
		tccks = 0x02
	}
	// T=0 parameters: [Fi/Di, TCCKS, GuardTime(TC1), WaitingInteger(TC2), ClockStop]
	parameters := []byte{fiDi, tccks, atr.tc1, atr.tc2, 0x00}
	_, err := r.command(ctx, ccidSetParameters, 0, 0, 0, parameters, ccidParameters)
	if err != nil {
		return fmt.Errorf("setting T=0 parameters: %w", err)
	}
	return nil
}

// ppsExchange performs a PPS (Protocol Parameter Selection) exchange with the card.
// This is the Go equivalent of libccid's PPS_Exchange.
func (r *usbfsReader) ppsExchange(ctx context.Context, protocol uint8, pps1 uint8) error {
	// Build PPS request: PPSS = 0xFF, PPS0 = protocol, PPS1 = TA1
	pps := []byte{0xFF, protocol | 0x10, pps1}
	// Append PPS1 check byte (XOR of all previous bytes)
	xor := byte(0)
	for _, b := range pps {
		xor ^= b
	}
	pps = append(pps, xor)

	response, err := r.command(ctx, ccidTransferBlock, 0, 0, 0, pps, ccidDataBlock)
	if err != nil {
		return fmt.Errorf("sending PPS request: %w", err)
	}
	if len(response.data) < 4 {
		return fmt.Errorf("PPS response too short: %d bytes", len(response.data))
	}
	// PPS response: PPSS(0xFF), PPS0, [PPS1], PCK
	// Verify PPSS = 0xFF and PPS0 matches
	if response.data[0] != 0xFF {
		return fmt.Errorf("PPS response has invalid PPSS: 0x%02X", response.data[0])
	}
	// PPS0 in response should have same protocol bits but without PTS1 indicator
	// (the response echoes the negotiated parameters, not the request indicators)
	// Just verify the check byte (PCK) is correct
	respXOR := byte(0)
	for i := 0; i < len(response.data)-1; i++ {
		respXOR ^= response.data[i]
	}
	if respXOR != response.data[len(response.data)-1] {
		return fmt.Errorf("PPS response PCK mismatch: expected 0x%02X, got 0x%02X", respXOR, response.data[len(response.data)-1])
	}
	return nil
}

// adaptBaudRate implements libccid's baud rate adaptation logic.
// It computes the card's baud rate from ATR's TA1 and compares it with the reader's max data rate.
// If the card is too fast, it steps down TA1 to find a compatible value.
// Returns the adapted TA1 value and true if adaptation was done.
func (r *usbfsReader) adaptBaudRate(atr ccidATR) (uint8, bool) {
	clock := r.device.descriptor.defaultClock
	maxRate := r.device.descriptor.maxDataRate
	if clock == 0 || maxRate == 0 {
		return atr.fiDi, false
	}

	f, d := fiDiTable(atr.fiDi)
	if f == 0 || d == 0 {
		f, d = 372, 1 // default for TA1=0x11
	}

	cardBaudRate := uint32(1000 * clock * d / f)
	defaultBaudRate := uint32(1000 * clock * 1 / 372) // default D=1, F=372

	// If the card can work at a higher baud rate than default, and the reader supports it
	if cardBaudRate > defaultBaudRate && cardBaudRate <= maxRate {
		return atr.fiDi, true
	}

	// If the card is too fast for the reader, try to lower TA1
	if cardBaudRate > maxRate+2 && atr.fiDi <= 0x97 {
		ta1 := atr.fiDi
		for ta1 > 0x94 {
			ta1--
			f2, d2 := fiDiTable(ta1)
			if f2 == 0 || d2 == 0 {
				continue
			}
			rate := uint32(1000 * clock * d2 / f2)
			if rate <= maxRate {
				return ta1, true
			}
		}
	}

	return atr.fiDi, false
}

// t0CardTimeout computes a T=0 communication timeout in milliseconds,
// aligned with libccid's T0_card_timeout function.
func (r *usbfsReader) t0CardTimeout(atr ccidATR, pps1 uint8) uint32 {
	clock := r.device.descriptor.defaultClock
	if clock == 0 {
		clock = 4000 // default 4 MHz
	}
	f, d := fiDiTable(pps1)
	if f == 0 || d == 0 {
		f, d = 372, 1
	}

	// T=0 block waiting time = (960 * TC2 + 960) * 372 * clock / f
	// This gives the WWT in clock cycles, convert to ms using clock (kHz)
	tc2 := atr.tc2
	if tc2 == 0 {
		tc2 = 0x0A // default
	}

	// WWT (clocks) = (960 * TC2 + 960) * F
	wwtCycles := uint32((960*int(tc2) + 960) * int(f))
	// WWT (ms) = WWT (clocks) / clock (kHz)
	if clock == 0 {
		return uint32(defaultUSBTimeout / time.Millisecond)
	}
	timeout := wwtCycles / clock
	// Add guard time and overhead, ensure minimum 5 seconds
	if timeout < 5000 {
		timeout = 5000
	}
	// Cap at 60 seconds
	if timeout > 60000 {
		timeout = 60000
	}
	return timeout
}

// fiDiTable returns the F and D values for a given TA1 (Fi/Di) byte.
// Based on ISO 7816-3 Table 7 and Table 8.
func fiDiTable(fiDi uint8) (f uint32, d uint32) {
	// Fi (clock rate conversion factor)
	fiTable := []uint32{372, 372, 558, 744, 1116, 1488, 1860, 0, 0, 512, 768, 1024, 1536, 2048, 0, 0}
	// Di (baud rate adaptation factor)
	diTable := []uint32{0, 1, 2, 4, 8, 16, 32, 64, 0, 0, 0, 20, 31, 40, 0, 0}

	fi := fiTable[(fiDi>>4)&0x0F]
	di := diTable[fiDi&0x0F]
	return fi, di
}

func (r *usbfsReader) transmit(ctx context.Context, request []byte) ([]byte, error) {
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
	if len(request) > maxPayload {
		return nil, fmt.Errorf("APDU length %d exceeds reader limit %d", len(request), maxPayload)
	}
	// CHARACTER exchange level: host must drive T=0 procedure bytes.
	if r.characterLevel {
		return r.transmitT0Character(ctx, request)
	}
	// T=1 protocol: use the T=1 state machine.
	if r.protocol == 1 && r.t1 != nil {
		return r.transmitT1(ctx, request)
	}
	response, err := r.command(ctx, ccidTransferBlock, 0, 0, 0, request, ccidDataBlock)
	if err != nil {
		if errors.Is(err, ErrCardNotPresent) {
			r.active = false
		}
		return nil, err
	}
	if response.chain != 0 {
		return nil, fmt.Errorf("reader returned unsupported CCID response chaining value 0x%02X", response.chain)
	}
	return response.data, nil
}

func (r *usbfsReader) ping(ctx context.Context) error {
	if r.closed || r.file == nil {
		return errors.New("built-in CCID reader is closed")
	}
	response, err := r.command(ctx, ccidGetSlotStatus, 0, 0, 0, nil, ccidSlotStatus)
	if err != nil {
		return err
	}
	switch response.status & ccidICCStatusMask {
	case ccidICCActive:
		r.active = true
		return nil
	case ccidICCInactive:
		r.active = false
		return errors.New("card is present but inactive")
	default:
		r.active = false
		return ErrCardNotPresent
	}
}

func (r *usbfsReader) close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	var errs []error
	if r.file != nil {
		if r.active {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_, err := r.command(ctx, ccidPowerOff, 0, 0, 0, nil, ccidSlotStatus)
			cancel()
			if err != nil && !errors.Is(err, ErrCardNotPresent) && !errors.Is(err, unix.ENODEV) {
				errs = append(errs, err)
			}
		}
		if err := r.release(); err != nil {
			errs = append(errs, err)
		}
		if err := r.file.Close(); err != nil {
			errs = append(errs, err)
		}
		r.file = nil
	}
	r.active = false
	return errors.Join(errs...)
}

func (r *usbfsReader) command(ctx context.Context, messageType, parameter0, parameter1, parameter2 uint8, data []byte, expectedType uint8) (ccidResponse, error) {
	if err := ctx.Err(); err != nil {
		return ccidResponse{}, err
	}
	sequence := r.sequence
	r.sequence++
	message := make([]byte, ccidHeaderLength+len(data))
	message[0] = messageType
	binary.LittleEndian.PutUint32(message[1:5], uint32(len(data)))
	message[5] = r.device.slot
	message[6] = sequence
	message[7] = parameter0
	message[8] = parameter1
	message[9] = parameter2
	copy(message[ccidHeaderLength:], data)
	if err := r.bulk(message, r.device.bulkOut, ctx, false); err != nil {
		return ccidResponse{}, fmt.Errorf("writing CCID command 0x%02X: %w", messageType, err)
	}
	for extension := 0; extension <= maximumTimeExtensions; extension++ {
		buffer := make([]byte, int(r.device.descriptor.maxMessageLength))
		length, err := r.bulkRead(buffer, r.device.bulkIn, ctx)
		if err != nil {
			return ccidResponse{}, fmt.Errorf("reading CCID response for 0x%02X: %w", messageType, err)
		}
		response, err := parseCCIDResponse(buffer[:length])
		if err != nil {
			return ccidResponse{}, err
		}
		if response.slot != r.device.slot || response.sequence != sequence {
			return ccidResponse{}, fmt.Errorf("CCID response correlation mismatch (slot=%d seq=%d, want slot=%d seq=%d)", response.slot, response.sequence, r.device.slot, sequence)
		}
		if response.messageType != expectedType {
			return ccidResponse{}, fmt.Errorf("unexpected CCID response type 0x%02X for command 0x%02X", response.messageType, messageType)
		}
		switch response.status & ccidCommandStatusMask {
		case ccidTimeExtension:
			continue
		case ccidCommandFailed:
			return ccidResponse{}, &ccidCommandError{messageType: messageType, status: response.status, errorCode: response.errorCode}
		case 0:
			return response, nil
		default:
			return ccidResponse{}, fmt.Errorf("reserved CCID command status 0x%02X", response.status)
		}
	}
	return ccidResponse{}, fmt.Errorf("CCID command 0x%02X exceeded time-extension limit", messageType)
}

func parseCCIDResponse(raw []byte) (ccidResponse, error) {
	if len(raw) < ccidHeaderLength {
		return ccidResponse{}, fmt.Errorf("CCID response is only %d bytes", len(raw))
	}
	length := int(binary.LittleEndian.Uint32(raw[1:5]))
	if length > len(raw)-ccidHeaderLength {
		return ccidResponse{}, fmt.Errorf("CCID response declares %d bytes but contains %d", length, len(raw)-ccidHeaderLength)
	}
	return ccidResponse{
		messageType: raw[0],
		slot:        raw[5],
		sequence:    raw[6],
		status:      raw[7],
		errorCode:   raw[8],
		chain:       raw[9],
		data:        append([]byte(nil), raw[ccidHeaderLength:ccidHeaderLength+length]...),
	}, nil
}

func (r *usbfsReader) bulk(message []byte, endpoint uint8, ctx context.Context, read bool) error {
	length, err := r.bulkTransfer(message, endpoint, ctx)
	if errors.Is(err, unix.EPIPE) {
		_ = r.clearHalt(endpoint)
		length, err = r.bulkTransfer(message, endpoint, ctx)
	}
	if err != nil {
		return err
	}
	if !read && length != len(message) {
		return fmt.Errorf("short USB bulk write: wrote %d of %d bytes", length, len(message))
	}
	return nil
}

func (r *usbfsReader) bulkRead(buffer []byte, endpoint uint8, ctx context.Context) (int, error) {
	length, err := r.bulkTransfer(buffer, endpoint, ctx)
	if errors.Is(err, unix.EPIPE) {
		_ = r.clearHalt(endpoint)
		length, err = r.bulkTransfer(buffer, endpoint, ctx)
	}
	return length, err
}

func (r *usbfsReader) bulkTransfer(buffer []byte, endpoint uint8, ctx context.Context) (int, error) {
	if len(buffer) == 0 {
		return 0, errors.New("USB bulk buffer is empty")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	transfer := usbfsBulkTransfer{
		Endpoint: uint32(endpoint),
		Length:   uint32(len(buffer)),
		Timeout:  r.usbFSTimeout(ctx),
		Data:     unsafe.Pointer(&buffer[0]),
	}
	for {
		result, err := usbFSIoctlResult(r.file.Fd(), usbFSBulkRequest(), unsafe.Pointer(&transfer))
		runtime.KeepAlive(buffer)
		if !errors.Is(err, unix.EINTR) {
			return result, err
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
	}
}

func (r *usbfsReader) clearHalt(endpoint uint8) error {
	value := uint32(endpoint)
	return usbFSIoctl(r.file.Fd(), usbFSClearHaltRequest(), unsafe.Pointer(&value))
}

func (r *usbfsReader) usbFSTimeout(ctx context.Context) uint32 {
	timeout := defaultUSBTimeout
	if r.cardTimeout > 0 {
		timeout = time.Duration(r.cardTimeout) * time.Millisecond
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 1
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	milliseconds := timeout.Milliseconds()
	if milliseconds < 1 {
		milliseconds = 1
	}
	if milliseconds > int64(^uint32(0)) {
		milliseconds = int64(^uint32(0))
	}
	return uint32(milliseconds)
}

const (
	usbFSIOCWrite = 1
	usbFSIOCRead  = 2
)

func usbFSIO(direction, number, size uintptr) uintptr {
	return (direction << 30) | (size << 16) | (uintptr('U') << 8) | number
}

func usbFSBulkRequest() uintptr {
	return usbFSIO(usbFSIOCRead|usbFSIOCWrite, 2, unsafe.Sizeof(usbfsBulkTransfer{}))
}

func usbFSSetInterfaceRequest() uintptr {
	return usbFSIO(usbFSIOCRead, 4, unsafe.Sizeof(usbfsSetInterface{}))
}

func usbFSClaimInterfaceRequest() uintptr {
	return usbFSIO(usbFSIOCRead, 15, unsafe.Sizeof(uint32(0)))
}

func usbFSReleaseInterfaceRequest() uintptr {
	return usbFSIO(usbFSIOCRead, 16, unsafe.Sizeof(uint32(0)))
}

func usbFSClearHaltRequest() uintptr {
	return usbFSIO(usbFSIOCRead, 21, unsafe.Sizeof(uint32(0)))
}

func usbFSIoctl(fd uintptr, request uintptr, value unsafe.Pointer) error {
	_, err := usbFSIoctlResult(fd, request, value)
	return err
}

func usbFSIoctlResult(fd uintptr, request uintptr, value unsafe.Pointer) (int, error) {
	result, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, request, uintptr(value))
	if errno != 0 {
		return 0, errno
	}
	return int(result), nil
}
