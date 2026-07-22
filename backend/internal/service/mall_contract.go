package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type MallCreditType string

const (
	MallCreditTypePermanent MallCreditType = "permanent"
	MallCreditTypeTemporary MallCreditType = "temporary"
)

type MallProductType string

const (
	MallProductTypeCurrency     MallProductType = "currency"
	MallProductTypeSubscription MallProductType = "subscription"
)

type SubscriptionBenefitType string

const (
	SubscriptionBenefitSub2                 SubscriptionBenefitType = "sub2"
	SubscriptionBenefitDailyTemporaryCredit SubscriptionBenefitType = "daily_temporary_credit"
)

func normalizeMallCreditType(raw string) (MallCreditType, error) {
	value := MallCreditType(strings.ToLower(strings.TrimSpace(raw)))
	if value == "" {
		value = MallCreditTypePermanent
	}
	if value != MallCreditTypePermanent && value != MallCreditTypeTemporary {
		return "", infraerrors.BadRequest("MALL_CREDIT_TYPE_INVALID", "credit type must be permanent or temporary")
	}
	return value, nil
}

func normalizeSubscriptionBenefitType(raw string) (SubscriptionBenefitType, error) {
	value := SubscriptionBenefitType(strings.ToLower(strings.TrimSpace(raw)))
	if value == "" {
		value = SubscriptionBenefitSub2
	}
	if value != SubscriptionBenefitSub2 && value != SubscriptionBenefitDailyTemporaryCredit {
		return "", infraerrors.BadRequest("SUBSCRIPTION_BENEFIT_TYPE_INVALID", "unsupported subscription benefit type")
	}
	return value, nil
}
