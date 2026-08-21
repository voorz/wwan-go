package simfile

import (
	"errors"
	"fmt"

	"github.com/voorz/wwan-go/apdu"
	"github.com/voorz/wwan-go/sim/tlv"
)

const (
	tagRecord      = 0x61
	tagRecordAID   = 0x4F
	tagRecordLabel = 0x50
)

type EFDirRecord struct {
	AID   []byte
	Label string
}

func (rec EFDirRecord) MarshalBinary() ([]byte, error) {
	if len(rec.AID) == 0 {
		return nil, errors.New("marshaling EF_DIR record: AID is empty")
	}

	inner := tlv.Items{{Tag: tagRecordAID, Value: append([]byte(nil), rec.AID...)}}
	if rec.Label != "" {
		inner = append(inner, tlv.Item{Tag: tagRecordLabel, Value: []byte(rec.Label)})
	}
	value, err := inner.MarshalBinary()
	if err != nil {
		return nil, err
	}

	return tlv.Items{{Tag: tagRecord, Value: value}}.MarshalBinary()
}

func (rec *EFDirRecord) UnmarshalBinary(data []byte) error {
	if len(trimEFDirPadding(data)) == 0 {
		*rec = EFDirRecord{}
		return nil
	}

	top, consumed, err := tlv.Consume(data)
	if err != nil {
		return malformedTLV(err)
	}
	if top.Tag != tagRecord {
		return fmt.Errorf("parsing EF_DIR record tag 0x%02X: %w", top.Tag, apdu.ErrMalformedResponse)
	}
	if len(trimEFDirPadding(data[consumed:])) != 0 {
		return apdu.ErrMalformedResponse
	}

	parsed := EFDirRecord{}
	inner := top.Value
	for len(trimEFDirPadding(inner)) != 0 {
		item, consumed, err := tlv.Consume(inner)
		if err != nil {
			return malformedTLV(err)
		}
		switch item.Tag {
		case tagRecordAID:
			parsed.AID = append([]byte(nil), item.Value...)
		case tagRecordLabel:
			parsed.Label = string(item.Value)
		}
		inner = inner[consumed:]
	}
	if len(parsed.AID) == 0 {
		return fmt.Errorf("parsing EF_DIR record AID: %w", apdu.ErrMalformedResponse)
	}

	*rec = parsed
	return nil
}

func trimEFDirPadding(data []byte) []byte {
	for len(data) > 0 && data[len(data)-1] == 0xFF {
		data = data[:len(data)-1]
	}
	return data
}
