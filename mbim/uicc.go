package mbim

import (
	"context"
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/voorz/wwan-go/apdu"
)

const (
	uiccChannelGroupDefault               = 1
	uiccATRMaximumSize                    = 33
	uiccApplicationIDMaximumSize          = 32
	uiccApplicationNameMaximumSize        = 256
	uiccPINKeyReferenceMaximumCount       = 8
	uiccOpenChannelResponseMaximumSize    = 256
	uiccAPDUCommandMaximumSize            = 261
	uiccLogicalChannelMaximum             = 19
	uiccActiveApplicationIndexUnavailable = uint32(0xFFFFFFFF)
)

type UICCApplicationType uint32

const (
	UICCApplicationTypeUnknown UICCApplicationType = iota
	UICCApplicationTypeMF
	UICCApplicationTypeMFSIM
	UICCApplicationTypeMFRUIM
	UICCApplicationTypeUSIM
	UICCApplicationTypeCSIM
	UICCApplicationTypeISIM
)

type UICCSecureMessaging uint32

const (
	UICCSecureMessagingNone UICCSecureMessaging = iota
	UICCSecureMessagingNoHeaderAuth
)

type UICCClassByteType uint32

const (
	UICCClassByteTypeInterIndustry UICCClassByteType = iota
	UICCClassByteTypeExtended
)

type UICCPassThroughAction uint32

const (
	UICCPassThroughActionDisable UICCPassThroughAction = iota
	UICCPassThroughActionEnable
)

type UICCPassThroughStatus uint32

const (
	UICCPassThroughStatusDisabled UICCPassThroughStatus = iota
	UICCPassThroughStatusEnabled
)

type UICCFileAccessibility uint32

const (
	UICCFileAccessibilityUnknown UICCFileAccessibility = iota
	UICCFileAccessibilityNotShareable
	UICCFileAccessibilityShareable
)

type UICCFileType uint32

const (
	UICCFileTypeUnknown UICCFileType = iota
	UICCFileTypeWorkingEF
	UICCFileTypeInternalEF
	UICCFileTypeDFOrADF
)

type UICCFileStructure uint32

const (
	UICCFileStructureUnknown UICCFileStructure = iota
	UICCFileStructureTransparent
	UICCFileStructureCyclic
	UICCFileStructureLinear
	UICCFileStructureBERTLV
)

func (c *Client) ListApplications(ctx context.Context) ([]Application, error) {
	request := ApplicationListRequest{TransactionID: c.nextTransactionID()}
	if err := c.transmit(ctx, request.Request()); err != nil {
		return nil, fmt.Errorf("listing MBIM applications: %w", err)
	}

	apps := make([]Application, 0, len(request.Response.Applications))
	for _, app := range request.Response.Applications {
		if len(app.AID) == 0 {
			continue
		}
		apps = append(apps, Application{
			AID:   slices.Clone(app.AID),
			Label: app.Label,
		})
	}
	return apps, nil
}

func (c *Client) QueryUICCATR(ctx context.Context) ([]byte, error) {
	if err := c.validateUICCSlotID(); err != nil {
		return nil, fmt.Errorf("querying MBIM UICC ATR: %w", err)
	}
	request := UICCATRQueryRequest{
		TransactionID: c.nextTransactionID(),
		MBIMExVersion: c.mbimExVersion,
		SlotID:        c.slot,
	}
	if err := c.transmit(ctx, request.Request()); err != nil {
		return nil, fmt.Errorf("querying MBIM UICC ATR: %w", err)
	}
	return slices.Clone(request.Response.ATR), nil
}

