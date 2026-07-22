package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/currencyproduct"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// CreateCurrencyProductRequest describes one internal-credit mall product.
// CreditedPermanentAmount remains accepted for old admin clients.
type CreateCurrencyProductRequest struct {
	Name                    string  `json:"name"`
	Description             string  `json:"description"`
	PaymentPrice            float64 `json:"payment_price"`
	PaymentCreditType       string  `json:"payment_credit_type"`
	CreditedType            string  `json:"credited_type"`
	CreditedAmount          float64 `json:"credited_amount"`
	CreditedPermanentAmount float64 `json:"credited_permanent_amount"`
	SortOrder               int     `json:"sort_order"`
	IsActive                bool    `json:"is_active"`
	ForSale                 bool    `json:"for_sale"`
	DailyPurchaseLimit      int     `json:"daily_purchase_limit"`
	TotalPurchaseLimit      int     `json:"total_purchase_limit"`
}

type UpdateCurrencyProductRequest struct {
	Name                    *string  `json:"name"`
	Description             *string  `json:"description"`
	PaymentPrice            *float64 `json:"payment_price"`
	PaymentCreditType       *string  `json:"payment_credit_type"`
	CreditedType            *string  `json:"credited_type"`
	CreditedAmount          *float64 `json:"credited_amount"`
	CreditedPermanentAmount *float64 `json:"credited_permanent_amount"`
	SortOrder               *int     `json:"sort_order"`
	IsActive                *bool    `json:"is_active"`
	ForSale                 *bool    `json:"for_sale"`
	DailyPurchaseLimit      *int     `json:"daily_purchase_limit"`
	TotalPurchaseLimit      *int     `json:"total_purchase_limit"`
}

func validateCurrencyProductName(name string) error {
	if strings.TrimSpace(name) == "" {
		return infraerrors.BadRequest("CURRENCY_PRODUCT_NAME_REQUIRED", "currency product name is required")
	}
	if len([]rune(name)) > 100 {
		return infraerrors.BadRequest("CURRENCY_PRODUCT_NAME_INVALID", "currency product name is too long")
	}
	return nil
}

func validateCurrencyProductPaymentPrice(value float64) (float64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, infraerrors.BadRequest("CURRENCY_PRODUCT_PAYMENT_PRICE_INVALID", "payment price must be positive")
	}
	normalized, err := normalizeLedgerAmount(value)
	if err != nil || normalized <= 0 {
		return 0, infraerrors.BadRequest("CURRENCY_PRODUCT_PAYMENT_PRICE_INVALID", "payment price is out of range")
	}
	return normalized, nil
}

func validateCurrencyProductCreditedAmount(value float64) (float64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, infraerrors.BadRequest("CURRENCY_PRODUCT_CREDIT_INVALID", "credited amount must be positive")
	}
	normalized, err := normalizeLedgerAmount(value)
	if err != nil || normalized <= 0 {
		return 0, infraerrors.BadRequest("CURRENCY_PRODUCT_CREDIT_INVALID", "credited amount is out of range")
	}
	return normalized, nil
}

func validateCurrencyProductCreate(req CreateCurrencyProductRequest) (string, float64, float64, error) {
	name, price, credited, _, _, err := validateCurrencyProductCreateInternal(req)
	return name, price, credited, err
}

func validateCurrencyProductCreateInternal(req CreateCurrencyProductRequest) (string, float64, float64, MallCreditType, MallCreditType, error) {
	if err := validateCurrencyProductName(req.Name); err != nil {
		return "", 0, 0, "", "", err
	}
	price, err := validateCurrencyProductPaymentPrice(req.PaymentPrice)
	if err != nil {
		return "", 0, 0, "", "", err
	}
	creditedInput := req.CreditedAmount
	if creditedInput == 0 {
		creditedInput = req.CreditedPermanentAmount
	}
	credited, err := validateCurrencyProductCreditedAmount(creditedInput)
	if err != nil {
		return "", 0, 0, "", "", err
	}
	paymentType, err := normalizeMallCreditType(req.PaymentCreditType)
	if err != nil {
		return "", 0, 0, "", "", err
	}
	creditedType, err := normalizeMallCreditType(req.CreditedType)
	if err != nil {
		return "", 0, 0, "", "", err
	}
	if err := validatePurchaseLimits(req.DailyPurchaseLimit, req.TotalPurchaseLimit); err != nil {
		return "", 0, 0, "", "", err
	}
	return strings.TrimSpace(req.Name), price, credited, paymentType, creditedType, nil
}

func validateCurrencyProductPatch(req UpdateCurrencyProductRequest) error {
	if req.Name != nil {
		if err := validateCurrencyProductName(*req.Name); err != nil {
			return err
		}
	}
	if req.PaymentPrice != nil {
		if _, err := validateCurrencyProductPaymentPrice(*req.PaymentPrice); err != nil {
			return err
		}
	}
	if req.CreditedPermanentAmount != nil {
		if _, err := validateCurrencyProductCreditedAmount(*req.CreditedPermanentAmount); err != nil {
			return err
		}
	}
	if req.CreditedAmount != nil {
		if _, err := validateCurrencyProductCreditedAmount(*req.CreditedAmount); err != nil {
			return err
		}
	}
	if req.PaymentCreditType != nil {
		if _, err := normalizeMallCreditType(*req.PaymentCreditType); err != nil {
			return err
		}
	}
	if req.CreditedType != nil {
		if _, err := normalizeMallCreditType(*req.CreditedType); err != nil {
			return err
		}
	}
	if err := validatePurchaseLimitPatch(req.DailyPurchaseLimit, req.TotalPurchaseLimit); err != nil {
		return err
	}
	return nil
}

