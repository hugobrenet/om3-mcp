package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	testSessionID = "40000000-0000-0000-0000-00000000000a"
	testExecID    = "40000000-0000-0000-0000-00000000000b"
)

// instanceActionClient answers the reads of an instance action, the
// instances, the topology and the execution, and records the POST.
type instanceActionClient struct {
	instances []string
	topology  string
	// execs are the answers to the successive reads of the execution; the
	// last one repeats.
	execs   []string
	execErr error
	postErr error
	// monitor are the monitor states the successive reads of the instance
	// give a clear; the last one repeats.
	monitor []string
	// logs are the journal entries of the execution; logErr fails their read.
	logs      []string
	logErr    error
	logQuery  url.Values
	logReads  int
	reads     []string
	execReads int
	posts     []string
	query     url.Values
}

func (c *instanceActionClient) GetJSON(_ context.Context, path string, query url.Values, output any) error {
	c.reads = append(c.reads, path)
	switch {
	case path == "/api/instance":
		items := c.instances
		if len(c.monitor) > 0 {
			state := c.monitor[min(c.instanceReads(), len(c.monitor)-1)]
			items = []string{instanceMonitorRecord("test/svc/web", "n1", "down", state)}
		}
		return json.Unmarshal([]byte(`{"items":[`+strings.Join(items, ",")+`]}`), output)
	case path == "/api/object":
		return json.Unmarshal([]byte(fmt.Sprintf(`{"items":[{"meta":{"object":%q},"data":{"topology":%q}}]}`, query.Get("path"), c.topology)), output)
	case strings.HasPrefix(path, "/api/node/name/n1/daemon/exec/id/"):
		c.execReads++
		if c.execErr != nil {
			return c.execErr
		}
		return json.Unmarshal([]byte(c.execs[min(c.execReads-1, len(c.execs)-1)]), output)
	}
	return fmt.Errorf("unexpected read %s", path)
}

func (c *instanceActionClient) GetSSE(_ context.Context, path string, query url.Values, consume func(string, string, []byte) error) error {
	c.logReads++
	c.logQuery = query
	if path != "/api/node/name/n1/log" {
		return fmt.Errorf("unexpected log read %s", path)
	}
	if c.logErr != nil {
		return c.logErr
	}
	for _, entry := range c.logs {
		if err := consume("log", "", []byte(entry)); err != nil {
			return err
		}
	}
	return nil
}

// execLogEntry is a journal entry of the execution, shaped after dev5.
func execLogEntry(level, message string) string {
	payload, _ := json.Marshal(map[string]string{"level": level, "node": "n1", "exec_id": testExecID, "obj_path": "test/svc/web", "time": "2026-10-22T12:00:01Z", "message": message})
	entry, _ := json.Marshal(map[string]string{"MESSAGE": message, "NODE": "n1", "EXEC_ID": testExecID, "PRIORITY": "4", "JSON": string(payload)})
	return string(entry)
}

func (c *instanceActionClient) instanceReads() int {
	count := 0
	for _, path := range c.reads {
		if path == "/api/instance" {
			count++
		}
	}
	return count - 1
}

func (c *instanceActionClient) PostJSON(_ context.Context, path string, query url.Values, _, output any) error {
	c.posts = append(c.posts, path)
	c.query = query
	if c.postErr != nil {
		return c.postErr
	}
	if output == nil {
		return nil
	}
	return json.Unmarshal([]byte(`{"session_id":"`+testSessionID+`","exec_id":"`+testExecID+`"}`), output)
}

func instanceMonitorRecord(object, node, avail, state string) string {
	return fmt.Sprintf(`{"meta":{"node":%q,"object":%q},"data":{"status":{"avail":%q},"monitor":{"state":%q}}}`, node, object, avail, state)
}

