package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestCurrencyProductCRUDSortSaleAndDelete(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	service := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)

	first, err := service.CreateCurrencyProduct(ctx, CreateCurrencyProductRequest{
		Name: "Ten credits", Description: "small", PaymentPrice: 9.99,
		CreditedPermanentAmount: 10.12345678, SortOrder: 20, IsActive: true, ForSale: true,
		DailyPurchaseLimit: 2, TotalPurchaseLimit: 5,
	})
	require.NoError(t, err)
	second, err := service.CreateCurrencyProduct(ctx, CreateCurrencyProductRequest{
		Name: "Five credits", PaymentPrice: 5, CreditedPermanentAmount: 5,
		SortOrder: 10, IsActive: true, ForSale: false,
	})
	require.NoError(t, err)

	all, err := service.ListCurrencyProducts(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{second.ID, first.ID}, []int64{all[0].ID, all[1].ID})
	forSale, err := service.ListCurrencyProductsForSale(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{first.ID}, []int64{forSale[0].ID})
	require.InDelta(t, 10.12345678, first.CreditedPermanentAmount, 0.000000001)
	require.Equal(t, "permanent", first.PaymentCreditType)
	require.Equal(t, "permanent", first.CreditedType)
	require.InDelta(t, 10.12345678, first.CreditedAmount, 0.000000001)
	require.Equal(t, 2, first.DailyPurchaseLimit)
	require.Equal(t, 5, first.TotalPurchaseLimit)

	updated, err := service.UpdateCurrencyProduct(ctx, first.ID, UpdateCurrencyProductRequest{
		ForSale: currencyProductBool(false), SortOrder: currencyProductInt(1),
		DailyPurchaseLimit: currencyProductInt(3), TotalPurchaseLimit: currencyProductInt(7),
	})
	require.NoError(t, err)
	require.False(t, updated.ForSale)
	require.Equal(t, 3, updated.DailyPurchaseLimit)
	require.Equal(t, 7, updated.TotalPurchaseLimit)
	forSale, err = service.ListCurrencyProductsForSale(ctx)
	require.NoError(t, err)
	require.Empty(t, forSale)

	require.NoError(t, service.DeleteCurrencyProduct(ctx, second.ID))
	_, err = service.GetCurrencyProduct(ctx, second.ID)
	require.Error(t, err)
}

func TestCurrencyProductPersistsIndependentPaymentAndCreditTypes(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	config := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	product, err := config.CreateCurrencyProduct(ctx, CreateCurrencyProductRequest{
		Name: "Temporary to permanent", PaymentPrice: 1.12345678,
		PaymentCreditType: "temporary", CreditedType: "permanent", CreditedAmount: 2.87654321,
		IsActive: true, ForSale: true,
	})
	require.NoError(t, err)
	require.Equal(t, "temporary", product.PaymentCreditType)
	require.Equal(t, "permanent", product.CreditedType)
	require.Equal(t, "1.12345678", formatLedgerAmount(product.PaymentPrice))
	require.Equal(t, "2.87654321", formatLedgerAmount(product.CreditedAmount))
	require.Equal(t, product.CreditedAmount, product.CreditedPermanentAmount)
}

func TestCurrencyProductRejectsNegativePurchaseLimits(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	service := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	_, err := service.CreateCurrencyProduct(ctx, CreateCurrencyProductRequest{
		Name: "Invalid limit", PaymentPrice: 1, CreditedPermanentAmount: 1, DailyPurchaseLimit: -1,
	})
	require.Equal(t, "INVALID_PURCHASE_LIMIT", infraerrorsReason(err))
}

func TestCurrencyProductPatchRejectsPurchaseLimitAboveInt32(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	service := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	product, err := service.CreateCurrencyProduct(ctx, CreateCurrencyProductRequest{
		Name: "Bounded limit", PaymentPrice: 1, CreditedPermanentAmount: 1,
	})
	require.NoError(t, err)
	tooLarge := maxPurchaseLimit + 1
	_, err = service.UpdateCurrencyProduct(ctx, product.ID, UpdateCurrencyProductRequest{
		TotalPurchaseLimit: &tooLarge,
	})
	require.Equal(t, "INVALID_PURCHASE_LIMIT", infraerrors.Reason(err))
	require.Equal(t, 400, infraerrors.Code(err))
}

func TestCurrencyProductValidationNormalizesAllAmountsToEightDecimals(t *testing.T) {
	_, price, credited, err := validateCurrencyProductCreate(CreateCurrencyProductRequest{
		Name: "Precise", PaymentPrice: 1.25, CreditedPermanentAmount: 0.00000001,
	})
	require.NoError(t, err)
	require.Equal(t, "1.25000000", formatLedgerAmount(price))
	require.Equal(t, "0.00000001", formatLedgerAmount(credited))

	_, price, _, err = validateCurrencyProductCreate(CreateCurrencyProductRequest{
		Name: "Rounded", PaymentPrice: 1.000000009, CreditedPermanentAmount: 1,
	})
	require.NoError(t, err)
	require.Equal(t, "1.00000001", formatLedgerAmount(price))
}

