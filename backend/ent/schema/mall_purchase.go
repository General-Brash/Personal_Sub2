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

// MallPurchase records one completed internal-credit settlement.
type MallPurchase struct{ ent.Schema }

func (MallPurchase) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "mall_purchases"}}
}

func (MallPurchase) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.String("product_type").MaxLen(20),
		field.Int64("product_id"),
		field.String("product_name").MaxLen(100).Default(""),
		field.Int64("idempotency_record_id").Unique(),
		field.String("payment_credit_type").MaxLen(20),
		field.Float("price").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.String("credited_type").MaxLen(20).Optional().Nillable(),
		field.Float("credited_amount").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).Optional().Nillable(),
		field.String("benefit_type").MaxLen(40).Optional().Nillable(),
		field.Int("benefit_days").Optional().Nillable(),
		field.Float("daily_temporary_credit_amount").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).Optional().Nillable(),
		field.Time("subscription_expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).Optional().Nillable(),
		field.Float("permanent_balance_before").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).Optional().Nillable(),
		field.Float("permanent_balance_after").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).Optional().Nillable(),
		field.Float("temporary_balance_before").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).Optional().Nillable(),
		field.Float("temporary_balance_after").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).Optional().Nillable(),
		field.String("status").MaxLen(20).Default("completed"),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (MallPurchase) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id", "created_at"), index.Fields("product_type", "product_id", "created_at")}
}
