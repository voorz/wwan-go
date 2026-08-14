package qcom

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/voorz/wwan-go/qcom/tlv"
)

// QoSSubscription identifies a modem subscription for the QoS service.
type QoSSubscription uint32

const (
	QoSSubscriptionPrimary QoSSubscription = 1 + iota
	QoSSubscriptionSecondary
	QoSSubscriptionTertiary
)

// QoSFlowStatus is the current state of a negotiated QoS flow.
type QoSFlowStatus uint8

const (
	QoSFlowStatusDefault QoSFlowStatus = iota
	QoSFlowStatusActivated
	QoSFlowStatusSuspended
	QoSFlowStatusGone
)

// QoSFlowEvent describes why a flow-status indication was emitted.
type QoSFlowEvent uint8

const (
	QoSFlowEventActivated QoSFlowEvent = 1 + iota
	QoSFlowEventSuspended
	QoSFlowEventGone
	QoSFlowEventModifyAccepted
	QoSFlowEventModifyRejected
	QoSFlowEventInfoCodeUpdated
)

// QoSFlowStatusUpdate is emitted for a flow owned by this QoS control point.
type QoSFlowStatusUpdate struct {
	ID          uint32
	Status      QoSFlowStatus
	Event       QoSFlowEvent
	Reason      uint8
	ReasonKnown bool
}

// QoSDataPortConfig selects a modern endpoint/mux or one legacy SIO port.
type QoSDataPortConfig struct {
	Endpoint   *DataEndpoint
	MuxID      *uint8
	LegacyPort *WDSSIOPort
}

// QoSResetRequest encodes QMI QoS Reset.
type QoSResetRequest struct {
	ClientID      uint8
	TransactionID uint16
	Timeout       time.Duration
}

// Request converts the reset into a QMI request.
func (r QoSResetRequest) Request() Request {
	return qosEmptyRequest(r.ClientID, r.TransactionID, r.Timeout, MessageQoSReset)
}

// QoSGetFlowStatusRequest encodes Get QoS Status.
type QoSGetFlowStatusRequest struct {
	ClientID      uint8
	TransactionID uint16
	Timeout       time.Duration
	FlowID        uint32
}

// Request converts the flow query into a QMI request.
func (r QoSGetFlowStatusRequest) Request() Request {
	return Request{
		Service:       ServiceQoS,
		ClientID:      r.ClientID,
		TransactionID: r.TransactionID,
		MessageID:     MessageQoSGetStatus,
		Timeout:       r.Timeout,
		TLVs:          tlv.TLVs{tlv.Uint(0x01, r.FlowID)},
	}
}

// QoSGetFlowStatusResponse is the parsed flow status response.
type QoSGetFlowStatusResponse struct {
	Status QoSFlowStatus
}

// UnmarshalTLVs parses the mandatory flow status.
func (r *QoSGetFlowStatusResponse) UnmarshalTLVs(tlvs tlv.TLVs) error {
	*r = QoSGetFlowStatusResponse{}
	value, ok := tlv.Value(tlvs, 0x01)
	if !ok {
		return errors.New("parsing QMI QoS flow status: status TLV is missing")
	}
	if len(value) != 1 {
		return fmt.Errorf("parsing QMI QoS flow status: status TLV length %d, want 1", len(value))
	}
	r.Status = QoSFlowStatus(value[0])
	return nil
}

// QoSFlowStatusIndication is the parsed per-flow status indication.
type QoSFlowStatusIndication struct {
	Update QoSFlowStatusUpdate
}

// UnmarshalTLVs parses the mandatory status aggregate and optional reason.
func (r *QoSFlowStatusIndication) UnmarshalTLVs(tlvs tlv.TLVs) error {
	*r = QoSFlowStatusIndication{}
	value, ok := tlv.Value(tlvs, 0x01)
	if !ok {
		return errors.New("parsing QMI QoS flow status indication: status TLV is missing")
	}
	if len(value) != 6 {
		return fmt.Errorf("parsing QMI QoS flow status indication: status TLV length %d, want 6", len(value))
	}
	r.Update.ID = binary.LittleEndian.Uint32(value[:4])
	r.Update.Status = QoSFlowStatus(value[4])
	r.Update.Event = QoSFlowEvent(value[5])
	if value, ok := tlv.Value(tlvs, 0x10); ok {
		if len(value) != 1 {
			return fmt.Errorf("parsing QMI QoS flow status indication: reason TLV length %d, want 1", len(value))
		}
		r.Update.Reason = value[0]
		r.Update.ReasonKnown = true
	}
	return nil
}

