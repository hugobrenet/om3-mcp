package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

type objectActionClient struct {
	t        *testing.T
	path     string
	response string
	err      error
	posts    int
}

func (c *objectActionClient) GetJSON(context.Context, string, url.Values, any) error {
	c.t.Error("an object action must not read before posting")
	return nil
}

func (c *objectActionClient) PostJSON(_ context.Context, path string, query url.Values, input any, output any) error {
	c.posts++
	if path != c.path || len(query) != 0 || input != nil {
		c.t.Errorf("got POST %s %v %v, want %s without query nor body", path, query, input, c.path)
	}
	if c.err != nil {
		return c.err
	}
	return json.Unmarshal([]byte(c.response), output)
}

func TestSubmitObjectActionQueuesAnOrchestration(t *testing.T) {
	for _, tc := range []struct {
		path   string
		action ObjectAction
		post   string
	}{
		{"prod/svc/web", ObjectActionFreeze, "/api/object/path/prod/svc/web/action/freeze"},
		{"web", ObjectActionUnfreeze, "/api/object/path/root/svc/web/action/unfreeze"},
		{"prod/vol/data", ObjectActionAbort, "/api/object/path/prod/vol/data/action/abort"},
		{"prod/cfg/settings", ObjectActionAbort, "/api/object/path/prod/cfg/settings/action/abort"},
	} {
		client := &objectActionClient{t: t, path: tc.post, response: `{"orchestration_id":"30000000-0000-0000-0000-00000000000A"}`}
		result, err := New(client).SubmitObjectAction(context.Background(), tc.path, tc.action)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.action, tc.path, err)
		}
		if client.posts != 1 || result.Action != string(tc.action) || result.OrchestrationID != "30000000-0000-0000-0000-00000000000a" || result.Provenance.Source == "" {
			t.Fatalf("%s %s gave %+v", tc.action, tc.path, result)
		}
	}
}

func TestSubmitObjectActionRejectsInvalidTargetsBeforePosting(t *testing.T) {
	for _, tc := range []struct {
		path   string
		action ObjectAction
	}{
		{"", ObjectActionFreeze},
		{"prod/svc/*", ObjectActionFreeze},
		{"prod/svc/a,prod/svc/b", ObjectActionFreeze},
		{"prod/cfg/settings", ObjectActionFreeze},
		{"prod/sec/secret", ObjectActionUnfreeze},
		{"prod/svc/web", ObjectAction("delete")},
	} {
		client := &objectActionClient{t: t}
		if _, err := New(client).SubmitObjectAction(context.Background(), tc.path, tc.action); err == nil || client.posts != 0 {
			t.Fatalf("%s %q was accepted or posted", tc.action, tc.path)
		}
	}
}

func TestSubmitObjectActionReportsDaemonRefusalsAndBadAnswers(t *testing.T) {
	client := &objectActionClient{t: t, path: "/api/object/path/prod/svc/web/action/freeze", err: fmt.Errorf("OpenSVC daemon POST returned HTTP 403 Forbidden")}
	if _, err := New(client).SubmitObjectAction(context.Background(), "prod/svc/web", ObjectActionFreeze); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("got %v, want the daemon refusal", err)
	}
	client = &objectActionClient{t: t, path: "/api/object/path/prod/svc/web/action/freeze", response: `{"orchestration_id":""}`}
	if _, err := New(client).SubmitObjectAction(context.Background(), "prod/svc/web", ObjectActionFreeze); err == nil {
		t.Fatal("an answer without orchestration id was accepted")
	}
}
