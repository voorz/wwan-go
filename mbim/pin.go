package mbim

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	basicPINTypeMaximum = PINTypeCorporatePUK
	pinLengthUnknown    = 0x00ffffff
)

type PINType uint32

const (
	PINTypeNone PINType = iota
	PINTypeCustom
	PINTypePIN1
	PINTypePIN2
	PINTypeDeviceSIM
	PINTypeDeviceFirstSIM
	PINTypeNetwork
	PINTypeNetworkSubset
	PINTypeServiceProvider
	PINTypeCorporate
	PINTypeSubsidy
	PINTypePUK1
	PINTypePUK2
	PINTypeDeviceFirstSIMPUK
	PINTypeNetworkPUK
	PINTypeNetworkSubsetPUK
	PINTypeServiceProviderPUK
	PINTypeCorporatePUK
	PINTypeNEV
	PINTypeADM
)

const PINTypeUnknown = PINTypeNone

type PINMode uint32

const (
	PINModeNotSupported PINMode = 0
	PINModeEnabled      PINMode = 1
	PINModeDisabled     PINMode = 2
)

type PINFormat uint32

const (
	PINFormatUnknown      PINFormat = 0
	PINFormatNumeric      PINFormat = 1
	PINFormatAlphanumeric PINFormat = 2
)

type PINState uint32

const (
	PINStateUnlocked PINState = 0
	PINStateLocked   PINState = 1
)

type PINOperation uint32

const (
	PINOperationEnter   PINOperation = 0
	PINOperationEnable  PINOperation = 1
	PINOperationDisable PINOperation = 2
	PINOperationChange  PINOperation = 3
)

type PINDesc struct {
	Mode      PINMode
	Format    PINFormat
	LengthMin uint32
	LengthMax uint32
}

type PINInfo struct {
	Type              PINType
	State             PINState
	RemainingAttempts uint32
}

type PINListInfo struct {
	PIN1            PINDesc
	PIN2            PINDesc
	DeviceSIM       PINDesc
	DeviceFirstSIM  PINDesc
	Network         PINDesc
	NetworkSubset   PINDesc
	ServiceProvider PINDesc
	Corporate       PINDesc
	Subsidy         PINDesc
	Custom          PINDesc
}

func validBasicPINType(pinType PINType) bool {
	return pinType <= basicPINTypeMaximum
}

func validPINLength(length uint32) bool {
	return length <= 16 || length == pinLengthUnknown
}

func (d PINDesc) validate() error {
	if d.Mode > PINModeDisabled {
		return fmt.Errorf("mode %d is outside 0..%d", d.Mode, PINModeDisabled)
	}
	if d.Format > PINFormatAlphanumeric {
		return fmt.Errorf("format %d is outside 0..%d", d.Format, PINFormatAlphanumeric)
	}
	if !validPINLength(d.LengthMin) {
		return fmt.Errorf("minimum length %d exceeds 16 and is not the unknown value %#x", d.LengthMin, pinLengthUnknown)
	}
	if !validPINLength(d.LengthMax) {
		return fmt.Errorf("maximum length %d exceeds 16 and is not the unknown value %#x", d.LengthMax, pinLengthUnknown)
	}
	if d.LengthMin != pinLengthUnknown && d.LengthMax != pinLengthUnknown && d.LengthMin > d.LengthMax {
		return fmt.Errorf("minimum length %d exceeds maximum length %d", d.LengthMin, d.LengthMax)
	}
	return nil
}

type PINRequest struct {
	TransactionID uint32
	Response      *PINInfo
}

func (r *PINRequest) Request() *Request {
	r.Response = new(PINInfo)
	return &Request{
		MessageType:   MessageTypeCommand,
		TransactionID: r.TransactionID,
		Timeout:       mbimCIDResponseTimeout,
		Command:       command(ServiceBasicConnect, CIDPIN, CommandTypeQuery, nil),
		Response:      r.Response,
	}
}

type PINSetRequest struct {
	TransactionID uint32
	Type          PINType
	Operation     PINOperation
	PIN           string
	NewPIN        string
	Response      *PINInfo
}

func (r *PINSetRequest) Request() *Request {
	pin := utf16Bytes(r.PIN)
	newPIN := utf16Bytes(r.NewPIN)
	data := make([]byte, 24)
	binary.LittleEndian.PutUint32(data[0:4], uint32(r.Type))
	binary.LittleEndian.PutUint32(data[4:8], uint32(r.Operation))
	data = appendRefValue(data, 8, pin)
	data = appendRefValue(data, 16, newPIN)

	r.Response = new(PINInfo)
	return &Request{
		MessageType:   MessageTypeCommand,
		TransactionID: r.TransactionID,
		Timeout:       mbimCIDLongResponseTimeout,
		Command:       command(ServiceBasicConnect, CIDPIN, CommandTypeSet, data),
		Response:      r.Response,
	}
}

