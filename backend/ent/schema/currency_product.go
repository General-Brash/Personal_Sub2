package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CurrencyProduct is a fixed internal-credit product sold through the mall.
// Legacy external orders keep immutable snapshots, so products can still be
// removed safely without changing historical fulfillment.
type CurrencyProduct struct {
	ent.Schema
}

func (CurrencyProduct) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "currency_products"}}
}

func (CurrencyProduct) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			MaxLen(100).
			NotEmpty(),
		field.String("description").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default(""),
		field.Float("payment_price").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.String("payment_credit_type").
			MaxLen(20).
			Default("permanent"),
		field.String("credited_type").
			MaxLen(20).
			Default("permanent"),
		field.Float("credited_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Float("credited_permanent_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Int("sort_order").Default(0),
		field.Bool("is_active").Default(true),
		field.Bool("for_sale").Default(true),
		field.Int("daily_purchase_limit").NonNegative().Default(0),
		field.Int("total_purchase_limit").NonNegative().Default(0),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CurrencyProduct) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("is_active", "for_sale", "sort_order"),
		index.Fields("sort_order"),
	}
}
