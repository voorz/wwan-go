package qmi

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"github.com/voorz/wwan-go/qcom"
	"github.com/voorz/wwan-go/qcom/tlv"
)

func TestBackendBandsReadsConcretePreference(t *testing.T) {
	backend := newRadioTestBackend(t,
		expectRadioRequest(t, qcom.ServiceNAS, qcom.MessageNASGetSystemSelectionPreference,
			uint64TLV(0x15, uint64(1)<<40)),
	)

	got, err := backend.Bands(t.Context())
	if err != nil {
		t.Fatalf("Bands() error = %v", err)
	}
	want := []Band{{Technology: TechnologyLTE, Number: 41}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Bands() = %+v, want %+v", got, want)
	}
}

func TestBackendBandsReadsNRPreferences(t *testing.T) {
	bandValue := func(t *testing.T, bands qcom.NASNR5GBandPreference) []byte {
		t.Helper()
		value, err := bands.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal NR bands: %v", err)
		}
		return value
	}

	sa := qcom.NASNR5GBandPreference{Bits65To128: uint64(1) << (78 - 65)}
	nsa := qcom.NASNR5GBandPreference{Bits1To64: uint64(1) << (41 - 1)}
	deprecated := qcom.NASNR5GBandPreference{Bits1To64: 1}
	want := []Band{
		{Technology: TechnologyNR5GNSA | TechnologyNR5GSA, Number: 41},
		{Technology: TechnologyNR5GNSA | TechnologyNR5GSA, Number: 78},
	}
	tests := []struct {
		name string
		tlvs []tlv.TLV
		want []Band
	}{
		{
			name: "union split SA and NSA preferences",
			tlvs: []tlv.TLV{
				tlv.Bytes(0x2C, bandValue(t, sa)),
				tlv.Bytes(0x2D, bandValue(t, nsa)),
			},
			want: want,
		},
		{
			name: "prefer split preferences over deprecated combined preference",
			tlvs: []tlv.TLV{
				tlv.Bytes(0x28, bandValue(t, deprecated)),
				tlv.Bytes(0x2C, bandValue(t, sa)),
				tlv.Bytes(0x2D, bandValue(t, nsa)),
			},
			want: want,
		},
		{
			name: "ignore deprecated combined preference when one split preference exists",
			tlvs: []tlv.TLV{
				tlv.Bytes(0x28, bandValue(t, deprecated)),
				tlv.Bytes(0x2C, bandValue(t, sa)),
			},
			want: []Band{{Technology: TechnologyNR5GNSA | TechnologyNR5GSA, Number: 78}},
		},
		{
			name: "fall back to deprecated combined preference",
			tlvs: []tlv.TLV{
				tlv.Bytes(0x28, bandValue(t, deprecated)),
			},
			want: []Band{{Technology: TechnologyNR5GNSA | TechnologyNR5GSA, Number: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := newRadioTestBackend(t,
				expectRadioRequest(t, qcom.ServiceNAS, qcom.MessageNASGetSystemSelectionPreference, tt.tlvs...),
			)

			got, err := backend.Bands(t.Context())
			if err != nil {
				t.Fatalf("Bands() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Bands() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBackendBandsRejectsUnavailablePreference(t *testing.T) {
	tests := []struct {
		name string
		tlvs []tlv.TLV
	}{
		{name: "missing preference"},
		{name: "empty known preference", tlvs: []tlv.TLV{uint64TLV(0x15, 0)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := newRadioTestBackend(t,
				expectRadioRequest(t, qcom.ServiceNAS, qcom.MessageNASGetSystemSelectionPreference, tt.tlvs...),
			)
			if _, err := backend.Bands(t.Context()); err == nil {
				t.Fatal("Bands() error = nil, want non-nil")
			}
		})
	}
}

func uint64TLV(typ byte, value uint64) tlv.TLV {
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, value)
	return tlv.Bytes(typ, data)
}

func TestBackendSetBandsRejectsEmptySelection(t *testing.T) {
	err := new(Backend).SetBands(t.Context(), nil)
	if err == nil || errors.Is(err, ErrNotSupported) {
		t.Fatalf("SetBands() error = %v, want required-bands error", err)
	}
}

func TestBackendSetBandsUsesExtendedLTEPreference(t *testing.T) {
	var current qcom.NASLTEBandPreferenceExtended
	currentValue, err := current.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal current LTE bands: %v", err)
	}
	want := qcom.NASLTEBandPreferenceExtended{Bits65To128: 1 << (66 - 65)}
	wantValue, err := want.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal wanted LTE bands: %v", err)
	}
	backend := newRadioTestBackend(t,
		expectRadioRequest(t, qcom.ServiceNAS, qcom.MessageNASGetSystemSelectionPreference,
			tlv.Bytes(0x23, currentValue)),
		func(req qcom.Request) (qcom.Response, error) {
			if req.Service != qcom.ServiceNAS || req.MessageID != qcom.MessageNASSetSystemSelectionPreference {
				t.Fatalf("unexpected QMI request: service=%#x message=%#x", req.Service, req.MessageID)
			}
			got, ok := tlv.Value(req.TLVs, 0x24)
			if !ok || !reflect.DeepEqual(got, wantValue) {
				t.Fatalf("extended LTE preference = % x, want % x", got, wantValue)
			}
			for _, typ := range []byte{0x15, 0x2B, 0x2F, 0x30} {
				if _, ok := tlv.Value(req.TLVs, typ); ok {
					t.Fatalf("unsupported preference TLV %#x is present", typ)
				}
			}
			return successfulStatusResponse(req), nil
		},
	)

	if err := backend.SetBands(t.Context(), []Band{{Technology: TechnologyLTE, Number: 66}}); err != nil {
		t.Fatalf("SetBands() error = %v", err)
	}
}

func TestBackendSetBandsUsesLegacyLTEPreference(t *testing.T) {
	backend := newRadioTestBackend(t,
		expectRadioRequest(t, qcom.ServiceNAS, qcom.MessageNASGetSystemSelectionPreference,
			uint64TLV(0x15, 0)),
		func(req qcom.Request) (qcom.Response, error) {
			got, ok := tlv.Value(req.TLVs, 0x15)
			want := uint64TLV(0x15, uint64(1)<<40).Value
			if !ok || !reflect.DeepEqual(got, want) {
				t.Fatalf("legacy LTE preference = % x, want % x", got, want)
			}
			if _, ok := tlv.Value(req.TLVs, 0x24); ok {
				t.Fatal("extended LTE preference TLV is present")
			}
			return successfulStatusResponse(req), nil
		},
	)

	if err := backend.SetBands(t.Context(), []Band{{Technology: TechnologyLTE, Number: 41}}); err != nil {
		t.Fatalf("SetBands() error = %v", err)
	}
}

func TestBackendSetBandsUsesSplitNRPreferences(t *testing.T) {
	tests := []struct {
		name string
		band Band
		want qcom.NASNR5GBandPreference
	}{
		{
			name: "combined NR family",
			band: Band{Technology: TechnologyNR5GNSA | TechnologyNR5GSA, Number: 78},
			want: qcom.NASNR5GBandPreference{Bits65To128: uint64(1) << (78 - 65)},
		},
		{
			name: "NSA input",
			band: Band{Technology: TechnologyNR5GNSA, Number: 41},
			want: qcom.NASNR5GBandPreference{Bits1To64: uint64(1) << (41 - 1)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, err := tt.want.MarshalBinary()
			if err != nil {
				t.Fatalf("marshal NR bands: %v", err)
			}
			backend := newRadioTestBackend(t,
				expectRadioRequest(t, qcom.ServiceNAS, qcom.MessageNASGetSystemSelectionPreference,
					tlv.Bytes(0x2C, make([]byte, 64)),
					tlv.Bytes(0x2D, make([]byte, 64))),
				func(req qcom.Request) (qcom.Response, error) {
					if req.Service != qcom.ServiceNAS || req.MessageID != qcom.MessageNASSetSystemSelectionPreference {
						t.Fatalf("unexpected QMI request: service=%#x message=%#x", req.Service, req.MessageID)
					}
					if _, ok := tlv.Value(req.TLVs, 0x2B); ok {
						t.Fatal("deprecated combined NR preference TLV is present")
					}
					for _, typ := range []byte{0x2F, 0x30} {
						got, ok := tlv.Value(req.TLVs, typ)
						if !ok || !reflect.DeepEqual(got, want) {
							t.Fatalf("NR preference TLV %#x = % x, want % x", typ, got, want)
						}
					}
					return successfulStatusResponse(req), nil
				},
			)

			if err := backend.SetBands(t.Context(), []Band{tt.band}); err != nil {
				t.Fatalf("SetBands() error = %v", err)
			}
		})
	}
}

func TestBackendSetBandsRejectsDeprecatedNRPreference(t *testing.T) {
	backend := newRadioTestBackend(t,
		expectRadioRequest(t, qcom.ServiceNAS, qcom.MessageNASGetSystemSelectionPreference,
			tlv.Bytes(0x28, make([]byte, 64))),
	)

	err := backend.SetBands(t.Context(), []Band{{Technology: TechnologyNR5GSA, Number: 78}})
	if err == nil {
		t.Fatal("SetBands() error = nil, want unavailable split NR preference error")
	}
}
