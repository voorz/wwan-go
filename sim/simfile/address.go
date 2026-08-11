package simfile

import (
	"errors"
	"fmt"
	"strings"
)

type Address string

func (a Address) String() string {
	return string(a)
}

func (a Address) MarshalText() ([]byte, error) {
	return []byte(string(a)), nil
}

func (a *Address) UnmarshalText(text []byte) error {
	*a = Address(string(text))
	return nil
}

func (a Address) MarshalBinary() ([]byte, error) {
	value := strings.TrimSpace(string(a))
	if value == "" {
		return nil, nil
	}

	tonNPI := byte(0x81)
	if strings.HasPrefix(value, "+") {
		tonNPI = 0x91
		value = strings.TrimPrefix(value, "+")
	}
	value = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(value)
	if value == "" {
		return nil, errors.New("marshaling address: value is empty")
	}
	if err := validateDigits(value, 1); err != nil {
		return nil, fmt.Errorf("marshaling address: %w", err)
	}

	var body BCD
	if err := body.UnmarshalText([]byte(value)); err != nil {
		return nil, fmt.Errorf("marshaling address: %w", err)
	}
	return append([]byte{tonNPI}, body...), nil
}

func (a *Address) UnmarshalBinary(data []byte) error {
	if len(data) == 0 {
		*a = ""
		return nil
	}

	digits := BCD(data[1:]).String()
	if err := validateDigits(digits, 0); err != nil {
		return fmt.Errorf("parsing address: %w", err)
	}
	if digits == "" {
		*a = ""
		return nil
	}

	if data[0] == 0x91 {
		digits = "+" + digits
	}
	*a = Address(digits)
	return nil
}
