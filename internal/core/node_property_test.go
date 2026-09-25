package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestListNodePropertiesPreservesTypesSortsFiltersAndPaginates(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/system/property", query: url.Values{},
		payload: `{"kind":"PropertyList","items":[
			{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"node_env","title":"environment","source":"config","value":"TST","error":""}},
			{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"cpu_threads","title":"cpu threads","source":"probe","value":4,"error":""}},
			{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"feature_enabled","title":"feature enabled","source":"probe","value":true,"error":""}},
			{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"serial","title":"serial","source":"probe","value":"","error":"not collected"}}
		]}`,
	}
	service := New(client)

	first, err := service.ListNodeProperties(context.Background(), ListNodePropertiesOptions{Sources: []string{"probe", "probe"}, Limit: 2})
	if err != nil {
		t.Fatalf("list first node property page: %v", err)
	}
	if first.Node != "node-a" || first.ReportedTotal != 4 || first.Total != 3 || first.Count != 2 || !first.Truncated {
		t.Fatalf("unexpected first page metadata: %#v", first)
	}
	if first.Properties[0].Name != "cpu_threads" || first.Properties[0].Value.Type != nodePropertyValueTypeNumber || first.Properties[0].Value.Number == nil || *first.Properties[0].Value.Number != 4 {
		t.Errorf("unexpected numeric property: %#v", first.Properties[0])
	}
	if first.Properties[1].Name != "feature_enabled" || first.Properties[1].Value.Type != nodePropertyValueTypeBoolean || first.Properties[1].Value.Boolean == nil || !*first.Properties[1].Value.Boolean || first.NextCursor != "feature_enabled" {
		t.Errorf("unexpected boolean property or cursor: %#v", first)
	}

	second, err := service.ListNodeProperties(context.Background(), ListNodePropertiesOptions{Sources: []string{"probe"}, Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("list second node property page: %v", err)
	}
	if second.Count != 1 || second.Truncated || second.NextCursor != "" || second.Properties[0].Name != "serial" || second.Properties[0].Value.String == nil || *second.Properties[0].Value.String != "" || second.Properties[0].Error != "not collected" {
		t.Errorf("unexpected second page: %#v", second)
	}
	if client.calls != 2 {
		t.Errorf("got %d daemon calls, want 2", client.calls)
	}
}

func TestListNodePropertiesCombinesExactNameAndSourceFilters(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/node-a/system/property", query: url.Values{},
		payload: `{"kind":"PropertyList","items":[
			{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"os_name","title":"os name","source":"probe","value":"linux","error":""}},
			{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"node_env","title":"environment","source":"config","value":"TST","error":""}}
		]}`,
	}
	result, err := New(client).ListNodeProperties(context.Background(), ListNodePropertiesOptions{
		Node: "node-a", Names: []string{"node_env", "os_name"}, Sources: []string{"config"},
	})
	if err != nil {
		t.Fatalf("list filtered node properties: %v", err)
	}
	if result.Total != 1 || result.Count != 1 || result.Properties[0].Name != "node_env" || result.Properties[0].Value.String == nil || *result.Properties[0].Value.String != "TST" {
		t.Errorf("unexpected filtered properties: %#v", result)
	}
}

func TestListNodePropertiesReturnsEmptyNonNilList(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/system/property", query: url.Values{}, payload: `{"kind":"PropertyList","items":[]}`,
	}
	result, err := New(client).ListNodeProperties(context.Background(), ListNodePropertiesOptions{})
	if err != nil {
		t.Fatalf("list empty node properties: %v", err)
	}
	if result.Node != localDaemonNodeAlias || result.ReportedTotal != 0 || result.Total != 0 || result.Count != 0 || result.Properties == nil || result.Truncated {
		t.Errorf("unexpected empty property list: %#v", result)
	}
}

func TestListNodePropertiesBoundsStringValueAndError(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/system/property", query: url.Values{},
		payload: `{"kind":"PropertyList","items":[{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"serial","title":"serial","source":"probe","value":"` + strings.Repeat("v", maxNodePropertyValueRunes+1) + `","error":"` + strings.Repeat("e", maxNodePropertyErrorRunes+1) + `"}}]}`,
	}
	result, err := New(client).ListNodeProperties(context.Background(), ListNodePropertiesOptions{})
	if err != nil {
		t.Fatalf("list bounded node properties: %v", err)
	}
	property := result.Properties[0]
	if !property.ValueTruncated || property.Value.String == nil || len([]rune(*property.Value.String)) != maxNodePropertyValueRunes || !property.ErrorTruncated || len([]rune(property.Error)) != maxNodePropertyErrorRunes {
		t.Errorf("property was not bounded: %#v", property)
	}
}

