package simfile

import "errors"

type SMSC string

func (s SMSC) String() string {
	return string(s)
}

func (s SMSC) MarshalText() ([]byte, error) {
	return []byte(string(s)), nil
}

func (s *SMSC) UnmarshalText(text []byte) error {
	*s = SMSC(string(text))
	return nil
}

func (s *SMSC) UnmarshalBinary(data []byte) error {
	y := len(data) - 28
	if y < 0 || y+26 > len(data) {
		return errors.New("reading EF_SMSP: malformed record")
	}

	sca := data[y+13 : y+25]
	if len(sca) < 2 {
		*s = ""
		return nil
	}
	length := int(sca[0])
	// The length octet describes bytes after itself, so it must still fit inside
	// the fixed 12-byte SCA field.
	if length <= 1 || length+1 > len(sca) {
		*s = ""
		return nil
	}
	if sca[1] != 0x91 {
		*s = ""
		return nil
	}

	var address Address
	if err := address.UnmarshalBinary(sca[1 : length+1]); err != nil {
		return err
	}
	if address == "" {
		return nil
	}

	*s = SMSC(address)
	return nil
}