func (c *Client) OpenChannel(ctx context.Context, aid []byte) (uint32, error) {
	if err := c.validateUICCSlotID(); err != nil {
		return 0, fmt.Errorf("opening MBIM UICC channel: %w", err)
	}
	if len(aid) > uiccApplicationIDMaximumSize {
		return 0, fmt.Errorf(
			"opening MBIM UICC channel: application ID length %d exceeds %d bytes: %w",
			len(aid),
			uiccApplicationIDMaximumSize,
			StatusParameterTooLong,
		)
	}
	request := OpenChannelRequest{
		TransactionID: c.nextTransactionID(),
		MBIMExVersion: c.mbimExVersion,
		SlotID:        c.slot,
		ApplicationID: slices.Clone(aid),
		ChannelGroup:  uiccChannelGroupDefault,
	}
	if err := c.transmit(ctx, request.Request()); err != nil {
		return 0, fmt.Errorf("opening MBIM UICC channel: %w", err)
	}
	if err := uiccStatusError(request.Response.Status); err != nil {
		return 0, fmt.Errorf("opening MBIM UICC channel: %w", err)
	}
	if request.Response.Channel == 0 || request.Response.Channel > uiccLogicalChannelMaximum {
		return 0, fmt.Errorf(
			"opening MBIM UICC channel: response channel %d is outside 1..%d: %w",
			request.Response.Channel,
			uiccLogicalChannelMaximum,
			ProtocolErrorInvalid,
		)
	}
	return request.Response.Channel, nil
}

func (c *Client) TransmitAPDU(ctx context.Context, channel uint32, command []byte) ([]byte, uint32, error) {
	if err := c.validateUICCSlotID(); err != nil {
		return nil, 0, fmt.Errorf("transmitting MBIM UICC APDU: %w", err)
	}
	if channel == 0 || channel > uiccLogicalChannelMaximum {
		return nil, 0, fmt.Errorf(
			"transmitting MBIM UICC APDU: channel %d is outside 1..%d: %w",
			channel,
			uiccLogicalChannelMaximum,
			StatusInvalidParameters,
		)
	}
	if len(command) > uiccAPDUCommandMaximumSize {
		return nil, 0, fmt.Errorf(
			"transmitting MBIM UICC APDU: command length %d exceeds %d bytes: %w",
			len(command),
			uiccAPDUCommandMaximumSize,
			StatusParameterTooLong,
		)
	}
	request := APDURequest{
		TransactionID:   c.nextTransactionID(),
		MBIMExVersion:   c.mbimExVersion,
		SlotID:          c.slot,
		Channel:         channel,
		SecureMessaging: UICCSecureMessagingNone,
		ClassByteType:   UICCClassByteTypeInterIndustry,
		Command:         slices.Clone(command),
	}
	if err := c.transmit(ctx, request.Request()); err != nil {
		return nil, 0, fmt.Errorf("transmitting MBIM UICC APDU: %w", err)
	}
	return slices.Clone(request.Response.Response), request.Response.Status, nil
}

func (c *Client) SetUICCReset(ctx context.Context, action UICCPassThroughAction) (UICCPassThroughStatus, error) {
	if err := c.validateUICCSlotID(); err != nil {
		return 0, fmt.Errorf("setting MBIM UICC reset: %w", err)
	}
	if action > UICCPassThroughActionEnable {
		return 0, fmt.Errorf("setting MBIM UICC reset: action %d is outside 0..1: %w", action, StatusInvalidParameters)
	}
	request := UICCResetSetRequest{
		TransactionID: c.nextTransactionID(),
		MBIMExVersion: c.mbimExVersion,
		SlotID:        c.slot,
		Action:        action,
	}
	if err := c.transmit(ctx, request.Request()); err != nil {
		return 0, fmt.Errorf("setting MBIM UICC reset: %w", err)
	}
	c.clearEnvelopeSupport()
	return request.Response.PassThroughStatus, nil
}

func (c *Client) QueryUICCReset(ctx context.Context) (UICCPassThroughStatus, error) {
	if err := c.validateUICCSlotID(); err != nil {
		return 0, fmt.Errorf("querying MBIM UICC reset: %w", err)
	}
	request := UICCResetQueryRequest{
		TransactionID: c.nextTransactionID(),
		MBIMExVersion: c.mbimExVersion,
		SlotID:        c.slot,
	}
	if err := c.transmit(ctx, request.Request()); err != nil {
		return 0, fmt.Errorf("querying MBIM UICC reset: %w", err)
	}
	return request.Response.PassThroughStatus, nil
}

