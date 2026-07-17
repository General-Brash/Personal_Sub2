package ent

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/idempotencyrecord"
)

func TestIdempotencyRecordGeneratedActorScope(t *testing.T) {
	record := IdempotencyRecord{
		OperationScope: "user.checkin",
		ActorScope:     "user:1",
	}
	if record.OperationScope != "user.checkin" || record.ActorScope != "user:1" {
		t.Fatal("generated idempotency record must expose operation and actor scopes")
	}
	if idempotencyrecord.FieldOperationScope != "operation_scope" || idempotencyrecord.FieldActorScope != "actor_scope" {
		t.Fatal("generated idempotency constants must match the migrated columns")
	}
}