func (r *PINInfo) UnmarshalBinary(data []byte) error {
	if len(data) != 12 {
		return fmt.Errorf("parsing MBIM PIN info: payload length is %d, want 12", len(data))
	}
	pinType := PINType(binary.LittleEndian.Uint32(data[0:4]))
	if !validBasicPINType(pinType) {
		return fmt.Errorf("parsing MBIM PIN info: type %d is outside 0..%d", pinType, basicPINTypeMaximum)
	}
	state := PINState(binary.LittleEndian.Uint32(data[4:8]))
	if state > PINStateLocked {
		return fmt.Errorf("parsing MBIM PIN info: state %d is outside 0..%d", state, PINStateLocked)
	}
	*r = PINInfo{
		Type:              pinType,
		State:             state,
		RemainingAttempts: binary.LittleEndian.Uint32(data[8:12]),
	}
	return nil
}

type PINListRequest struct {
	TransactionID uint32
	Response      *PINListInfo
}

func (r *PINListRequest) Request() *Request {
	r.Response = new(PINListInfo)
	return &Request{
		MessageType:   MessageTypeCommand,
		TransactionID: r.TransactionID,
		Timeout:       mbimCIDResponseTimeout,
		Command:       command(ServiceBasicConnect, CIDPINList, CommandTypeQuery, nil),
		Response:      r.Response,
	}
}

func (r *PINListInfo) UnmarshalBinary(data []byte) error {
	if len(data) != 160 {
		return fmt.Errorf("parsing MBIM PIN list: payload length is %d, want 160", len(data))
	}
	names := [...]string{
		"PIN1", "PIN2", "device SIM", "device first SIM", "network",
		"network subset", "service provider", "corporate", "subsidy", "custom",
	}
	descs := make([]PINDesc, 10)
	for i := range descs {
		offset := i * 16
		descs[i] = PINDesc{
			Mode:      PINMode(binary.LittleEndian.Uint32(data[offset : offset+4])),
			Format:    PINFormat(binary.LittleEndian.Uint32(data[offset+4 : offset+8])),
			LengthMin: binary.LittleEndian.Uint32(data[offset+8 : offset+12]),
			LengthMax: binary.LittleEndian.Uint32(data[offset+12 : offset+16]),
		}
		if err := descs[i].validate(); err != nil {
			return fmt.Errorf("parsing MBIM PIN list %s descriptor: %w", names[i], err)
		}
	}
	*r = PINListInfo{
		PIN1:            descs[0],
		PIN2:            descs[1],
		DeviceSIM:       descs[2],
		DeviceFirstSIM:  descs[3],
		Network:         descs[4],
		NetworkSubset:   descs[5],
		ServiceProvider: descs[6],
		Corporate:       descs[7],
		Subsidy:         descs[8],
		Custom:          descs[9],
	}
	return nil
}

func (c *Client) PIN(ctx context.Context) (PINInfo, error) {
	request := PINRequest{TransactionID: c.nextTransactionID()}
	if err := c.transmit(ctx, request.Request()); err != nil {
		return *request.Response, fmt.Errorf("reading MBIM PIN state: %w", err)
	}
	return *request.Response, nil
}

func (c *Client) SetPIN(ctx context.Context, pinType PINType, operation PINOperation, pin, newPIN string) (PINInfo, error) {
	if !validBasicPINType(pinType) {
		return PINInfo{}, fmt.Errorf("setting MBIM PIN: type %d is outside 0..%d", pinType, basicPINTypeMaximum)
	}
	if operation > PINOperationChange {
		return PINInfo{}, fmt.Errorf("setting MBIM PIN: operation %d is outside 0..%d", operation, PINOperationChange)
	}
	if size := len(utf16Bytes(pin)); size > 32 {
		return PINInfo{}, fmt.Errorf("setting MBIM PIN: PIN length %d exceeds 32 bytes", size)
	}
	if size := len(utf16Bytes(newPIN)); size > 32 {
		return PINInfo{}, fmt.Errorf("setting MBIM PIN: new PIN length %d exceeds 32 bytes", size)
	}
	newPINApplicable := operation == PINOperationChange ||
		(operation == PINOperationEnter && (pinType == PINTypePUK1 || pinType == PINTypePUK2))
	if newPIN != "" && !newPINApplicable {
		return PINInfo{}, errors.New("setting MBIM PIN: new PIN is only valid for change or PUK1/PUK2 enter operations")
	}
	request := PINSetRequest{
		TransactionID: c.nextTransactionID(),
		Type:          pinType,
		Operation:     operation,
		PIN:           pin,
		NewPIN:        newPIN,
	}
	if err := c.transmit(ctx, request.Request()); err != nil {
		return *request.Response, fmt.Errorf("setting MBIM PIN: %w", err)
	}
	return *request.Response, nil
}

func (c *Client) PINList(ctx context.Context) (PINListInfo, error) {
	request := PINListRequest{TransactionID: c.nextTransactionID()}
	if err := c.transmit(ctx, request.Request()); err != nil {
		return PINListInfo{}, fmt.Errorf("reading MBIM PIN list: %w", err)
	}
	return *request.Response, nil
}
