package core

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	maxTaskEnv      = 32
	maxTaskEnvBytes = 4096
)

// taskRunReadAgainFor bounds the reads of the last run of a task once the
// execution ended; a variable for the tests.
var taskRunReadAgainFor = 5 * time.Second

// taskEnvKey is the form of the name of a variable a run exports.
var taskEnvKey = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// TaskRun is the last run of one task, read again after a run.
type TaskRun struct {
	RID string `json:"rid" jsonschema:"the task"`
	// OutputLogged is the log keyword of the task.
	OutputLogged bool `json:"output_logged" jsonschema:"whether the task logs its output, its log keyword: get_node_logs on the node, filtered on the exec_id, then gives its stdout and stderr; false, only its exit code is kept"`
	// ThisRun tells whether the last run read is the one of this call.
	ThisRun         bool   `json:"this_run" jsonschema:"whether the last run read is the one of this call, its session: false, the task recorded no run of it, refused before it ran, such as by run_requires or max_parallel, or still running"`
	LastRunExitCode *int   `json:"last_run_exitcode,omitempty" jsonschema:"the exit code of the last run of the task, 0 succeeded"`
	LastRunAt       string `json:"last_run_at,omitempty" jsonschema:"when the last run of the task ended"`
}

// validateTaskEnv checks the variables a run exports: KEY=VALUE, a name of
// capitals, digits and underscores, bounded. A name changing what the command
// runs, rather than what it does, is refused: PATH, LD_*, OPENSVC_*.
func validateTaskEnv(env []string) error {
	if len(env) > maxTaskEnv {
		return fmt.Errorf("at most %d env variables are accepted", maxTaskEnv)
	}
	seen := make(map[string]bool, len(env))
	for index, variable := range env {
		key, value, ok := strings.Cut(variable, "=")
		if !ok || !taskEnvKey.MatchString(key) {
			return fmt.Errorf("env %d must be KEY=VALUE, the key of capitals, digits and underscores", index)
		}
		if key == "PATH" || strings.HasPrefix(key, "LD_") || strings.HasPrefix(key, "OPENSVC_") {
			return fmt.Errorf("env %s is refused: PATH, LD_* and OPENSVC_* change what the command runs", key)
		}
		if len(variable) > maxTaskEnvBytes || strings.ContainsAny(value, "\x00\n\r") {
			return fmt.Errorf("env %s must be at most %d bytes, on one line", key, maxTaskEnvBytes)
		}
		if seen[key] {
			return fmt.Errorf("env %s is given twice", key)
		}
		seen[key] = true
	}
	return nil
}

// refuseTaskRun checks the tasks a run names before the call: each a task
// of the object, and none whose author asked a human to confirm its run,
// confirmation=true, which only om <path> instance run --confirm on the node
// gives. It returns the log keyword of each task.
func (s *Service) refuseTaskRun(ctx context.Context, reference ClusterObjectReference, node string, rids []string) (map[string]bool, error) {
	if err := s.refuseUnknownResources(ctx, reference, "run", rids); err != nil {
		return nil, err
	}
	keywords := make([]string, 0, 4*len(rids))
	for _, rid := range rids {
		for _, option := range []string{"confirmation", "log"} {
			keywords = append(keywords, rid+"."+option, rid+"."+option+"@"+node)
		}
	}
	config, err := s.GetObjectConfig(ctx, GetObjectConfigOptions{Path: reference.Path, Keywords: keywords, Limit: maxGetObjectConfigLimit})
	if err != nil {
		return nil, fmt.Errorf("run tasks of %s on %s: read their confirmation and log keywords: %w", reference.Path, node, err)
	}
	// A value scoped to the node wins over the value of the section.
	values := make(map[string]string, len(config.Items))
	for _, item := range config.Items {
		if item.Value == "" {
			continue
		}
		key, scope, scoped := strings.Cut(item.Keyword, "@")
		if scoped && scope != node {
			continue
		}
		if _, set := values[key]; !set || scoped {
			values[key] = item.Value
		}
	}
	logged := make(map[string]bool, len(rids))
	for _, rid := range rids {
		// A confirmation the MCP cannot read as false is taken as asked.
		if confirmation, ok := taskBool(values[rid+".confirmation"]); !ok || confirmation {
			return nil, fmt.Errorf("run %s of %s: its author asks a human to confirm each run, confirmation=true; the MCP does not confirm for them: an operator runs om %s instance run --rid %s --confirm on %s", rid, reference.Path, reference.Path, rid, node)
		}
		logged[rid], _ = taskBool(values[rid+".log"])
	}
	return logged, nil
}

// taskBool reads a boolean keyword value as om3 does, empty for false; ok is
// false for a value om3 would refuse, or a reference the MCP does not
// evaluate.
func taskBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes", "t", "true", "1":
		return true, true
	case "n", "no", "f", "false", "0", "0.0", "", "none", "[]", "{}":
		return false, true
	}
	return false, false
}

// readTaskRuns reads the last run of each task in the instance status. The
// daemon publishes it once the run ends: when the execution ended, the MCP
// reads it again a few seconds until the run of its session shows.
func (s *Service) readTaskRuns(ctx context.Context, reference ClusterObjectReference, node, session string, rids []string, logged map[string]bool, ended bool) ([]TaskRun, error) {
	deadline := time.Now().Add(taskRunReadAgainFor)
	for attempt := 0; ; attempt++ {
		status, err := s.GetInstanceStatus(ctx, GetInstanceStatusOptions{Path: reference.Path, Node: node})
		if err != nil {
			return nil, fmt.Errorf("read the instance status: %w", err)
		}
		runs := make([]TaskRun, 0, len(rids))
		for _, rid := range rids {
			run := TaskRun{RID: rid, OutputLogged: logged[rid]}
			if status.Status != nil {
				if detail, ok := status.Status.Resources[rid]; ok {
					run.LastRunAt, _ = detail.Info["last_run_at"].(string)
					if number, ok := detail.Info["last_run_exitcode"].(json.Number); ok {
						if code, err := number.Int64(); err == nil {
							exitCode := int(code)
							run.LastRunExitCode = &exitCode
						}
					}
					lastSession, _ := detail.Info["last_run_session_id"].(string)
					run.ThisRun = session != "" && strings.EqualFold(lastSession, session)
				}
			}
			runs = append(runs, run)
		}
		if !ended || !slices.ContainsFunc(runs, func(run TaskRun) bool { return !run.ThisRun }) || !time.Now().Before(deadline) {
			return runs, nil
		}
		timer := time.NewTimer(refreshPollDelay(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return runs, nil
		case <-timer.C:
		}
	}
}