// QoSGetNetworkStatusRequest encodes Get QoS Network Status.
type QoSGetNetworkStatusRequest struct {
	ClientID      uint8
	TransactionID uint16
	Timeout       time.Duration
}

// Request converts the query into a QMI request.
func (r QoSGetNetworkStatusRequest) Request() Request {
	return qosEmptyRequest(r.ClientID, r.TransactionID, r.Timeout, MessageQoSGetNetworkStatus)
}

// QoSGetNetworkStatusResponse is the parsed network QoS support status.
type QoSGetNetworkStatusResponse struct {
	Supported bool
}

// UnmarshalTLVs parses the mandatory support flag.
func (r *QoSGetNetworkStatusResponse) UnmarshalTLVs(tlvs tlv.TLVs) error {
	*r = QoSGetNetworkStatusResponse{}
	value, ok := tlv.Value(tlvs, 0x01)
	if !ok {
		return errors.New("parsing QMI QoS network status: support TLV is missing")
	}
	if len(value) != 1 {
		return fmt.Errorf("parsing QMI QoS network status: support TLV length %d, want 1", len(value))
	}
	r.Supported = value[0] != 0
	return nil
}

// QoSBindDataPortRequest encodes Bind Data Port.
type QoSBindDataPortRequest struct {
	ClientID      uint8
	TransactionID uint16
	Timeout       time.Duration
	Config        QoSDataPortConfig
}

// Request validates and converts a modern or legacy data-port binding.
func (r QoSBindDataPortRequest) Request() (Request, error) {
	modern := r.Config.Endpoint != nil || r.Config.MuxID != nil
	if !modern && r.Config.LegacyPort == nil {
		return Request{}, errors.New("encoding QMI QoS data port: no port selected")
	}
	if modern && r.Config.LegacyPort != nil {
		return Request{}, errors.New("encoding QMI QoS data port: modern and legacy ports are mutually exclusive")
	}
	var tlvs tlv.TLVs
	if r.Config.Endpoint != nil {
		value, _ := r.Config.Endpoint.MarshalBinary() // Fixed-width endpoint encoding cannot fail.
		tlvs = append(tlvs, tlv.Bytes(0x10, value))
	}
	if r.Config.MuxID != nil {
		tlvs = append(tlvs, tlv.Uint(0x11, *r.Config.MuxID))
	}
	if r.Config.LegacyPort != nil {
		tlvs = append(tlvs, tlv.Uint(0x12, uint16(*r.Config.LegacyPort)))
	}
	return Request{
		Service:       ServiceQoS,
		ClientID:      r.ClientID,
		TransactionID: r.TransactionID,
		MessageID:     MessageQoSBindDataPort,
		Timeout:       r.Timeout,
		TLVs:          tlvs,
	}, nil
}

// QoSBindSubscriptionRequest encodes Bind Subscription.
type QoSBindSubscriptionRequest struct {
	ClientID      uint8
	TransactionID uint16
	Timeout       time.Duration
	Subscription  QoSSubscription
}

// Request validates and converts the subscription binding.
func (r QoSBindSubscriptionRequest) Request() (Request, error) {
	if err := validateQoSSubscription(r.Subscription); err != nil {
		return Request{}, err
	}
	return Request{
		Service:       ServiceQoS,
		ClientID:      r.ClientID,
		TransactionID: r.TransactionID,
		MessageID:     MessageQoSBindSubscription,
		Timeout:       r.Timeout,
		TLVs:          tlv.TLVs{tlv.Uint(0x01, uint32(r.Subscription))},
	}, nil
}

// QoSGetBindSubscriptionRequest encodes Get Bind Subscription.
type QoSGetBindSubscriptionRequest struct {
	ClientID      uint8
	TransactionID uint16
	Timeout       time.Duration
}

