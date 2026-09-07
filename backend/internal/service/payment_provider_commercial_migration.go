package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

const settingProviderCommercialSettingsMigratedV1 = "PAYMENT_PROVIDER_COMMERCIAL_SETTINGS_MIGRATED_V1"

// ensureProviderCommercialSettingsMigrated copies the legacy global commercial
// settings into every provider channel once. Runtime checkout no longer reads
// those global values after this migration.
func (s *PaymentConfigService) ensureProviderCommercialSettingsMigrated(ctx context.Context) error {
	if s == nil || s.entClient == nil || s.settingRepo == nil {
		return nil
	}
	s.migrationMu.Lock()
	defer s.migrationMu.Unlock()

	marker, err := s.settingRepo.GetMultiple(ctx, []string{settingProviderCommercialSettingsMigratedV1})
	if err != nil {
		return err
	}
	if marker[settingProviderCommercialSettingsMigratedV1] == "true" {
		return nil
	}

	legacyKeys := []string{
		SettingMinRechargeAmount,
		SettingMaxRechargeAmount,
		SettingDailyRechargeLimit,
		SettingBalanceRechargeMult,
		SettingSubscriptionUSDToCNYRate,
		SettingRechargeFeeRate,
	}
	values, err := s.settingRepo.GetMultiple(ctx, legacyKeys)
	if err != nil {
		return err
	}
	legacy := s.parsePaymentConfig(values)
	instances, err := s.entClient.PaymentProviderInstance.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("query provider instances: %w", err)
	}

	for _, instance := range instances {
		limits := payment.InstanceLimits{}
		if strings.TrimSpace(instance.Limits) != "" {
			_ = json.Unmarshal([]byte(instance.Limits), &limits)
		}
		config, err := s.decryptConfig(instance.Config)
		if err != nil {
			return fmt.Errorf("decrypt provider instance %d: %w", instance.ID, err)
		}
		currency := paymentProviderConfigCurrency(instance.ProviderKey, config)
		for _, paymentType := range providerCommercialPaymentTypes(instance.ProviderKey, instance.SupportedTypes) {
			channel := limits[paymentType]
			if channel.SingleMin <= 0 {
				channel.SingleMin = legacy.MinAmount
			}
			if channel.SingleMax <= 0 {
				channel.SingleMax = legacy.MaxAmount
			}
			if channel.DailyLimit <= 0 {
				channel.DailyLimit = legacy.DailyLimit
			}
			if channel.BalanceMultiplier == nil {
				channel.BalanceMultiplier = commercialFloat(normalizeBalanceRechargeMultiplier(legacy.BalanceRechargeMultiplier))
			}
			if channel.SubscriptionMultiplier == nil {
				multiplier := 1.0
				if currency == payment.DefaultPaymentCurrency && legacy.SubscriptionUSDToCNYRate > 0 {
					multiplier = legacy.SubscriptionUSDToCNYRate
				}
				channel.SubscriptionMultiplier = commercialFloat(multiplier)
			}
			if channel.FeeRate == nil {
				channel.FeeRate = commercialFloat(legacy.RechargeFeeRate)
			}
			if channel.FixedFee == nil {
				channel.FixedFee = commercialFloat(0)
			}
			limits[paymentType] = channel
		}
		encoded, err := json.Marshal(limits)
		if err != nil {
			return fmt.Errorf("encode provider instance %d commercial settings: %w", instance.ID, err)
		}
		if _, err := s.entClient.PaymentProviderInstance.UpdateOneID(instance.ID).SetLimits(string(encoded)).Save(ctx); err != nil {
			return fmt.Errorf("update provider instance %d commercial settings: %w", instance.ID, err)
		}
	}
	return s.settingRepo.Set(ctx, settingProviderCommercialSettingsMigratedV1, "true")
}

func providerCommercialPaymentTypes(providerKey, supportedTypes string) []string {
	if providerKey == payment.TypeStripe {
		return []string{payment.TypeStripe}
	}
	types := splitTypes(supportedTypes)
	if len(types) > 0 {
		return types
	}
	if providerKey == payment.TypeAlipay || providerKey == payment.TypeWxpay || providerKey == payment.TypeAirwallex {
		return []string{providerKey}
	}
	return nil
}

func commercialFloat(value float64) *float64 {
	return &value
}
