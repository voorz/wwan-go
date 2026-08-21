package mbim

import (
	"context"
	"errors"
	"fmt"
	"slices"

	mbimproto "github.com/voorz/wwan-go/mbim"
)

const (
	mbimExVersion20 = 0x0200
	mbimExVersion30 = 0x0300
	modeNR5G        = TechnologyNR5GNSA | TechnologyNR5GSA
)

var errModePreferenceUnavailable = errors.New("mode preference is unavailable")

func (b *Backend) Modes(ctx context.Context) ([]Mode, Mode, error) {
	if b.client.Version().MBIMExVersion < mbimExVersion20 {
		return nil, Mode{}, fmt.Errorf("reading modes: MBIMEx 2.0 is required: %w", ErrNotSupported)
	}
	supported, all, err := b.supportedModes(ctx)
	if err != nil {
		return nil, Mode{}, err
	}

	registration, err := b.client.RegistrationState(ctx)
	if err != nil {
		return nil, Mode{}, err
	}
	current := Mode{Allowed: canonicalModeTechnology(technologyFromDataClass(registration.PreferredDataClasses)) & all}
	if current.Allowed == 0 {
		return nil, Mode{}, errModePreferenceUnavailable
	}
	if !slices.Contains(supported, current) {
		return nil, Mode{}, fmt.Errorf("reading modes: current mode %#x is not supported", current.Allowed)
	}
	return supported, current, nil
}

func (b *Backend) SetModes(ctx context.Context, mode Mode) error {
	if mode.Allowed&^TechnologyAny != 0 {
		return fmt.Errorf("setting modes: allowed technologies %#x are invalid", mode.Allowed)
	}
	mode.Allowed = canonicalModeTechnology(mode.Allowed)
	if mode.Allowed == 0 {
		return fmt.Errorf("setting modes: allowed technologies %#x are invalid", mode.Allowed)
	}
	if mode.Preferred != 0 {
		return fmt.Errorf("setting modes: preferred technologies are unsupported: %w", ErrNotSupported)
	}
	if b.client.Version().MBIMExVersion < mbimExVersion20 {
		return fmt.Errorf("setting modes: MBIMEx 2.0 is required: %w", ErrNotSupported)
	}

	supported, _, err := b.supportedModes(ctx)
	if err != nil {
		return fmt.Errorf("validating mode: %w", err)
	}
	if !slices.Contains(supported, mode) {
		return fmt.Errorf("setting modes: mode %#x is not supported: %w", mode.Allowed, ErrNotSupported)
	}
	registration, err := b.client.RegistrationState(ctx)
	if err != nil {
		return fmt.Errorf("setting modes: %w", err)
	}
	providerID, action, err := modeRegistration(registration)
	if err != nil {
		return fmt.Errorf("setting modes: %w", err)
	}
	registration, err = b.client.SetRegistrationState(
		ctx,
		providerID,
		action,
		dataClass(b.client.Version().MBIMExVersion, mode.Allowed),
	)
	if err != nil {
		return fmt.Errorf("setting modes: %w", err)
	}
	actual := canonicalModeTechnology(technologyFromDataClass(registration.PreferredDataClasses))
	if actual != mode.Allowed {
		return fmt.Errorf("setting modes: modem selected %#x, want %#x", actual, mode.Allowed)
	}
	return nil
}

func (b *Backend) supportedModes(ctx context.Context) ([]Mode, Technology, error) {
	caps, err := b.client.DeviceCaps(ctx)
	if err != nil {
		return nil, 0, err
	}
	all := canonicalModeTechnology(technologyFromDataClass(caps.DataClass))
	if all == 0 {
		return nil, 0, ErrNotSupported
	}
	return modeCombinations(all), all, nil
}

func modeRegistration(registration mbimproto.RegistrationStateInfo) (string, mbimproto.RegisterAction, error) {
	switch registration.RegisterMode {
	case mbimproto.RegisterModeAutomatic:
		return "", mbimproto.RegisterActionAutomatic, nil
	case mbimproto.RegisterModeManual:
		if registration.ProviderID == "" {
			return "", mbimproto.RegisterActionAutomatic, errors.New("manual registration provider is unavailable")
		}
		return registration.ProviderID, mbimproto.RegisterActionManual, nil
	default:
		return "", mbimproto.RegisterActionAutomatic, fmt.Errorf("registration mode %d is unavailable", registration.RegisterMode)
	}
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

func modeCombinations(all Technology) []Mode {
	all = canonicalModeTechnology(all)
	if all == 0 {
		return nil
	}

	families := []Technology{TechnologyGSM, TechnologyUMTS, TechnologyLTE, modeNR5G}
	result := []Mode{{Allowed: all}}
	for mask := 1; mask < 1<<len(families); mask++ {
		var allowed Technology
		for index, family := range families {
			if mask&(1<<index) != 0 {
				allowed |= family
			}
		}
		if allowed == all || allowed&^all != 0 {
			continue
		}
		result = append(result, Mode{Allowed: allowed})
	}
	return result
}
