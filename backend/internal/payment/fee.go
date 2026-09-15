package payment

import (
	"github.com/shopspring/decimal"
)

func CalculatePayAmount(rechargeAmount float64, feeRate float64) string {
	return CalculatePayAmountForCurrency(rechargeAmount, feeRate, DefaultPaymentCurrency)
}

// CalculatePayAmountForCurrency 按币种精度计算应付金额，手续费向上取整到该币种最小支付单位。
func CalculatePayAmountForCurrency(rechargeAmount float64, feeRate float64, currency string) string {
	return CalculatePayAmountForCurrencyWithFixed(rechargeAmount, feeRate, 0, currency)
}

// CalculatePayAmountForCurrencyWithFixed calculates the final gateway amount.
// The percentage fee is rounded up to the currency's smallest unit, then the
// fixed fee (denominated in the gateway currency) is added.
func CalculatePayAmountForCurrencyWithFixed(rechargeAmount float64, feeRate float64, fixedFee float64, currency string) string {
	fractionDigits := int32(CurrencyMaxFractionDigits(currency))
	amount := decimal.NewFromFloat(rechargeAmount)
	fee := decimal.Zero
	if feeRate > 0 {
		rate := decimal.NewFromFloat(feeRate)
		fee = amount.Mul(rate).Div(decimal.NewFromInt(100)).RoundUp(fractionDigits)
	}
	fixed := decimal.Zero
	if fixedFee > 0 {
		fixed = decimal.NewFromFloat(fixedFee).Round(fractionDigits)
	}
	return amount.Add(fee).Add(fixed).StringFixed(fractionDigits)
}
