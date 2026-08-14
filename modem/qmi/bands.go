package qmi

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/voorz/wwan-go/qcom"
)

type legacyBand struct {
	mask uint64
	band Band
}

var errBandPreferenceUnavailable = errors.New("current band preference is unavailable")

var legacyBands = []legacyBand{
	{mask: 1 << 7, band: Band{Technology: TechnologyGSM, Number: 1800}},
	{mask: 1<<8 | 1<<9, band: Band{Technology: TechnologyGSM, Number: 900}},
	{mask: 1 << 21, band: Band{Technology: TechnologyGSM, Number: 1900}},
	{mask: 1 << 19, band: Band{Technology: TechnologyGSM, Number: 850}},
	{mask: 1 << 16, band: Band{Technology: TechnologyGSM, Number: 450}},
	{mask: 1 << 17, band: Band{Technology: TechnologyGSM, Number: 480}},
	{mask: 1 << 18, band: Band{Technology: TechnologyGSM, Number: 750}},
	{mask: 1 << 22, band: Band{Technology: TechnologyUMTS, Number: 1}},
	{mask: 1 << 23, band: Band{Technology: TechnologyUMTS, Number: 2}},
	{mask: 1 << 24, band: Band{Technology: TechnologyUMTS, Number: 3}},
	{mask: 1 << 25, band: Band{Technology: TechnologyUMTS, Number: 4}},
	{mask: 1 << 26, band: Band{Technology: TechnologyUMTS, Number: 5}},
	{mask: 1 << 27, band: Band{Technology: TechnologyUMTS, Number: 6}},
	{mask: 1 << 48, band: Band{Technology: TechnologyUMTS, Number: 7}},
	{mask: 1 << 49, band: Band{Technology: TechnologyUMTS, Number: 8}},
	{mask: 1 << 50, band: Band{Technology: TechnologyUMTS, Number: 9}},
	{mask: 1 << 61, band: Band{Technology: TechnologyUMTS, Number: 11}},
	{mask: 1 << 60, band: Band{Technology: TechnologyUMTS, Number: 19}},
}

func (b *Backend) SupportedBands(ctx context.Context) ([]Band, error) {
	caps, err := b.client.BandCapabilities(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading QMI supported bands: %w", err)
	}
	bands := bandsFromLegacyMask(caps.BandMask)
	if caps.LTEBandsKnown {
		for _, number := range caps.LTEBands {
			bands = append(bands, Band{Technology: TechnologyLTE, Number: number})
		}
	} else if caps.LTEBandMaskKnown {
		bands = append(bands, bandsFromWords(TechnologyLTE, []uint64{caps.LTEBandMask})...)
	}
	if caps.NR5GBandsKnown {
		for _, number := range caps.NR5GBands {
			bands = append(bands, Band{Technology: TechnologyNR5GNSA | TechnologyNR5GSA, Number: number})
		}
	}
	return normalizeBands(bands), nil
}

func (b *Backend) Bands(ctx context.Context) ([]Band, error) {
	pref, err := b.client.SystemSelectionPreference(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading QMI selected bands: %w", err)
	}
	var bands []Band
	if pref.BandPreferenceKnown {
		bands = append(bands, bandsFromLegacyMask(uint64(pref.BandPreference))...)
	}
	if pref.LTEBandsExtendedKnown {
		bands = append(bands, bandsFromWords(TechnologyLTE, lteWords(pref.LTEBandsExtended))...)
	} else if pref.LTEBandPreferenceKnown {
		bands = append(bands, bandsFromWords(TechnologyLTE, []uint64{uint64(pref.LTEBandPreference)})...)
	}
	// The split SA/NSA TLVs supersede the deprecated combined TLV. Union all
	// split values the modem reports so the UI reflects the complete preference.
	if pref.NR5GSABandsKnown || pref.NR5GNSABandsKnown {
		var nr qcom.NASNR5GBandPreference
		words := nrWordsMutable(&nr)
		if pref.NR5GSABandsKnown {
			for i, word := range nrWords(pref.NR5GSABands) {
				*words[i] |= word
			}
		}
		if pref.NR5GNSABandsKnown {
			for i, word := range nrWords(pref.NR5GNSABands) {
				*words[i] |= word
			}
		}
		bands = append(bands, bandsFromWords(TechnologyNR5GNSA|TechnologyNR5GSA, nrWords(nr))...)
	} else if pref.NR5GBandsKnown {
		bands = append(bands, bandsFromWords(TechnologyNR5GNSA|TechnologyNR5GSA, nrWords(pref.NR5GBands))...)
	}
	bands = normalizeBands(bands)
	if len(bands) == 0 {
		return nil, fmt.Errorf("reading QMI selected bands: %w", errBandPreferenceUnavailable)
	}
	return bands, nil
}

func (b *Backend) SetBands(ctx context.Context, bands []Band) error {
	if len(bands) == 0 {
		return errors.New("setting QMI bands: bands are required")
	}
	legacy, lte, nr, err := bandMasks(bands)
	if err != nil {
		return err
	}
	preference, err := b.client.SystemSelectionPreference(ctx)
	if err != nil {
		return fmt.Errorf("setting QMI bands: reading current preference: %w", err)
	}
	config, err := bandSelectionConfig(preference, legacy, lte, nr)
	if err != nil {
		return err
	}
	if err := b.client.SetSystemSelectionPreference(ctx, config); err != nil {
		return fmt.Errorf("setting QMI bands: %w", err)
	}
	return nil
}

