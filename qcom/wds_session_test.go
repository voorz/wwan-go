package qcom

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/voorz/wwan-go/qcom/tlv"
)

func TestOpenPDN(t *testing.T) {
	tests := []struct {
		name string
		cfg  PDNConfig
	}{
		{
			name: "authenticated IPv4 call on selected subscription and mux",
			cfg: PDNConfig{
				APN:            " internet ",
				Authentication: WDSAuthenticationPAP | WDSAuthenticationCHAP,
				Username:       "subscriber",
				Password:       "secret",
				IPPreference:   WDSIPPreferenceIPv4,
				Subscription:   ptr(WDSSubscriptionSecondary),
				MuxDataPort:    &WDSMuxDataPort{MuxID: 3},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &fakeTransport{t: t, calls: []transportCall{
				{resp: allocatedClientResponse(ServiceWDS, 2)},
				{
					check: func(req Request) {
						if req.MessageID != MessageWDSBindSubscription {
							t.Fatalf("message = 0x%04X, want bind subscription", req.MessageID)
						}
					},
					resp: successResponse(MessageWDSBindSubscription),
				},
				{
					check: func(req Request) {
						if req.MessageID != MessageWDSBindMuxDataPort {
							t.Fatalf("message = 0x%04X, want bind mux data port", req.MessageID)
						}
					},
					resp: successResponse(MessageWDSBindMuxDataPort),
				},
				{
					check: func(req Request) {
						if req.MessageID != MessageWDSSetClientIPFamily {
							t.Fatalf("message = 0x%04X, want set IP family", req.MessageID)
						}
					},
					resp: successResponse(MessageWDSSetClientIPFamily),
				},
				{
					check: func(req Request) {
						if req.MessageID != MessageWDSStartNetworkInterface {
							t.Fatalf("message = 0x%04X, want start network", req.MessageID)
						}
						assertTLV(t, req.TLVs, 0x14, []byte("internet"))
						assertTLV(t, req.TLVs, 0x16, []byte{byte(WDSAuthenticationPAP | WDSAuthenticationCHAP)})
						assertTLV(t, req.TLVs, 0x17, []byte("subscriber"))
						assertTLV(t, req.TLVs, 0x18, []byte("secret"))
					},
					resp: successResponse(MessageWDSStartNetworkInterface, tlv.Uint(0x01, uint32(0x01020304))),
				},
				{
					check: func(req Request) {
						assertTLV(t, req.TLVs, 0x10, uint32ValueForTest(uint32(WDSRuntimeRequestedNetworkSettings)))
					},
					resp: successResponse(MessageWDSGetRuntimeSettings,
						tlv.Bytes(0x1E, []byte{2, 0, 0, 10}),
						tlv.Bytes(0x2B, []byte{byte(WDSIPFamilyIPv4)}),
					),
				},
				{resp: successResponse(MessageWDSStopNetworkInterface)},
				{resp: successResponse(MessageReleaseClientID)},
			}}
			client := &Client{transport: transport, slot: 1}

			session, err := client.OpenPDN(context.Background(), tt.cfg)
			if err != nil {
				t.Fatalf("OpenPDN() error = %v", err)
			}
			info := session.Info()
			if !info.LocalIPv4.Equal(net.IPv4(10, 0, 0, 2)) || info.IPFamily != WDSIPFamilyIPv4 || !info.PacketDataReady {
				t.Fatalf("Info() = %+v", info)
			}
			if err := session.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if got := transport.callCount(); got != len(transport.calls) {
				t.Fatalf("Do() calls = %d, want %d", got, len(transport.calls))
			}
		})
	}
}