func execRecord(state string, exitCode *int, ended bool, failure string) string {
	record := map[string]any{
		"session_id": testSessionID, "exec_id": testExecID, "node": "n1", "origin": "api",
		"command": "om test/svc/web instance start", "state": state, "started_at": "2026-10-22T12:00:00Z",
	}
	if exitCode != nil {
		record["exit_code"] = *exitCode
	}
	if ended {
		record["ended_at"] = "2026-10-22T12:00:02Z"
	}
	if failure != "" {
		record["error"] = failure
	}
	data, _ := json.Marshal(record)
	return string(data)
}

func TestRunInstanceActionSendsItsOptionsAndReadsTheExecution(t *testing.T) {
	one := 1
	for _, tc := range []struct {
		action  InstanceAction
		options InstanceActionOptions
		query   string
	}{
		{InstanceActionStart, InstanceActionOptions{Rids: []string{"fs#1", "app#2"}, DisableRollback: true}, "disable_rollback=true&rid=fs%231%2Capp%232"},
		{InstanceActionStop, InstanceActionOptions{InterruptSyncs: true, ConfigUpdatedAt: "2026-10-22T12:00:00.5Z"}, "config_updated_at=2026-10-22T12%3A00%3A00.5Z&interrupt_syncs=true"},
		{InstanceActionRestart, InstanceActionOptions{}, ""},
		{InstanceActionFreeze, InstanceActionOptions{}, ""},
		{InstanceActionUnfreeze, InstanceActionOptions{}, ""},
		{InstanceActionUnprovision, InstanceActionOptions{Rids: []string{"fs#1"}, Leader: true}, "leader=true&rid=fs%231"},
		{InstanceActionUnprovision, InstanceActionOptions{}, ""},
	} {
		client := &instanceActionClient{
			instances: []string{instanceMonitorRecord("test/svc/web", "n1", "down", "idle")},
			topology:  "failover",
			execs:     []string{execRecord("running", nil, false, ""), execRecord("failed", &one, true, "exit status 1")},
			logs:      []string{execLogEntry("warn", `ip#1: stderr: Cannot find device "eth0"`), execLogEntry("error", "ip#1: start: ip exit code 1")},
		}
		result, err := New(client).RunInstanceAction(context.Background(), "test/svc/web", "n1", tc.action, tc.options)
		if err != nil {
			t.Fatalf("%s: %v", tc.action, err)
		}
		if len(client.posts) != 1 || client.posts[0] != "/api/node/name/n1/instance/path/test/svc/web/action/"+string(tc.action) || client.query.Encode() != tc.query {
			t.Fatalf("%s: posted %v %s", tc.action, client.posts, client.query.Encode())
		}
		if !result.Completed || result.State != "failed" || result.ExitCode == nil || *result.ExitCode != 1 || result.Error == nil || *result.Error != "exit status 1" ||
			result.ExecID != testExecID || result.SessionID != testSessionID || result.Instance.Availability != "down" || client.execReads != 2 {
			t.Fatalf("%s: %+v, %d reads", tc.action, result, client.execReads)
		}
		// A failure carries the end of the journal of its execution, in time
		// order, read once.
		if client.logReads != 1 || client.logQuery.Get("lines") != "21" || client.logQuery.Get("filter") != "EXEC_ID="+testExecID ||
			len(result.LogTail) != 2 || result.LogTail[0].Level != "warn" || result.LogTail[0].Message != `ip#1: stderr: Cannot find device "eth0"` ||
			result.LogTail[1].Level != "error" || result.LogTruncated || result.LogError != "" {
			t.Fatalf("%s: log %+v, %d reads, query %v", tc.action, result.LogTail, client.logReads, client.logQuery)
		}
	}
}

