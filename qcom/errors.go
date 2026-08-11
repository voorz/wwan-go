package qcom

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/damonto/wwan-go/qcom/tlv"
)

var (
	errNoResultTLV    = errors.New("no result TLV found")
	errShortResultTLV = errors.New("result TLV too short")
)

// ErrWDSIPv4Only reports that the network restricted a dual-stack WDS call to IPv4.
var ErrWDSIPv4Only = errors.New("WDS network allows IPv4 only")

// ErrWDSIPv6Only reports that the network restricted a dual-stack WDS call to IPv6.
var ErrWDSIPv6Only = errors.New("WDS network allows IPv6 only")

// QMIError represents QMI protocol errors as defined in libqmi
// These correspond to the "Error" field in QMI Result TLVs
type QMIError uint16

// WDSBindMuxDataPortError reports an error from the WDS mux binding command.
// Callers can use errors.As to distinguish bind compatibility failures from
// errors returned by later WDS operations.
type WDSBindMuxDataPortError struct {
	Err error
}

func (e *WDSBindMuxDataPortError) Error() string {
	return fmt.Sprintf("binding WDS mux data port: %v", e.Err)
}

func (e *WDSBindMuxDataPortError) Unwrap() error {
	return e.Err
}

const (
	QMIErrorNone                        QMIError = 0     /*< nick=None >*/
	QMIErrorMalformedMessage            QMIError = 1     /*< nick=MalformedMessage >*/
	QMIErrorNoMemory                    QMIError = 2     /*< nick=NoMemory >*/
	QMIErrorInternal                    QMIError = 3     /*< nick=Internal >*/
	QMIErrorAborted                     QMIError = 4     /*< nick=Aborted >*/
	QMIErrorClientIDsExhausted          QMIError = 5     /*< nick=ClientIdsExhausted >*/
	QMIErrorUnabortableTransaction      QMIError = 6     /*< nick=UnabortableTransaction >*/
	QMIErrorInvalidClientID             QMIError = 7     /*< nick=InvalidClientId >*/
	QMIErrorNoThresholdsProvided        QMIError = 8     /*< nick=NoThresholdsProvided >*/
	QMIErrorInvalidHandle               QMIError = 9     /*< nick=InvalidHandle >*/
	QMIErrorInvalidProfile              QMIError = 10    /*< nick=InvalidProfile >*/
	QMIErrorInvalidPINID                QMIError = 11    /*< nick=InvalidPINId >*/
	QMIErrorIncorrectPIN                QMIError = 12    /*< nick=IncorrectPin >*/
	QMIErrorNoNetworkFound              QMIError = 13    /*< nick=NoNetworkFound >*/
	QMIErrorCallFailed                  QMIError = 14    /*< nick=CallFailed >*/
	QMIErrorOutOfCall                   QMIError = 15    /*< nick=OutOfCall >*/
	QMIErrorNotProvisioned              QMIError = 16    /*< nick=NotProvisioned >*/
	QMIErrorMissingArgument             QMIError = 17    /*< nick=MissingArgument >*/
	QMIErrorArgumentTooLong             QMIError = 19    /*< nick=ArgumentTooLong >*/
	QMIErrorInvalidTransactionID        QMIError = 22    /*< nick=InvalidTransactionId >*/
	QMIErrorDeviceInUse                 QMIError = 23    /*< nick=DeviceInUse >*/
	QMIErrorNetworkUnsupported          QMIError = 24    /*< nick=NetworkUnsupported >*/
	QMIErrorDeviceUnsupported           QMIError = 25    /*< nick=DeviceUnsupported >*/
	QMIErrorNoEffect                    QMIError = 26    /*< nick=NoEffect >*/
	QMIErrorNoFreeProfile               QMIError = 27    /*< nick=NoFreeProfile >*/
	QMIErrorInvalidPDPType              QMIError = 28    /*< nick=InvalidPDPType >*/
	QMIErrorInvalidTechnologyPreference QMIError = 29    /*< nick=InvalidTechnologyPreference >*/
	QMIErrorInvalidProfileType          QMIError = 30    /*< nick=InvalidProfileType >*/
	QMIErrorInvalidServiceType          QMIError = 31    /*< nick=InvalidServiceType >*/
	QMIErrorInvalidRegisterAction       QMIError = 32    /*< nick=InvalidRegisterAction >*/
	QMIErrorInvalidPSAttachAction       QMIError = 33    /*< nick=InvalidPSAttachAction >*/
	QMIErrorAuthenticationFailed        QMIError = 34    /*< nick=AuthenticationFailed >*/
	QMIErrorPINBlocked                  QMIError = 35    /*< nick=PINBlocked >*/
	QMIErrorPINAlwaysBlocked            QMIError = 36    /*< nick=PINAlwaysBlocked >*/
	QMIErrorUIMUninitialized            QMIError = 37    /*< nick=UIMUninitialized >*/
	QMIErrorMaximumQoSRequestsInUse     QMIError = 38    /*< nick=MaximumQoSRequestsInUse >*/
	QMIErrorIncorrectFlowFilter         QMIError = 39    /*< nick=IncorrectFlowFilter >*/
	QMIErrorNetworkQoSUnaware           QMIError = 40    /*< nick=NetworkQoSUnaware >*/
	QMIErrorInvalidQoSID                QMIError = 41    /*< nick=InvalidQoSId >*/
	QMIErrorRequestedNumberUnsupported  QMIError = 42    /*< nick=RequestedNumberUnsupported >*/
	QMIErrorInterfaceNotFound           QMIError = 43    /*< nick=InterfaceNotFound >*/
	QMIErrorFlowSuspended               QMIError = 44    /*< nick=FlowSuspended >*/
	QMIErrorInvalidDataFormat           QMIError = 45    /*< nick=InvalidDataFormat >*/
	QMIErrorGeneralError                QMIError = 46    /*< nick=GeneralError >*/
	QMIErrorUnknownError                QMIError = 47    /*< nick=UnknownError >*/
	QMIErrorInvalidArgument             QMIError = 48    /*< nick=InvalidArgument >*/
	QMIErrorInvalidIndex                QMIError = 49    /*< nick=InvalidIndex >*/
	QMIErrorNoEntry                     QMIError = 50    /*< nick=NoEntry >*/
	QMIErrorDeviceStorageFull           QMIError = 51    /*< nick=DeviceStorageFull >*/
	QMIErrorDeviceNotReady              QMIError = 52    /*< nick=DeviceNotReady >*/
	QMIErrorNetworkNotReady             QMIError = 53    /*< nick=NetworkNotReady >*/
	QMIErrorWMSCauseCode                QMIError = 54    /*< nick=WMSCauseCode >*/
	QMIErrorWMSMessageNotSent           QMIError = 55    /*< nick=WMSMessageNotSent >*/
	QMIErrorWMSMessageDeliveryFailure   QMIError = 56    /*< nick=WMSMessageDeliveryFailure >*/
	QMIErrorWMSInvalidMessageID         QMIError = 57    /*< nick=WMSInvalidMessageId >*/
	QMIErrorWMSEncoding                 QMIError = 58    /*< nick=WMSEncoding >*/
	QMIErrorAuthenticationLock          QMIError = 59    /*< nick=AuthenticationLock >*/
	QMIErrorInvalidTransition           QMIError = 60    /*< nick=InvalidTransition >*/
	QMIErrorNotMcastInterface           QMIError = 61    /*< nick=NotMcastInterface >*/
	QMIErrorMaximumMcastRequestsInUse   QMIError = 62    /*< nick=MaximumMcastRequestsInUse >*/
	QMIErrorInvalidMcastHandle          QMIError = 63    /*< nick=InvalidMcastHandle >*/
	QMIErrorInvalidIPFamilyPreference   QMIError = 64    /*< nick=InvalidIPFamilyPreference >*/
	QMIErrorSessionInactive             QMIError = 65    /*< nick=SessionInactive >*/
	QMIErrorSessionInvalid              QMIError = 66    /*< nick=SessionInvalid >*/
	QMIErrorSessionOwnership            QMIError = 67    /*< nick=SessionOwnership >*/
	QMIErrorInsufficientResources       QMIError = 68    /*< nick=InsufficientResources >*/
	QMIErrorDisabled                    QMIError = 69    /*< nick=Disabled >*/
	QMIErrorInvalidOperation            QMIError = 70    /*< nick=InvalidOperation >*/
	QMIErrorInvalidQMICommand           QMIError = 71    /*< nick=InvalidQMICommand >*/
	QMIErrorWMSTPDUType                 QMIError = 72    /*< nick=WMSTPDUType >*/
	QMIErrorWMSSMSCAddress              QMIError = 73    /*< nick=WMSSMSCAddress >*/
	QMIErrorInformationUnavailable      QMIError = 74    /*< nick=InformationUnavailable >*/
	QMIErrorSegmentTooLong              QMIError = 75    /*< nick=SegmentTooLong >*/
	QMIErrorSegmentOrder                QMIError = 76    /*< nick=SegmentOrder >*/
	QMIErrorBundlingNotSupported        QMIError = 77    /*< nick=BundlingNotSupported >*/
	QMIErrorOperationPartialFailure     QMIError = 78    /*< nick=OperationPartialFailure >*/
	QMIErrorPolicyMismatch              QMIError = 79    /*< nick=PolicyMismatch >*/
	QMIErrorSIMFileNotFound             QMIError = 80    /*< nick=SIMFileNotFound >*/
	QMIErrorExtendedInternal            QMIError = 81    /*< nick=ExtendedInternal >*/
	QMIErrorAccessDenied                QMIError = 82    /*< nick=AccessDenied >*/
	QMIErrorHardwareRestricted          QMIError = 83    /*< nick=HardwareRestricted >*/
	QMIErrorACKNotSent                  QMIError = 84    /*< nick=ACKNotSent >*/
	QMIErrorInjectTimeout               QMIError = 85    /*< nick=InjectTimeout >*/
	QMIErrorIncompatibleState           QMIError = 90    /*< nick=IncompatibleState >*/
	QMIErrorFDNRestrict                 QMIError = 91    /*< nick=FDNRestrict >*/
	QMIErrorSUPSFailureCase             QMIError = 92    /*< nick=SUPSFailureCase >*/
	QMIErrorNoRadio                     QMIError = 93    /*< nick=NoRadio >*/
	QMIErrorNotSupported                QMIError = 94    /*< nick=NotSupported >*/
	QMIErrorNoSubscription              QMIError = 95    /*< nick=NoSubscription >*/
	QMIErrorCardCallControlFailed       QMIError = 96    /*< nick=CardCallControlFailed >*/
	QMIErrorNetworkAborted              QMIError = 97    /*< nick=NetworkAborted >*/
	QMIErrorMsgBlocked                  QMIError = 98    /*< nick=MsgBlocked >*/
	QMIErrorInvalidSessionType          QMIError = 100   /*< nick=InvalidSessionType >*/
	QMIErrorInvalidPbType               QMIError = 101   /*< nick=InvalidPbType >*/
	QMIErrorNoSIM                       QMIError = 102   /*< nick=NoSim >*/
	QMIErrorPbNotReady                  QMIError = 103   /*< nick=PbNotReady >*/
	QMIErrorPINRestriction              QMIError = 104   /*< nick=PINRestriction >*/
	QMIErrorPIN2Restriction             QMIError = 105   /*< nick=PIN1Restriction >*/
	QMIErrorPUKRestriction              QMIError = 106   /*< nick=PUKRestriction >*/
	QMIErrorPUK2Restriction             QMIError = 107   /*< nick=PUK2Restriction >*/
	QMIErrorPbAccessRestricted          QMIError = 108   /*< nick=PbAccessRestricted >*/
	QMIErrorPbDeleteInProgress          QMIError = 109   /*< nick=PbDeleteInProgress >*/
	QMIErrorPbTextTooLong               QMIError = 110   /*< nick=PbTextTooLong >*/
	QMIErrorPbNumberTooLong             QMIError = 111   /*< nick=PbNumberTooLong >*/
	QMIErrorPbHiddenKeyRestriction      QMIError = 112   /*< nick=PbHiddenKeyRestriction >*/
	QMIErrorPbNotAvailable              QMIError = 113   /*< nick=PbNotAvailable >*/
	QMIErrorDeviceMemoryError           QMIError = 114   /*< nick=DeviceMemoryError >*/
	QMIErrorNoPermission                QMIError = 115   /*< nick=NoPermission >*/
	QMIErrorTooSoon                     QMIError = 116   /*< nick=TooSoon >*/
	QMIErrorTimeNotAcquired             QMIError = 117   /*< nick=TimeNotAcquired >*/
	QMIErrorOperationInProgress         QMIError = 118   /*< nick=OperationInProgress >*/
	QMIErrorFWWriteFailed               QMIError = 388   /*< nick=FWWriteFailed >*/
	QMIErrorFWInfoReadFailed            QMIError = 389   /*< nick=FWInfoReadFailed >*/
	QMIErrorFWFileNotFound              QMIError = 390   /*< nick=FWFileNotFound >*/
	QMIErrorFWDirNotFound               QMIError = 391   /*< nick=FWDirNotFound >*/
	QMIErrorFWAlreadyActivated          QMIError = 392   /*< nick=FWAlreadyActivated >*/
	QMIErrorFWCannotGenericImage        QMIError = 393   /*< nick=FWCannotGenericImage >*/
	QMIErrorFWFileOpenFailed            QMIError = 400   /*< nick=FWFileOpenFailed >*/
	QMIErrorFWUpdateDiscontinuousFrame  QMIError = 401   /*< nick=FWUpdateDiscontinuousFrame >*/
	QMIErrorFWUpdateFailed              QMIError = 402   /*< nick=FWUpdateFailed >*/
	QMIErrorCATEventRegistrationFailed  QMIError = 61441 /*< nick=CatEventRegistrationFailed >*/
	QMIErrorCATInvalidTerminalResponse  QMIError = 61442 /*< nick=CatInvalidTerminalResponse >*/
	QMIErrorCATInvalidEnvelopeCommand   QMIError = 61443 /*< nick=CatInvalidEnvelopeCommand >*/
	QMIErrorCATEnvelopeCommandBusy      QMIError = 61444 /*< nick=CatEnvelopeCommandBusy >*/
	QMIErrorCATEnvelopeCommandFailed    QMIError = 61445 /*< nick=CatEnvelopeCommandFailed >*/
)

