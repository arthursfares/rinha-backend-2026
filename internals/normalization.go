package internals

import (
	"slices"
)

const (
	maxAmount            = 10000.0
	maxInstallments      = 12.0
	amountVsAvgRatio     = 10.0
	maxMinutes           = 1440.0
	maxKm                = 1000.0
	maxTxCount24h        = 20.0
	maxMerchantAvgAmount = 10000.0
)

var mcc_risk_map = map[string]float64{
  "5411": 0.15,
  "5812": 0.30,
  "5912": 0.20,
  "5944": 0.45,
  "7801": 0.80,
  "7802": 0.75,
  "7995": 0.85,
  "4511": 0.35,
  "5311": 0.25,
  "5999": 0.50,
}

func clamp(val float64) float64 {
	clamped_val := max(0, min(val, 1))
	return clamped_val
}

func NormalizeValues(e Event) [DIM]float64 {
	amount := clamp(e.Transaction.Amount / maxAmount)
	installments := clamp(float64(e.Transaction.Installments) / maxInstallments)
	amount_vs_avg := clamp((e.Transaction.Amount/e.Customer.AvgAmount) / amountVsAvgRatio)
	hour_of_day := float64(e.Transaction.RequestedAt.Hour()) / 23.0
	
	standardWeekday := int(e.Transaction.RequestedAt.Weekday())
	mondayZeroBased := (standardWeekday + 6) % 7
	day_of_week := float64(mondayZeroBased) / 6.0

	minutes_since_last_tx := -1.0
	if e.LastTransaction != nil {
		delta := e.Transaction.RequestedAt.Sub(e.LastTransaction.Timestamp)
		minutes_since_last_tx = clamp(delta.Minutes() / maxMinutes)
	}

	km_from_last_tx := -1.0
	if e.LastTransaction != nil {
		km_from_last_tx = clamp(e.LastTransaction.KmFromCurrent / maxKm)
	}

	km_from_home := clamp(e.Terminal.KmFromHome / maxKm)

	tx_count_24h := clamp(float64(e.Customer.TxCount24h) / maxTxCount24h)

	is_online := 0.0
	if e.Terminal.IsOnline { is_online = 1 }

	card_present := 0
	if e.Terminal.CardPresent { card_present = 1 }

	known_merchant_bool := slices.Contains(e.Customer.KnownMerchants, e.Merchant.ID)
	unkown_merchant := 1
	if known_merchant_bool { unkown_merchant = 0 }

	mcc_risk, ok := mcc_risk_map[e.Merchant.MCC]
	if !ok { mcc_risk = 0.5 }

	merchant_avg_amount := clamp(e.Merchant.AvgAmount / maxMerchantAvgAmount)

	return [DIM]float64{
		amount,
		installments,
		amount_vs_avg,
		hour_of_day,
		day_of_week,
		minutes_since_last_tx,
		km_from_last_tx,
		km_from_home,
		tx_count_24h,
		float64(is_online),
		float64(card_present),
		float64(unkown_merchant),
		mcc_risk,
		merchant_avg_amount,
	}
}
