package qmi

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/damonto/wwan-go/qcom"
)

const modeNR5G = TechnologyNR5GNSA | TechnologyNR5GSA

var (
	errModePreferenceUnavailable = errors.New("mode preference is unavailable")
	modeFamilies                 = [...]Technology{TechnologyGSM, TechnologyUMTS, TechnologyLTE, modeNR5G}
)

type modeState struct {
	supported  []Mode
	current    Mode
	preference qcom.NASSystemSelectionPreference
}

func (b *Backend) Modes(ctx context.Context) ([]Mode, Mode, error) {
	state, err := b.readModeState(ctx)
	if err != nil {
		return nil, Mode{}, err
	}
	return state.supported, state.current, nil
}

func (b *Backend) readModeState(ctx context.Context) (modeState, error) {
	caps, err := b.client.DeviceCapabilities(ctx)
	if err != nil {
		return modeState{}, err
	}

	all := canonicalModeTechnology(technologyFromDMSRadios(caps.RadioInterfaces))
	if all == 0 {
		return modeState{}, ErrNotSupported
	}

	preference, err := b.client.SystemSelectionPreference(ctx)
	if err != nil {
		if isUnsupported(err) {
			return modeState{}, fmt.Errorf("reading modes: system selection preference is required: %w", ErrNotSupported)
		}
		return modeState{}, err
	}

	current := Mode{Allowed: all}
	if preference.ModePreferenceKnown {
		current.Allowed = canonicalModeTechnology(technologyFromModePreference(preference.ModePreference)) & all
		if current.Allowed == 0 {
			return modeState{}, errModePreferenceUnavailable
		}
	} else if len(enabledModeFamilies(all)) > 1 {
		return modeState{}, errModePreferenceUnavailable
	}
	current.Preferred = preferredMode(preference, current.Allowed)

	acquisitionFamilies := Technology(0)
	acquisitionKnown := preference.AcquisitionOrderKnown && len(preference.AcquisitionOrder) > 0
	if acquisitionKnown {
		acquisitionFamilies = canonicalModeTechnology(technologyFromNASRadios(preference.AcquisitionOrder)) & all
	}
	supported := modeCombinations(
		all,
		acquisitionFamilies,
		!acquisitionKnown && preference.GWAcquisitionOrderKnown,
	)

	if !slices.Contains(supported, current) {
		return modeState{}, fmt.Errorf(
			"reading modes: current mode allowed=%#x preferred=%#x is not supported",
			current.Allowed,
			current.Preferred,
		)
	}
	return modeState{supported: supported, current: current, preference: preference}, nil
}

func (b *Backend) SetModes(ctx context.Context, mode Mode) error {
	if mode.Allowed&^TechnologyAny != 0 {
		return fmt.Errorf("setting modes: allowed technologies %#x are invalid", mode.Allowed)
	}
	if mode.Preferred&^TechnologyAny != 0 {
		return fmt.Errorf("setting modes: preferred technologies %#x are invalid", mode.Preferred)
	}
	mode.Allowed = canonicalModeTechnology(mode.Allowed)
	mode.Preferred = canonicalModeTechnology(mode.Preferred)
	if mode.Allowed == 0 {
		return fmt.Errorf("setting modes: allowed technologies %#x are invalid", mode.Allowed)
	}
	if mode.Preferred != 0 && (len(enabledModeFamilies(mode.Preferred)) != 1 || mode.Preferred&^mode.Allowed != 0) {
		return fmt.Errorf("setting modes: preferred technologies %#x are invalid", mode.Preferred)
	}

	state, err := b.readModeState(ctx)
	if err != nil {
		return fmt.Errorf("validating mode: %w", err)
	}
	if !slices.Contains(state.supported, mode) {
		return fmt.Errorf(
			"setting modes: mode allowed=%#x preferred=%#x is not supported: %w",
			mode.Allowed,
			mode.Preferred,
			ErrNotSupported,
		)
	}

	config, err := modeSelectionConfig(mode, state.preference)
	if err != nil {
		return err
	}
	if err := b.client.SetSystemSelectionPreference(ctx, config); err != nil {
		if isUnsupported(err) {
			return fmt.Errorf("setting modes: %w: %w", ErrNotSupported, err)
		}
		return err
	}
	return nil
}

func modeSelectionConfig(mode Mode, preference qcom.NASSystemSelectionPreference) (qcom.NASSystemSelectionConfig, error) {
	modePreference := modePreferenceFromTechnology(mode.Allowed)
	duration := qcom.NASChangePermanent
	config := qcom.NASSystemSelectionConfig{
		ModePreference: &modePreference,
		ChangeDuration: &duration,
	}

	acquisitionKnown := preference.AcquisitionOrderKnown && len(preference.AcquisitionOrder) > 0
	if mode.Preferred != 0 {
		if acquisitionKnown {
			order, ok := preferredAcquisitionOrder(preference.AcquisitionOrder, mode.Preferred)
			if !ok {
				return qcom.NASSystemSelectionConfig{}, fmt.Errorf(
					"setting modes: preferred technology %#x is unavailable in acquisition order",
					mode.Preferred,
				)
			}
			config.AcquisitionOrder = order
		} else if mode.Allowed != TechnologyGSM|TechnologyUMTS || !preference.GWAcquisitionOrderKnown {
			return qcom.NASSystemSelectionConfig{}, fmt.Errorf(
				"setting modes: preferred technology %#x is unsupported: %w",
				mode.Preferred,
				ErrNotSupported,
			)
		}
	}
	if preference.GWAcquisitionOrderKnown && mode.Allowed&(TechnologyGSM|TechnologyUMTS) == TechnologyGSM|TechnologyUMTS {
		// Qualcomm firmware may use the dedicated GSM/WCDMA TLV even when the
		// general acquisition order is also supported, so keep both in sync.
		order := qcom.NASGWAcquisitionAutomatic
		switch mode.Preferred {
		case TechnologyGSM:
			order = qcom.NASGWAcquisitionGSMThenWCDMA
		case TechnologyUMTS:
			order = qcom.NASGWAcquisitionWCDMAThenGSM
		}
		config.GWAcquisitionOrder = &order
	}
	return config, nil
}