var qmiErrorText = map[QMIError]string{
	QMIErrorNone:                        "no error",
	QMIErrorMalformedMessage:            "malformed message",
	QMIErrorNoMemory:                    "no memory",
	QMIErrorInternal:                    "internal error",
	QMIErrorAborted:                     "aborted",
	QMIErrorClientIDsExhausted:          "client IDs exhausted",
	QMIErrorUnabortableTransaction:      "unabortable transaction",
	QMIErrorInvalidClientID:             "invalid client ID",
	QMIErrorNoThresholdsProvided:        "no thresholds provided",
	QMIErrorInvalidHandle:               "invalid handle",
	QMIErrorInvalidProfile:              "invalid profile",
	QMIErrorInvalidPINID:                "invalid PIN ID",
	QMIErrorIncorrectPIN:                "incorrect PIN",
	QMIErrorNoNetworkFound:              "no network found",
	QMIErrorCallFailed:                  "call failed",
	QMIErrorOutOfCall:                   "out of call",
	QMIErrorNotProvisioned:              "not provisioned",
	QMIErrorMissingArgument:             "missing argument",
	QMIErrorArgumentTooLong:             "argument too long",
	QMIErrorInvalidTransactionID:        "invalid transaction ID",
	QMIErrorDeviceInUse:                 "device in use",
	QMIErrorNetworkUnsupported:          "network unsupported",
	QMIErrorDeviceUnsupported:           "device unsupported",
	QMIErrorNoEffect:                    "no effect",
	QMIErrorNoFreeProfile:               "no free profile",
	QMIErrorInvalidPDPType:              "invalid PDP type",
	QMIErrorInvalidTechnologyPreference: "invalid technology preference",
	QMIErrorInvalidProfileType:          "invalid profile type",
	QMIErrorInvalidServiceType:          "invalid service type",
	QMIErrorInvalidRegisterAction:       "invalid register action",
	QMIErrorInvalidPSAttachAction:       "invalid PS attach action",
	QMIErrorAuthenticationFailed:        "authentication failed",
	QMIErrorPINBlocked:                  "PIN blocked",
	QMIErrorPINAlwaysBlocked:            "PIN always blocked",
	QMIErrorUIMUninitialized:            "UIM uninitialized",
	QMIErrorMaximumQoSRequestsInUse:     "maximum QoS requests in use",
	QMIErrorIncorrectFlowFilter:         "incorrect flow filter",
	QMIErrorNetworkQoSUnaware:           "network QoS unaware",
	QMIErrorInvalidQoSID:                "invalid QoS ID",
	QMIErrorRequestedNumberUnsupported:  "requested number unsupported",
	QMIErrorInterfaceNotFound:           "interface not found",
	QMIErrorFlowSuspended:               "flow suspended",
	QMIErrorInvalidDataFormat:           "invalid data format",
	QMIErrorGeneralError:                "general error",
	QMIErrorUnknownError:                "unknown error",
	QMIErrorInvalidArgument:             "invalid argument",
	QMIErrorInvalidIndex:                "invalid index",
	QMIErrorNoEntry:                     "no entry",
	QMIErrorDeviceStorageFull:           "device storage full",
	QMIErrorDeviceNotReady:              "device not ready",
	QMIErrorNetworkNotReady:             "network not ready",
	QMIErrorWMSCauseCode:                "WMS cause code",
	QMIErrorWMSMessageNotSent:           "WMS message not sent",
	QMIErrorWMSMessageDeliveryFailure:   "WMS message delivery failure",
	QMIErrorWMSInvalidMessageID:         "WMS invalid message ID",
	QMIErrorWMSEncoding:                 "WMS encoding",
	QMIErrorAuthenticationLock:          "authentication lock",
	QMIErrorInvalidTransition:           "invalid transition",
	QMIErrorNotMcastInterface:           "not multicast interface",
	QMIErrorMaximumMcastRequestsInUse:   "maximum multicast requests in use",
	QMIErrorInvalidMcastHandle:          "invalid multicast handle",
	QMIErrorInvalidIPFamilyPreference:   "invalid IP family preference",
	QMIErrorSessionInactive:             "session inactive",
	QMIErrorSessionInvalid:              "session invalid",
	QMIErrorSessionOwnership:            "session ownership",
	QMIErrorInsufficientResources:       "insufficient resources",
	QMIErrorDisabled:                    "disabled",
	QMIErrorInvalidOperation:            "invalid operation",
	QMIErrorInvalidQMICommand:           "invalid QMI command",
	QMIErrorWMSTPDUType:                 "WMS TPDU type",
	QMIErrorWMSSMSCAddress:              "WMS SMSC address",
	QMIErrorInformationUnavailable:      "information unavailable",
	QMIErrorSegmentTooLong:              "segment too long",
	QMIErrorSegmentOrder:                "segment order",
	QMIErrorBundlingNotSupported:        "bundling not supported",
	QMIErrorOperationPartialFailure:     "operation partial failure",
	QMIErrorPolicyMismatch:              "policy mismatch",
	QMIErrorSIMFileNotFound:             "SIM file not found",
	QMIErrorExtendedInternal:            "extended internal error",
	QMIErrorAccessDenied:                "access denied",
	QMIErrorHardwareRestricted:          "hardware restricted",
	QMIErrorACKNotSent:                  "ACK not sent",
	QMIErrorInjectTimeout:               "inject timeout",
	QMIErrorIncompatibleState:           "incompatible state",
	QMIErrorFDNRestrict:                 "FDN restrict",
	QMIErrorSUPSFailureCase:             "SUPS failure case",
	QMIErrorNoRadio:                     "no radio",
	QMIErrorNotSupported:                "not supported",
	QMIErrorNoSubscription:              "no subscription",
	QMIErrorCardCallControlFailed:       "card call control failed",
	QMIErrorNetworkAborted:              "network aborted",
	QMIErrorMsgBlocked:                  "message blocked",
	QMIErrorInvalidSessionType:          "invalid session type",
	QMIErrorInvalidPbType:               "invalid phonebook type",
	QMIErrorNoSIM:                       "no SIM",
	QMIErrorPbNotReady:                  "phonebook not ready",
	QMIErrorPINRestriction:              "PIN restriction",
	QMIErrorPIN2Restriction:             "PIN2 restriction",
	QMIErrorPUKRestriction:              "PUK restriction",
	QMIErrorPUK2Restriction:             "PUK2 restriction",
	QMIErrorPbAccessRestricted:          "phonebook access restricted",
	QMIErrorPbDeleteInProgress:          "phonebook delete in progress",
	QMIErrorPbTextTooLong:               "phonebook text too long",
	QMIErrorPbNumberTooLong:             "phonebook number too long",
	QMIErrorPbHiddenKeyRestriction:      "phonebook hidden key restriction",
	QMIErrorPbNotAvailable:              "phonebook not available",
	QMIErrorDeviceMemoryError:           "device memory error",
	QMIErrorNoPermission:                "no permission",
	QMIErrorTooSoon:                     "too soon",
	QMIErrorTimeNotAcquired:             "time not acquired",
	QMIErrorOperationInProgress:         "operation in progress",
	QMIErrorFWWriteFailed:               "firmware write failed",
	QMIErrorFWInfoReadFailed:            "firmware info read failed",
	QMIErrorFWFileNotFound:              "firmware file not found",
	QMIErrorFWDirNotFound:               "firmware directory not found",
	QMIErrorFWAlreadyActivated:          "firmware already activated",
	QMIErrorFWCannotGenericImage:        "firmware cannot generic image",
	QMIErrorFWFileOpenFailed:            "firmware file open failed",
	QMIErrorFWUpdateDiscontinuousFrame:  "firmware update discontinuous frame",
	QMIErrorFWUpdateFailed:              "firmware update failed",
	QMIErrorCATEventRegistrationFailed:  "CAT event registration failed",
	QMIErrorCATInvalidTerminalResponse:  "CAT invalid terminal response",
	QMIErrorCATInvalidEnvelopeCommand:   "CAT invalid envelope command",
	QMIErrorCATEnvelopeCommandBusy:      "CAT envelope command busy",
	QMIErrorCATEnvelopeCommandFailed:    "CAT envelope command failed",
}

