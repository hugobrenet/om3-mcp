package core

import (
	"context"
	"net/url"
	"testing"
)

const clusterIPResourcePayload = `{
	"kind": "ResourceList",
	"items": [
		{
			"meta": {"node": "node-b", "object": "prod/svc/db", "rid": "ip#1"},
			"data": {
				"config": {"is_disabled": false, "is_monitored": true, "is_standby": true},
				"monitor": {"restart": {"remaining": 2, "last_at": "2026-09-22T08:00:00Z"}},
				"status": {
					"type": "ip.netns", "label": "192.0.2.20", "status": "down",
					"monitor": true, "standby": true,
					"provisioned": {"state": "true", "mtime": "2026-09-22T07:00:00Z"},
					"tags": ["vip", "database"],
					"info": {"ipaddr": "192.0.2.20", "dev": "eth0", "netmask": 24, "expose": ["tcp:5432"], "hostname": "db.example.test"},
					"log": [{"level": "error", "message": "address not available"}]
				}
			}
		},
		{
			"meta": {"node": "node-a", "object": "prod/svc/app", "rid": "ip#0"},
			"data": {
				"config": {"is_disabled": false, "is_monitored": false, "is_standby": false},
				"status": {
					"type": "ip.host", "label": "192.0.2.10", "status": "up",
					"provisioned": {"state": "true"},
					"info": {"ipaddr": "192.0.2.10", "dev": "ens3", "netmask": 24, "expose": []}
				}
			}
		},
		{
			"meta": {"node": "node-a", "object": "prod/svc/app", "rid": "ip#unexpected"},
			"data": {"status": {"type": "container.docker", "status": "up"}}
		}
	]
}`

func TestListClusterIPResourcesReturnsDaemonFactsAndPaginates(t *testing.T) {
	client := &recordingJSONGetter{
		t:       t,
		path:    "/api/resource",
		query:   url.Values{"path": {clusterIPResourcePathSelector}, "resource": {"ip#*"}},
		payload: clusterIPResourcePayload,
	}
	service := New(client)

	first, err := service.ListClusterIPResources(context.Background(), ListClusterIPResourcesOptions{Limit: 1})
	if err != nil {
		t.Fatalf("list first cluster IP resource page: %v", err)
	}
	if first.Total != 2 || first.Count != 1 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("got unexpected first page %+v", first)
	}
	resource := first.Resources[0]
	if resource.Node != "node-a" || resource.Object.Path != "prod/svc/app" || resource.RID != "ip#0" {
		t.Errorf("got unexpected first IP resource %+v", resource)
	}
	if resource.Type != "ip.host" || resource.Status != "up" {
		t.Errorf("got unexpected daemon status %+v", resource)
	}
	if resource.Info.IPAddr != "192.0.2.10" || resource.Info.Dev != "ens3" ||
		resource.Info.Netmask == nil || *resource.Info.Netmask != 24 {
		t.Errorf("got unexpected IP facts %+v", resource.Info)
	}
	if resource.Info.Expose == nil {
		t.Error("empty expose list must be encoded as an empty array")
	}

	secondClient := &recordingJSONGetter{
		t:       t,
		path:    "/api/resource",
		query:   url.Values{"path": {clusterIPResourcePathSelector}, "resource": {"ip#*"}},
		payload: clusterIPResourcePayload,
	}
	second, err := New(secondClient).ListClusterIPResources(context.Background(), ListClusterIPResourcesOptions{
		Limit: 1, Cursor: first.NextCursor,
	})
	if err != nil {
		t.Fatalf("list second cluster IP resource page: %v", err)
	}
	if second.Count != 1 || second.Truncated {
		t.Fatalf("got unexpected second page %+v", second)
	}
	resource = second.Resources[0]
	if resource.Node != "node-b" || resource.Object.Path != "prod/svc/db" || resource.Info.Hostname != "db.example.test" {
		t.Errorf("got unexpected second IP resource %+v", resource)
	}
	if resource.Type != "ip.netns" || resource.Label != "192.0.2.20" || resource.Status != "down" {
		t.Errorf("got unexpected direct resource facts %+v", resource)
	}
}

func TestListClusterIPResourcesAppliesExactFilters(t *testing.T) {
	client := &recordingJSONGetter{
		t:       t,
		path:    "/api/resource",
		query:   url.Values{"resource": {"ip#*"}, "path": {"prod/svc/app"}, "node": {"node-a"}},
		payload: clusterIPResourcePayload,
	}
	result, err := New(client).ListClusterIPResources(context.Background(), ListClusterIPResourcesOptions{
		Path: " prod/svc/app ", Node: " node-a ",
	})
	if err != nil {
		t.Fatalf("list filtered cluster IP resources: %v", err)
	}
	if result.PathFilter != "prod/svc/app" || result.NodeFilter != "node-a" || result.Total != 1 {
		t.Fatalf("got unexpected filtered result %+v", result)
	}
}

func TestListClusterIPResourcesRejectsInvalidInputBeforeDaemonCall(t *testing.T) {
	for _, test := range []struct {
		name    string
		options ListClusterIPResourcesOptions
	}{
		{name: "object selector", options: ListClusterIPResourcesOptions{Path: "**"}},
		{name: "node selector", options: ListClusterIPResourcesOptions{Node: "node-*"}},
		{name: "limit", options: ListClusterIPResourcesOptions{Limit: 201}},
		{name: "cursor", options: ListClusterIPResourcesOptions{Cursor: "not-base64!"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t}
			if _, err := New(client).ListClusterIPResources(context.Background(), test.options); err == nil {
				t.Fatal("expected input validation error")
			}
			if client.calls != 0 {
				t.Fatalf("got %d daemon calls, want 0", client.calls)
			}
		})
	}
}
