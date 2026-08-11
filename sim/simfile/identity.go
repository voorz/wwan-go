package simfile

import (
	"errors"
	"fmt"
	"strings"
)

type ICCID string

func (id ICCID) String() string {
	return string(id)
}

func (id ICCID) MarshalBinary() ([]byte, error) {
	if id == "" {
		return nil, errors.New("marshaling ICCID: value is too short")
	}
	var bcd BCD
	if err := bcd.UnmarshalText([]byte(id)); err != nil {
		return nil, fmt.Errorf("marshaling ICCID: %w", err)
	}
	return bcd, nil
}

func (id *ICCID) UnmarshalBinary(data []byte) error {
	digits := BCD(data).String()
	if digits == "" {
		return errors.New("parsing EF_ICCID: value is too short")
	}

	*id = ICCID(digits)
	return nil
}

func (id ICCID) MarshalText() ([]byte, error) {
	if id == "" {
		return nil, errors.New("marshaling ICCID: value is too short")
	}
	var bcd BCD
	if err := bcd.UnmarshalText([]byte(id)); err != nil {
		return nil, fmt.Errorf("marshaling ICCID: %w", err)
	}
	return []byte(bcd.String()), nil
}

func (id *ICCID) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return errors.New("parsing ICCID: value is too short")
	}
	var bcd BCD
	if err := bcd.UnmarshalText(text); err != nil {
		return fmt.Errorf("parsing ICCID: %w", err)
	}

	*id = ICCID(bcd.String())
	return nil
}

type IMSI struct {
	Digits string
	MCC    string
}

func (i IMSI) String() string {
	return i.Digits
}

func (i IMSI) MarshalBinary() ([]byte, error) {
	if err := validateDigits(i.Digits, 6); err != nil {
		return nil, fmt.Errorf("marshaling IMSI: %w", err)
	}

	var body BCD
	if err := body.UnmarshalText([]byte("9" + i.Digits)); err != nil {
		return nil, fmt.Errorf("marshaling IMSI: %w", err)
	}
	if len(body) > 0xFF {
		return nil, errors.New("marshaling IMSI: encoded payload exceeds 255 bytes")
	}

	out := make([]byte, 0, len(body)+1)
	out = append(out, byte(len(body)))
	out = append(out, body...)
	return out, nil
}

func (i *IMSI) UnmarshalBinary(data []byte) error {
	if len(data) == 0 {
		return errors.New("reading EF_IMSI: empty payload")
	}
	length := int(data[0])
	if len(data) < length+1 {
		return errors.New("reading EF_IMSI: truncated payload")
	}

	digits := strings.TrimPrefix(BCD(data[1:1+length]).String(), "9")
	if err := i.setDigits(digits); err != nil {
		return fmt.Errorf("reading EF_IMSI: %w", err)
	}
	return nil
}

func (i IMSI) MarshalText() ([]byte, error) {
	if err := validateDigits(i.Digits, 6); err != nil {
		return nil, fmt.Errorf("marshaling IMSI: %w", err)
	}
	return []byte(i.Digits), nil
}

func (i *IMSI) UnmarshalText(text []byte) error {
	if err := i.setDigits(string(text)); err != nil {
		return fmt.Errorf("parsing IMSI: %w", err)
	}
	return nil
}

func (i *IMSI) setDigits(digits string) error {
	if err := validateDigits(digits, 6); err != nil {
		return err
	}

	*i = IMSI{
		Digits: digits,
		MCC:    digits[:3],
	}
	return nil
}

func validateDigits(value string, minLength int) error {
	if len(value) < minLength {
		return errors.New("value is too short")
	}
	if strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return errors.New("value contains non-decimal digits")
	}
	return nil
}