func TestCurrencyProductOrderSnapshotsSurviveProductDeletion(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	configService := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	product, err := configService.CreateCurrencyProduct(ctx, CreateCurrencyProductRequest{
		Name: "Exact balance", PaymentPrice: 12.34, CreditedPermanentAmount: 12.34567891,
		IsActive: true, ForSale: true,
	})
	require.NoError(t, err)
	user, err := client.User.Create().SetEmail("currency-product@example.com").SetPasswordHash("hash").SetUsername("currency-product").Save(ctx)
	require.NoError(t, err)

	service := &PaymentService{entClient: client}
	order, err := service.createOrderInTxWithProduct(ctx, CreateOrderRequest{
		UserID: user.ID, ProductID: product.ID, PaymentType: payment.TypeAlipay,
		OrderType: payment.OrderTypeBalance, ClientIP: "127.0.0.1", SrcHost: "example.com",
	}, &User{ID: user.ID, Email: user.Email, Username: user.Username}, nil, product,
		&PaymentConfig{MaxPendingOrders: 3, OrderTimeoutMin: 30}, product.CreditedPermanentAmount,
		product.PaymentPrice, 0, product.PaymentPrice, nil)
	require.NoError(t, err)
	require.Equal(t, product.ID, *order.CurrencyProductID)
	require.Equal(t, product.Name, *order.CurrencyProductName)
	require.InDelta(t, product.PaymentPrice, *order.CurrencyProductPaymentPrice, 0.000000001)
	require.InDelta(t, product.CreditedPermanentAmount, *order.CurrencyProductCreditedAmount, 0.000000001)
	require.Equal(t, "12.34567891", formatLedgerAmount(order.Amount))

	require.NoError(t, configService.DeleteCurrencyProduct(ctx, product.ID))
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, product.ID, *reloaded.CurrencyProductID)
	require.Equal(t, product.Name, *reloaded.CurrencyProductName)
	require.Equal(t, "12.34567891", formatLedgerAmount(*reloaded.CurrencyProductCreditedAmount))
}

func TestCurrencyProductOrderInputRejectsClientAmountAndUnavailableProduct(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	configService := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	product, err := configService.CreateCurrencyProduct(ctx, CreateCurrencyProductRequest{
		Name: "Only product id", PaymentPrice: 5, CreditedPermanentAmount: 5, IsActive: true, ForSale: true,
	})
	require.NoError(t, err)
	service := &PaymentService{configService: configService}
	cfg := &PaymentConfig{MinAmount: 1, MaxAmount: 100}
	_, _, err = service.validateOrderInput(ctx, CreateOrderRequest{
		OrderType: payment.OrderTypeBalance, ProductID: int64(product.ID), Amount: 1,
	}, cfg)
	require.Error(t, err)
	require.Equal(t, "CURRENCY_PRODUCT_INPUT_INVALID", infraerrorsReason(err))

	_, _, err = service.validateOrderInput(ctx, CreateOrderRequest{
		OrderType: payment.OrderTypeBalance, ProductID: int64(product.ID),
	}, cfg)
	require.NoError(t, err)
	_, err = configService.UpdateCurrencyProduct(ctx, int64(product.ID), UpdateCurrencyProductRequest{ForSale: currencyProductBool(false)})
	require.NoError(t, err)
	_, _, err = service.validateOrderInput(ctx, CreateOrderRequest{
		OrderType: payment.OrderTypeBalance, ProductID: int64(product.ID),
	}, cfg)
	require.Equal(t, "CURRENCY_PRODUCT_NOT_AVAILABLE", infraerrorsReason(err))
}

func TestCurrencyProductOrderRequiresBalanceTypeAndBypassesCustomRechargeLimits(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	configService := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	product, err := configService.CreateCurrencyProduct(ctx, CreateCurrencyProductRequest{
		Name: "Shelf price", PaymentPrice: 5, CreditedPermanentAmount: 8,
		IsActive: true, ForSale: true,
	})
	require.NoError(t, err)
	service := &PaymentService{configService: configService}
	cfg := &PaymentConfig{MinAmount: 10, MaxAmount: 100}

	_, _, err = service.validateOrderInput(ctx, CreateOrderRequest{
		OrderType: "unknown", ProductID: product.ID,
	}, cfg)
	require.Error(t, err)
	require.Equal(t, "INVALID_ORDER_TYPE", infraerrorsReason(err))

	_, selected, err := service.validateOrderInput(ctx, CreateOrderRequest{
		OrderType: payment.OrderTypeBalance, ProductID: product.ID,
	}, cfg)
	require.NoError(t, err)
	require.Equal(t, product.ID, selected.ID)
}

func currencyProductBool(value bool) *bool { return &value }
func currencyProductInt(value int) *int    { return &value }