func bandSelectionConfig(
	preference qcom.NASSystemSelectionPreference,
	legacy qcom.NASBandPreference,
	lte qcom.NASLTEBandPreferenceExtended,
	nr qcom.NASNR5GBandPreference,
) (qcom.NASSystemSelectionConfig, error) {
	duration := qcom.NASChangePermanent
	config := qcom.NASSystemSelectionConfig{
		BandPreference: (*qcom.NASBandPreference)(&legacy),
		ChangeDuration: &duration,
	}

	if preference.LTEBandsExtendedKnown {
		config.LTEBandsExtended = &lte
	} else if preference.LTEBandPreferenceKnown {
		lteLegacy := qcom.NASLTEBandPreference(lte.Bits1To64)
		config.LTEBandPreference = &lteLegacy
	} else if bandsWordsSet(lteWords(lte)) {
		return qcom.NASSystemSelectionConfig{}, errors.New("setting QMI bands: LTE band preference is unavailable")
	}

	// QMI marks the combined NR TLV deprecated. Only write split masks that the
	// modem advertises, because LTE-only devices may reject unknown NR fields.
	if preference.NR5GSABandsKnown {
		config.NR5GSABands = &nr
	}
	if preference.NR5GNSABandsKnown {
		config.NR5GNSABands = &nr
	}
	if bandsWordsSet(nrWords(nr)) && config.NR5GSABands == nil && config.NR5GNSABands == nil {
		return qcom.NASSystemSelectionConfig{}, errors.New("setting QMI bands: split NR5G band preference is unavailable")
	}
	return config, nil
}

func bandMasks(bands []Band) (qcom.NASBandPreference, qcom.NASLTEBandPreferenceExtended, qcom.NASNR5GBandPreference, error) {
	var legacy qcom.NASBandPreference
	var lte qcom.NASLTEBandPreferenceExtended
	var nr qcom.NASNR5GBandPreference
	for _, band := range bands {
		switch band.Technology {
		case TechnologyGSM, TechnologyUMTS:
			entry, ok := findLegacyBand(band)
			if !ok {
				return 0, lte, nr, fmt.Errorf("setting QMI bands: unsupported legacy band %+v", band)
			}
			legacy |= qcom.NASBandPreference(entry.mask)
		case TechnologyLTE:
			if !setWordBit(lteWordsMutable(&lte), band.Number) {
				return 0, lte, nr, fmt.Errorf("setting QMI bands: LTE band %d is outside 1..256", band.Number)
			}
		case TechnologyNR5GNSA, TechnologyNR5GSA, TechnologyNR5GNSA | TechnologyNR5GSA:
			if !setWordBit(nrWordsMutable(&nr), band.Number) {
				return 0, lte, nr, fmt.Errorf("setting QMI bands: NR band %d is outside 1..512", band.Number)
			}
		default:
			return 0, lte, nr, fmt.Errorf("setting QMI bands: technology %#x is unsupported", band.Technology)
		}
	}
	return legacy, lte, nr, nil
}

func findLegacyBand(band Band) (legacyBand, bool) {
	for _, entry := range legacyBands {
		if entry.band == band {
			return entry, true
		}
	}
	return legacyBand{}, false
}

func bandsFromLegacyMask(mask uint64) []Band {
	var bands []Band
	for _, entry := range legacyBands {
		if mask&entry.mask != 0 {
			bands = append(bands, entry.band)
		}
	}
	return bands
}

func bandsFromWords(technology Technology, words []uint64) []Band {
	var bands []Band
	for wordIndex, word := range words {
		for bit := range 64 {
			if word&(uint64(1)<<bit) != 0 {
				bands = append(bands, Band{Technology: technology, Number: uint16(wordIndex*64 + bit + 1)})
			}
		}
	}
	return bands
}

func bandsWordsSet(words []uint64) bool {
	return slices.ContainsFunc(words, func(word uint64) bool { return word != 0 })
}

func normalizeBands(bands []Band) []Band {
	slices.SortFunc(bands, func(a, b Band) int {
		if a.Technology != b.Technology {
			return int(a.Technology) - int(b.Technology)
		}
		return int(a.Number) - int(b.Number)
	})
	return slices.Compact(bands)
}

func setWordBit(words []*uint64, number uint16) bool {
	if number == 0 || int(number) > len(words)*64 {
		return false
	}
	index := int(number - 1)
	*words[index/64] |= uint64(1) << (index % 64)
	return true
}

func lteWords(value qcom.NASLTEBandPreferenceExtended) []uint64 {
	return []uint64{value.Bits1To64, value.Bits65To128, value.Bits129To192, value.Bits193To256}
}

func lteWordsMutable(value *qcom.NASLTEBandPreferenceExtended) []*uint64 {
	return []*uint64{&value.Bits1To64, &value.Bits65To128, &value.Bits129To192, &value.Bits193To256}
}

func nrWords(value qcom.NASNR5GBandPreference) []uint64 {
	return []uint64{value.Bits1To64, value.Bits65To128, value.Bits129To192, value.Bits193To256,
		value.Bits257To320, value.Bits321To384, value.Bits385To448, value.Bits449To512}
}

func nrWordsMutable(value *qcom.NASNR5GBandPreference) []*uint64 {
	return []*uint64{&value.Bits1To64, &value.Bits65To128, &value.Bits129To192, &value.Bits193To256,
		&value.Bits257To320, &value.Bits321To384, &value.Bits385To448, &value.Bits449To512}
}
