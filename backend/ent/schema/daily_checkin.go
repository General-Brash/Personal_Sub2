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

// DailyCheckin stores an immutable snapshot of one user's Beijing business-day reward.
type DailyCheckin struct {
	ent.Schema
}

func (DailyCheckin) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "daily_checkins"}}
}

func (DailyCheckin) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (DailyCheckin) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.Time("checkin_date").
			SchemaType(map[string]string{dialect.Postgres: "date"}),
		field.Int("streak_day").Positive(),
		field.Int("reward_day").Positive(),
		field.Float("reward_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
	}
}

func (DailyCheckin) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("daily_checkins").
			Field("user_id").
			Required().
			Unique().
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.To("temporary_credit_grant", TemporaryCreditGrant.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.Restrict)),
	}
}

func (DailyCheckin) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "checkin_date").
			StorageKey("dailycheckin_user_id_checkin_date_desc").
			Annotations(entsql.DescColumns("checkin_date")),
		index.Fields("user_id", "checkin_date").Unique(),
	}
}
