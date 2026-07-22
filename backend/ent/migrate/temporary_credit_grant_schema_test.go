package migrate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTemporaryCreditGrantMigrateSchemaIncludesMallSources(t *testing.T) {
	var enums []string
	for _, column := range TemporaryCreditGrantsColumns {
		if column.Name == "source" {
			enums = column.Enums
			break
		}
	}

	require.ElementsMatch(t, []string{
		"checkin", "admin_grant", "bank_advance", "bank_exchange", "mall_product", "subscription",
	}, enums)
}
