package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

var accountTodayStatsBatchCache = newSnapshotCache(30 * time.Second)

func buildAccountTodayStatsBatchCacheKey(accountIDs []int64) string {
	var b strings.Builder
	b.Grow(32 + len(accountIDs)*6)
	_, _ = b.WriteString("accounts_today_stats:")
	_, _ = b.WriteString(timezone.Today().Format("2006-01-02"))
	_ = b.WriteByte(':')
	for i, id := range accountIDs {
		if i > 0 {
			_ = b.WriteByte(',')
		}
		_, _ = b.WriteString(strconv.FormatInt(id, 10))
	}
	return b.String()
}
