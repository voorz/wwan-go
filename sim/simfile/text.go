package simfile

import (
	"errors"

	"github.com/damonto/wwan-go/apdu"
	"github.com/damonto/wwan-go/sim/tlv"
)

type Text string

func (t Text) String() string {
	return string(t)
}

func (t Text) MarshalText() ([]byte, error) {
	return []byte(string(t)), nil
}

func (t *Text) UnmarshalText(data []byte) error {
	*t = Text(string(data))
	return nil
}

func (t *Text) UnmarshalBinary(data []byte) error {
	for len(data) > 0 && (data[len(data)-1] == 0xFF || data[len(data)-1] == 0x00) {
		data = data[:len(data)-1]
	}
	if len(data) == 0 {
		return errors.New("parsing TLV string: empty payload")
	}

	var items tlv.Items
	err := items.UnmarshalBinary(data)
	if err == nil {
		for _, item := range items {
			if len(item.Value) == 0 {
				continue
			}
			*t = Text(string(item.Value))
			return nil
		}
		return errors.New("parsing TLV string: missing value")
	}

	for _, b := range data {
		if b < 0x20 || b > 0x7E {
			return malformedTLV(err)
		}
	}
	*t = Text(string(data))
	return nil
}

func malformedTLV(err error) error {
	if errors.Is(err, tlv.ErrMalformed) {
		return apdu.ErrMalformedResponse
	}
	return err
}
