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

// PaymentRefundAttempt persists one provider refund attempt and its local deduction hold.
type PaymentRefundAttempt struct {
	ent.Schema
}

func (PaymentRefundAttempt) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "payment_refund_attempts"}}
}

func (PaymentRefundAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.String("attempt_id").MaxLen(64).Unique(),
		field.Int64("order_id").Positive(),
		field.Float("refund_amount").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Float("gateway_amount").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.String("reason").Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("original_order_status").MaxLen(30),
		field.String("source").MaxLen(30).Default("admin"),
		field.Bool("deduct_balance"),
		field.Bool("force"),
		field.String("deduction_type").MaxLen(20).Default("none"),
		field.Float("held_balance_amount").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).Default(0),
		field.Int64("subscription_id").Optional().Nillable(),
		field.Int("subscription_days").Default(0),
		field.Time("subscription_original_expires_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("subscription_original_status").Optional().Nillable().MaxLen(20),
		field.Bool("subscription_revoked").Default(false),
		field.String("deduction_state").MaxLen(20).Default("none"),
		field.String("provider_refund_id").Optional().Nillable().MaxLen(128),
		field.String("provider_state").MaxLen(20).Default("calling"),
		field.String("provider_result").Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Bool("manual_review").Default(false),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PaymentRefundAttempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("order_id", "created_at"),
		index.Fields("order_id", "provider_state"),
		index.Fields("manual_review", "provider_state"),
	}
}
