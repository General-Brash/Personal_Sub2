package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/setting"
)

// CompareAndSetMultiple locks the complete policy bundle, including base rewards,
// and updates it atomically. It works across processes, unlike an in-memory lock.
func (r *settingRepository) CompareAndSetMultiple(ctx context.Context, expected, updates map[string]string) (ok bool, err error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			err = errors.Join(err, fmt.Errorf("rollback setting policy transaction: %w", rollbackErr))
		}
	}()
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := tx.Setting.Create().SetKey(key).SetValue("").OnConflictColumns(setting.FieldKey).DoNothing().Exec(ctx); err != nil {
			return false, err
		}
		row, err := tx.Setting.Query().Where(setting.KeyEQ(key)).ForUpdate().Only(ctx)
		if err != nil {
			return false, err
		}
		if row.Value != expected[key] {
			return false, nil
		}
	}
	keys = keys[:0]
	for key := range updates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := tx.Setting.Create().SetKey(key).SetValue(updates[key]).SetUpdatedAt(time.Now()).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	committed = true
	return true, nil
}
