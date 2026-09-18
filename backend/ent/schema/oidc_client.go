package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type OIDCClient struct{ ent.Schema }

func (OIDCClient) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_clients"}}
}
func (OIDCClient) Fields() []ent.Field {
	return []ent.Field{
		field.String("client_id").MaxLen(128).NotEmpty(),
		field.String("name").MaxLen(200).NotEmpty(),
		field.String("owner").MaxLen(200).NotEmpty(),
		field.String("client_type").Default("confidential"),
		field.Bool("enabled").Default(true),
		field.Bool("trusted_skip_consent").Default(false),
		field.Int64("policy_version").Default(1),
		field.Int64("created_by"),
		field.Time("created_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("updated_by"),
		field.Time("updated_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("disabled_by").Optional().Nillable(),
		field.Time("disabled_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("disabled_reason").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("last_change_reason").Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int64("version").Default(1),
	}
}
func (OIDCClient) Indexes() []ent.Index { return []ent.Index{index.Fields("client_id").Unique()} }

type OIDCClientSecret struct{ ent.Schema }

func (OIDCClientSecret) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_client_secrets"}}
}
func (OIDCClientSecret) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("client_pk"),
		field.String("secret_digest").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("fingerprint").MaxLen(64),
		field.String("status").MaxLen(20),
		field.Time("not_before").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("created_by"),
		field.String("created_reason").Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("created_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("revoked_by").Optional().Nillable(),
		field.Time("revoked_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("revoke_reason").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
	}
}
func (OIDCClientSecret) Indexes() []ent.Index {
	return []ent.Index{index.Fields("client_pk", "fingerprint").Unique(), index.Fields("client_pk", "status")}
}

type OIDCClientRedirectURI struct{ ent.Schema }

func (OIDCClientRedirectURI) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_client_redirect_uris"}}
}
func (OIDCClientRedirectURI) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("client_pk"),
		field.String("redirect_uri").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Bool("enabled").Default(true),
		field.Int64("created_by"),
		field.Time("created_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("disabled_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}
func (OIDCClientRedirectURI) Indexes() []ent.Index {
	return []ent.Index{index.Fields("client_pk", "redirect_uri").Unique()}
}

type OIDCClientScope struct{ ent.Schema }

func (OIDCClientScope) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oidc_client_scopes"}}
}
func (OIDCClientScope) Fields() []ent.Field {
	return []ent.Field{field.Int64("client_pk"), field.String("scope").MaxLen(64), field.Bool("enabled").Default(true)}
}
func (OIDCClientScope) Indexes() []ent.Index {
	return []ent.Index{index.Fields("client_pk", "scope").Unique()}
}