func TestOpenPDNsDispatchesOrderedStartsAfterPreparingAllClients(t *testing.T) {
	tests := []struct {
		name        string
		preferences [2]WDSIPPreference
		families    [2]WDSIPFamily
	}{
		{
			name:        "IPv4 then IPv6",
			preferences: [2]WDSIPPreference{WDSIPPreferenceIPv4, WDSIPPreferenceIPv6},
			families:    [2]WDSIPFamily{WDSIPFamilyIPv4, WDSIPFamilyIPv6},
		},
		{
			name:        "IPv6 then IPv4",
			preferences: [2]WDSIPPreference{WDSIPPreferenceIPv6, WDSIPPreferenceIPv4},
			families:    [2]WDSIPFamily{WDSIPFamilyIPv6, WDSIPFamilyIPv4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secondStartDispatched := make(chan struct{})
			firstWaitedEarly := make(chan struct{}, 1)
			calls := make([]transportCall, 0, 14)
			for i, family := range tt.families {
				clientID := uint8(i + 2)
				calls = append(calls,
					transportCall{resp: allocatedClientResponse(ServiceWDS, clientID)},
					transportCall{
						check: func(req Request) {
							if req.MessageID != MessageWDSBindMuxDataPort || req.ClientID != clientID {
								t.Fatalf("bind mux request = %+v", req)
							}
						},
						resp: successResponse(MessageWDSBindMuxDataPort),
					},
					transportCall{
						check: func(req Request) {
							if req.MessageID != MessageWDSSetClientIPFamily || req.ClientID != clientID {
								t.Fatalf("set family request = %+v", req)
							}
							assertTLV(t, req.TLVs, 0x01, []byte{byte(family)})
						},
						resp: successResponse(MessageWDSSetClientIPFamily),
					},
				)
			}
			for i, preference := range tt.preferences {
				clientID := uint8(i + 2)
				call := transportCall{
					check: func(req Request) {
						if req.MessageID != MessageWDSStartNetworkInterface || req.ClientID != clientID {
							t.Fatalf("Start Network request = %+v", req)
						}
						assertTLV(t, req.TLVs, 0x19, []byte{byte(preference)})
						if i == 1 {
							close(secondStartDispatched)
						}
					},
					resp: successResponse(
						MessageWDSStartNetworkInterface,
						tlv.Uint(0x01, uint32(0x01020304+i)),
					),
				}
				if i == 0 {
					call.wait = func() {
						select {
						case <-secondStartDispatched:
						default:
							firstWaitedEarly <- struct{}{}
						}
					}
				}
				calls = append(calls, call)
			}
			for _, family := range tt.families {
				calls = append(calls, transportCall{
					resp: successResponse(MessageWDSGetRuntimeSettings, tlv.Bytes(0x2B, []byte{byte(family)})),
				})
			}
			for range tt.families {
				calls = append(calls,
					transportCall{resp: successResponse(MessageWDSStopNetworkInterface)},
					transportCall{resp: successResponse(MessageReleaseClientID)},
				)
			}

			transport := &fakeTransport{t: t, calls: calls}
			client := &Client{transport: transport, slot: 1}
			mux := &WDSMuxDataPort{MuxID: 1}
			configs := make([]PDNConfig, len(tt.preferences))
			for i, preference := range tt.preferences {
				configs[i] = PDNConfig{APN: "internet", IPPreference: preference, MuxDataPort: mux}
			}

			results, err := client.OpenPDNs(t.Context(), configs)
			if err != nil {
				t.Fatalf("OpenPDNs() error = %v", err)
			}
			select {
			case <-firstWaitedEarly:
				t.Fatal("first response awaited before second Start Network was dispatched")
			default:
			}
			if len(results) != len(tt.families) {
				t.Fatalf("OpenPDNs() results = %d, want %d", len(results), len(tt.families))
			}
			for i, result := range results {
				if result.Err != nil || result.Session == nil {
					t.Fatalf("OpenPDNs() result %d = %+v", i, result)
				}
				if got := result.Session.Info().IPFamily; got != tt.families[i] {
					t.Fatalf("session %d family = %d, want %d", i, got, tt.families[i])
				}
				if err := result.Session.Close(); err != nil {
					t.Fatalf("Close(%d) error = %v", i, err)
				}
			}
			if got := transport.callCount(); got != len(transport.calls) {
				t.Fatalf("transport calls = %d, want %d", got, len(transport.calls))
			}
		})
	}
}

