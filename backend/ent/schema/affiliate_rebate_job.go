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

// AffiliateRebateJob is the durable outbox entry for a positive balance credit.
type AffiliateRebateJob struct {
	ent.Schema
}

func (AffiliateRebateJob) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{
		Table: "affiliate_rebate_jobs",
		Checks: map[string]string{
			"affiliate_rebate_jobs_source_kind_check": "source_kind IN ('redeem', 'admin_recharge')",
			"affiliate_rebate_jobs_status_check":      "status IN ('pending', 'processing', 'succeeded', 'skipped', 'failed')",
			"affiliate_rebate_jobs_amount_check":      "base_amount > 0",
			"affiliate_rebate_jobs_attempts_check":    "attempts >= 0",
		},
	}}
}

func (AffiliateRebateJob) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("invitee_user_id").Immutable(),
		field.Int64("source_redeem_code_id").Immutable(),
		field.Enum("source_kind").Values("redeem", "admin_recharge").Immutable(),
		field.Float("base_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Positive().
			Immutable(),
		field.Enum("status").Values("pending", "processing", "succeeded", "skipped", "failed").Default("pending"),
		field.Int("attempts").NonNegative().Default(0),
		field.Time("next_retry_at").Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("last_error").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("last_error_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("processing_started_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("succeeded_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("skipped_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("failed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (AffiliateRebateJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source_redeem_code_id").Unique(),
		index.Fields("next_retry_at", "id").Annotations(entsql.IndexWhere("status IN ('pending', 'failed')")),
		index.Fields("processing_started_at", "id").Annotations(entsql.IndexWhere("status = 'processing'")),
		index.Fields("invitee_user_id", "created_at"),
	}
}
