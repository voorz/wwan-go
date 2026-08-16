package mbim

import "context"

func (b *Backend) SupportedBands(ctx context.Context) ([]Band, error) {
	caps, err := b.client.DeviceCaps(ctx)
	if err != nil {
		return nil, err
	}
	var bands []Band
	for bit := range 32 {
		if caps.WCDMABandClass&(uint32(1)<<bit) != 0 {
			bands = append(bands, Band{Technology: TechnologyUMTS, Number: uint16(bit + 1)})
		}
	}
	for _, number := range caps.LTEBandClasses {
		bands = append(bands, Band{Technology: TechnologyLTE, Number: number})
	}
	for _, number := range caps.NRBandClasses {
		bands = append(bands, Band{Technology: TechnologyNR5GNSA | TechnologyNR5GSA, Number: number})
	}
	return bands, nil
}

// Bands is unavailable in native MBIM: Device Caps reports supported bands,
// but the standard defines no query for the active band preference.
func (*Backend) Bands(context.Context) ([]Band, error) { return nil, ErrNotSupported }

// SetBands is unavailable in native MBIM because no standard CID selects bands.
func (*Backend) SetBands(context.Context, []Band) error { return ErrNotSupported }