func (c *Client) SetUICCTerminalCapability(ctx context.Context, capabilities [][]byte) error {
	if err := c.validateUICCSlotID(); err != nil {
		return fmt.Errorf("setting MBIM UICC terminal capability: %w", err)
	}
	request := UICCTerminalCapabilitySetRequest{
		TransactionID: c.nextTransactionID(),
		MBIMExVersion: c.mbimExVersion,
		SlotID:        c.slot,
		Capabilities:  cloneByteSlices(capabilities),
	}
	if err := c.transmit(ctx, request.Request()); err != nil {
		return fmt.Errorf("setting MBIM UICC terminal capability: %w", err)
	}
	return nil
}

func (c *Client) QueryUICCTerminalCapability(ctx context.Context) ([][]byte, error) {
	if err := c.validateUICCSlotID(); err != nil {
		return nil, fmt.Errorf("querying MBIM UICC terminal capability: %w", err)
	}
	request := UICCTerminalCapabilityQueryRequest{
		TransactionID: c.nextTransactionID(),
		MBIMExVersion: c.mbimExVersion,
		SlotID:        c.slot,
	}
	if err := c.transmit(ctx, request.Request()); err != nil {
		return nil, fmt.Errorf("querying MBIM UICC terminal capability: %w", err)
	}
	return cloneByteSlices(request.Response.Capabilities), nil
}

func (c *Client) CloseChannel(ctx context.Context, channel uint32) error {
	if err := c.validateUICCSlotID(); err != nil {
		return fmt.Errorf("closing MBIM UICC channel: %w", err)
	}
	if channel > uiccLogicalChannelMaximum {
		return fmt.Errorf(
			"closing MBIM UICC channel: channel %d exceeds %d: %w",
			channel,
			uiccLogicalChannelMaximum,
			StatusInvalidParameters,
		)
	}
	request := CloseChannelRequest{
		TransactionID: c.nextTransactionID(),
		MBIMExVersion: c.mbimExVersion,
		SlotID:        c.slot,
		Channel:       channel,
		ChannelGroup:  uiccChannelGroupDefault,
	}
	if err := c.transmit(ctx, request.Request()); err != nil {
		return fmt.Errorf("closing MBIM UICC channel: %w", err)
	}
	if err := uiccStatusError(request.Response.Status); err != nil {
		return fmt.Errorf("closing MBIM UICC channel: %w", err)
	}
	return nil
}

func cloneByteSlices(values [][]byte) [][]byte {
	if values == nil {
		return nil
	}
	clones := make([][]byte, len(values))
	for i, value := range values {
		clones[i] = slices.Clone(value)
	}
	return clones
}

func uiccStatusError(status uint32) error {
	if uiccStatusOK(status) {
		return nil
	}
	return apdu.StatusError{SW: uiccStatusCode(status)}
}

func uiccStatusOK(status uint32) bool {
	statusCode := uiccStatusCode(status)
	return status == 0 || statusCode == 0x9000 || statusCode&0xff00 == 0x9100
}

func uiccStatusCode(status uint32) uint16 {
	var sw [2]byte
	binary.LittleEndian.PutUint16(sw[:], uint16(status&0xffff))
	return binary.BigEndian.Uint16(sw[:])
}

func cardStatusError(sw1, sw2 uint32) error {
	if sw1 == 0x90 && sw2 == 0x00 {
		return nil
	}
	return fmt.Errorf("unexpected status word 0x%02X%02X", sw1, sw2)
}

type ApplicationListRequest struct {
	TransactionID uint32
	Response      *ApplicationListResponse
}

func (r *ApplicationListRequest) Request() *Request {
	r.Response = new(ApplicationListResponse)
	return &Request{
		MessageType:   MessageTypeCommand,
		TransactionID: r.TransactionID,
		Command: command(
			ServiceMSUICCLowLevelAccess,
			CIDUICCApplicationList,
			CommandTypeQuery,
			nil,
		),
		Response: r.Response,
	}
}