func (s *PaymentConfigService) ListCurrencyProducts(ctx context.Context) ([]*dbent.CurrencyProduct, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("payment config service is not configured")
	}
	return s.entClient.CurrencyProduct.Query().
		Order(currencyproduct.BySortOrder(), currencyproduct.ByID()).
		All(ctx)
}

func (s *PaymentConfigService) ListCurrencyProductsForSale(ctx context.Context) ([]*dbent.CurrencyProduct, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("payment config service is not configured")
	}
	return s.entClient.CurrencyProduct.Query().
		Where(currencyproduct.IsActiveEQ(true), currencyproduct.ForSaleEQ(true)).
		Order(currencyproduct.BySortOrder(), currencyproduct.ByID()).
		All(ctx)
}

func (s *PaymentConfigService) GetCurrencyProduct(ctx context.Context, id int64) (*dbent.CurrencyProduct, error) {
	if id <= 0 {
		return nil, infraerrors.NotFound("CURRENCY_PRODUCT_NOT_FOUND", "currency product not found")
	}
	product, err := s.entClient.CurrencyProduct.Get(ctx, id)
	if err != nil {
		return nil, infraerrors.NotFound("CURRENCY_PRODUCT_NOT_FOUND", "currency product not found")
	}
	return product, nil
}

func (s *PaymentConfigService) GetCurrencyProductForSale(ctx context.Context, id int64) (*dbent.CurrencyProduct, error) {
	if id <= 0 {
		return nil, infraerrors.NotFound("CURRENCY_PRODUCT_NOT_AVAILABLE", "currency product is not available")
	}
	product, err := s.entClient.CurrencyProduct.Query().
		Where(currencyproduct.IDEQ(id), currencyproduct.IsActiveEQ(true), currencyproduct.ForSaleEQ(true)).
		Only(ctx)
	if err != nil {
		return nil, infraerrors.NotFound("CURRENCY_PRODUCT_NOT_AVAILABLE", "currency product is not available")
	}
	return product, nil
}

func (s *PaymentConfigService) CreateCurrencyProduct(ctx context.Context, req CreateCurrencyProductRequest) (*dbent.CurrencyProduct, error) {
	name, price, credited, paymentType, creditedType, err := validateCurrencyProductCreateInternal(req)
	if err != nil {
		return nil, err
	}
	return s.entClient.CurrencyProduct.Create().
		SetName(name).
		SetDescription(req.Description).
		SetPaymentPrice(price).
		SetPaymentCreditType(string(paymentType)).
		SetCreditedType(string(creditedType)).
		SetCreditedAmount(credited).
		SetCreditedPermanentAmount(credited).
		SetSortOrder(req.SortOrder).
		SetIsActive(req.IsActive).
		SetForSale(req.ForSale).
		SetDailyPurchaseLimit(req.DailyPurchaseLimit).
		SetTotalPurchaseLimit(req.TotalPurchaseLimit).
		Save(ctx)
}

func (s *PaymentConfigService) UpdateCurrencyProduct(ctx context.Context, id int64, req UpdateCurrencyProductRequest) (*dbent.CurrencyProduct, error) {
	if err := validateCurrencyProductPatch(req); err != nil {
		return nil, err
	}
	u := s.entClient.CurrencyProduct.UpdateOneID(id)
	if req.Name != nil {
		u.SetName(strings.TrimSpace(*req.Name))
	}
	if req.Description != nil {
		u.SetDescription(*req.Description)
	}
	if req.PaymentPrice != nil {
		price, _ := validateCurrencyProductPaymentPrice(*req.PaymentPrice)
		u.SetPaymentPrice(price)
	}
	if req.PaymentCreditType != nil {
		paymentType, _ := normalizeMallCreditType(*req.PaymentCreditType)
		u.SetPaymentCreditType(string(paymentType))
	}
	if req.CreditedType != nil {
		creditedType, _ := normalizeMallCreditType(*req.CreditedType)
		u.SetCreditedType(string(creditedType))
	}
	if req.CreditedAmount != nil {
		credited, _ := validateCurrencyProductCreditedAmount(*req.CreditedAmount)
		u.SetCreditedAmount(credited).SetCreditedPermanentAmount(credited)
	}
	if req.CreditedPermanentAmount != nil {
		credited, _ := validateCurrencyProductCreditedAmount(*req.CreditedPermanentAmount)
		u.SetCreditedPermanentAmount(credited).SetCreditedAmount(credited)
	}
	if req.SortOrder != nil {
		u.SetSortOrder(*req.SortOrder)
	}
	if req.IsActive != nil {
		u.SetIsActive(*req.IsActive)
	}
	if req.ForSale != nil {
		u.SetForSale(*req.ForSale)
	}
	if req.DailyPurchaseLimit != nil {
		u.SetDailyPurchaseLimit(*req.DailyPurchaseLimit)
	}
	if req.TotalPurchaseLimit != nil {
		u.SetTotalPurchaseLimit(*req.TotalPurchaseLimit)
	}
	product, err := u.Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, infraerrors.NotFound("CURRENCY_PRODUCT_NOT_FOUND", "currency product not found")
		}
		return nil, err
	}
	return product, nil
}

func (s *PaymentConfigService) DeleteCurrencyProduct(ctx context.Context, id int64) error {
	if id <= 0 {
		return infraerrors.NotFound("CURRENCY_PRODUCT_NOT_FOUND", "currency product not found")
	}
	if err := s.entClient.CurrencyProduct.DeleteOneID(id).Exec(ctx); err != nil {
		if dbent.IsNotFound(err) {
			return infraerrors.NotFound("CURRENCY_PRODUCT_NOT_FOUND", "currency product not found")
		}
		return err
	}
	return nil
}
