package service

import (
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

const checkinAmountRandomRuleVersion = "checkin-amount-random-v1"

var checkinRandomAmountPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,11})(?:\.[0-9]{1,2})?$`)

type CheckinRandomRewardPolicy struct {
	Enabled   bool   `json:"enabled"`
	MinAmount string `json:"min_amount"`
	MaxAmount string `json:"max_amount"`
}

func DefaultCheckinRandomRewardPolicy() CheckinRandomRewardPolicy {
	return CheckinRandomRewardPolicy{MinAmount: "1.00", MaxAmount: "1.00"}
}

func parseCheckinRandomAmountCents(raw string) (int64, error) {
	if !checkinRandomAmountPattern.MatchString(raw) {
		return 0, ErrDailyCheckinPolicyInvalid
	}
	integer, decimals, _ := strings.Cut(raw, ".")
	whole, _ := strconv.ParseInt(integer, 10, 64)
	var fraction int64
	if decimals != "" {
		fraction, _ = strconv.ParseInt(decimals, 10, 64)
		if len(decimals) == 1 {
			fraction *= 10
		}
	}
	// The grammar bounds whole below 10^12, so cents cannot overflow int64.
	cents := whole*100 + fraction
	if cents < 1 || cents >= 100000000000000 {
		return 0, ErrDailyCheckinPolicyInvalid
	}
	return cents, nil
}

func formatCheckinRandomCents(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

func (p CheckinRandomRewardPolicy) centsRange() (int64, int64, error) {
	min, err := parseCheckinRandomAmountCents(p.MinAmount)
	if err != nil {
		return 0, 0, err
	}
	max, err := parseCheckinRandomAmountCents(p.MaxAmount)
	if err != nil || max < min {
		return 0, 0, ErrDailyCheckinPolicyInvalid
	}
	return min, max, nil
}

func (p CheckinRandomRewardPolicy) normalized() (CheckinRandomRewardPolicy, error) {
	min, max, err := p.centsRange()
	if err != nil {
		return CheckinRandomRewardPolicy{}, err
	}
	p.MinAmount, p.MaxAmount = formatCheckinRandomCents(min), formatCheckinRandomCents(max)
	return p, nil
}

type checkinRandomReader func([]byte) (int, error)

func (read checkinRandomReader) Read(buffer []byte) (int, error) {
	n, err := read(buffer)
	if err == nil && n != len(buffer) {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

func (s *CheckinService) sampleCheckinRandomReward(p CheckinRandomRewardPolicy) (int64, error) {
	min, max, err := p.centsRange()
	if err != nil {
		return 0, err
	}
	read := s.randRead
	if read == nil {
		read = cryptorand.Read
	}
	if min == max {
		// A degenerate range still observes secure-source failure; rand.Int(1)
		// otherwise returns zero without reading its Reader.
		var probe [1]byte
		if _, err := io.ReadFull(checkinRandomReader(read), probe[:]); err != nil {
			return 0, ErrCheckinRandomUnavailable
		}
		return min, nil
	}
	// crypto/rand.Int uses rejection sampling, including both amount endpoints.
	offset, err := cryptorand.Int(checkinRandomReader(read), big.NewInt(max-min+1))
	if err != nil {
		return 0, ErrCheckinRandomUnavailable
	}
	return min + offset.Int64(), nil
}
