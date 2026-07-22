package schema

import (
	"testing"

	"entgo.io/ent/entc/load"
	"github.com/stretchr/testify/require"
)

func TestTemporaryCreditGrantSchemaIncludesMallSources(t *testing.T) {
	spec, err := (&load.Config{Path: "."}).Load()
	require.NoError(t, err)

	var sourceEnums []string
	for _, current := range spec.Schemas {
		if current.Name != "TemporaryCreditGrant" {
			continue
		}
		for _, field := range current.Fields {
			if field.Name != "source" {
				continue
			}
			for _, enum := range field.Enums {
				sourceEnums = append(sourceEnums, enum.V)
			}
		}
	}

	require.ElementsMatch(t, []string{
		"checkin", "admin_grant", "bank_advance", "bank_exchange", "mall_product", "subscription",
	}, sourceEnums)
}
