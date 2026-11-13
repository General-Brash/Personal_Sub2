package admin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type proxyTrafficRepoStub struct {
	ids []int64
	err error
}

func (*proxyTrafficRepoStub) SaveSnapshots(context.Context, string, []service.ProxyTrafficSnapshot) error {
	return nil
}

func (r *proxyTrafficRepoStub) GetUsage(_ context.Context, ids []int64, _, _ string) (map[int64]service.ProxyTrafficUsage, error) {
	r.ids = append([]int64(nil), ids...)
	return map[int64]service.ProxyTrafficUsage{7: {TodayBytes: 123, Days30Bytes: 456}}, r.err
}

func TestProxyTrafficHandlerBatchFieldsAndZeroUsage(t *testing.T) {
	repo := &proxyTrafficRepoStub{}
	h := &ProxyHandler{traffic: service.NewProxyTrafficService(repo, nil)}
	rows := make([]dto.AdminProxyWithAccountCount, 2)
	rows[0].ID, rows[1].ID = 7, 8
	h.attachTraffic(context.Background(), rows)
	require.Equal(t, []int64{7, 8}, repo.ids)
	require.Equal(t, int64(123), *rows[0].TrafficTodayBytes)
	require.Equal(t, int64(456), *rows[0].Traffic30DaysBytes)
	require.NotNil(t, rows[1].TrafficTodayBytes)
	require.Zero(t, *rows[1].TrafficTodayBytes)
	encoded, err := json.Marshal(rows[1])
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"traffic_today_bytes":0`)
	require.Contains(t, string(encoded), `"traffic_30d_bytes":0`)
}

func TestProxyTrafficHandlerUnavailableStatsAreNotZero(t *testing.T) {
	repo := &proxyTrafficRepoStub{err: errors.New("unavailable")}
	h := &ProxyHandler{traffic: service.NewProxyTrafficService(repo, nil)}
	rows := make([]dto.AdminProxyWithAccountCount, 1)
	rows[0].ID = 7
	h.attachTraffic(context.Background(), rows)
	require.Nil(t, rows[0].TrafficTodayBytes)
	require.Nil(t, rows[0].Traffic30DaysBytes)
}
