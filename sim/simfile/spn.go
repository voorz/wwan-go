package simfile

import (
	"errors"
	"strings"
	"unicode/utf16"
)

type ServiceProviderName string

func (n ServiceProviderName) String() string {
	return string(n)
}

func (n ServiceProviderName) MarshalText() ([]byte, error) {
	return []byte(n), nil
}

func (n *ServiceProviderName) UnmarshalText(text []byte) error {
	*n = ServiceProviderName(string(text))
	return nil
}

func (n *ServiceProviderName) UnmarshalBinary(data []byte) error {
	if len(data) < 2 {
		return errors.New("parsing EF_SPN: payload is too short")
	}
	data = trimFF(data[1:])
	if len(data) == 0 {
		*n = ""
		return nil
	}
	if data[0] != 0x80 {
		*n = ServiceProviderName(strings.TrimSpace(string(data)))
		return nil
	}
	data = data[1:]
	if len(data)%2 != 0 {
		return errors.New("parsing EF_SPN: odd UCS-2 payload")
	}
	code := make([]uint16, len(data)/2)
	for i := range code {
		code[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
	}
	*n = ServiceProviderName(strings.TrimSpace(string(utf16.Decode(code))))
	return nil
}

func trimFF(data []byte) []byte {
	for len(data) > 0 && data[len(data)-1] == 0xFF {
		data = data[:len(data)-1]
	}
	return data
}
