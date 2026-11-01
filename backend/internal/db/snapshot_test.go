package db

import (
	"context"
	"testing"
)

func TestSnapshotRejectsUnexpectedSchemaObjects(t *testing.T) {
	d := openTest(t)
	if e := VerifySchema(context.Background(), d); e != nil {
		t.Fatal(e)
	}
	if _, e := d.Exec("CREATE TRIGGER unexpected_owner_trigger AFTER INSERT ON owner BEGIN SELECT 1; END"); e != nil {
		t.Fatal(e)
	}
	if e := VerifySchema(context.Background(), d); e == nil {
		t.Fatal("accepted extra schema trigger")
	}
}
