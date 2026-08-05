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

// PaymentPurchaseLimitEvent records one purchase-limit lifecycle event.
// Rolling policies query these events by occurred_at; released events never count.
type PaymentPurchaseLimitEvent struct{ ent.Schema }

func (PaymentPurchaseLimitEvent) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "payment_purchase_limit_events"}}
}

func (PaymentPurchaseLimitEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id").Positive(),
		field.String("product_type").MaxLen(20),
		field.Int64("product_id").Positive(),
		field.String("source_type").MaxLen(20),
		field.Int64("source_id").Positive(),
		field.String("period_type").MaxLen(10).Default("daily"),
		field.Time("period_start").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "date"}),
		field.String("status").MaxLen(20).Default("reserved"),
		field.Time("occurred_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PaymentPurchaseLimitEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source_type", "source_id").Unique(),
		index.Fields("user_id", "product_type", "product_id", "status", "occurred_at"),
	}
}
