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

// PaymentPurchaseCounter stores per-user product counts for one daily or total period.
type PaymentPurchaseCounter struct {
	ent.Schema
}

func (PaymentPurchaseCounter) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "payment_purchase_counters"}}
}

func (PaymentPurchaseCounter) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id").Positive(),
		field.String("product_type").MaxLen(20),
		field.Int64("product_id").Positive(),
		field.String("period_type").MaxLen(10),
		field.Time("period_start").SchemaType(map[string]string{dialect.Postgres: "date"}),
		field.Int("reserved_count").NonNegative().Default(0),
		field.Int("consumed_count").NonNegative().Default(0),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PaymentPurchaseCounter) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "product_type", "product_id", "period_type", "period_start").Unique(),
		index.Fields("user_id", "period_type", "period_start"),
	}
}
