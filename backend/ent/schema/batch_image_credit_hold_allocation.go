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

// BatchImageCreditHoldAllocation records the portion reserved from one
// temporary-credit grant for a batch-image hold.
type BatchImageCreditHoldAllocation struct {
	ent.Schema
}

func (BatchImageCreditHoldAllocation) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{
		Table: "batch_image_credit_hold_allocations",
		Checks: map[string]string{
			"batch_image_credit_hold_allocations_amounts_check":         "reserved_amount > 0 AND captured_amount >= 0 AND refunded_amount >= 0 AND expired_amount >= 0 AND captured_amount + refunded_amount + expired_amount <= reserved_amount",
			"batch_image_credit_hold_allocations_settlement_check":      "((captured_amount = 0 AND refunded_amount = 0 AND expired_amount = 0) OR captured_amount + refunded_amount + expired_amount = reserved_amount)",
			"batch_image_credit_hold_allocations_expiry_snapshot_check": "grant_expires_at > created_at",
			"batch_image_credit_hold_allocations_timestamps_check":      "updated_at >= created_at",
		},
	}}
}

func (BatchImageCreditHoldAllocation) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("hold_id").Immutable(),
		// The migration uses (hold_id, batch_id) as a composite FK so an
		// allocation cannot be attached to a different batch accidentally.
		field.String("batch_id").MaxLen(64).Immutable(),
		field.Int64("grant_id").Immutable(),
		field.Time("grant_expires_at").
			Immutable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Float("reserved_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Immutable(),
		field.Float("captured_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Default(0),
		field.Float("refunded_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Default(0),
		field.Float("expired_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Default(0),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (BatchImageCreditHoldAllocation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("hold", BatchImageCreditHold.Type).
			Ref("allocations").
			Field("hold_id").
			Required().
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.From("grant", TemporaryCreditGrant.Type).
			Ref("batch_image_credit_hold_allocations").
			Field("grant_id").
			Required().
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Restrict)),
	}
}

func (BatchImageCreditHoldAllocation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("hold_id", "grant_id").Unique(),
		index.Fields("batch_id"),
		index.Fields("grant_id", "created_at"),
	}
}
