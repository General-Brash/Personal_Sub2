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

// PaymentPurchaseReservation ties one product purchase slot to one payment order.
type PaymentPurchaseReservation struct {
	ent.Schema
}

func (PaymentPurchaseReservation) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "payment_purchase_reservations"}}
}

func (PaymentPurchaseReservation) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("order_id").Positive().Unique(),
		field.Int64("user_id").Positive(),
		field.String("product_type").MaxLen(20),
		field.Int64("product_id").Positive(),
		field.Time("daily_period_start").SchemaType(map[string]string{dialect.Postgres: "date"}),
		field.String("status").MaxLen(20).Default("reserved"),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PaymentPurchaseReservation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "product_type", "product_id", "status"),
	}
}