func TestOpenPDNsResultSemantics(t *testing.T) {
	startSuccess := func(handle uint32) Response {
		return successResponse(MessageWDSStartNetworkInterface, tlv.Uint(0x01, handle))
	}
	tests := []struct {
		name         string
		start        [2]Response
		wantSessions [2]bool
		wantErr      bool
	}{
		{
			name:         "IPv4 succeeds",
			start:        [2]Response{startSuccess(0x01020304), errorResponse(MessageWDSStartNetworkInterface, QMIErrorCallFailed)},
			wantSessions: [2]bool{true, false},
		},
		{
			name:         "IPv6 succeeds",
			start:        [2]Response{errorResponse(MessageWDSStartNetworkInterface, QMIErrorCallFailed), startSuccess(0x01020306)},
			wantSessions: [2]bool{false, true},
		},
		{
			name: "both fail",
			start: [2]Response{
				errorResponse(MessageWDSStartNetworkInterface, QMIErrorCallFailed),
				errorResponse(MessageWDSStartNetworkInterface, QMIErrorCallFailed),
			},
			wantErr: true,
		},
	}
	families := [2]WDSIPFamily{WDSIPFamilyIPv4, WDSIPFamilyIPv6}
	preferences := [2]WDSIPPreference{WDSIPPreferenceIPv4, WDSIPPreferenceIPv6}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := make([]transportCall, 0, 14)
			for i := range families {
				calls = append(calls,
					transportCall{resp: allocatedClientResponse(ServiceWDS, uint8(i+2))},
					transportCall{resp: successResponse(MessageWDSBindMuxDataPort)},
					transportCall{resp: successResponse(MessageWDSSetClientIPFamily)},
				)
			}
			for i, response := range tt.start {
				clientID := uint8(i + 2)
				calls = append(calls, transportCall{
					check: func(req Request) {
						if req.MessageID != MessageWDSStartNetworkInterface || req.ClientID != clientID {
							t.Fatalf("Start Network request = %+v", req)
						}
					},
					resp: response,
				})
			}
			for i, wantSession := range tt.wantSessions {
				if wantSession {
					calls = append(calls, transportCall{
						resp: successResponse(MessageWDSGetRuntimeSettings, tlv.Bytes(0x2B, []byte{byte(families[i])})),
					})
					continue
				}
				calls = append(calls, transportCall{resp: successResponse(MessageReleaseClientID)})
			}
			for _, wantSession := range tt.wantSessions {
				if wantSession {
					calls = append(calls,
						transportCall{resp: successResponse(MessageWDSStopNetworkInterface)},
						transportCall{resp: successResponse(MessageReleaseClientID)},
					)
				}
			}

			transport := &fakeTransport{t: t, calls: calls}
			client := &Client{transport: transport, slot: 1}
			mux := &WDSMuxDataPort{MuxID: 1}
			configs := []PDNConfig{
				{APN: "internet", IPPreference: preferences[0], MuxDataPort: mux},
				{APN: "internet", IPPreference: preferences[1], MuxDataPort: mux},
			}

			results, err := client.OpenPDNs(t.Context(), configs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("OpenPDNs() error = %v, want error %t", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, QMIErrorCallFailed) {
				t.Fatalf("OpenPDNs() error = %v, want call failed", err)
			}
			if len(results) != len(tt.wantSessions) {
				t.Fatalf("OpenPDNs() results = %d, want %d", len(results), len(tt.wantSessions))
			}
			for i, result := range results {
				if (result.Session != nil) != tt.wantSessions[i] {
					t.Fatalf("result %d session = %v, want present %t", i, result.Session, tt.wantSessions[i])
				}
				if (result.Err == nil) != tt.wantSessions[i] {
					t.Fatalf("result %d error = %v, want session %t", i, result.Err, tt.wantSessions[i])
				}
				if result.Session != nil {
					if err := result.Session.Close(); err != nil {
						t.Fatalf("Close(%d) error = %v", i, err)
					}
				}
			}
			if got := transport.callCount(); got != len(transport.calls) {
				t.Fatalf("transport calls = %d, want %d", got, len(transport.calls))
			}
		})
	}
}

func TestOpenPDNsRejectsUnsupportedTransportBeforePreparing(t *testing.T) {
	tests := []struct {
		name    string
		configs []PDNConfig
	}{
		{
			name: "ordered dispatch unavailable",
			configs: []PDNConfig{
				{IPPreference: WDSIPPreferenceIPv4, MuxDataPort: &WDSMuxDataPort{MuxID: 1}},
				{IPPreference: WDSIPPreferenceIPv6, MuxDataPort: &WDSMuxDataPort{MuxID: 1}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &doOnlyTransport{}
			client := &Client{transport: transport, slot: 1}

			results, err := client.OpenPDNs(t.Context(), tt.configs)
			if !errors.Is(err, errRequestDispatchUnsupported) {
				t.Fatalf("OpenPDNs() error = %v, want unsupported dispatch", err)
			}
			if results != nil {
				t.Fatalf("OpenPDNs() results = %+v, want nil", results)
			}
			if got := transport.callCount(); got != 0 {
				t.Fatalf("Do() calls = %d, want 0", got)
			}
		})
	}
}

func TestPDNSessionClose(t *testing.T) {
	tests := []struct {
		name     string
		stopResp Response
		wantErr  error
	}{
		{name: "stops active packet call", stopResp: successResponse(MessageWDSStopNetworkInterface)},
		{name: "accepts already stopped packet call", stopResp: errorResponse(MessageWDSStopNetworkInterface, QMIErrorNoEffect)},
		{name: "returns other stop error", stopResp: errorResponse(MessageWDSStopNetworkInterface, QMIErrorCallFailed), wantErr: QMIErrorCallFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &fakeTransport{t: t, calls: []transportCall{
				{resp: tt.stopResp},
				{resp: successResponse(MessageReleaseClientID)},
			}}
			done := make(chan struct{})
			session := &PDNSession{
				client:           &Client{transport: transport, slot: 1},
				timeout:          DefaultRequestTimeout,
				wdsClientID:      2,
				wdsClientReady:   true,
				releaseWDSClient: true,
				packetDataHandle: 0x01020304,
				connectionStatus: WDSConnectionStatusConnected,
				done:             done,
			}

			err := session.Close()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Close() error = %v, want %v", err, tt.wantErr)
			}
			if session.wdsClientID != 0 || session.wdsClientReady || session.releaseWDSClient || session.packetDataHandle != 0 {
				t.Fatalf("closed session state = client %d ready %t release %t handle %d", session.wdsClientID, session.wdsClientReady, session.releaseWDSClient, session.packetDataHandle)
			}
			select {
			case <-done:
			default:
				t.Fatal("Close() did not close session lifetime")
			}
			if got := transport.callCount(); got != len(transport.calls) {
				t.Fatalf("Do() calls = %d, want %d", got, len(transport.calls))
			}
		})
	}
}

