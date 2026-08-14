package modem

import (
	"context"
	"errors"
	"testing"

	mbimproto "github.com/damonto/wwan-go/mbim"
	"github.com/damonto/wwan-go/qcom"
)

func TestModemQMIClientUsesResolvedEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		portType PortType
		access   Access
		closed   bool
		wantErr  bool
	}{
		{name: "direct", portType: PortQMI, access: AccessDirect},
		{name: "proxy", portType: PortQMI, access: AccessProxy},
		{name: "wrong protocol", portType: PortMBIM, access: AccessDirect, wantErr: true},
		{name: "unresolved access", portType: PortQMI, access: AccessAuto, wantErr: true},
		{name: "invalid access", portType: PortQMI, access: Access(99), wantErr: true},
		{name: "closed", portType: PortQMI, access: AccessDirect, closed: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantClient := new(qcom.Client)
			called := false
			m := newModem(Port{Type: tt.portType, Path: "/dev/cdc-wdm0"}, tt.access, unsupportedBackend{})
			m.clients.openQMI = func(_ context.Context, config clientOpenConfig) (*qcom.Client, error) {
				called = true
				if config.device != "/dev/cdc-wdm0" || config.access != tt.access || config.slot != 2 {
					t.Errorf("QMI endpoint = (%q, %s, %d), want (/dev/cdc-wdm0, %s, 2)", config.device, config.access, config.slot, tt.access)
				}
				return wantClient, nil
			}

			if tt.closed {
				if err := m.Close(); err != nil {
					t.Fatalf("Close() error = %v", err)
				}
			}
			got, err := m.QMIClient(context.Background(), 2)
			if (err != nil) != tt.wantErr {
				t.Fatalf("QMIClient() error = %v, wantErr %t", err, tt.wantErr)
			}
			if tt.wantErr {
				if called {
					t.Fatal("QMIClient() called opener after validation error")
				}
				return
			}
			if !called || got != wantClient {
				t.Fatalf("QMIClient() = %p, called = %t; want %p, true", got, called, wantClient)
			}
		})
	}
}

func TestModemMBIMClientUsesResolvedEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		portType PortType
		access   Access
		closed   bool
		wantErr  bool
	}{
		{name: "direct", portType: PortMBIM, access: AccessDirect},
		{name: "proxy", portType: PortMBIM, access: AccessProxy},
		{name: "wrong protocol", portType: PortQMI, access: AccessDirect, wantErr: true},
		{name: "unresolved access", portType: PortMBIM, access: AccessAuto, wantErr: true},
		{name: "invalid access", portType: PortMBIM, access: Access(99), wantErr: true},
		{name: "closed", portType: PortMBIM, access: AccessDirect, closed: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantClient := new(mbimproto.Client)
			called := false
			m := newModem(Port{Type: tt.portType, Path: "/dev/cdc-wdm0"}, tt.access, unsupportedBackend{})
			m.clients.openMBIM = func(_ context.Context, config clientOpenConfig) (*mbimproto.Client, error) {
				called = true
				if config.device != "/dev/cdc-wdm0" || config.access != tt.access || config.slot != 2 {
					t.Errorf("MBIM endpoint = (%q, %s, %d), want (/dev/cdc-wdm0, %s, 2)", config.device, config.access, config.slot, tt.access)
				}
				return wantClient, nil
			}

			if tt.closed {
				if err := m.Close(); err != nil {
					t.Fatalf("Close() error = %v", err)
				}
			}
			got, err := m.MBIMClient(context.Background(), 2)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MBIMClient() error = %v, wantErr %t", err, tt.wantErr)
			}
			if tt.wantErr {
				if called {
					t.Fatal("MBIMClient() called opener after validation error")
				}
				return
			}
			if !called || got != wantClient {
				t.Fatalf("MBIMClient() = %p, called = %t; want %p, true", got, called, wantClient)
			}
		})
	}
}

func TestModemProtocolClientPropagatesOpenError(t *testing.T) {
	openErr := errors.New("open")
	tests := []struct {
		name     string
		portType PortType
		run      func(*Modem) error
	}{
		{
			name:     "QMI",
			portType: PortQMI,
			run: func(m *Modem) error {
				m.clients.openQMI = func(context.Context, clientOpenConfig) (*qcom.Client, error) {
					return nil, openErr
				}
				_, err := m.QMIClient(context.Background(), 1)
				return err
			},
		},
		{
			name:     "MBIM",
			portType: PortMBIM,
			run: func(m *Modem) error {
				m.clients.openMBIM = func(context.Context, clientOpenConfig) (*mbimproto.Client, error) {
					return nil, openErr
				}
				_, err := m.MBIMClient(context.Background(), 1)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModem(Port{Type: tt.portType, Path: "/dev/cdc-wdm0"}, AccessDirect, unsupportedBackend{})
			if err := tt.run(m); !errors.Is(err, openErr) {
				t.Fatalf("client open error = %v, want %v", err, openErr)
			}
		})
	}
}
