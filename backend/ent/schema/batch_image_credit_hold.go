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

// BatchImageCreditHold records the permanent and temporary portions reserved
// for one batch-image job.
type BatchImageCreditHold struct {
	ent.Schema
}

func (BatchImageCreditHold) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{
		Table: "batch_image_credit_holds",
		Checks: map[string]string{
			"batch_image_credit_holds_amounts_nonnegative_check":   "hold_amount >= 0 AND temporary_reserved_amount >= 0 AND permanent_reserved_amount >= 0 AND captured_amount >= 0 AND temporary_captured_amount >= 0 AND permanent_captured_amount >= 0 AND expired_unrestored_amount >= 0",
			"batch_image_credit_holds_reserved_conservation_check": "temporary_reserved_amount + permanent_reserved_amount = hold_amount",
			"batch_image_credit_holds_captured_conservation_check": "temporary_captured_amount + permanent_captured_amount = captured_amount AND temporary_captured_amount <= temporary_reserved_amount AND permanent_captured_amount <= permanent_reserved_amount AND captured_amount <= hold_amount",
			"batch_image_credit_holds_expired_bound_check":         "expired_unrestored_amount <= temporary_reserved_amount - temporary_captured_amount",
			"batch_image_credit_holds_fingerprint_check":           "(trim(reserve_fingerprint) <> '' AND (terminal_fingerprint IS NULL OR trim(terminal_fingerprint) <> ''))",
			"batch_image_credit_holds_terminal_state_check":        "((status = 'reserved' AND captured_amount = 0 AND temporary_captured_amount = 0 AND permanent_captured_amount = 0 AND expired_unrestored_amount = 0 AND terminal_fingerprint IS NULL AND settled_at IS NULL) OR (status = 'captured' AND terminal_fingerprint IS NOT NULL AND settled_at IS NOT NULL AND settled_at >= reserved_at) OR (status = 'released' AND captured_amount = 0 AND temporary_captured_amount = 0 AND permanent_captured_amount = 0 AND terminal_fingerprint IS NOT NULL AND settled_at IS NOT NULL AND settled_at >= reserved_at))",
		},
	}}
}

func (BatchImageCreditHold) Fields() []ent.Field {
	return []ent.Field{
		// The SQL migration enforces this business-key FK against
		// batch_image_jobs(batch_id); Ent edges target numeric entity IDs only.
		field.String("batch_id").MaxLen(64).Immutable(),
		field.Int64("user_id").Immutable(),
		field.Int64("api_key_id").Immutable(),
		field.Int64("group_id").Optional().Nillable().Immutable(),
		field.Enum("status").Values("reserved", "captured", "released").Default("reserved"),
		field.Float("hold_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Immutable(),
		field.Float("temporary_reserved_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Default(0).
			Immutable(),
		field.Float("permanent_reserved_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Default(0).
			Immutable(),
		field.Float("captured_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Default(0),
		field.Float("temporary_captured_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Default(0),
		field.Float("permanent_captured_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Default(0),
		field.Float("expired_unrestored_amount").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}).
			Default(0),
		field.String("reserve_fingerprint").MaxLen(128).Immutable(),
		field.String("terminal_fingerprint").MaxLen(128).Optional().Nillable(),
		field.Time("reserved_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("settled_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (BatchImageCreditHold) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("batch_image_credit_holds").
			Field("user_id").
			Required().
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.From("api_key", APIKey.Type).
			Ref("batch_image_credit_holds").
			Field("api_key_id").
			Required().
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Restrict)),
		edge.From("group", Group.Type).
			Ref("batch_image_credit_holds").
			Field("group_id").
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("allocations", BatchImageCreditHoldAllocation.Type).
			Annotations(entsql.OnDelete(entsql.Restrict)),
	}
}

func (BatchImageCreditHold) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("batch_id").Unique(),
		index.Fields("user_id", "status", "reserved_at"),
		index.Fields("api_key_id", "reserved_at"),
		index.Fields("group_id", "reserved_at").
			Annotations(entsql.IndexWhere("group_id IS NOT NULL")),
		index.Fields("status", "updated_at"),
	}
}
