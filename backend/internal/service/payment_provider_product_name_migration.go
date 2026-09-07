package service

import (
	"context"
	"fmt"
	"strings"
)

const (
	settingProviderProductNameSettingsMigratedV1 = "PAYMENT_PROVIDER_PRODUCT_NAME_SETTINGS_MIGRATED_V1"
	providerProductNamePrefixConfigKey           = "productNamePrefix"
	defaultProviderProductNamePrefix             = "Sub2API"
)

// ensureProviderOwnedSettingsMigrated moves every legacy global setting that
// is now owned by a payment provider before the provider configuration is read.
func (s *PaymentConfigService) ensureProviderOwnedSettingsMigrated(ctx context.Context) error {
	if err := s.ensureProviderCommercialSettingsMigrated(ctx); err != nil {
		return err
	}
	return s.ensureProviderProductNameSettingsMigrated(ctx)
}

// ensureProviderProductNameSettingsMigrated copies the legacy global product
// prefix into each existing provider once. The suffix is intentionally not
// copied: it is always derived from that provider's payment currency.
func (s *PaymentConfigService) ensureProviderProductNameSettingsMigrated(ctx context.Context) error {
	if s == nil || s.entClient == nil || s.settingRepo == nil {
		return nil
	}
	s.migrationMu.Lock()
	defer s.migrationMu.Unlock()

	marker, err := s.settingRepo.GetMultiple(ctx, []string{settingProviderProductNameSettingsMigratedV1})
	if err != nil {
		return err
	}
	if marker[settingProviderProductNameSettingsMigratedV1] == "true" {
		return nil
	}

	legacy, err := s.settingRepo.GetMultiple(ctx, []string{SettingProductNamePrefix})
	if err != nil {
		return err
	}
	prefix := strings.TrimSpace(legacy[SettingProductNamePrefix])
	if prefix == "" {
		prefix = defaultProviderProductNamePrefix
	}

	instances, err := s.entClient.PaymentProviderInstance.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("query provider instances: %w", err)
	}
	for _, instance := range instances {
		config, err := s.decryptConfig(instance.Config)
		if err != nil {
			return fmt.Errorf("decrypt provider instance %d: %w", instance.ID, err)
		}
		if config == nil {
			config = map[string]string{}
		}
		if strings.TrimSpace(config[providerProductNamePrefixConfigKey]) != "" {
			continue
		}
		config[providerProductNamePrefixConfigKey] = prefix
		encoded, err := s.encryptConfig(config)
		if err != nil {
			return fmt.Errorf("encode provider instance %d product settings: %w", instance.ID, err)
		}
		if _, err := s.entClient.PaymentProviderInstance.UpdateOneID(instance.ID).SetConfig(encoded).Save(ctx); err != nil {
			return fmt.Errorf("update provider instance %d product settings: %w", instance.ID, err)
		}
	}
	return s.settingRepo.Set(ctx, settingProviderProductNameSettingsMigratedV1, "true")
}
