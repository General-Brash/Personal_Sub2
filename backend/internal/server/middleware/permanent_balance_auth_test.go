package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type permanentBalanceAuthCheckerStub struct {
	err    error
	calls  int
	userID int64
}

func (s *permanentBalanceAuthCheckerStub) CheckPermanentBalanceEligibility(_ context.Context, userID int64) error {
	s.calls++
	s.userID = userID
	return s.err
}

func newPermanentBalanceSubscriptionFixture(t *testing.T) (*service.APIKey, *service.APIKeyService, *service.SubscriptionService) {
	t.Helper()
	apiKey := newAvailableCreditAuthTestAPIKey()
	group := &service.Group{
		ID:               7,
		Status:           service.StatusActive,
		Hydrated:         true,
		SubscriptionType: service.SubscriptionTypeSubscription,
	}
	apiKey.Group = group
	apiKey.GroupID = &group.ID
	apiKey.User.Balance = 10
	apiKeyService := newAvailableCreditAuthTestService(apiKey, &availableCreditEligibilityCheckerStub{})
	now := time.Now()
	subscription := &service.UserSubscription{
		ID:        11,
		UserID:    apiKey.User.ID,
		GroupID:   group.ID,
		Status:    service.SubscriptionStatusActive,
		ExpiresAt: now.Add(time.Hour),
	}
	subscriptionService := service.NewSubscriptionService(nil, fakeGoogleSubscriptionRepo{
		getActive: func(context.Context, int64, int64) (*service.UserSubscription, error) {
			clone := *subscription
			return &clone, nil
		},
	}, nil, nil, &config.Config{RunMode: config.RunModeStandard})
	t.Cleanup(subscriptionService.Stop)
	return apiKey, apiKeyService, subscriptionService
}

func TestAPIKeyAuthRejectsNegativePermanentBalanceForSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)
	apiKey, apiKeyService, subscriptionService := newPermanentBalanceSubscriptionFixture(t)
	checker := &permanentBalanceAuthCheckerStub{err: service.ErrInsufficientBalance}
	apiKeyService.SetPermanentBalanceEligibilityChecker(checker)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiKeyService, subscriptionService, &config.Config{RunMode: config.RunModeStandard})))
	router.GET("/t", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/t", nil)
	request.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Equal(t, 1, checker.calls)
	require.Equal(t, apiKey.User.ID, checker.userID)
}

func TestGoogleAPIKeyAuthRejectsNegativePermanentBalanceForSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)
	apiKey, apiKeyService, subscriptionService := newPermanentBalanceSubscriptionFixture(t)
	checker := &permanentBalanceAuthCheckerStub{err: service.ErrInsufficientBalance}
	apiKeyService.SetPermanentBalanceEligibilityChecker(checker)
	router := gin.New()
	router.Use(APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, &config.Config{RunMode: config.RunModeStandard}))
	router.GET("/v1beta/test", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1beta/test", nil)
	request.Header.Set("x-goog-api-key", apiKey.Key)
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Equal(t, 1, checker.calls)
	require.Equal(t, apiKey.User.ID, checker.userID)
}

func TestAPIKeyAuthSkipBillingRoutesDoNotApplyPermanentBalanceBlock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	apiKey := newAvailableCreditAuthTestAPIKey()
	apiKey.User.Balance = 10
	apiKeyService := newAvailableCreditAuthTestService(apiKey, &availableCreditEligibilityCheckerStub{})
	checker := &permanentBalanceAuthCheckerStub{err: service.ErrInsufficientBalance}
	apiKeyService.SetPermanentBalanceEligibilityChecker(checker)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiKeyService, nil, &config.Config{RunMode: config.RunModeStandard})))
	router.GET("/v1/usage", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	request.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Zero(t, checker.calls)
}
