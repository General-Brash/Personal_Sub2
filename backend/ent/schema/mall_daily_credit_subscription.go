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

// MallDailyCreditSubscription tracks the last pre-created day for one plan.
type MallDailyCreditSubscription struct{ ent.Schema }

func (MallDailyCreditSubscription) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "mall_daily_credit_subscriptions"}}
}

func (MallDailyCreditSubscription) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.Int64("plan_id"),
		field.Time("starts_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_grant_date").SchemaType(map[string]string{dialect.Postgres: "date"}),
		field.Time("expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("status").MaxLen(20).Default("active"),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (MallDailyCreditSubscription) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id", "plan_id").Unique(), index.Fields("user_id", "expires_at")}
}
