package modem

import (
	"context"
	"fmt"

	mbimproto "github.com/damonto/wwan-go/mbim"
	"github.com/damonto/wwan-go/qcom"
	qmiproto "github.com/damonto/wwan-go/qcom/qmi"
)

type clientOpenConfig struct {
	device string
	access Access
	slot   uint8
}

type clientOpeners struct {
	openQMI  func(context.Context, clientOpenConfig) (*qcom.Client, error)
	openMBIM func(context.Context, clientOpenConfig) (*mbimproto.Client, error)
}

// QMIClient opens an independently owned QMI client using the modem's
// resolved device and access method. The caller must close the returned
// client. Direct clients share the process-wide transport but own their QMI
// service client IDs.
func (m *Modem) QMIClient(ctx context.Context, slot uint8) (*qcom.Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed || m.backend == nil {
		return nil, ErrClosed
	}
	if m.port.Type != PortQMI {
		return nil, fmt.Errorf("opening QMI client: modem protocol is %s", m.port.Protocol())
	}
	if m.access != AccessProxy && m.access != AccessDirect {
		return nil, unresolvedAccessError("QMI")
	}
	return m.clients.openQMI(ctx, clientOpenConfig{
		device: m.port.Path,
		access: m.access,
		slot:   slot,
	})
}

// MBIMClient opens an independently owned MBIM client using the modem's
// resolved device and access method. The caller must close the returned
// client. Direct clients share the process-wide MBIM dispatcher.
func (m *Modem) MBIMClient(ctx context.Context, slot uint8) (*mbimproto.Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed || m.backend == nil {
		return nil, ErrClosed
	}
	if m.port.Type != PortMBIM {
		return nil, fmt.Errorf("opening MBIM client: modem protocol is %s", m.port.Protocol())
	}
	if m.access != AccessProxy && m.access != AccessDirect {
		return nil, unresolvedAccessError("MBIM")
	}
	return m.clients.openMBIM(ctx, clientOpenConfig{
		device: m.port.Path,
		access: m.access,
		slot:   slot,
	})
}

func openQMIClient(ctx context.Context, config clientOpenConfig) (*qcom.Client, error) {
	var option qmiproto.Option
	switch config.access {
	case AccessProxy:
		option = qmiproto.WithProxy(config.device)
	case AccessDirect:
		option = qmiproto.WithDirect(config.device)
	default:
		return nil, unresolvedAccessError("QMI")
	}

	transport, err := qmiproto.Open(ctx, option)
	if err != nil {
		return nil, fmt.Errorf("opening QMI client transport: %w", err)
	}
	client, err := qcom.NewClient(transport, qcom.WithSlot(config.slot))
	if err != nil {
		_ = transport.Close() // Cleanup cannot change the client-construction error.
		return nil, err
	}
	return client, nil
}

func openMBIMClient(ctx context.Context, config clientOpenConfig) (*mbimproto.Client, error) {
	var option mbimproto.Option
	switch config.access {
	case AccessProxy:
		option = mbimproto.WithProxy(config.device)
	case AccessDirect:
		option = mbimproto.WithDirect(config.device)
	default:
		return nil, unresolvedAccessError("MBIM")
	}

	client, err := mbimproto.Open(ctx, option, mbimproto.WithSlot(int(config.slot)))
	if err != nil {
		return nil, fmt.Errorf("opening MBIM client: %w", err)
	}
	return client, nil
}

func unresolvedAccessError(protocol string) error {
	return fmt.Errorf("opening %s client: modem access method is unresolved", protocol)
}
