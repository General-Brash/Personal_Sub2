package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TemporaryCreditGrant is a non-transferable, expiring temporary-credit batch.
type TemporaryCreditGrant struct {
	ent.Schema
}

func (TemporaryCreditGrant) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "temporary_credit_grants"}}
}

func (TemporaryCreditGrant) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (TemporaryCreditGrant) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.Enum("source").Values("checkin", "admin_grant"),
		field.Int64("checkin_id").Optional().Nillable(),
		field.Float("amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Float("remaining_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Time("expires_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("notes").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default(""),
		field.Int64("granted_by").Optional().Nillable(),
	}
}

func (TemporaryCreditGrant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("temporary_credit_grants").
			Field("user_id").
			Required().
			Unique().
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.From("checkin", DailyCheckin.Type).
			Ref("temporary_credit_grant").
			Field("checkin_id").
			Unique().
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.From("granted_by_user", User.Type).
			Ref("granted_temporary_credit_grants").
			Field("granted_by").
			Unique().
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("consumptions", TemporaryCreditConsumption.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
	}
}

func (TemporaryCreditGrant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("checkin_id").
			Unique().
			Annotations(entsql.IndexWhere("checkin_id IS NOT NULL")),
		index.Fields("user_id", "expires_at", "id").
			Annotations(entsql.IndexWhere("remaining_amount > 0")),
	}
}
