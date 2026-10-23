package core

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestGetObjectConfig(t *testing.T) {
	client := &recordingJSONGetter{
		t:    t,
		path: "/api/object/path/example/svc/cache/config",
		query: url.Values{
			"evaluate": {"false"},
			"kw":       {"container#redis.image", "container#redis.image_pull_policy"},
		},
		payload: `{
			"kind":"KeywordList",
			"items":[
				{"object":"example/svc/cache","node":"","keyword":"container#redis.image_pull_policy","value":"once","evaluated_as":""},
				{"object":"example/svc/cache","node":"","keyword":"container#redis.image","value":"redis:7-alpine","evaluated_as":""}
			]
		}`,
	}

	result, err := New(client).GetObjectConfig(context.Background(), GetObjectConfigOptions{
		Path: " example/svc/cache ",
		Keywords: []string{
			" container#redis.image_pull_policy ",
			"container#redis.image",
			"container#redis.image",
		},
	})
	if err != nil {
		t.Fatalf("get object config: %v", err)
	}
	if result.Object.Path != "example/svc/cache" || result.Total != 2 || result.Count != 2 || result.Truncated || result.ValuesTruncated != 0 {
		t.Fatalf("got unexpected object config metadata %+v", result)
	}
	wantFilter := []string{"container#redis.image", "container#redis.image_pull_policy"}
	if !reflect.DeepEqual(result.KeywordFilter, wantFilter) {
		t.Errorf("got keyword filter %#v, want %#v", result.KeywordFilter, wantFilter)
	}
	wantItems := []ObjectConfigKeyword{
		{Keyword: "container#redis.image", Value: "redis:7-alpine"},
		{Keyword: "container#redis.image_pull_policy", Value: "once"},
	}
	if !reflect.DeepEqual(result.Items, wantItems) {
		t.Errorf("got items %#v, want %#v", result.Items, wantItems)
	}
}

func TestGetObjectConfigBoundsItemsAndValues(t *testing.T) {
	longValue := strings.Repeat("é", maxObjectConfigValueRunes+1)
	client := &recordingJSONGetter{
		t:     t,
		path:  "/api/object/path/example/svc/cache/config",
		query: url.Values{"evaluate": {"false"}},
		payload: `{
			"kind":"KeywordList",
			"items":[
				{"object":"example/svc/cache","keyword":"z.option","value":"omitted","evaluated_as":""},
				{"object":"example/svc/cache","keyword":"a.option","value":` + quoteJSON(t, longValue) + `,"evaluated_as":""}
			]
		}`,
	}

	result, err := New(client).GetObjectConfig(context.Background(), GetObjectConfigOptions{
		Path: "example/svc/cache", Limit: 1,
	})
	if err != nil {
		t.Fatalf("get object config: %v", err)
	}
	if result.Total != 2 || result.Count != 1 || !result.Truncated || result.ValuesTruncated != 1 {
		t.Fatalf("got unexpected bounded metadata %+v", result)
	}
	if got := []rune(result.Items[0].Value); len(got) != maxObjectConfigValueRunes || got[len(got)-1] != '…' || !result.Items[0].ValueTruncated {
		t.Errorf("value was not bounded by runes: length=%d item=%+v", len(got), result.Items[0])
	}
}

func TestGetObjectConfigAppliesAggregateValueBudget(t *testing.T) {
	response := daemonObjectConfig{Kind: "KeywordList"}
	for index := 0; index < 17; index++ {
		response.Items = append(response.Items, daemonObjectConfigItem{
			Object:  "example/svc/cache",
			Keyword: "option." + string(rune('a'+index)),
			Value:   strings.Repeat("x", maxObjectConfigValueRunes),
		})
	}
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal daemon response: %v", err)
	}
	client := &recordingJSONGetter{
		t: t, path: "/api/object/path/example/svc/cache/config",
		query: url.Values{"evaluate": {"false"}}, payload: string(payload),
	}

	result, err := New(client).GetObjectConfig(context.Background(), GetObjectConfigOptions{Path: "example/svc/cache"})
	if err != nil {
		t.Fatalf("get object config: %v", err)
	}
	if result.Count != 17 || result.Truncated || result.ValuesTruncated != 1 {
		t.Fatalf("got unexpected aggregate budget metadata %+v", result)
	}
	if result.Items[16].Value != "" || !result.Items[16].ValueTruncated {
		t.Errorf("last value was not omitted after exhausting aggregate budget: %+v", result.Items[16])
	}
}

func TestGetObjectConfigRejectsInvalidInputBeforeDaemonCall(t *testing.T) {
	tests := []struct {
		name    string
		options GetObjectConfigOptions
	}{
		{name: "limit", options: GetObjectConfigOptions{Path: "example/svc/cache", Limit: maxGetObjectConfigLimit + 1}},
		{name: "empty keyword", options: GetObjectConfigOptions{Path: "example/svc/cache", Keywords: []string{" "}}},
		{name: "long keyword", options: GetObjectConfigOptions{Path: "example/svc/cache", Keywords: []string{strings.Repeat("x", maxObjectConfigKeywordLength+1)}}},
		{name: "too many keywords", options: GetObjectConfigOptions{Path: "example/svc/cache", Keywords: make([]string, maxObjectConfigKeywordFilters+1)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t}
			if _, err := New(client).GetObjectConfig(context.Background(), test.options); err == nil {
				t.Fatal("GetObjectConfig succeeded, want an error")
			}
			if client.calls != 0 {
				t.Fatalf("got %d daemon calls, want 0", client.calls)
			}
		})
	}
}