func canonicalModeTechnology(technology Technology) Technology {
	var result Technology
	if technology&TechnologyGSM != 0 {
		result |= TechnologyGSM
	}
	if technology&TechnologyUMTS != 0 {
		result |= TechnologyUMTS
	}
	if technology&(TechnologyLTE|TechnologyLTECatM|TechnologyLTENB) != 0 {
		result |= TechnologyLTE
	}
	if technology&modeNR5G != 0 {
		result |= modeNR5G
	}
	return result
}

func modeCombinations(all Technology, acquisitionFamilies Technology, gwAcquisition bool) []Mode {
	all = canonicalModeTechnology(all)
	if all == 0 {
		return nil
	}

	allowedModes := []Technology{all}
	for mask := 1; mask < 1<<len(modeFamilies); mask++ {
		var allowed Technology
		for index, family := range modeFamilies {
			if mask&(1<<index) != 0 {
				allowed |= family
			}
		}
		if allowed == all || allowed&^all != 0 {
			continue
		}
		allowedModes = append(allowedModes, allowed)
	}

	var result []Mode
	for _, allowed := range allowedModes {
		enabled := enabledModeFamilies(allowed)
		if len(enabled) == 1 {
			result = append(result, Mode{Allowed: allowed})
			continue
		}

		addedPreferred := false
		for _, family := range enabled {
			if acquisitionFamilies&family == family {
				result = append(result, Mode{Allowed: allowed, Preferred: family})
				addedPreferred = true
			}
		}
		if addedPreferred {
			continue
		}
		if gwAcquisition && allowed == TechnologyGSM|TechnologyUMTS {
			result = append(result,
				Mode{Allowed: allowed},
				Mode{Allowed: allowed, Preferred: TechnologyGSM},
				Mode{Allowed: allowed, Preferred: TechnologyUMTS},
			)
			continue
		}
		result = append(result, Mode{Allowed: allowed})
	}
	return result
}

func enabledModeFamilies(technology Technology) []Technology {
	technology = canonicalModeTechnology(technology)
	result := make([]Technology, 0, len(modeFamilies))
	for _, family := range modeFamilies {
		if technology&family == family {
			result = append(result, family)
		}
	}
	return result
}

func preferredMode(preference qcom.NASSystemSelectionPreference, allowed Technology) Technology {
	if preference.AcquisitionOrderKnown && len(preference.AcquisitionOrder) > 0 {
		for _, radio := range preference.AcquisitionOrder {
			candidate := canonicalModeTechnology(technologyFromNASRadio(radio))
			if candidate != 0 && candidate != allowed && candidate&^allowed == 0 {
				return candidate
			}
		}
		return 0
	}
	if allowed != TechnologyGSM|TechnologyUMTS || !preference.GWAcquisitionOrderKnown {
		return 0
	}
	switch preference.GWAcquisitionOrder {
	case qcom.NASGWAcquisitionGSMThenWCDMA:
		return TechnologyGSM
	case qcom.NASGWAcquisitionWCDMAThenGSM:
		return TechnologyUMTS
	default:
		return 0
	}
}

func preferredAcquisitionOrder(order []qcom.NASRadioInterface, preferred Technology) ([]qcom.NASRadioInterface, bool) {
	result := make([]qcom.NASRadioInterface, 0, len(order))
	for _, radio := range order {
		if canonicalModeTechnology(technologyFromNASRadio(radio)) == preferred {
			result = append(result, radio)
		}
	}
	if len(result) == 0 {
		return nil, false
	}
	for _, radio := range order {
		if canonicalModeTechnology(technologyFromNASRadio(radio)) != preferred {
			result = append(result, radio)
		}
	}
	return result, true
}

func technologyFromModePreference(preference qcom.NASModePreference) Technology {
	var result Technology
	if preference&qcom.NASModePreferenceGSM != 0 {
		result |= TechnologyGSM
	}
	if preference&qcom.NASModePreferenceUMTS != 0 {
		result |= TechnologyUMTS
	}
	if preference&qcom.NASModePreferenceLTE != 0 {
		result |= TechnologyLTE
	}
	if preference&qcom.NASModePreferenceNR5G != 0 {
		result |= modeNR5G
	}
	return result
}

func modePreferenceFromTechnology(technology Technology) qcom.NASModePreference {
	technology = canonicalModeTechnology(technology)
	var result qcom.NASModePreference
	if technology&TechnologyGSM != 0 {
		result |= qcom.NASModePreferenceGSM
	}
	if technology&TechnologyUMTS != 0 {
		result |= qcom.NASModePreferenceUMTS
	}
	if technology&TechnologyLTE != 0 {
		result |= qcom.NASModePreferenceLTE
	}
	if technology&modeNR5G != 0 {
		result |= qcom.NASModePreferenceNR5G
	}
	return result
}
