package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// executionLogLines are the last lines of the journal of a failed execution
// an outcome carries.
const executionLogLines = 20

// ExecutionOutcome is what the MCP read of an execution a node forked for an
// action: whether it ended within the wait, how, and for a failure the end
// of its journal.
type ExecutionOutcome struct {
	// Completed says whether the execution ended within the wait.
	Completed bool    `json:"completed" jsonschema:"whether the execution ended within the wait; false, it still runs, or could not be read"`
	State     string  `json:"state,omitempty" jsonschema:"the state of the execution reported by OpenSVC, when read"`
	ExitCode  *int    `json:"exit_code,omitempty" jsonschema:"the exit code of the execution when it ended: 0 succeeded"`
	Error     *string `json:"error,omitempty" jsonschema:"the bounded error of the execution reported by OpenSVC, when it failed"`
	// ExecutionError is why the execution could not be read: the action is
	// running or ran all the same.
	ExecutionError string `json:"execution_error,omitempty" jsonschema:"why the execution could not be read, such as the root access its read requires, or 410 for an execution the daemon ran and has dropped since; the action was accepted all the same: get_instance_status or get_node_status gives its outcome"`
	// LogTail is the end of the journal of a failed execution, where its
	// cause is: the error of the execution is the exit of the process.
	LogTail      []ExecutionLogLine `json:"log_tail,omitempty" jsonschema:"for an execution that failed, the last 20 lines at most of its journal on the node, in time order, where its cause is; get_node_logs with the node and exec_id gives more"`
	LogTruncated bool               `json:"log_truncated,omitempty" jsonschema:"whether older lines of the journal of the execution, or message content, were left out"`
	LogError     string             `json:"log_error,omitempty" jsonschema:"why the journal of the failed execution could not be read; the execution result stands"`
}

type ExecutionLogLine struct {
	Time    string `json:"time" jsonschema:"the time of the line"`
	Level   string `json:"level,omitempty" jsonschema:"the OpenSVC log level, when given"`
	Message string `json:"message" jsonschema:"the bounded message"`
}

// waitExecution reads the execution until it ends or the wait expires. A
// read refused, such as for a caller without root access, ends the wait with
// the reason: the action runs all the same.
func (s *Service) waitExecution(ctx context.Context, node string, execID string, wait time.Duration) (ExecutionOutcome, error) {
	var outcome ExecutionOutcome
	endpoint := fmt.Sprintf("/api/node/name/%s/daemon/exec/id/%s", node, execID)
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for attempt := 0; ; attempt++ {
		timer := time.NewTimer(refreshPollDelay(attempt))
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return outcome, ctx.Err()
		case <-timer.C:
		}
		var item daemonExecutionResponse
		if err := s.client.GetJSON(waitCtx, endpoint, nil, &item); err != nil {
			if ctx.Err() != nil {
				return outcome, ctx.Err()
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return outcome, nil
			}
			var problem daemonProblem
			if errors.As(err, &problem) && problem.HTTPStatus() == http.StatusForbidden {
				outcome.ExecutionError = "reading the execution requires root access: " + err.Error()
			} else {
				outcome.ExecutionError = err.Error()
			}
			return outcome, nil
		}
		if _, err := validateDaemonExecutionResponse(item); err != nil {
			outcome.ExecutionError = fmt.Sprintf("read execution %s: %s", execID, err)
			return outcome, nil
		}
		execution, _ := daemonExecutionOutput(item)
		outcome.State = execution.State
		if execution.EndedAt != nil {
			outcome.Completed = true
			outcome.ExitCode = execution.ExitCode
			outcome.Error = execution.Error
			if outcome.ExitCode == nil || *outcome.ExitCode != 0 {
				s.readExecutionLog(ctx, node, execID, &outcome)
			}
			return outcome, nil
		}
	}
}

// readExecutionLog gives a failed execution the end of its journal, read
// once, where the cause of the failure is.
func (s *Service) readExecutionLog(ctx context.Context, node string, execID string, outcome *ExecutionOutcome) {
	logs, err := s.GetNodeLogs(ctx, GetNodeLogsOptions{Node: node, ExecID: execID, Lines: executionLogLines})
	if err != nil {
		outcome.LogError = err.Error()
		return
	}
	outcome.LogTail = make([]ExecutionLogLine, 0, len(logs.Entries))
	for _, entry := range logs.Entries {
		outcome.LogTail = append(outcome.LogTail, ExecutionLogLine{Time: entry.Timestamp, Level: entry.Level, Message: entry.Message})
	}
	outcome.LogTruncated = logs.Truncated
}
