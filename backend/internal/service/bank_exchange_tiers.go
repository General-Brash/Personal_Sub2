package service

import (
	"encoding/json"
	"math"
)

// BankExchangeTier is a marginal-rate band. UpTo is an inclusive cumulative
// daily permanent-exchange boundary; nil marks the final unbounded band.
type BankExchangeTier struct {
	UpTo *float64 `json:"up_to,omitempty"`
	Rate float64  `json:"rate"`
}

type BankExchangeTierDTO struct {
	UpTo *string `json:"up_to"`
	Rate string  `json:"rate"`
}

type BankExchangeTierAllocation struct {
	TierIndex       int    `json:"tier_index"`
	Rate            string `json:"rate"`
	PermanentAmount string `json:"permanent_amount"`
	TemporaryAmount string `json:"temporary_amount"`
}

func normalizeBankExchangeTiers(tiers []BankExchangeTier, fallbackRate float64) ([]BankExchangeTier, error) {
	if len(tiers) == 0 {
		if fallbackRate <= 0 {
			return nil, ErrBankPolicyInvalid
		}
		tiers = []BankExchangeTier{{Rate: fallbackRate}}
	}
	copyTiers := append([]BankExchangeTier(nil), tiers...)
	// Do not silently reorder administrator input. Stable ordering is part of
	// the policy contract and makes boundary mistakes visible to callers.
	for i := range copyTiers {
		rate, err := normalizeLedgerAmount(copyTiers[i].Rate)
		if err != nil || rate <= 0 {
			return nil, ErrBankPolicyInvalid
		}
		copyTiers[i].Rate = rate
		if copyTiers[i].UpTo != nil {
			boundary, err := normalizeLedgerAmount(*copyTiers[i].UpTo)
			if err != nil || boundary <= 0 {
				return nil, ErrBankPolicyInvalid
			}
			copyTiers[i].UpTo = &boundary
		}
		if i == len(copyTiers)-1 {
			if copyTiers[i].UpTo != nil {
				return nil, ErrBankPolicyInvalid
			}
		} else if copyTiers[i].UpTo == nil {
			return nil, ErrBankPolicyInvalid
		}
		if i > 0 && copyTiers[i-1].UpTo != nil && copyTiers[i].UpTo != nil && *copyTiers[i].UpTo <= *copyTiers[i-1].UpTo {
			return nil, ErrBankPolicyInvalid
		}
	}
	return copyTiers, nil
}

func bankExchangeTierDTOs(tiers []BankExchangeTier) []BankExchangeTierDTO {
	out := make([]BankExchangeTierDTO, 0, len(tiers))
	for _, tier := range tiers {
		item := BankExchangeTierDTO{Rate: formatLedgerAmount(tier.Rate)}
		if tier.UpTo != nil {
			value := formatLedgerAmount(*tier.UpTo)
			item.UpTo = &value
		}
		out = append(out, item)
	}
	return out
}

func parseBankExchangeTierDTOs(dtos []BankExchangeTierDTO, fallbackRate float64) ([]BankExchangeTier, error) {
	if len(dtos) == 0 {
		return normalizeBankExchangeTiers(nil, fallbackRate)
	}
	tiers := make([]BankExchangeTier, 0, len(dtos))
	for _, dto := range dtos {
		rate, err := ParseStrictPositiveLedgerAmount(dto.Rate)
		if err != nil {
			return nil, ErrBankPolicyInvalid
		}
		var upTo *float64
		if dto.UpTo != nil {
			value, err := ParseStrictPositiveLedgerAmount(*dto.UpTo)
			if err != nil {
				return nil, ErrBankPolicyInvalid
			}
			upTo = &value
		}
		tiers = append(tiers, BankExchangeTier{UpTo: upTo, Rate: rate})
	}
	return normalizeBankExchangeTiers(tiers, fallbackRate)
}

