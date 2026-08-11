package qcom

import (
	"encoding/binary"
	"errors"
	"os"
	"regexp"
	"testing"

	"github.com/damonto/wwan-go/qcom/tlv"
)

func TestQMIErrorFallbackIncludesCode(t *testing.T) {
	err := QMIError(65000)

	if got, want := err.Error(), "QMI error 65000"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, QMIError(65000)) {
		t.Fatal("QMIError should remain comparable through errors.Is")
	}
}

func TestQMIErrorInvalidArgumentHasText(t *testing.T) {
	if got, want := QMIErrorInvalidArgument.Error(), "invalid argument"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestQMIErrorLaterCodesHaveText(t *testing.T) {
	tests := map[QMIError]string{
		QMIErrorInvalidIndex:               "invalid index",
		QMIErrorOperationInProgress:        "operation in progress",
		QMIErrorCATEnvelopeCommandFailed:   "CAT envelope command failed",
		QMIErrorFWUpdateDiscontinuousFrame: "firmware update discontinuous frame",
	}

	for code, want := range tests {
		if got := code.Error(); got != want {
			t.Fatalf("%d Error() = %q, want %q", code, got, want)
		}
	}
}

func TestQMIErrorTextCoversDeclaredErrors(t *testing.T) {
	data, err := os.ReadFile("errors.go")
	if err != nil {
		t.Fatalf("read errors.go: %v", err)
	}

	constRe := regexp.MustCompile(`(?m)^\s*(QMIError[A-Za-z0-9]+)\s+QMIError\s*=`)
	mapRe := regexp.MustCompile(`(?m)^\s*(QMIError[A-Za-z0-9]+):\s*"`)

	mapped := make(map[string]bool)
	for _, match := range mapRe.FindAllStringSubmatch(string(data), -1) {
		mapped[match[1]] = true
	}

	for _, match := range constRe.FindAllStringSubmatch(string(data), -1) {
		if !mapped[match[1]] {
			t.Fatalf("%s is declared but not mapped to text", match[1])
		}
	}
}

func TestResultError(t *testing.T) {
	tests := []struct {
		name    string
		tlvs    tlv.TLVs
		wantErr error
	}{
		{
			name: "success",
			tlvs: tlv.TLVs{
				tlv.Bytes(0x02, []byte{0x00, 0x00, 0x00, 0x00}),
			},
		},
		{
			name: "failure",
			tlvs: tlv.TLVs{
				tlv.Bytes(0x02, binary.LittleEndian.AppendUint16([]byte{0x01, 0x00}, uint16(QMIErrorNotSupported))),
			},
			wantErr: QMIErrorNotSupported,
		},
		{
			name:    "missing",
			tlvs:    nil,
			wantErr: errNoResultTLV,
		},
		{
			name: "truncated",
			tlvs: tlv.TLVs{
				tlv.Bytes(0x02, []byte{0x00}),
			},
			wantErr: errShortResultTLV,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ResultError(tt.tlvs)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ResultError() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResultError() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestWDSStartNetworkErrorIncludesCallEndReason(t *testing.T) {
	err := &WDSStartNetworkError{
		Err:              QMIErrorCallFailed,
		CallEndReason:    WDSCallEndReasonGenericUnspecified,
		HasCallEndReason: true,
		VerboseCallEndReason: WDSVerboseCallEndReason{
			Type:   WDSVerboseCallEndReasonTypeInternal,
			Reason: WDSVerboseCallEndReasonInternalInterfaceInUseConfigMatch,
		},
		HasVerboseCallEndReason: true,
	}

	if !errors.Is(err, QMIErrorCallFailed) {
		t.Fatal("WDSStartNetworkError should unwrap the QMI error")
	}

	want := "start WDS network: call failed: call end reason generic-unspecified (1): verbose call end reason [internal] interface-in-use-config-match (2,241)"
	if got := err.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestWDSStartNetworkErrorIPFamilyRestriction(t *testing.T) {
	tests := []struct {
		name   string
		reason WDSVerboseCallEndReason
		has    bool
		target error
		want   bool
	}{
		{
			name:   "3GPP IPv4 only",
			reason: WDSVerboseCallEndReason{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPIPv4OnlyAllowed},
			has:    true,
			target: ErrWDSIPv4Only,
			want:   true,
		},
		{
			name:   "3GPP IPv6 only",
			reason: WDSVerboseCallEndReason{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPIPv6OnlyAllowed},
			has:    true,
			target: ErrWDSIPv6Only,
			want:   true,
		},
		{
			name:   "different target",
			reason: WDSVerboseCallEndReason{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPIPv4OnlyAllowed},
			has:    true,
			target: ErrWDSIPv6Only,
		},
		{
			name:   "same value in internal namespace",
			reason: WDSVerboseCallEndReason{Type: WDSVerboseCallEndReasonTypeInternal, Reason: WDSVerboseCallEndReason3GPPIPv4OnlyAllowed},
			has:    true,
			target: ErrWDSIPv4Only,
		},
		{
			name:   "missing verbose reason",
			reason: WDSVerboseCallEndReason{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPIPv4OnlyAllowed},
			target: ErrWDSIPv4Only,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &WDSStartNetworkError{
				Err:                     QMIErrorCallFailed,
				VerboseCallEndReason:    tt.reason,
				HasVerboseCallEndReason: tt.has,
			}
			if got := errors.Is(err, tt.target); got != tt.want {
				t.Fatalf("errors.Is() = %t, want %t", got, tt.want)
			}
			if !errors.Is(err, QMIErrorCallFailed) {
				t.Fatal("WDSStartNetworkError should keep unwrapping the QMI error")
			}
		})
	}
}

func TestWDSVerboseCallEndReasonString(t *testing.T) {
	tests := []struct {
		name   string
		reason WDSVerboseCallEndReason
		want   string
	}{
		{
			name:   "3GPP IPv4 only",
			reason: WDSVerboseCallEndReason{Type: WDSVerboseCallEndReasonType3GPP, Reason: WDSVerboseCallEndReason3GPPIPv4OnlyAllowed},
			want:   "pdn-type-ipv4-only-allowed",
		},
		{
			name: "internal namespace",
			reason: WDSVerboseCallEndReason{
				Type:   WDSVerboseCallEndReasonTypeInternal,
				Reason: WDSVerboseCallEndReasonInternalInterfaceInUseConfigMatch,
			},
			want: "interface-in-use-config-match",
		},
		{
			name:   "unknown",
			reason: WDSVerboseCallEndReason{Type: WDSVerboseCallEndReasonTypePPP, Reason: 50},
			want:   "reason-50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.reason.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWDSIPPreferenceString(t *testing.T) {
	tests := []struct {
		name       string
		preference WDSIPPreference
		want       string
	}{
		{name: "default", preference: WDSIPPreferenceDefault, want: "default"},
		{name: "IPv4", preference: WDSIPPreferenceIPv4, want: "ipv4"},
		{name: "IPv6", preference: WDSIPPreferenceIPv6, want: "ipv6"},
		{name: "unspecified", preference: WDSIPPreferenceUnspecified, want: "unspecified"},
		{name: "unknown", preference: WDSIPPreference(9), want: "preference-9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.preference.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
