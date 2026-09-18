package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type OIDCSigningKey struct{ ent.Schema }

func (OIDCSigningKey) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_signing_keys"}}
}
func (OIDCSigningKey) Fields() []ent.Field {
	return []ent.Field{
		field.String("kid").MaxLen(128), field.String("alg").Default("RS256"), field.String("public_jwk").SchemaType(map[string]string{dialect.Postgres: "text"}), field.String("private_key_ciphertext").SchemaType(map[string]string{dialect.Postgres: "text"}), field.String("fingerprint").MaxLen(64), field.String("status").MaxLen(32), field.Time("not_before").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("not_after").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("activated_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("retired_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("revoked_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Int64("created_by").Optional().Nillable(), field.Int64("last_changed_by").Optional().Nillable(), field.String("last_change_reason").Default("").SchemaType(map[string]string{dialect.Postgres: "text"}), field.String("key_encryption_version").Default("v1"),
	}
}
func (OIDCSigningKey) Indexes() []ent.Index {
	return []ent.Index{index.Fields("kid").Unique(), index.Fields("fingerprint").Unique(), index.Fields("status")}
}