type UICCApplication struct {
	Type                 UICCApplicationType
	AID                  []byte
	Label                string
	PINKeyReferenceCount uint32
	PINKeyReferences     []byte
}

var _ encoding.BinaryUnmarshaler = (*UICCApplication)(nil)

type ApplicationListResponse struct {
	Version                  uint32
	ActiveApplicationIndex   uint32
	ApplicationListSizeBytes uint32
	Applications             []UICCApplication
}

func (r *ApplicationListResponse) UnmarshalBinary(data []byte) error {
	if len(data) < 16 {
		return errors.New("parsing MBIM application list: payload is truncated")
	}
	version := binary.LittleEndian.Uint32(data[:4])
	if version != 1 {
		return fmt.Errorf("parsing MBIM application list: version is %d, want 1", version)
	}
	applicationCount := binary.LittleEndian.Uint32(data[4:8])
	activeApplicationIndex := binary.LittleEndian.Uint32(data[8:12])
	if activeApplicationIndex != uiccActiveApplicationIndexUnavailable && activeApplicationIndex >= applicationCount {
		return fmt.Errorf(
			"parsing MBIM application list: active application index %d is outside application count %d",
			activeApplicationIndex,
			applicationCount,
		)
	}
	applicationListSizeBytes := binary.LittleEndian.Uint32(data[12:16])
	refs, err := offsetSizeRefs(data, 16, applicationCount)
	if err != nil {
		return fmt.Errorf("parsing MBIM application list: %w", err)
	}
	dataStart := uint32(16) + applicationCount*8
	actualApplicationListSize := uint32(len(data)) - dataStart
	if applicationListSizeBytes != actualApplicationListSize {
		return fmt.Errorf(
			"parsing MBIM application list: application list size is %d, want %d",
			applicationListSizeBytes,
			actualApplicationListSize,
		)
	}

	applications := make([]UICCApplication, 0, applicationCount)
	for i, ref := range refs {
		var app UICCApplication
		if err := app.UnmarshalBinary(ref.bytes(data)); err != nil {
			return fmt.Errorf("parsing MBIM application list entry %d: %w", i, err)
		}
		applications = append(applications, app)
	}
	*r = ApplicationListResponse{
		Version:                  version,
		ActiveApplicationIndex:   activeApplicationIndex,
		ApplicationListSizeBytes: applicationListSizeBytes,
		Applications:             applications,
	}
	return nil
}

