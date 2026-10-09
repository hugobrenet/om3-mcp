package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestGetCallerIdentityReportsTheNameAndGrants(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/auth/whoami", query: url.Values{},
		payload: `{"auth":"jwt-openid","grant":{"admin":["prod"],"guest":["test","prod"]},"name":"alice","namespace":"system","raw_grant":"guest:test admin admin:prod guest:prod guest:test"}`,
	}
	identity, err := New(client).GetCallerIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []CallerGrant{{Role: "admin"}, {Role: "admin", Namespace: "prod"}, {Role: "guest", Namespace: "prod"}, {Role: "guest", Namespace: "test"}}
	if identity.Name != "alice" || len(identity.Grants) != len(want) || identity.GrantsTruncated || identity.Provenance.Source == "" {
		t.Fatalf("unexpected identity %+v", identity)
	}
	for i := range want {
		if identity.Grants[i] != want[i] {
			t.Fatalf("grants %+v, want %+v", identity.Grants, want)
		}
	}
	data, _ := json.Marshal(identity)
	if strings.Contains(string(data), "jwt-openid") || strings.Contains(string(data), "auth") {
		t.Fatalf("the authentication method leaked: %s", data)
	}
}

func TestGetCallerIdentityAcceptsNoGrant(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/auth/whoami", query: url.Values{}, payload: `{"name":"bob","grant":{},"raw_grant":""}`}
	identity, err := New(client).GetCallerIdentity(context.Background())
	if err != nil || identity.Name != "bob" || identity.Grants == nil || len(identity.Grants) != 0 {
		t.Fatalf("got %+v, %v", identity, err)
	}
}

func TestGetCallerIdentityRejectsMalformedResponses(t *testing.T) {
	for _, payload := range []string{`{"raw_grant":"root"}`, `{"name":"alice"}`, `{"name":"alice","raw_grant":":prod"}`} {
		client := &recordingJSONGetter{t: t, path: "/api/auth/whoami", query: url.Values{}, payload: payload}
		if _, err := New(client).GetCallerIdentity(context.Background()); err == nil {
			t.Fatalf("payload %s was accepted", payload)
		}
	}
}