func TestOpenPDNLegacyMuxKeepsRequestedIPFamily(t *testing.T) {
	tests := []struct {
		name       string
		preference WDSIPPreference
	}{
		{name: "IPv4", preference: WDSIPPreferenceIPv4},
		{name: "IPv6", preference: WDSIPPreferenceIPv6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &fakeTransport{t: t, calls: []transportCall{
				{resp: allocatedClientResponse(ServiceWDS, 2)},
				{
					check: func(req Request) {
						if req.MessageID != MessageWDSLegacyBindMuxDataPort {
							t.Fatalf("MessageID = 0x%04X, want Legacy Bind Data Port", req.MessageID)
						}
					},
					resp: successResponse(MessageWDSLegacyBindMuxDataPort),
				},
				{
					check: func(req Request) {
						if req.MessageID != MessageWDSSetClientIPFamily {
							t.Fatalf("MessageID = 0x%04X, want Set Client IP Family", req.MessageID)
						}
					},
					resp: successResponse(MessageWDSSetClientIPFamily),
				},
				{
					check: func(req Request) {
						if req.MessageID != MessageWDSStartNetworkInterface {
							t.Fatalf("MessageID = 0x%04X, want Start Network", req.MessageID)
						}
						assertTLV(t, req.TLVs, 0x19, []byte{byte(tt.preference)})
					},
					resp: successResponse(MessageWDSStartNetworkInterface, tlv.Uint(0x01, uint32(0x01020304))),
				},
				{resp: successResponse(MessageWDSGetRuntimeSettings)},
				{resp: successResponse(MessageWDSStopNetworkInterface)},
				{resp: successResponse(MessageReleaseClientID)},
			}}
			client := &Client{transport: transport, slot: 1}

			session, err := client.OpenPDN(context.Background(), PDNConfig{
				IPPreference:      tt.preference,
				LegacyMuxDataPort: WDSSIOPortA2MuxRMNET0,
			})
			if err != nil {
				t.Fatalf("OpenPDN() error = %v", err)
			}
			if err := session.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if got := transport.callCount(); got != len(transport.calls) {
				t.Fatalf("Do() calls = %d, want %d", got, len(transport.calls))
			}
		})
	}
}

