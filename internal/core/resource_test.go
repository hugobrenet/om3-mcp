package core

import (
	"context"
	"net/url"
	"testing"
)

func TestListObjectResources(t *testing.T) {
	client := &recordingJSONGetter{
		t:     t,
		path:  "/api/resource",
		query: url.Values{"path": {"prod/svc/mysql"}, "node": {"node-a"}},
		payload: `{
			"kind": "ResourceList",
			"items": [
				{"meta": {"node": "node-a", "object": "prod/svc/mysql", "rid": "ip#1"}, "data": {
					"config": {"is_disabled": true, "is_monitored": true, "is_standby": false},
					"monitor": {"restart": {"remaining": 2, "last_at": "2026-07-15T10:00:00Z"}},
					"status": {"type": "ip.host", "label": "192.0.2.10", "status": "down", "disable": false,
						"monitor": false, "optional": true, "standby": true, "encap": true,
						"provisioned": {"state": "true", "mtime": "2026-07-15T09:00:00Z"},
						"tags": ["front", "critical"], "log": [{"level": "error", "message": "address not available"}]}
				}}
			]
		}`,
	}

	result, err := New(client).ListObjectResources(context.Background(), ListObjectResourcesOptions{
		Path: "prod/svc/mysql", Node: " node-a ", Limit: 100,
	})
	if err != nil {
		t.Fatalf("list object resources: %v", err)
	}
	if result.Total != 1 || result.Count != 1 || result.Truncated {
		t.Fatalf("got unexpected resource list %+v", result)
	}
	resource := result.Resources[0]
	if resource.RID != "ip#1" || resource.Status != "down" || resource.RestartRemaining != 2 {
		t.Errorf("got unexpected resource %+v", resource)
	}
	if resource.ConfigFlags == nil || !resource.ConfigFlags.IsDisabled || !resource.ConfigFlags.IsMonitored || resource.ConfigFlags.IsStandby {
		t.Errorf("got unexpected config flags %+v", resource.ConfigFlags)
	}
	if resource.StatusFlags == nil || resource.StatusFlags.Disable || resource.StatusFlags.Monitor || !resource.StatusFlags.Optional || !resource.StatusFlags.Standby || !resource.StatusFlags.Encap {
		t.Errorf("got unexpected status flags %+v", resource.StatusFlags)
	}
	if len(resource.Logs) != 1 || resource.Logs[0].Message != "address not available" {
		t.Errorf("got unexpected resource logs %+v", resource.Logs)
	}
	if len(resource.Tags) != 2 || resource.Tags[0] != "critical" {
		t.Errorf("got unsorted resource tags %+v", resource.Tags)
	}
}

func TestListObjectResourcesPreservesMissingFlagSources(t *testing.T) {
	client := &recordingJSONGetter{
		t:     t,
		path:  "/api/resource",
		query: url.Values{"path": {"prod/svc/mysql"}},
		payload: `{"items":[
			{"meta":{"node":"node-a","object":"prod/svc/mysql","rid":"app#config-only"},"data":{"config":{"is_disabled":false,"is_monitored":true,"is_standby":false}}},
			{"meta":{"node":"node-a","object":"prod/svc/mysql","rid":"app#status-only"},"data":{"status":{"disable":true,"monitor":false,"optional":false,"standby":true,"encap":false}}}
		]}`,
	}
	result, err := New(client).ListObjectResources(context.Background(), ListObjectResourcesOptions{Path: "prod/svc/mysql"})
	if err != nil {
		t.Fatalf("list object resources: %v", err)
	}
	if result.Count != 2 {
		t.Fatalf("resources = %+v", result.Resources)
	}
	configOnly := result.Resources[0]
	statusOnly := result.Resources[1]
	if configOnly.ConfigFlags == nil || configOnly.StatusFlags != nil {
		t.Errorf("config-only flag sources = %+v", configOnly)
	}
	if statusOnly.ConfigFlags != nil || statusOnly.StatusFlags == nil || !statusOnly.StatusFlags.Disable || !statusOnly.StatusFlags.Standby {
		t.Errorf("status-only flag sources = %+v", statusOnly)
	}
}

func TestListObjectResourcesRejectsInvalidCursorBeforeDaemonCall(t *testing.T) {
	client := &recordingJSONGetter{t: t}
	_, err := New(client).ListObjectResources(context.Background(), ListObjectResourcesOptions{Path: "prod/svc/mysql", Cursor: "not-base64!"})
	if err == nil {
		t.Fatal("expected invalid cursor error")
	}
	if client.calls != 0 {
		t.Fatalf("got %d daemon calls, want 0", client.calls)
	}
}