func (a *UICCApplication) UnmarshalBinary(data []byte) error {
	if len(data) < 32 {
		return errors.New("application entry is truncated")
	}
	applicationType := UICCApplicationType(binary.LittleEndian.Uint32(data[:4]))
	if applicationType > UICCApplicationTypeISIM {
		return fmt.Errorf("application type %d is outside 0..%d", applicationType, UICCApplicationTypeISIM)
	}

	aidRef, err := readOffsetSizeRef(data, 4)
	if err != nil {
		return fmt.Errorf("application ID: %w", err)
	}
	labelRef, err := readOffsetSizeRef(data, 12)
	if err != nil {
		return fmt.Errorf("application name: %w", err)
	}
	pinKeyReferencesRef, err := readOffsetSizeRef(data, 24)
	if err != nil {
		return fmt.Errorf("PIN key references: %w", err)
	}
	refs := []valueRef{aidRef, labelRef, pinKeyReferencesRef}
	if err := validateRecordDataBufferRefs(data, 32, refs); err != nil {
		return fmt.Errorf("application data buffer: %w", err)
	}
	if aidRef.size > uiccApplicationIDMaximumSize {
		return fmt.Errorf("application ID size %d exceeds %d bytes", aidRef.size, uiccApplicationIDMaximumSize)
	}
	if applicationType >= UICCApplicationTypeMF && applicationType <= UICCApplicationTypeMFRUIM && aidRef.size != 0 {
		return fmt.Errorf("application type %d must not include an application ID", applicationType)
	}
	if labelRef.size > uiccApplicationNameMaximumSize {
		return fmt.Errorf("application name size %d exceeds %d bytes", labelRef.size, uiccApplicationNameMaximumSize)
	}
	label := labelRef.bytes(data)
	for len(label) != 0 && label[len(label)-1] == 0 {
		label = label[:len(label)-1]
	}
	if !utf8.Valid(label) {
		return errors.New("application name is not valid UTF-8")
	}
	pinKeyReferenceCount := binary.LittleEndian.Uint32(data[20:24])
	if pinKeyReferenceCount > uiccPINKeyReferenceMaximumCount {
		return fmt.Errorf(
			"PIN key reference count %d exceeds %d",
			pinKeyReferenceCount,
			uiccPINKeyReferenceMaximumCount,
		)
	}
	if pinKeyReferencesRef.size > uiccPINKeyReferenceMaximumCount {
		return fmt.Errorf(
			"PIN key reference size %d exceeds %d bytes",
			pinKeyReferencesRef.size,
			uiccPINKeyReferenceMaximumCount,
		)
	}
	if pinKeyReferenceCount > pinKeyReferencesRef.size {
		return fmt.Errorf(
			"PIN key reference count %d exceeds reference size %d",
			pinKeyReferenceCount,
			pinKeyReferencesRef.size,
		)
	}

	*a = UICCApplication{
		Type:                 applicationType,
		AID:                  aidRef.bytes(data),
		Label:                string(label),
		PINKeyReferenceCount: pinKeyReferenceCount,
		PINKeyReferences:     pinKeyReferencesRef.bytes(data),
	}
	return nil
}

type UICCATRQueryRequest struct {
	TransactionID uint32
	MBIMExVersion uint16
	SlotID        uint32
	Response      *UICCATRResponse
}

func (r *UICCATRQueryRequest) Request() *Request {
	var data []byte
	if r.MBIMExVersion >= mbimExVersion40 {
		data = binary.LittleEndian.AppendUint32(nil, r.SlotID)
	}

	r.Response = new(UICCATRResponse)
	return &Request{
		MessageType:   MessageTypeCommand,
		TransactionID: r.TransactionID,
		Timeout:       mbimCIDLongResponseTimeout,
		Command: command(
			ServiceMSUICCLowLevelAccess,
			CIDUICCATR,
			CommandTypeQuery,
			data,
		),
		Response: r.Response,
	}
}

type UICCATRResponse struct {
	ATR []byte
}

func (r *UICCATRResponse) UnmarshalBinary(data []byte) error {
	value, err := uiccByteArrayRef(data, 0, 8)
	if err != nil {
		return fmt.Errorf("parsing MBIM UICC ATR: %w", err)
	}
	if len(value) > uiccATRMaximumSize {
		return fmt.Errorf("parsing MBIM UICC ATR: size %d exceeds %d bytes", len(value), uiccATRMaximumSize)
	}
	r.ATR = value
	return nil
}

type OpenChannelRequest struct {
	TransactionID uint32
	MBIMExVersion uint16
	SlotID        uint32
	ApplicationID []byte
	SelectP2Arg   uint32
	ChannelGroup  uint32
	Response      *OpenChannelResponse
}

func (r *OpenChannelRequest) Request() *Request {
	appIDOffset := 16
	if r.MBIMExVersion >= mbimExVersion40 {
		appIDOffset = 20
	}
	data := uiccRefHeader(appIDOffset, r.ApplicationID)
	data = binary.LittleEndian.AppendUint32(data, r.SelectP2Arg)
	data = binary.LittleEndian.AppendUint32(data, r.ChannelGroup)
	if r.MBIMExVersion >= mbimExVersion40 {
		data = binary.LittleEndian.AppendUint32(data, r.SlotID)
	}
	data = append(data, r.ApplicationID...)

	r.Response = new(OpenChannelResponse)
	return &Request{
		MessageType:   MessageTypeCommand,
		TransactionID: r.TransactionID,
		Timeout:       mbimCIDResponseTimeout,
		Command: command(
			ServiceMSUICCLowLevelAccess,
			CIDUICCOpenChannel,
			CommandTypeSet,
			data,
		),
		Response: r.Response,
	}
}