func (q QMIError) Error() string {
	if text, ok := qmiErrorText[q]; ok {
		return text
	}
	return fmt.Sprintf("QMI error %d", q)
}

// WDSStartNetworkError keeps modem call-end details attached to a QMI failure.
type WDSStartNetworkError struct {
	Err                     error
	CallEndReason           WDSCallEndReason
	HasCallEndReason        bool
	VerboseCallEndReason    WDSVerboseCallEndReason
	HasVerboseCallEndReason bool
}

func (e *WDSStartNetworkError) Error() string {
	if e == nil {
		return "<nil>"
	}

	msg := "start WDS network"
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	if e.HasCallEndReason {
		msg += fmt.Sprintf(": call end reason %s (%d)", e.CallEndReason, uint16(e.CallEndReason))
	}
	if e.HasVerboseCallEndReason {
		reason := e.VerboseCallEndReason
		msg += fmt.Sprintf(": verbose call end reason [%s] %s (%d,%d)",
			reason.Type,
			reason,
			uint16(reason.Type),
			reason.Reason,
		)
	}
	return msg
}

func (e *WDSStartNetworkError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is reports network address-family restrictions while preserving Err through Unwrap.
func (e *WDSStartNetworkError) Is(target error) bool {
	if e == nil || !e.HasVerboseCallEndReason || e.VerboseCallEndReason.Type != WDSVerboseCallEndReasonType3GPP {
		return false
	}
	switch target {
	case ErrWDSIPv4Only:
		return e.VerboseCallEndReason.Reason == WDSVerboseCallEndReason3GPPIPv4OnlyAllowed
	case ErrWDSIPv6Only:
		return e.VerboseCallEndReason.Reason == WDSVerboseCallEndReason3GPPIPv6OnlyAllowed
	default:
		return false
	}
}

func (r WDSCallEndReason) String() string {
	if text, ok := wdsCallEndReasonText[r]; ok {
		return text
	}
	return fmt.Sprintf("WDS call end reason %d", r)
}

func (t WDSVerboseCallEndReasonType) String() string {
	if text, ok := wdsVerboseCallEndReasonTypeText[t]; ok {
		return text
	}
	return fmt.Sprintf("type-%d", t)
}

func (r WDSVerboseCallEndReason) String() string {
	if text, ok := wdsVerboseCallEndReasonText[r]; ok {
		return text
	}
	return fmt.Sprintf("reason-%d", r.Reason)
}

var wdsCallEndReasonText = map[WDSCallEndReason]string{
	WDSCallEndReasonGenericUnspecified: "generic-unspecified",
}

var wdsVerboseCallEndReasonTypeText = map[WDSVerboseCallEndReasonType]string{
	WDSVerboseCallEndReasonTypeMIP:      "mip",
	WDSVerboseCallEndReasonTypeInternal: "internal",
	WDSVerboseCallEndReasonTypeCM:       "cm",
	WDSVerboseCallEndReasonType3GPP:     "3gpp",
	WDSVerboseCallEndReasonTypePPP:      "ppp",
	WDSVerboseCallEndReasonTypeEHRPD:    "ehrpd",
	WDSVerboseCallEndReasonTypeIPv6:     "ipv6",
}

var wdsVerboseCallEndReasonText = map[WDSVerboseCallEndReason]string{
	{Type: WDSVerboseCallEndReasonTypeInternal, Reason: WDSVerboseCallEndReasonInternalPDNIPv4CallDisallowed}:     "pdn-ipv4-call-disallowed",
	{Type: WDSVerboseCallEndReasonTypeInternal, Reason: WDSVerboseCallEndReasonInternalPDNIPv6CallDisallowed}:     "pdn-ipv6-call-disallowed",
	{Type: WDSVerboseCallEndReasonTypeInternal, Reason: WDSVerboseCallEndReasonInternalCallAlreadyPresent}:        "call-already-present",
	{Type: WDSVerboseCallEndReasonTypeInternal, Reason: WDSVerboseCallEndReasonInternalInterfaceInUse}:            "interface-in-use",
	{Type: WDSVerboseCallEndReasonTypeInternal, Reason: WDSVerboseCallEndReasonInternalInterfaceInUseConfigMatch}: "interface-in-use-config-match",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPOperatorDeterminedBarring}:         "operator-determined-barring",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPLLCSNDCPFailure}:                   "llc-sndcp-failure",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPInsufficientResources}:             "insufficient-resources",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPUnknownAPN}:                        "unknown-apn",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPUnknownPDP}:                        "unknown-pdp",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPAuthenticationFailed}:              "authentication-failed",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPGGSNReject}:                        "ggsn-reject",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPActivationReject}:                  "activation-reject",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPOptionNotSupported}:                "option-not-supported",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPOptionUnsubscribed}:                "option-unsubscribed",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPOptionTemporarilyOutOfOrder}:       "option-temporarily-out-of-order",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPNSAPIAlreadyUsed}:                  "nsapi-already-used",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPRegularDeactivation}:               "regular-deactivation",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPQoSNotAccepted}:                    "qos-not-accepted",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPNetworkFailure}:                    "network-failure",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPUMTSReactivationRequested}:         "umts-reactivation-requested",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPFeatureNotSupported}:               "feature-not-supported",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPTFTSemanticError}:                  "tft-semantic-error",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPTFTSyntaxError}:                    "tft-syntax-error",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPUnknownPDPContext}:                 "unknown-pdp-context",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPFilterSemanticError}:               "filter-semantic-error",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPFilterSyntaxError}:                 "filter-syntax-error",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPPDPWithoutActiveTFT}:               "pdp-without-active-tft",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPIPv4OnlyAllowed}:                   "pdn-type-ipv4-only-allowed",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPIPv6OnlyAllowed}:                   "pdn-type-ipv6-only-allowed",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPSingleAddressBearerOnly}:           "single-address-bearer-only",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPInvalidTransactionID}:              "invalid-transaction-id",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPMessageIncorrectSemantic}:          "message-incorrect-semantic",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPInvalidMandatoryInformation}:       "invalid-mandatory-information",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPMessageTypeUnsupported}:            "message-type-unsupported",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPMessageTypeIncompatibleState}:      "message-type-incompatible-state",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPUnknownInformationElement}:         "unknown-information-element",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPConditionalInformationError}:       "conditional-information-error",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPMessageProtocolStateMismatch}:      "message-protocol-state-mismatch",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPProtocolError}:                     "protocol-error",
	{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPAPNTypeConflict}:                   "apn-type-conflict",
}

func ResultError(tlvs tlv.TLVs) error {
	item, ok := tlvs.Find(0x02)
	if !ok {
		return errNoResultTLV
	}
	return ResultTLVError(item)
}

func ResultTLVError(item tlv.TLV) error {
	if len(item.Value) < 4 {
		return fmt.Errorf("%w, expected 4 bytes, got %d", errShortResultTLV, len(item.Value))
	}
	if binary.LittleEndian.Uint16(item.Value[0:2]) == uint16(QMIResultSuccess) {
		return nil
	}
	return QMIError(binary.LittleEndian.Uint16(item.Value[2:4]))
}