func marshalBankExchangeTiers(tiers []BankExchangeTier) string {
	raw, err := json.Marshal(bankExchangeTierDTOs(tiers))
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseStoredBankExchangeTiers(raw string, fallbackRate float64) ([]BankExchangeTier, error) {
	var dtos []BankExchangeTierDTO
	if err := json.Unmarshal([]byte(raw), &dtos); err != nil {
		return nil, ErrBankPolicyInvalid
	}
	return parseBankExchangeTierDTOs(dtos, fallbackRate)
}

// calculateBankTieredExchange allocates one request across the marginal bands
// using eight-decimal rounding at each allocation boundary.
func calculateBankTieredExchange(tiers []BankExchangeTier, cumulativeBefore, requested float64) (float64, []BankExchangeTierAllocation, error) {
	if requested <= 0 || cumulativeBefore < 0 {
		return 0, nil, ErrBankAmountInvalid
	}
	if _, err := normalizeBankExchangeTiers(tiers, 0); err != nil {
		return 0, nil, ErrBankPolicyInvalid
	}
	cursor, remaining := cumulativeBefore, requested
	var granted float64
	allocations := make([]BankExchangeTierAllocation, 0, len(tiers))
	for index, tier := range tiers {
		if remaining <= ledgerAmountEpsilon {
			break
		}
		if tier.UpTo != nil && cursor >= *tier.UpTo-ledgerAmountEpsilon {
			continue
		}
		portion := remaining
		if tier.UpTo != nil {
			portion = math.Min(portion, *tier.UpTo-cursor)
		}
		portion, _ = normalizeLedgerAmount(portion)
		if portion <= ledgerAmountEpsilon {
			continue
		}
		partGranted, err := normalizeLedgerAmount(portion * tier.Rate)
		if err != nil {
			return 0, nil, ErrBankAmountInvalid
		}
		granted, err = normalizeLedgerAmount(granted + partGranted)
		if err != nil {
			return 0, nil, ErrBankAmountInvalid
		}
		allocations = append(allocations, BankExchangeTierAllocation{
			TierIndex:       index,
			Rate:            formatLedgerAmount(tier.Rate),
			PermanentAmount: formatLedgerAmount(portion),
			TemporaryAmount: formatLedgerAmount(partGranted),
		})
		cursor, _ = normalizeLedgerAmount(cursor + portion)
		remaining, _ = normalizeLedgerAmount(remaining - portion)
	}
	if remaining > ledgerAmountEpsilon {
		return 0, nil, ErrBankPolicyInvalid
	}
	return granted, allocations, nil
}

func bankExchangeProgress(tiers []BankExchangeTier, date string, cumulative float64) BankExchangeProgress {
	progress := BankExchangeProgress{
		Date:                    date,
		PermanentExchangedToday: formatLedgerAmount(cumulative),
		CurrentTierIndex:        len(tiers) - 1,
	}
	if len(tiers) == 0 {
		return progress
	}
	current := len(tiers) - 1
	for index, tier := range tiers {
		if tier.UpTo == nil || cumulative < *tier.UpTo-ledgerAmountEpsilon {
			current = index
			break
		}
	}
	progress.CurrentTierIndex = current
	progress.CurrentTierRate = formatLedgerAmount(tiers[current].Rate)
	if tiers[current].UpTo != nil {
		value := formatLedgerAmount(*tiers[current].UpTo)
		progress.CurrentTierUpTo = &value
	}
	if current+1 < len(tiers) {
		nextRate := formatLedgerAmount(tiers[current+1].Rate)
		progress.NextTierRate = &nextRate
		if tiers[current].UpTo != nil {
			remaining := *tiers[current].UpTo - cumulative
			if remaining < 0 {
				remaining = 0
			}
			remaining, _ = normalizeLedgerAmount(remaining)
			value := formatLedgerAmount(remaining)
			progress.AmountUntilNextTier = &value
		}
	}
	return progress
}