type OpenChannelResponse struct {
	Status   uint32
	Channel  uint32
	Response []byte
}

func (r *OpenChannelResponse) UnmarshalBinary(data []byte) error {
	if len(data) < 16 {
		return errors.New("parsing MBIM open channel: payload is truncated")
	}
	status := binary.LittleEndian.Uint32(data[:4])
	if status > 0xffff {
		return fmt.Errorf("parsing MBIM open channel: status %#x exceeds two bytes", status)
	}
	channel := binary.LittleEndian.Uint32(data[4:8])
	if channel > uiccLogicalChannelMaximum {
		return fmt.Errorf("parsing MBIM open channel: channel %d exceeds %d", channel, uiccLogicalChannelMaximum)
	}
	value, err := uiccByteArrayRef(data, 8, 16)
	if err != nil {
		return fmt.Errorf("parsing MBIM open channel response: %w", err)
	}
	if len(value) > uiccOpenChannelResponseMaximumSize {
		return fmt.Errorf(
			"parsing MBIM open channel response: size %d exceeds %d bytes",
			len(value),
			uiccOpenChannelResponseMaximumSize,
		)
	}
	*r = OpenChannelResponse{Status: status, Channel: channel, Response: value}
	return nil
}

type CloseChannelRequest struct {
	TransactionID uint32
	MBIMExVersion uint16
	SlotID        uint32
	Channel       uint32
	ChannelGroup  uint32
	Response      *CloseChannelResponse
}

func (r *CloseChannelRequest) Request() *Request {
	data := binary.LittleEndian.AppendUint32(nil, r.Channel)
	data = binary.LittleEndian.AppendUint32(data, r.ChannelGroup)
	if r.MBIMExVersion >= mbimExVersion40 {
		data = binary.LittleEndian.AppendUint32(data, r.SlotID)
	}

	r.Response = new(CloseChannelResponse)
	return &Request{
		MessageType:   MessageTypeCommand,
		TransactionID: r.TransactionID,
		Timeout:       mbimCIDResponseTimeout,
		Command: command(
			ServiceMSUICCLowLevelAccess,
			CIDUICCCloseChannel,
			CommandTypeSet,
			data,
		),
		Response: r.Response,
	}
}

type CloseChannelResponse struct {
	Status uint32
}

func (r *CloseChannelResponse) UnmarshalBinary(data []byte) error {
	if len(data) != 4 {
		return fmt.Errorf("parsing MBIM close channel: payload length is %d, want 4", len(data))
	}
	status := binary.LittleEndian.Uint32(data[:4])
	if status > 0xffff {
		return fmt.Errorf("parsing MBIM close channel: status %#x exceeds two bytes", status)
	}
	r.Status = status
	return nil
}

type APDURequest struct {
	TransactionID   uint32
	MBIMExVersion   uint16
	SlotID          uint32
	Channel         uint32
	SecureMessaging UICCSecureMessaging
	ClassByteType   UICCClassByteType
	Command         []byte
	Response        *APDUResponse
}

func (r *APDURequest) Request() *Request {
	commandOffset := uint32(20)
	if r.MBIMExVersion >= mbimExVersion40 {
		commandOffset = 24
	}

	data := binary.LittleEndian.AppendUint32(nil, r.Channel)
	data = binary.LittleEndian.AppendUint32(data, uint32(r.SecureMessaging))
	data = binary.LittleEndian.AppendUint32(data, uint32(r.ClassByteType))
	data = binary.LittleEndian.AppendUint32(data, uint32(len(r.Command)))
	data = binary.LittleEndian.AppendUint32(data, commandOffset)
	if r.MBIMExVersion >= mbimExVersion40 {
		data = binary.LittleEndian.AppendUint32(data, r.SlotID)
	}
	data = append(data, r.Command...)

	r.Response = new(APDUResponse)
	return &Request{
		MessageType:   MessageTypeCommand,
		TransactionID: r.TransactionID,
		Timeout:       mbimCIDLongResponseTimeout,
		Command: command(
			ServiceMSUICCLowLevelAccess,
			CIDUICCAPDU,
			CommandTypeSet,
			data,
		),
		Response: r.Response,
	}
}