func TestRunInstanceActionReturnsAnExecutionStillRunning(t *testing.T) {
	client := &instanceActionClient{
		instances: []string{instanceMonitorRecord("test/svc/web", "n1", "down", "idle")},
		execs:     []string{execRecord("running", nil, false, "")},
	}
	result, err := New(client).RunInstanceAction(context.Background(), "test/svc/web", "n1", InstanceActionFreeze, InstanceActionOptions{Wait: time.Second})
	if err != nil || result.Completed || result.State != "running" || result.ExitCode != nil || result.ExecutionError != "" || client.logReads != 0 {
		t.Fatalf("%+v %v", result, err)
	}

	// A success reads no journal; a journal that cannot be read leaves the
	// result of the execution as it is.
	zero := 0
	client = &instanceActionClient{
		instances: []string{instanceMonitorRecord("test/svc/web", "n1", "down", "idle")},
		execs:     []string{execRecord("succeeded", &zero, true, "")},
	}
	result, err = New(client).RunInstanceAction(context.Background(), "test/svc/web", "n1", InstanceActionFreeze, InstanceActionOptions{})
	if err != nil || !result.Completed || client.logReads != 0 || result.LogTail != nil {
		t.Fatalf("success: %+v %v", result, err)
	}
	two := 2
	client = &instanceActionClient{
		instances: []string{instanceMonitorRecord("test/svc/web", "n1", "down", "idle")},
		execs:     []string{execRecord("failed", &two, true, "exit status 2")},
		logErr:    daemonProblemError{status: http.StatusForbidden, detail: "need root"},
	}
	result, err = New(client).RunInstanceAction(context.Background(), "test/svc/web", "n1", InstanceActionStop, InstanceActionOptions{})
	if err != nil || !result.Completed || *result.ExitCode != 2 || result.LogTail != nil || !strings.Contains(result.LogError, "need root") {
		t.Fatalf("log refused: %+v %v", result, err)
	}

	// An operator may run the action, not read its execution.
	client = &instanceActionClient{
		instances: []string{instanceMonitorRecord("test/svc/web", "n1", "down", "idle")},
		execErr:   daemonProblemError{status: http.StatusForbidden, detail: "need root"},
	}
	result, err = New(client).RunInstanceAction(context.Background(), "test/svc/web", "n1", InstanceActionFreeze, InstanceActionOptions{})
	if err != nil || result.Completed || !strings.HasPrefix(result.ExecutionError, "reading the execution requires root access") || client.execReads != 1 {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestRunInstanceActionRefusesBeforeTheCall(t *testing.T) {
	up := instanceMonitorRecord("test/svc/web", "n2", "up", "idle")
	down := instanceMonitorRecord("test/svc/web", "n1", "down", "idle")
	for _, tc := range []struct {
		name      string
		path      string
		node      string
		action    InstanceAction
		options   InstanceActionOptions
		instances []string
		topology  string
		want      string
	}{
		{"selector", "test/svc/*", "n1", InstanceActionStart, InstanceActionOptions{}, nil, "", "without wildcard or selector"},
		{"kind", "test/cfg/web", "n1", InstanceActionStart, InstanceActionOptions{}, nil, "", "svc, vol objects only"},
		{"node alias", "test/svc/web", "_", InstanceActionStart, InstanceActionOptions{}, nil, "", "the _ alias"},
		{"rid selector", "test/svc/web", "n1", InstanceActionStart, InstanceActionOptions{Rids: []string{"fs"}}, nil, "", "one exact resource id"},
		{"rids on freeze", "test/svc/web", "n1", InstanceActionFreeze, InstanceActionOptions{Rids: []string{"fs#1"}}, nil, "", "start, stop, restart and unprovision actions only"},
		{"leader on stop", "test/svc/web", "n1", InstanceActionStop, InstanceActionOptions{Leader: true}, nil, "", "unprovision action only"},
		{"rollback on stop", "test/svc/web", "n1", InstanceActionStop, InstanceActionOptions{DisableRollback: true}, nil, "", "start and restart actions only"},
		{"syncs on start", "test/svc/web", "n1", InstanceActionStart, InstanceActionOptions{InterruptSyncs: true}, nil, "", "stop action only"},
		{"config time", "test/svc/web", "n1", InstanceActionStart, InstanceActionOptions{ConfigUpdatedAt: "yesterday"}, nil, "", "RFC 3339"},
		{"wait", "test/svc/web", "n1", InstanceActionStart, InstanceActionOptions{Wait: 61 * time.Second}, nil, "", "between 1 and 60 seconds"},
		{"no instance", "test/svc/web", "n3", InstanceActionStop, InstanceActionOptions{}, []string{down, up}, "", "no instance on that node"},
		{"failover start", "test/svc/web", "n1", InstanceActionStart, InstanceActionOptions{}, []string{down, up}, "failover", "its instance on n2 is up"},
		{"failover restart", "test/svc/web", "n1", InstanceActionRestart, InstanceActionOptions{Rids: []string{"fs#1"}}, []string{down, instanceMonitorRecord("test/svc/web", "n2", "warn", "idle")}, "failover", "its instance on n2 is warn"},
		{"unprovision up", "test/svc/web", "n2", InstanceActionUnprovision, InstanceActionOptions{}, []string{down, up}, "", "stop it first, stop_instance"},
		{"unprovision warn", "test/svc/web", "n2", InstanceActionUnprovision, InstanceActionOptions{Leader: true}, []string{down, instanceMonitorRecord("test/svc/web", "n2", "warn", "idle")}, "", "the instance is warn"},
	} {
		client := &instanceActionClient{instances: tc.instances, topology: tc.topology}
		_, err := New(client).RunInstanceAction(context.Background(), tc.path, tc.node, tc.action, tc.options)
		if err == nil || !strings.Contains(err.Error(), tc.want) || len(client.posts) != 0 {
			t.Fatalf("%s: %v, posts %v", tc.name, err, client.posts)
		}
	}

	// A flex service, or a stop, runs beside an instance up elsewhere.
	for _, tc := range []struct {
		action   InstanceAction
		topology string
	}{{InstanceActionStart, "flex"}, {InstanceActionStop, "failover"}, {InstanceActionFreeze, "failover"}, {InstanceActionUnprovision, "failover"}} {
		client := &instanceActionClient{instances: []string{down, up}, topology: tc.topology, execs: []string{execRecord("succeeded", new(int), true, "")}}
		if _, err := New(client).RunInstanceAction(context.Background(), "test/svc/web", "n1", tc.action, InstanceActionOptions{}); err != nil || len(client.posts) != 1 {
			t.Fatalf("%s %s: %v", tc.action, tc.topology, err)
		}
	}

	// The refusal of the daemon is given as it says it.
	client := &instanceActionClient{instances: []string{down}, postErr: daemonProblemError{status: http.StatusConflict, detail: "config is older"}}
	if _, err := New(client).RunInstanceAction(context.Background(), "test/svc/web", "n1", InstanceActionStop, InstanceActionOptions{}); err == nil || !strings.Contains(err.Error(), "config is older") {
		t.Fatalf("daemon refusal: %v", err)
	}
}

func TestClearInstanceLiftsAFailureOnly(t *testing.T) {
	client := &instanceActionClient{monitor: []string{"start failed", "start failed", "idle"}}
	result, err := New(client).ClearInstance(context.Background(), "test/svc/web", "n1")
	if err != nil || !result.Cleared || result.PreviousState != "start failed" || result.CurrentState != "idle" {
		t.Fatalf("%+v %v", result, err)
	}
	if len(client.posts) != 1 || client.posts[0] != "/api/node/name/n1/instance/path/test/svc/web/clear" {
		t.Fatalf("posted %v", client.posts)
	}

	for state, want := range map[string]string{
		"idle":     "nothing to clear",
		"starting": `its monitor state is "starting", not a failure`,
		"stopped":  `its monitor state is "stopped", not a failure`,
	} {
		client := &instanceActionClient{monitor: []string{state}}
		if _, err := New(client).ClearInstance(context.Background(), "test/svc/web", "n1"); err == nil || !strings.Contains(err.Error(), want) || len(client.posts) != 0 {
			t.Fatalf("%s: %v, posts %v", state, err, client.posts)
		}
	}
	client = &instanceActionClient{}
	if _, err := New(client).ClearInstance(context.Background(), "test/sec/web", "dev5n1,dev5n2"); err == nil || len(client.reads) != 0 {
		t.Fatalf("node selector: %v", err)
	}
}
