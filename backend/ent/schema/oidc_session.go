package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type OIDCSubject struct{ ent.Schema }

func (OIDCSubject) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_subjects"}}
}
func (OIDCSubject) Fields() []ent.Field {
	return []ent.Field{field.Int64("user_id"), field.String("subject").MaxLen(255).NotEmpty(), field.Time("created_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"})}
}
func (OIDCSubject) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id").Unique(), index.Fields("subject").Unique()}
}

type OIDCBrowserSession struct{ ent.Schema }

func (OIDCBrowserSession) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_browser_sessions"}}
}
func (OIDCBrowserSession) Fields() []ent.Field {
	return []ent.Field{
		field.String("handle_digest").MaxLen(64), field.Int64("user_id"),
		field.Time("auth_time").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("amr").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("created_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_seen_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("idle_expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("absolute_expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("revoked_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("revoke_reason").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int64("session_version").Default(1),
	}
}
func (OIDCBrowserSession) Indexes() []ent.Index {
	return []ent.Index{index.Fields("handle_digest").Unique(), index.Fields("user_id"), index.Fields("idle_expires_at")}
}

type OIDCAuthorizationTransaction struct{ ent.Schema }

func (OIDCAuthorizationTransaction) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_authorization_transactions"}}
}
func (OIDCAuthorizationTransaction) Fields() []ent.Field {
	return []ent.Field{
		field.String("handle_digest").MaxLen(64), field.Int64("client_pk"), field.Int64("browser_session_id").Optional().Nillable(), field.Int64("user_id").Optional().Nillable(),
		field.String("redirect_uri").SchemaType(map[string]string{dialect.Postgres: "text"}), field.String("scope_snapshot").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("state_ciphertext").SchemaType(map[string]string{dialect.Postgres: "text"}), field.String("state_fingerprint").MaxLen(64), field.String("nonce_ciphertext").SchemaType(map[string]string{dialect.Postgres: "text"}), field.String("nonce_fingerprint").MaxLen(64),
		field.String("code_challenge").MaxLen(128), field.String("code_challenge_method").Default("S256"), field.String("prompt").SchemaType(map[string]string{dialect.Postgres: "text"}), field.Int64("max_age_seconds").Optional().Nillable(), field.String("display").Optional().Nillable(),
		field.Time("auth_time").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Int64("consent_id").Optional().Nillable(), field.String("status").MaxLen(32), field.Time("created_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Time("consumed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}), field.Int64("version").Default(1),
	}
}
func (OIDCAuthorizationTransaction) Indexes() []ent.Index {
	return []ent.Index{index.Fields("handle_digest").Unique(), index.Fields("expires_at"), index.Fields("status")}
}