func TestOpenPDNLegacyMuxFallback(t *testing.T) {
	fallback := WDSMuxDataPort{
		Endpoint: &DataEndpoint{Type: DataEndpointBAMDMUX, InterfaceID: 3},
		MuxID:    0,
	}
	tests := []struct {
		name         string
		legacyError  QMIError
		wantFallback bool
		wantErr      error
	}{
		{
			name:         "device unsupported uses modern bind",
			legacyError:  QMIErrorDeviceUnsupported,
			wantFallback: true,
		},
		{
			name:        "other error is returned",
			legacyError: QMIErrorNotSupported,
			wantErr:     QMIErrorNotSupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectMessage := func(messageID MessageID, resp Response) transportCall {
				return transportCall{
					check: func(req Request) {
						if req.MessageID != messageID {
							t.Fatalf("MessageID = 0x%04X, want 0x%04X", req.MessageID, messageID)
						}
					},
					resp: resp,
				}
			}
			calls := []transportCall{
				expectMessage(MessageAllocateClientID, allocatedClientResponse(ServiceWDS, 2)),
				expectMessage(MessageWDSLegacyBindMuxDataPort, errorResponse(MessageWDSLegacyBindMuxDataPort, tt.legacyError)),
			}
			if tt.wantFallback {
				calls = append(calls,
					transportCall{
						check: func(req Request) {
							if req.MessageID != MessageWDSBindMuxDataPort {
								t.Fatalf("MessageID = 0x%04X, want Bind Mux Data Port", req.MessageID)
							}
							assertTLV(t, req.TLVs, 0x10, []byte{0x05, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00})
							assertTLV(t, req.TLVs, 0x11, []byte{0x00})
						},
						resp: successResponse(MessageWDSBindMuxDataPort),
					},
					expectMessage(MessageWDSStartNetworkInterface, successResponse(MessageWDSStartNetworkInterface, tlv.Uint(0x01, uint32(0x01020304)))),
					expectMessage(MessageWDSGetRuntimeSettings, successResponse(MessageWDSGetRuntimeSettings)),
					expectMessage(MessageWDSStopNetworkInterface, successResponse(MessageWDSStopNetworkInterface)),
					expectMessage(MessageReleaseClientID, successResponse(MessageReleaseClientID)),
				)
			} else {
				calls = append(calls, expectMessage(MessageReleaseClientID, successResponse(MessageReleaseClientID)))
			}
			transport := &fakeTransport{t: t, calls: calls}
			client := &Client{transport: transport, slot: 1}

			session, err := client.OpenPDN(context.Background(), PDNConfig{
				LegacyMuxDataPort: WDSSIOPortA2MuxRMNET3,
				LegacyMuxFallback: &fallback,
			})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("OpenPDN() error = %v, want %v", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("OpenPDN() error = %v", err)
				}
				if err := session.Close(); err != nil {
					t.Fatalf("Close() error = %v", err)
				}
			}
			if got := transport.callCount(); got != len(transport.calls) {
				t.Fatalf("Do() calls = %d, want %d", got, len(transport.calls))
			}
		})
	}
}

func TestOpenPDNUsesImplicitQRTRClient(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "client ID zero owns packet handle"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &implicitClientTransport{fakeTransport: fakeTransport{t: t, calls: []transportCall{
				{resp: successResponse(MessageWDSStartNetworkInterface, tlv.Uint(0x01, uint32(0x01020304)))},
				{resp: successResponse(MessageWDSGetRuntimeSettings)},
				{resp: successResponse(MessageWDSStopNetworkInterface)},
			}}}
			client := &Client{transport: transport, slot: 1}

			session, err := client.OpenPDN(context.Background(), PDNConfig{})
			if err != nil {
				t.Fatalf("OpenPDN() error = %v", err)
			}
			if err := session.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if len(transport.services) != 1 || transport.services[0] != ServiceWDS {
				t.Fatalf("ClientID() services = %v, want WDS", transport.services)
			}
		})
	}
}

func TestOpenPDNValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  PDNConfig
		want string
	}{
		{name: "conflicting ports", cfg: PDNConfig{MuxDataPort: &WDSMuxDataPort{}, LegacyMuxDataPort: WDSSIOPortA2MuxRMNET0}, want: "mutually exclusive"},
		{name: "fallback without legacy port", cfg: PDNConfig{LegacyMuxFallback: &WDSMuxDataPort{}}, want: "requires a legacy mux data port"},
		{name: "APN too long", cfg: PDNConfig{APN: strings.Repeat("a", wdsAPNMaxLength+1)}, want: "validating WDS APN: value length"},
		{name: "username too long", cfg: PDNConfig{Username: strings.Repeat("u", wdsUsernameMaxLength+1)}, want: "validating WDS username: value length"},
		{name: "password too long", cfg: PDNConfig{Password: strings.Repeat("p", wdsPasswordMaxLength+1)}, want: "validating WDS password: value length"},
		{name: "APN NUL", cfg: PDNConfig{APN: "inter\x00net"}, want: "validating WDS APN: value contains a NUL byte"},
		{name: "unsupported authentication", cfg: PDNConfig{Authentication: 0x80}, want: "unsupported WDS authentication mask"},
		{name: "unsupported IP preference", cfg: PDNConfig{IPPreference: 9}, want: "unsupported WDS IP preference"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := (&Client{}).OpenPDN(context.Background(), tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("OpenPDN() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

type implicitClientTransport struct {
	fakeTransport
	services []ServiceType
}

func (t *implicitClientTransport) ClientID(_ context.Context, service ServiceType) (uint8, error) {
	t.services = append(t.services, service)
	return 0, nil
}
