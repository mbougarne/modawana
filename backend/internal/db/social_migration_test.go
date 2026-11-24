package db

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"github.com/mbougarne/modawana/backend/migrations"
)

func TestSocialDestinationMigrationPreservesMastodonConnectionAndJob(t *testing.T) {
	ctx := context.Background()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.SetMaxOpenConns(1)
	exec(t, d, "PRAGMA foreign_keys=ON")
	before := migrationTestSource(t)
	for name := range before {
		if name >= "0027" {
			delete(before, name)
		}
	}
	if err := migrate(ctx, d, before); err != nil {
		t.Fatal(err)
	}
	exec(t, d, `INSERT INTO social_connections(id,slot,instance,account_id,account,account_url,token)
		VALUES ('connection',1,'https://social.example','123','owner','',x'cabbed')`)
	exec(t, d, `INSERT INTO outbox_events(id,name,contract_version,source_id,source_version,payload,created_at)
		VALUES ('event','social.scheduled',1,'job',1,'{}',1)`)
	exec(t, d, `INSERT INTO outbox_deliveries(id,event_id,plugin_id,plugin_slug,available_at,updated_at)
		VALUES ('job','event','','social',2,1)`)
	exec(t, d, `INSERT INTO social_posts(id,connection_id,instance,account,content_key,locale,revision_id,path,body,request_hash,created_at,scheduled_at)
		VALUES ('job','connection','https://social.example','owner','article','en','rev','/en/articles/article','original text','hash',1,2)`)
	if err := migrate(ctx, d, migrations.Files); err != nil {
		t.Fatal(err)
	}
	var provider, jobProvider, connection, body string
	var token []byte
	var scheduled int
	err = d.QueryRow(`SELECT c.provider,c.token,p.provider,p.connection_id,p.body,p.scheduled_at
		FROM social_posts p JOIN social_connections c ON c.id=p.connection_id WHERE p.id='job'`).
		Scan(&provider, &token, &jobProvider, &connection, &body, &scheduled)
	if err != nil || provider != "mastodon" || jobProvider != provider || !bytes.Equal(token, []byte{0xca, 0xbb, 0xed}) ||
		connection != "connection" || body != "original text" || scheduled != 2 {
		t.Fatal("the migration changed existing Mastodon records", err)
	}
}