func TestGetObjectConfigRejectsUnexpectedDaemonData(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "kind", payload: `{"kind":"OtherList","items":[]}`},
		{name: "object", payload: `{"kind":"KeywordList","items":[{"object":"lab/svc/other","keyword":"id","value":"1"}]}`},
		{name: "empty keyword", payload: `{"kind":"KeywordList","items":[{"object":"example/svc/cache","keyword":"","value":"1"}]}`},
		{name: "evaluated value", payload: `{"kind":"KeywordList","items":[{"object":"example/svc/cache","keyword":"id","value":"1","evaluated":"1","evaluated_as":"node-a"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingJSONGetter{
				t: t, path: "/api/object/path/example/svc/cache/config",
				query: url.Values{"evaluate": {"false"}}, payload: test.payload,
			}
			if _, err := New(client).GetObjectConfig(context.Background(), GetObjectConfigOptions{Path: "example/svc/cache"}); err == nil {
				t.Fatal("GetObjectConfig succeeded, want an error")
			}
		})
	}
}

func quoteJSON(t *testing.T, value string) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON string: %v", err)
	}
	return string(payload)
}

// The data keys of a sec are secrets: they are left out, counted, and a
// filter naming one is refused before the call. Those of a cfg stay.
func TestGetObjectConfigLeavesOutTheKeysOfASecret(t *testing.T) {
	payload := `{"kind":"KeywordList","items":[
		{"object":"test/sec/web","node":"","keyword":"data.private_key","value":"crypt:AAAA","evaluated_as":""},
		{"object":"test/sec/web","node":"","keyword":"cn","value":"web.example.com","evaluated_as":""},
		{"object":"test/sec/web","node":"","keyword":"data.certificate","value":"crypt:BBBB","evaluated_as":""}]}`
	client := &recordingJSONGetter{t: t, path: "/api/object/path/test/sec/web/config", query: url.Values{"evaluate": {"false"}}, payload: payload}
	result, err := New(client).GetObjectConfig(context.Background(), GetObjectConfigOptions{Path: "test/sec/web", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Count != 1 || result.Truncated || result.Items[0].Keyword != "cn" || result.DataKeysOmitted != 2 || result.DataKeysNote == "" {
		t.Fatalf("got %+v", result)
	}
	for _, keyword := range []string{"data.private_key", "DATA.certificate"} {
		getter := &recordingJSONGetter{t: t}
		if _, err := New(getter).GetObjectConfig(context.Background(), GetObjectConfigOptions{Path: "test/usr/jane", Keywords: []string{keyword}}); err == nil || !strings.Contains(err.Error(), "list_object_data_keys") || getter.calls != 0 {
			t.Fatalf("%s: %v", keyword, err)
		}
	}
	cfg := strings.ReplaceAll(payload, "test/sec/web", "test/cfg/web")
	client = &recordingJSONGetter{t: t, path: "/api/object/path/test/cfg/web/config", query: url.Values{"evaluate": {"false"}}, payload: cfg}
	result, err = New(client).GetObjectConfig(context.Background(), GetObjectConfigOptions{Path: "test/cfg/web"})
	if err != nil || result.Total != 3 || result.DataKeysOmitted != 0 || result.DataKeysNote != "" {
		t.Fatalf("cfg: %+v %v", result, err)
	}
}

// The secret keywords are left out of the response and counted, set but
// not returned; the others stay.
func TestGetObjectConfigLeavesOutTheSecretKeywords(t *testing.T) {
	payload := `{"kind":"KeywordList","items":[
		{"object":"cluster","node":"","keyword":"cluster.secret","value":"s3cr3t-value","evaluated_as":""},
		{"object":"cluster","node":"","keyword":"node.uuid","value":"0f5c3a1e-uuid-value","evaluated_as":""},
		{"object":"cluster","node":"","keyword":"cluster.name","value":"dev5","evaluated_as":""}]}`
	client := &recordingJSONGetter{t: t, path: "/api/object/path/root/ccfg/cluster/config", query: url.Values{"evaluate": {"false"}}, payload: payload}
	result, err := New(client).GetObjectConfig(context.Background(), GetObjectConfigOptions{Path: "cluster"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Count != 1 || result.Truncated || result.Items[0].Keyword != "cluster.name" || result.SecretsOmitted != 2 {
		t.Fatalf("got %+v", result)
	}
	if out, _ := json.Marshal(result); strings.Contains(string(out), "s3cr3t-value") || strings.Contains(string(out), "uuid-value") {
		t.Fatalf("a secret was returned: %s", out)
	}
}