type APDUResponse struct {
	Status   uint32
	Response []byte
}

func (r *APDUResponse) UnmarshalBinary(data []byte) error {
	if len(data) < 12 {
		return errors.New("parsing MBIM APDU: payload is truncated")
	}
	status := binary.LittleEndian.Uint32(data[:4])
	if status > 0xffff {
		return fmt.Errorf("parsing MBIM APDU: status %#x exceeds two bytes", status)
	}
	value, err := uiccByteArrayRef(data, 4, 12)
	if err != nil {
		return fmt.Errorf("parsing MBIM APDU response: %w", err)
	}
	*r = APDUResponse{Status: status, Response: value}
	return nil
}

type UICCTerminalCapabilitySetRequest struct {
	TransactionID uint32
	MBIMExVersion uint16
	SlotID        uint32
	Capabilities  [][]byte
}

func (r *UICCTerminalCapabilitySetRequest) Request() *Request {
	data := terminalCapabilityData(r.Capabilities)
	if r.MBIMExVersion >= mbimExVersion40 {
		data = terminalCapabilityDataEx4(r.SlotID, r.Capabilities)
	}

	return &Request{
		MessageType:   MessageTypeCommand,
		TransactionID: r.TransactionID,
		Timeout:       mbimCIDResponseTimeout,
		Command: command(
			ServiceMSUICCLowLevelAccess,
			CIDUICCTerminalCapability,
			CommandTypeSet,
			data,
		),
	}
}

type UICCTerminalCapabilityQueryRequest struct {
	TransactionID uint32
	MBIMExVersion uint16
	SlotID        uint32
	Response      *UICCTerminalCapabilityResponse
}

func (r *UICCTerminalCapabilityQueryRequest) Request() *Request {
	var data []byte
	if r.MBIMExVersion >= mbimExVersion40 {
		data = binary.LittleEndian.AppendUint32(nil, r.SlotID)
	}

	r.Response = new(UICCTerminalCapabilityResponse)
	return &Request{
		MessageType:   MessageTypeCommand,
		TransactionID: r.TransactionID,
		Timeout:       mbimCIDResponseTimeout,
		Command: command(
			ServiceMSUICCLowLevelAccess,
			CIDUICCTerminalCapability,
			CommandTypeQuery,
			data,
		),
		Response: r.Response,
	}
}

type UICCTerminalCapabilityResponse struct {
	Capabilities [][]byte
}

func (r *UICCTerminalCapabilityResponse) UnmarshalBinary(data []byte) error {
	if len(data) < 4 {
		return errors.New("parsing MBIM terminal capability: payload is truncated")
	}
	capabilityCount := binary.LittleEndian.Uint32(data[:4])
	refs, err := offsetSizeRefs(data, 4, capabilityCount)
	if err != nil {
		return fmt.Errorf("parsing MBIM terminal capability: %w", err)
	}

	var capabilities [][]byte
	if capabilityCount != 0 {
		capabilities = make([][]byte, capabilityCount)
	}
	for i, ref := range refs {
		capabilities[i] = ref.bytes(data)
	}
	r.Capabilities = capabilities
	return nil
}

type UICCResetSetRequest struct {
	TransactionID uint32
	MBIMExVersion uint16
	SlotID        uint32
	Action        UICCPassThroughAction
	Response      *UICCResetResponse
}

