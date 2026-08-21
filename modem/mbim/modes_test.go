package mbim

import (
	"slices"
	"testing"

	mbimproto "github.com/voorz/wwan-go/mbim"
)

func TestCanonicalModeTechnology(t *testing.T) {
	tests := []struct {
		name string
		in   Technology
		want Technology
	}{
		{name: "LTE variants", in: TechnologyLTECatM | TechnologyLTENB, want: TechnologyLTE},
		{name: "NR variants", in: TechnologyNR5GSA, want: modeNR5G},
		{
			name: "all families",
			in:   TechnologyGSM | TechnologyUMTS | TechnologyLTE | TechnologyNR5GNSA,
			want: TechnologyGSM | TechnologyUMTS | TechnologyLTE | modeNR5G,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canonicalModeTechnology(tt.in); got != tt.want {
				t.Fatalf("canonicalModeTechnology(%#x) = %#x, want %#x", tt.in, got, tt.want)
			}
		})
	}
}

func TestModeCombinationsContainCurrentSubsets(t *testing.T) {
	all := TechnologyGSM | TechnologyUMTS | TechnologyLTE | modeNR5G
	got := modeCombinations(all)
	want := []Mode{
		{Allowed: all},
		{Allowed: TechnologyLTE},
		{Allowed: TechnologyLTE | modeNR5G},
		{Allowed: modeNR5G},
	}
	for _, mode := range want {
		if !slices.Contains(got, mode) {
			t.Errorf("modeCombinations(%#x) does not contain %+v", all, mode)
		}
	}
}

func TestModeRegistration(t *testing.T) {
	tests := []struct {
		name         string
		registration mbimproto.RegistrationStateInfo
		wantProvider string
		wantAction   mbimproto.RegisterAction
		wantErr      bool
	}{
		{
			name: "keep automatic registration",
			registration: mbimproto.RegistrationStateInfo{
				RegisterMode: mbimproto.RegisterModeAutomatic,
			},
			wantAction: mbimproto.RegisterActionAutomatic,
		},
		{
			name: "automatic registration does not reuse the current provider",
			registration: mbimproto.RegistrationStateInfo{
				RegisterMode: mbimproto.RegisterModeAutomatic,
				ProviderID:   "46000",
			},
			wantAction: mbimproto.RegisterActionAutomatic,
		},
		{
			name: "keep manual registration provider",
			registration: mbimproto.RegistrationStateInfo{
				RegisterMode: mbimproto.RegisterModeManual,
				ProviderID:   "46001",
			},
			wantProvider: "46001",
			wantAction:   mbimproto.RegisterActionManual,
		},
		{
			name: "reject manual registration without provider",
			registration: mbimproto.RegistrationStateInfo{
				RegisterMode: mbimproto.RegisterModeManual,
			},
			wantAction: mbimproto.RegisterActionAutomatic,
			wantErr:    true,
		},
		{
			name:       "reject unknown registration mode",
			wantAction: mbimproto.RegisterActionAutomatic,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, action, err := modeRegistration(tt.registration)
			if (err != nil) != tt.wantErr {
				t.Fatalf("modeRegistration() error = %v, wantErr %t", err, tt.wantErr)
			}
			if provider != tt.wantProvider || action != tt.wantAction {
				t.Fatalf("modeRegistration() = %q, %d, want %q, %d", provider, action, tt.wantProvider, tt.wantAction)
			}
		})
	}
}

func TestDataClassUsesNegotiatedMBIMExVersion(t *testing.T) {
	tests := []struct {
		name       string
		version    uint16
		technology Technology
		want       mbimproto.DataClass
	}{
		{
			name:       "MBIMEx 2 keeps NSA and SA bits",
			version:    mbimExVersion20,
			technology: TechnologyNR5GNSA | TechnologyNR5GSA,
			want:       mbimproto.DataClass5GNSA | mbimproto.DataClass5GSA,
		},
		{
			name:       "MBIMEx 2 keeps NSA only",
			version:    mbimExVersion20,
			technology: TechnologyNR5GNSA,
			want:       mbimproto.DataClass5GNSA,
		},
		{
			name:       "MBIMEx 2 keeps SA only",
			version:    mbimExVersion20,
			technology: TechnologyNR5GSA,
			want:       mbimproto.DataClass5GSA,
		},
		{
			name:       "MBIMEx 3 uses generic 5G bit for NSA",
			version:    mbimExVersion30,
			technology: TechnologyNR5GNSA,
			want:       mbimproto.DataClass5G,
		},
		{
			name:       "MBIMEx 4 uses generic 5G bit for SA",
			version:    0x0400,
			technology: TechnologyLTE | TechnologyNR5GSA,
			want:       mbimproto.DataClassLTE | mbimproto.DataClass5G,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dataClass(tt.version, tt.technology); got != tt.want {
				t.Fatalf("dataClass(%#x, %#x) = %#x, want %#x", tt.version, tt.technology, got, tt.want)
			}
		})
	}
}
