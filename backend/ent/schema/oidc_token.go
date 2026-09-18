package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type OIDCAuthorizationCode struct{ ent.Schema }

func (OIDCAuthorizationCode) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_authorization_codes"}}
}
func (OIDCAuthorizationCode) Fields() []ent.Field {
	return []ent.Field{
		field.String("code_digest").MaxLen(64), field.Int64("transaction_id"), field.Int64("client_pk"), field.Int64("user_id"), field.Int64("consent_id"), field.Int64("browser_session_id").Optional().Nillable(), field.String("redirect_uri").SchemaType(map[string]string{dialect.Postgres: "text"}), field.String("scope_snapshot").SchemaType(map[string]string{dialect.Postgres: "text"}), field.String("nonce_ciphertext").SchemaType(map[string]string{dialect.Postgres: "text"}), field.String("nonce_fingerprint").MaxLen(64), field.String("code_challenge").MaxLen(128), field.String("code_challenge_method").Default("S256"), field.Time("auth_time").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.String("status").MaxLen(32), field.Time("issued_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("consumed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}
func (OIDCAuthorizationCode) Indexes() []ent.Index {
	return []ent.Index{index.Fields("code_digest").Unique(), index.Fields("transaction_id").Unique(), index.Fields("expires_at")}
}

type OIDCAccessToken struct{ ent.Schema }

func (OIDCAccessToken) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_access_tokens"}}
}
func (OIDCAccessToken) Fields() []ent.Field {
	return []ent.Field{field.String("token_digest").MaxLen(64), field.Int64("client_pk"), field.Int64("user_id"), field.Int64("consent_id"), field.String("purpose").Default("userinfo"), field.String("scope_snapshot").SchemaType(map[string]string{dialect.Postgres: "text"}), field.Time("issued_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Int64("revoked_by").Optional().Nillable(), field.Time("revoked_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.String("revoke_reason").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"})}
}
func (OIDCAccessToken) Indexes() []ent.Index {
	return []ent.Index{index.Fields("token_digest").Unique(), index.Fields("client_pk", "user_id"), index.Fields("expires_at")}
}

type OIDCRefreshTokenFamily struct{ ent.Schema }

func (OIDCRefreshTokenFamily) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_refresh_token_families"}}
}
func (OIDCRefreshTokenFamily) Fields() []ent.Field {
	return []ent.Field{field.String("family_id").MaxLen(64), field.Int64("client_pk"), field.Int64("user_id"), field.Int64("consent_id"), field.String("scope_snapshot").SchemaType(map[string]string{dialect.Postgres: "text"}), field.String("status").MaxLen(32), field.Time("created_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("last_used_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("idle_expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("absolute_expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("revoked_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Int64("revoked_by").Optional().Nillable(), field.String("revoke_reason").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}), field.Time("replay_detected_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.String("replay_fingerprint").Optional().Nillable().MaxLen(64), field.Int64("version").Default(1)}
}
func (OIDCRefreshTokenFamily) Indexes() []ent.Index {
	return []ent.Index{index.Fields("family_id").Unique(), index.Fields("client_pk", "user_id"), index.Fields("status")}
}

type OIDCRefreshToken struct{ ent.Schema }

func (OIDCRefreshToken) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_refresh_tokens"}}
}
func (OIDCRefreshToken) Fields() []ent.Field {
	return []ent.Field{field.String("token_digest").MaxLen(64), field.String("family_id").MaxLen(64), field.Int64("parent_token_id").Optional().Nillable(), field.String("status").MaxLen(32), field.Time("issued_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("rotated_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("used_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("replayed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("revoked_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Int64("child_token_id").Optional().Nillable()}
}
func (OIDCRefreshToken) Indexes() []ent.Index {
	return []ent.Index{index.Fields("token_digest").Unique(), index.Fields("family_id", "status"), index.Fields("expires_at")}
}

type OIDCConsent struct{ ent.Schema }

func (OIDCConsent) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_consents"}}
}
func (OIDCConsent) Fields() []ent.Field {
	return []ent.Field{field.Int64("user_id"), field.Int64("client_pk"), field.String("scope_snapshot").SchemaType(map[string]string{dialect.Postgres: "text"}), field.String("scope_set_hash").MaxLen(64), field.Int64("policy_version"), field.String("source").MaxLen(32), field.String("status").MaxLen(32), field.Int64("approved_by").Optional().Nillable(), field.Time("approved_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Int64("revoked_by").Optional().Nillable(), field.Time("revoked_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.String("revoke_reason").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"})}
}
func (OIDCConsent) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id", "client_pk", "policy_version", "scope_set_hash").Unique().Annotations(entsql.IndexWhere("status = 'active'")), index.Fields("status")}
}