// Request converts the query into a QMI request.
func (r QoSGetBindSubscriptionRequest) Request() Request {
	return qosEmptyRequest(r.ClientID, r.TransactionID, r.Timeout, MessageQoSGetBindSubscription)
}

// QoSGetBindSubscriptionResponse is the parsed bound subscription.
type QoSGetBindSubscriptionResponse struct {
	Subscription      QoSSubscription
	SubscriptionKnown bool
}

// UnmarshalTLVs parses the optional bound subscription.
func (r *QoSGetBindSubscriptionResponse) UnmarshalTLVs(tlvs tlv.TLVs) error {
	*r = QoSGetBindSubscriptionResponse{}
	value, ok := tlv.Value(tlvs, 0x10)
	if !ok {
		return nil
	}
	if len(value) != 4 {
		return fmt.Errorf("parsing QMI QoS bound subscription: subscription TLV length %d, want 4", len(value))
	}
	r.Subscription = QoSSubscription(binary.LittleEndian.Uint32(value))
	r.SubscriptionKnown = true
	return nil
}

// QoSReset resets this control point's QoS service state.
func (c *Client) QoSReset(ctx context.Context) error {
	req := QoSResetRequest{Timeout: DefaultRequestTimeout}.Request()
	if err := c.qosResultRequest(ctx, req); err != nil {
		return fmt.Errorf("resetting QMI QoS control point: %w", err)
	}
	return nil
}

