package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyTrafficRepositorySavesAbsoluteSnapshots(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	source := "00000000-0000-4000-8000-000000000001"
	mock.ExpectExec(`(?s)INSERT INTO proxy_traffic_daily.*ON CONFLICT.*GREATEST\(proxy_traffic_daily.upload_bytes, EXCLUDED.upload_bytes\).*GREATEST\(proxy_traffic_daily.download_bytes, EXCLUDED.download_bytes\)`).
		WithArgs(int64(7), "2026-11-13", source, int64(12), int64(34)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	repo := NewProxyTrafficRepository(db)
	require.NoError(t, repo.SaveSnapshots(context.Background(), source, []service.ProxyTrafficSnapshot{{ProxyID: 7, UsageDate: "2026-11-13", UploadBytes: 12, DownloadBytes: 34}}))
	require.NoError(t, repo.SaveSnapshots(context.Background(), source, nil))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyTrafficRepositoryAggregatesBatchAndPropagatesErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewProxyTrafficRepository(db)
	query := `(?s)SELECT proxy_id,.*FILTER \(WHERE usage_date = \$2::date\).*proxy_id = ANY\(\$1\).*usage_date BETWEEN \$3::date AND \$2::date.*GROUP BY proxy_id`
	mock.ExpectQuery(query).WithArgs("{7,8}", "2026-11-13", "2026-10-15").
		WillReturnRows(sqlmock.NewRows([]string{"proxy_id", "today", "days30"}).AddRow(7, 100, 300))
	usage, err := repo.GetUsage(context.Background(), []int64{7, 8}, "2026-11-13", "2026-10-15")
	require.NoError(t, err)
	require.Equal(t, service.ProxyTrafficUsage{TodayBytes: 100, Days30Bytes: 300}, usage[7])
	require.Zero(t, usage[8])
	mock.ExpectQuery(query).WillReturnError(errors.New("db unavailable"))
	_, err = repo.GetUsage(context.Background(), []int64{7}, "2026-11-13", "2026-10-15")
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyTrafficRepositoryResolvesActualAccountNode(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &proxyTrafficRepository{db: db}
	query := `(?s)SELECT p.id, p.protocol, p.host, p.port,.*FROM accounts a JOIN proxies p ON p.id = a.proxy_id.*a.id = \$1.*a.deleted_at IS NULL.*p.deleted_at IS NULL`
	mock.ExpectQuery(query).WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "protocol", "host", "port", "username", "password"}).AddRow(8, "http", "same.example", 8080, "", ""))
	proxy, err := repo.ResolveAccountProxy(context.Background(), 11)
	require.NoError(t, err)
	require.Equal(t, int64(8), proxy.ID)
	require.Equal(t, "http://same.example:8080", proxy.URL())
	mock.ExpectQuery(query).WithArgs(int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "protocol", "host", "port", "username", "password"}))
	proxy, err = repo.ResolveAccountProxy(context.Background(), 12)
	require.NoError(t, err)
	require.Nil(t, proxy)
	require.NoError(t, mock.ExpectationsWereMet())
}
