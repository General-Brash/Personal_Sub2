package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type proxyTrafficRepository struct {
	db *sql.DB
}

func NewProxyTrafficRepository(db *sql.DB) service.ProxyTrafficRepository {
	return &proxyTrafficRepository{db: db}
}

func (r *proxyTrafficRepository) ResolveAccountProxy(ctx context.Context, accountID int64) (*service.Proxy, error) {
	var proxy service.Proxy
	err := r.db.QueryRowContext(ctx, `
		SELECT p.id, p.protocol, p.host, p.port, COALESCE(p.username, ''), COALESCE(p.password, '')
		FROM accounts a JOIN proxies p ON p.id = a.proxy_id
		WHERE a.id = $1 AND a.deleted_at IS NULL AND p.deleted_at IS NULL
	`, accountID).Scan(&proxy.ID, &proxy.Protocol, &proxy.Host, &proxy.Port, &proxy.Username, &proxy.Password)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &proxy, err
}

func (r *proxyTrafficRepository) SaveSnapshots(ctx context.Context, sourceID string, snapshots []service.ProxyTrafficSnapshot) error {
	const batchSize = 500
	for start := 0; start < len(snapshots); start += batchSize {
		end := min(start+batchSize, len(snapshots))
		args := make([]any, 0, (end-start)*5)
		values := make([]string, 0, end-start)
		for _, snapshot := range snapshots[start:end] {
			offset := len(args)
			values = append(values, fmt.Sprintf("($%d, $%d::date, $%d::uuid, $%d, $%d)", offset+1, offset+2, offset+3, offset+4, offset+5))
			args = append(args, snapshot.ProxyID, snapshot.UsageDate, sourceID, snapshot.UploadBytes, snapshot.DownloadBytes)
		}
		query := `INSERT INTO proxy_traffic_daily (proxy_id, usage_date, source_id, upload_bytes, download_bytes)
			VALUES ` + strings.Join(values, ",") + `
			ON CONFLICT (proxy_id, usage_date, source_id) DO UPDATE SET
				upload_bytes = GREATEST(proxy_traffic_daily.upload_bytes, EXCLUDED.upload_bytes),
				download_bytes = GREATEST(proxy_traffic_daily.download_bytes, EXCLUDED.download_bytes),
				updated_at = NOW()`
		if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return nil
}

func (r *proxyTrafficRepository) GetUsage(ctx context.Context, proxyIDs []int64, today, since string) (map[int64]service.ProxyTrafficUsage, error) {
	result := make(map[int64]service.ProxyTrafficUsage, len(proxyIDs))
	if len(proxyIDs) == 0 {
		return result, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT proxy_id,
			COALESCE(SUM(upload_bytes + download_bytes) FILTER (WHERE usage_date = $2::date), 0)::bigint,
			COALESCE(SUM(upload_bytes + download_bytes), 0)::bigint
		FROM proxy_traffic_daily
		WHERE proxy_id = ANY($1) AND usage_date BETWEEN $3::date AND $2::date
		GROUP BY proxy_id
	`, pq.Array(proxyIDs), today, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var proxyID int64
		var usage service.ProxyTrafficUsage
		if err := rows.Scan(&proxyID, &usage.TodayBytes, &usage.Days30Bytes); err != nil {
			return nil, err
		}
		result[proxyID] = usage
	}
	return result, rows.Err()
}