// QoSFlowStatus reads the current status of one negotiated flow.
func (c *Client) QoSFlowStatus(ctx context.Context, flowID uint32) (QoSFlowStatus, error) {
	var status QoSFlowStatus
	err := c.withServiceClient(ctx, ServiceQoS, func(clientID uint8) error {
		req := QoSGetFlowStatusRequest{ClientID: clientID, Timeout: DefaultRequestTimeout, FlowID: flowID}.Request()
		resp, err := c.requestServiceWithTimeout(ctx, req.Service, req.ClientID, req.MessageID, req.TLVs, req.Timeout)
		if err != nil {
			return err
		}
		if err := resultOK(resp); err != nil {
			return err
		}
		var parsed QoSGetFlowStatusResponse
		if err := parsed.UnmarshalTLVs(resp.TLVs); err != nil {
			return err
		}
		status = parsed.Status
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("reading QMI QoS flow %d status: %w", flowID, err)
	}
	return status, nil
}

// QoSNetworkSupported reports whether the active network supports QoS.
func (c *Client) QoSNetworkSupported(ctx context.Context) (bool, error) {
	var supported bool
	err := c.withServiceClient(ctx, ServiceQoS, func(clientID uint8) error {
		req := QoSGetNetworkStatusRequest{ClientID: clientID, Timeout: DefaultRequestTimeout}.Request()
		resp, err := c.requestServiceWithTimeout(ctx, req.Service, req.ClientID, req.MessageID, req.TLVs, req.Timeout)
		if err != nil {
			return err
		}
		if err := resultOK(resp); err != nil {
			return err
		}
		var parsed QoSGetNetworkStatusResponse
		if err := parsed.UnmarshalTLVs(resp.TLVs); err != nil {
			return err
		}
		supported = parsed.Supported
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("reading QMI QoS network status: %w", err)
	}
	return supported, nil
}

// QoSBindDataPort binds this QoS control point to a data channel.
func (c *Client) QoSBindDataPort(ctx context.Context, config QoSDataPortConfig) error {
	req, err := (QoSBindDataPortRequest{Timeout: DefaultRequestTimeout, Config: config}).Request()
	if err != nil {
		return err
	}
	if err := c.qosResultRequest(ctx, req); err != nil {
		return fmt.Errorf("binding QMI QoS data port: %w", err)
	}
	return nil
}

// QoSBindSubscription binds this QoS control point to a subscription.
func (c *Client) QoSBindSubscription(ctx context.Context, subscription QoSSubscription) error {
	req, err := (QoSBindSubscriptionRequest{Timeout: DefaultRequestTimeout, Subscription: subscription}).Request()
	if err != nil {
		return err
	}
	if err := c.qosResultRequest(ctx, req); err != nil {
		return fmt.Errorf("binding QMI QoS subscription: %w", err)
	}
	return nil
}

// QoSBoundSubscription reads the subscription bound to this QoS control point.
func (c *Client) QoSBoundSubscription(ctx context.Context) (QoSSubscription, error) {
	var subscription QoSSubscription
	err := c.withServiceClient(ctx, ServiceQoS, func(clientID uint8) error {
		req := QoSGetBindSubscriptionRequest{ClientID: clientID, Timeout: DefaultRequestTimeout}.Request()
		resp, err := c.requestServiceWithTimeout(ctx, req.Service, req.ClientID, req.MessageID, req.TLVs, req.Timeout)
		if err != nil {
			return err
		}
		if err := resultOK(resp); err != nil {
			return err
		}
		var parsed QoSGetBindSubscriptionResponse
		if err := parsed.UnmarshalTLVs(resp.TLVs); err != nil {
			return err
		}
		if !parsed.SubscriptionKnown {
			return errors.New("parsing QMI QoS bound subscription: subscription TLV is missing")
		}
		subscription = parsed.Subscription
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("reading QMI QoS bound subscription: %w", err)
	}
	return subscription, nil
}

// QoSWatchFlowStatus subscribes to status changes for flows owned by this control point.
func (c *Client) QoSWatchFlowStatus(ctx context.Context) (<-chan QoSFlowStatusUpdate, error) {
	transport, err := c.indicationTransport()
	if err != nil {
		return nil, err
	}
	clientID, err := c.serviceClientID(ctx, ServiceQoS)
	if err != nil {
		return nil, fmt.Errorf("watching QMI QoS flow status: %w", err)
	}
	watchCtx, cancel := context.WithCancel(ctx)
	indications, err := transport.Indications(watchCtx, ServiceQoS, clientID, MessageQoSStatus)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("watching QMI QoS flow status: %w", err)
	}
	out := make(chan QoSFlowStatusUpdate, 8)
	go func() {
		defer close(out)
		defer cancel()
		for indication := range indications {
			var parsed QoSFlowStatusIndication
			if err := parsed.UnmarshalTLVs(indication.TLVs); err != nil {
				return
			}
			select {
			case out <- parsed.Update:
			case <-watchCtx.Done():
				return
			}
		}
	}()
	return out, nil
}

// QoSWatchNetworkStatus subscribes to changes in network QoS support.
func (c *Client) QoSWatchNetworkStatus(ctx context.Context) (<-chan bool, error) {
	transport, err := c.indicationTransport()
	if err != nil {
		return nil, err
	}
	clientID, err := c.serviceClientID(ctx, ServiceQoS)
	if err != nil {
		return nil, fmt.Errorf("watching QMI QoS network status: %w", err)
	}
	watchCtx, cancel := context.WithCancel(ctx)
	indications, err := transport.Indications(watchCtx, ServiceQoS, clientID, MessageQoSNetworkStatus)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("watching QMI QoS network status: %w", err)
	}
	out := make(chan bool, 8)
	go func() {
		defer close(out)
		defer cancel()
		for indication := range indications {
			var parsed QoSGetNetworkStatusResponse
			if err := parsed.UnmarshalTLVs(indication.TLVs); err != nil {
				return
			}
			select {
			case out <- parsed.Supported:
			case <-watchCtx.Done():
				return
			}
		}
	}()
	return out, nil
}

func qosEmptyRequest(clientID uint8, transactionID uint16, timeout time.Duration, message MessageID) Request {
	return Request{
		Service:       ServiceQoS,
		ClientID:      clientID,
		TransactionID: transactionID,
		MessageID:     message,
		Timeout:       timeout,
	}
}

func (c *Client) qosResultRequest(ctx context.Context, req Request) error {
	return c.withServiceClient(ctx, ServiceQoS, func(clientID uint8) error {
		req.ClientID = clientID
		resp, err := c.requestServiceWithTimeout(ctx, req.Service, req.ClientID, req.MessageID, req.TLVs, req.Timeout)
		if err != nil {
			return err
		}
		return resultOK(resp)
	})
}

func validateQoSSubscription(subscription QoSSubscription) error {
	if subscription < QoSSubscriptionPrimary || subscription > QoSSubscriptionTertiary {
		return fmt.Errorf("QMI QoS subscription %d is out of range", subscription)
	}
	return nil
}