func TestListNodePropertiesRejectsInvalidInputsBeforeCallingDaemon(t *testing.T) {
	tests := map[string]ListNodePropertiesOptions{
		"node selector":    {Node: "node*"},
		"spaced node":      {Node: " node-a"},
		"invalid limit":    {Limit: maxNodePropertyLimit + 1},
		"empty name":       {Names: []string{""}},
		"spaced source":    {Sources: []string{" probe"}},
		"too many names":   {Names: make([]string, maxNodePropertyFilters+1)},
		"invalid cursor":   {Cursor: "bad\ncursor"},
		"oversized cursor": {Cursor: strings.Repeat("x", maxNodePropertyCursorRunes+1)},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t}
			if _, err := New(client).ListNodeProperties(context.Background(), options); err == nil {
				t.Fatal("expected validation error")
			}
			if client.calls != 0 {
				t.Errorf("daemon was called %d times", client.calls)
			}
		})
	}
}

func TestListNodePropertiesRejectsMalformedDaemonData(t *testing.T) {
	tests := map[string]string{
		"unexpected list kind": `{"kind":"OtherList","items":[]}`,
		"unexpected item kind": `{"kind":"PropertyList","items":[{"kind":"OtherItem","meta":{"node":"node-a"},"data":{"name":"os_name","title":"os name","source":"probe","value":"linux","error":""}}]}`,
		"unexpected node":      `{"kind":"PropertyList","items":[{"kind":"PropertyItem","meta":{"node":"node-b"},"data":{"name":"os_name","title":"os name","source":"probe","value":"linux","error":""}}]}`,
		"empty name":           `{"kind":"PropertyList","items":[{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"","title":"os name","source":"probe","value":"linux","error":""}}]}`,
		"empty source":         `{"kind":"PropertyList","items":[{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"os_name","title":"os name","source":"","value":"linux","error":""}}]}`,
		"control title":        `{"kind":"PropertyList","items":[{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"os_name","title":"bad\ntitle","source":"probe","value":"linux","error":""}}]}`,
		"null value":           `{"kind":"PropertyList","items":[{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"os_name","title":"os name","source":"probe","value":null,"error":""}}]}`,
		"object value":         `{"kind":"PropertyList","items":[{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"os_name","title":"os name","source":"probe","value":{},"error":""}}]}`,
		"duplicate name":       `{"kind":"PropertyList","items":[{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"os_name","title":"os name","source":"probe","value":"linux","error":""}},{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"os_name","title":"os name","source":"default","value":"","error":""}}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t, path: "/api/node/name/node-a/system/property", query: url.Values{}, payload: payload}
			if _, err := New(client).ListNodeProperties(context.Background(), ListNodePropertiesOptions{Node: "node-a"}); err == nil {
				t.Fatal("expected malformed daemon response error")
			}
		})
	}
}

func TestListNodePropertiesRejectsInconsistentLocalNodeData(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/system/property", query: url.Values{},
		payload: `{"kind":"PropertyList","items":[
			{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"os_name","title":"os name","source":"probe","value":"linux","error":""}},
			{"kind":"PropertyItem","meta":{"node":"node-b"},"data":{"name":"os_arch","title":"os arch","source":"probe","value":"amd64","error":""}}
		]}`,
	}
	if _, err := New(client).ListNodeProperties(context.Background(), ListNodePropertiesOptions{}); err == nil || !strings.Contains(err.Error(), "inconsistent node") {
		t.Fatalf("got inconsistent local node error %v", err)
	}
}

func TestListNodePropertiesRejectsStaleCursor(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/system/property", query: url.Values{},
		payload: `{"kind":"PropertyList","items":[{"kind":"PropertyItem","meta":{"node":"node-a"},"data":{"name":"os_name","title":"os name","source":"probe","value":"linux","error":""}}]}`,
	}
	if _, err := New(client).ListNodeProperties(context.Background(), ListNodePropertiesOptions{Cursor: "os_release"}); err == nil || !strings.Contains(err.Error(), "no longer present") {
		t.Fatalf("got stale cursor error %v", err)
	}
}
