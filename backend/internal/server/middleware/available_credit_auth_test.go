package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type availableCreditEligibilityCheckerStub struct {
	err    error
	calls  int
	userID int64
}

func (s *availableCreditEligibilityCheckerStub) CheckAvailableCreditEligibility(_ context.Context, userID int64) error {
	s.calls++
	s.userID = userID
	return s.err
}

func newAvailableCreditAuthTestAPIKey() *service.APIKey {
	user := &service.User{
		ID:          42,
		Role:        service.RoleUser,
		Status:      service.StatusActive,
		Balance:     0,
		Concurrency: 1,
	}
	return &service.APIKey{
		ID:     7,
		UserID: user.ID,
		Key:    "available-credit-key",
		Status: service.StatusActive,
		User:   user,
	}
}

func newAvailableCreditAuthTestService(apiKey *service.APIKey, checker *availableCreditEligibilityCheckerStub) *service.APIKeyService {
	apiKeyService := service.NewAPIKeyService(fakeAPIKeyRepo{
		getByKey: func(_ context.Context, key string) (*service.APIKey, error) {
			if key != apiKey.Key {
				return nil, service.ErrAPIKeyNotFound
			}
			keyCopy := *apiKey
			userCopy := *apiKey.User
			keyCopy.User = &userCopy
			return &keyCopy, nil
		},
	}, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeStandard})
	if checker != nil {
		apiKeyService.SetAvailableCreditEligibilityChecker(checker)
	}
	return apiKeyService
}

func TestAPIKeyAuthAvailableCreditAllowsTemporaryCreditWhenPermanentBalanceIsZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	apiKey := newAvailableCreditAuthTestAPIKey()
	checker := &availableCreditEligibilityCheckerStub{}
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(newAvailableCreditAuthTestService(apiKey, checker), nil, &config.Config{RunMode: config.RunModeStandard})))
	router.GET("/t", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/t", nil)
	request.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, 1, checker.calls)
	require.Equal(t, apiKey.UserID, checker.userID)
}

func TestAPIKeyAuthAvailableCreditRejectsExhaustedCredit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	apiKey := newAvailableCreditAuthTestAPIKey()
	checker := &availableCreditEligibilityCheckerStub{err: service.ErrInsufficientBalance}
	router := gin.New()
	downstreamCalls := 0
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(newAvailableCreditAuthTestService(apiKey, checker), nil, &config.Config{RunMode: config.RunModeStandard})))
	router.GET("/t", func(c *gin.Context) {
		downstreamCalls++
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/t", nil)
	request.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Equal(t, 0, downstreamCalls)
	require.Equal(t, 1, checker.calls)
}

func TestAPIKeyAuthAvailableCreditFallsBackToPermanentBalanceWithoutChecker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	apiKey := newAvailableCreditAuthTestAPIKey()
	router := gin.New()
	downstreamCalls := 0
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(newAvailableCreditAuthTestService(apiKey, nil), nil, &config.Config{RunMode: config.RunModeStandard})))
	router.GET("/t", func(c *gin.Context) {
		downstreamCalls++
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/t", nil)
	request.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Equal(t, 0, downstreamCalls)
}

func TestAPIKeyAuthGoogleAvailableCreditAllowsTemporaryCreditWhenPermanentBalanceIsZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	apiKey := newAvailableCreditAuthTestAPIKey()
	checker := &availableCreditEligibilityCheckerStub{}
	router := gin.New()
	router.Use(APIKeyAuthWithSubscriptionGoogle(newAvailableCreditAuthTestService(apiKey, checker), nil, &config.Config{RunMode: config.RunModeStandard}))
	router.GET("/t", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/t", nil)
	request.Header.Set("x-goog-api-key", apiKey.Key)
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, 1, checker.calls)
	require.Equal(t, apiKey.UserID, checker.userID)
}

func TestAPIKeyAuthGoogleAvailableCreditRejectsExhaustedCredit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	apiKey := newAvailableCreditAuthTestAPIKey()
	checker := &availableCreditEligibilityCheckerStub{err: service.ErrInsufficientBalance}
	router := gin.New()
	downstreamCalls := 0
	router.Use(APIKeyAuthWithSubscriptionGoogle(newAvailableCreditAuthTestService(apiKey, checker), nil, &config.Config{RunMode: config.RunModeStandard}))
	router.GET("/t", func(c *gin.Context) {
		downstreamCalls++
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/t", nil)
	request.Header.Set("x-goog-api-key", apiKey.Key)
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Equal(t, 0, downstreamCalls)
	require.Equal(t, 1, checker.calls)
}

func TestAPIKeyAuthGoogleAvailableCreditFallsBackToPermanentBalanceWithoutChecker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	apiKey := newAvailableCreditAuthTestAPIKey()
	router := gin.New()
	downstreamCalls := 0
	router.Use(APIKeyAuthWithSubscriptionGoogle(newAvailableCreditAuthTestService(apiKey, nil), nil, &config.Config{RunMode: config.RunModeStandard}))
	router.GET("/t", func(c *gin.Context) {
		downstreamCalls++
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/t", nil)
	request.Header.Set("x-goog-api-key", apiKey.Key)
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Equal(t, 0, downstreamCalls)
}

func TestAPIKeyAuthGoogleAvailableCreditFallsBackToPositivePermanentBalanceWithoutChecker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	apiKey := newAvailableCreditAuthTestAPIKey()
	apiKey.User.Balance = 1
	router := gin.New()
	router.Use(APIKeyAuthWithSubscriptionGoogle(newAvailableCreditAuthTestService(apiKey, nil), nil, &config.Config{RunMode: config.RunModeStandard}))
	router.GET("/t", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/t", nil)
	request.Header.Set("x-goog-api-key", apiKey.Key)
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
}