func (r *UICCResetSetRequest) Request() *Request {
	data := binary.LittleEndian.AppendUint32(nil, uint32(r.Action))
	if r.MBIMExVersion >= mbimExVersion40 {
		data = binary.LittleEndian.AppendUint32(data, r.SlotID)
	}

	r.Response = new(UICCResetResponse)
	return &Request{
		MessageType:   MessageTypeCommand,
		TransactionID: r.TransactionID,
		Timeout:       mbimCIDLongResponseTimeout,
		Command: command(
			ServiceMSUICCLowLevelAccess,
			CIDUICCReset,
			CommandTypeSet,
			data,
		),
		Response: r.Response,
	}
}

type UICCResetQueryRequest struct {
	TransactionID uint32
	MBIMExVersion uint16
	SlotID        uint32
	Response      *UICCResetResponse
}

func (r *UICCResetQueryRequest) Request() *Request {
	var data []byte
	if r.MBIMExVersion >= mbimExVersion40 {
		data = binary.LittleEndian.AppendUint32(nil, r.SlotID)
	}

	r.Response = new(UICCResetResponse)
	return &Request{
		MessageType:   MessageTypeCommand,
		TransactionID: r.TransactionID,
		Timeout:       mbimCIDLongResponseTimeout,
		Command: command(
			ServiceMSUICCLowLevelAccess,
			CIDUICCReset,
			CommandTypeQuery,
			data,
		),
		Response: r.Response,
	}
}

type UICCResetResponse struct {
	PassThroughStatus UICCPassThroughStatus
}

func (r *UICCResetResponse) UnmarshalBinary(data []byte) error {
	if len(data) != 4 {
		return fmt.Errorf("parsing MBIM UICC reset: payload length is %d, want 4", len(data))
	}
	status := UICCPassThroughStatus(binary.LittleEndian.Uint32(data[:4]))
	if status > UICCPassThroughStatusEnabled {
		return fmt.Errorf("parsing MBIM UICC reset: pass-through status is %d, want 0 or 1", status)
	}
	r.PassThroughStatus = status
	return nil
}

func uiccRefHeader(offset int, value []byte) []byte {
	data := binary.LittleEndian.AppendUint32(nil, uint32(len(value)))
	data = binary.LittleEndian.AppendUint32(data, uint32(offset))
	return data
}

func uiccByteArrayRef(data []byte, fieldOffset, dataStart uint32) ([]byte, error) {
	ref, err := readSizeOffsetRef(data, fieldOffset)
	if err != nil {
		return nil, err
	}
	if err := validateDataBufferRefs(data, dataStart, []valueRef{ref}); err != nil {
		return nil, err
	}
	return ref.bytes(data), nil
}

func terminalCapabilityData(capabilities [][]byte) []byte {
	capabilityCount := uint32(len(capabilities))
	data := binary.LittleEndian.AppendUint32(nil, capabilityCount)
	capabilityOffset := 4 + len(capabilities)*8
	for _, capability := range capabilities {
		data = binary.LittleEndian.AppendUint32(data, uint32(capabilityOffset))
		data = binary.LittleEndian.AppendUint32(data, uint32(len(capability)))
		capabilityOffset = align4(capabilityOffset + len(capability))
	}
	for _, capability := range capabilities {
		data = append(data, capability...)
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
	}
	return data
}

func terminalCapabilityDataEx4(slotID uint32, capabilities [][]byte) []byte {
	capabilityCount := uint32(len(capabilities))
	data := binary.LittleEndian.AppendUint32(nil, slotID)
	data = binary.LittleEndian.AppendUint32(data, capabilityCount)
	capabilityOffset := 8 + len(capabilities)*8
	for _, capability := range capabilities {
		data = binary.LittleEndian.AppendUint32(data, uint32(capabilityOffset))
		data = binary.LittleEndian.AppendUint32(data, uint32(len(capability)))
		capabilityOffset = align4(capabilityOffset + len(capability))
	}
	for _, capability := range capabilities {
		data = append(data, capability...)
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
	}
	return data
}
