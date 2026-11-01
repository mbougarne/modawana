package access

import (
	"context"
	"testing"
)

func TestFoundationDeniesEverySurface(t *testing.T) {
	for _, surface := range []Surface{Content, Media, Preview, Search, Feed, Collection} {
		for _, audience := range []Audience{Anonymous, Learner, Owner, Publisher} {
			d, e := (DenyAll{}).Resolve(context.Background(), Request{Surface: surface, Principal: Principal{Audience: audience}})
			if e != nil || d.Visibility != Hidden || d.AllowsBody() {
				t.Fatal("foundation grant")
			}
		}
	}
	if (Decision{Visibility: PublicTeaser, RevisionID: "r"}).AllowsBody() {
		t.Fatal("teaser grants body")
	}
}
