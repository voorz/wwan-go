package qcom

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/damonto/wwan-go/qcom/tlv"
)

const (
	slotReadyTimeout  = 5 * time.Second
	slotPollInterval  = 500 * time.Millisecond
	slotStatusTimeout = 1 * time.Second
)

func (c *Client) ActivateSlot(ctx context.Context) error {
	status, err := c.SlotStatus(ctx)
	if err != nil {
		if errors.Is(err, QMIErrorNotSupported) {
			c.logicalSlot.Store(1)
			return nil
		}
		return fmt.Errorf("activating slot %d: %w", c.slot, err)
	}
	logicalSlot, active, err := status.activeLogicalSlot(c.slot)
	if err != nil {
		return fmt.Errorf("activating slot %d: %w", c.slot, err)
	}
	if active {
		c.logicalSlot.Store(uint32(logicalSlot))
		return nil
	}
	logicalSlot, err = status.firstActiveLogicalSlot()
	if err != nil {
		return fmt.Errorf("activating slot %d: %w", c.slot, err)
	}
	if err := c.SwitchSlot(ctx, logicalSlot, uint32(c.slot)); err != nil && !errors.Is(err, QMIErrorNoEffect) {
		return fmt.Errorf("activating slot %d: %w", c.slot, err)
	}

	activationCtx, cancel := context.WithTimeout(ctx, slotReadyTimeout)
	defer cancel()
	if err := c.waitForSlotMapping(activationCtx, logicalSlot); err != nil {
		return fmt.Errorf("activating slot %d: %w", c.slot, err)
	}
	if err := c.waitForCardReady(activationCtx, logicalSlot); err != nil {
		return fmt.Errorf("activating slot %d: %w", c.slot, err)
	}
	c.logicalSlot.Store(uint32(logicalSlot))
	return nil
}

func (s SlotStatus) activeLogicalSlot(physicalSlot uint8) (uint8, bool, error) {
	index := int(physicalSlot) - 1
	if index < 0 || index >= len(s.Slots) {
		return 0, false, fmt.Errorf("physical slot %d is missing from slot status", physicalSlot)
	}
	physicalSlotStatus := s.Slots[index]
	if physicalSlotStatus.PhysicalSlotStatus != SlotStateActive {
		return 0, false, nil
	}
	logicalSlot := physicalSlotStatus.LogicalSlot
	if logicalSlot == 0 {
		return 0, false, fmt.Errorf("active physical slot %d has no logical-slot mapping", physicalSlot)
	}
	return logicalSlot, true, nil
}

func (s SlotStatus) firstActiveLogicalSlot() (uint8, error) {
	for physicalSlot, status := range s.Slots {
		if status.PhysicalSlotStatus != SlotStateActive {
			continue
		}
		if status.LogicalSlot == 0 {
			return 0, fmt.Errorf("active physical slot %d has no logical-slot mapping", physicalSlot+1)
		}
		return status.LogicalSlot, nil
	}
	return 0, errors.New("no active logical slot")
}

func (c *Client) SlotStatus(ctx context.Context) (SlotStatus, error) {
	resp, err := c.requestWithTimeout(ctx, MessageGetSlotStatus, nil, slotStatusTimeout)
	if err != nil {
		return SlotStatus{}, err
	}
	if err := resultOK(resp); err != nil {
		return SlotStatus{}, err
	}
	var status SlotStatus
	if err := status.UnmarshalTLVs(resp.TLVs); err != nil {
		return SlotStatus{}, err
	}
	return status, nil
}

func (c *Client) SwitchSlot(ctx context.Context, logicalSlot uint8, physicalSlot uint32) error {
	resp, err := c.request(ctx, MessageSwitchSlot, tlv.TLVs{
		tlv.Uint(0x01, logicalSlot),
		tlv.Uint(0x02, physicalSlot),
	})
	if err != nil {
		return err
	}
	return resultOK(resp)
}

func (c *Client) waitForSlotMapping(ctx context.Context, logicalSlot uint8) error {
	return pollSlotActivation(ctx, "waiting for slot mapping", func(ctx context.Context) (bool, error) {
		status, err := c.SlotStatus(ctx)
		if err != nil {
			return false, err
		}
		mappedSlot, active, err := status.activeLogicalSlot(c.slot)
		if err != nil || !active {
			return false, err
		}
		if mappedSlot != logicalSlot {
			return false, fmt.Errorf("physical slot %d maps to logical slot %d, want %d", c.slot, mappedSlot, logicalSlot)
		}
		return true, nil
	})
}

func (c *Client) waitForCardReady(ctx context.Context, logicalSlot uint8) error {
	return pollSlotActivation(ctx, "waiting for card readiness", func(ctx context.Context) (bool, error) {
		status, err := c.CardStatus(ctx)
		return err == nil && status.logicalSlotReady(logicalSlot), err
	})
}

func pollSlotActivation(ctx context.Context, action string, check func(context.Context) (bool, error)) error {
	var lastErr error
	for {
		ready, err := check(ctx)
		if err == nil && ready {
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			if lastErr != nil && !errors.Is(lastErr, ctx.Err()) {
				return fmt.Errorf("%s: %w", action, errors.Join(ctx.Err(), lastErr))
			}
			return fmt.Errorf("%s: %w", action, ctx.Err())
		}

		timer := time.NewTimer(slotPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			if lastErr != nil {
				return fmt.Errorf("%s: %w", action, errors.Join(ctx.Err(), lastErr))
			}
			return fmt.Errorf("%s: %w", action, ctx.Err())
		case <-timer.C:
		}
	}
}

func (c *Client) CardStatus(ctx context.Context) (CardStatus, error) {
	resp, err := c.request(ctx, MessageGetCardStatus, nil)
	if err != nil {
		return CardStatus{}, err
	}
	if err := resultOK(resp); err != nil {
		return CardStatus{}, err
	}
	var status CardStatus
	if err := status.UnmarshalTLVs(resp.TLVs); err != nil {
		return CardStatus{}, err
	}
	return status, nil
}

func (c *Client) Slot() uint8 {
	return c.slot
}

func (c *Client) activeLogicalSlot() uint8 {
	if slot := uint8(c.logicalSlot.Load()); slot != 0 {
		return slot
	}
	return 1
}
