package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TemporaryCreditConsumption records one allocation from an expiring credit batch.
type TemporaryCreditConsumption struct {
	ent.Schema
}

func (TemporaryCreditConsumption) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "temporary_credit_consumptions"}}
}

func (TemporaryCreditConsumption) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("grant_id"),
		field.Int64("usage_log_id").Optional().Nillable(),
		field.String("request_id").MaxLen(255).Optional().Nillable().Immutable(),
		field.Float("amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (TemporaryCreditConsumption) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("grant", TemporaryCreditGrant.Type).
			Ref("consumptions").
			Field("grant_id").
			Required().
			Unique().
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.From("usage_log", UsageLog.Type).
			Ref("temporary_credit_consumptions").
			Field("usage_log_id").
			Unique().
			Annotations(entsql.OnDelete(entsql.Restrict)),
	}
}

func (TemporaryCreditConsumption) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("grant_id", "usage_log_id").
			Unique().
			Annotations(entsql.IndexWhere("usage_log_id IS NOT NULL")),
		index.Fields("grant_id", "request_id").
			Unique().
			Annotations(entsql.IndexWhere("request_id IS NOT NULL")),
		index.Fields("grant_id", "created_at"),
	}
}
