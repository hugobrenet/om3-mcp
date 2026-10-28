package tools

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type NodeRefreshInput struct {
	Node        string `json:"node" jsonschema:"the exact name of the node; no wildcard, selector or the _ alias"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type ScanNodeSCSIInput struct {
	Node        string `json:"node" jsonschema:"the exact name of the node; no wildcard, selector or the _ alias"`
	HBA         string `json:"hba,omitempty" jsonschema:"optional port name of one host to scan: an FC WWN, such as 0x500143802426baf4, or an iSCSI initiator name, iqn.…; omitted, every host"`
	Target      string `json:"target,omitempty" jsonschema:"optional port name of one target of that host, an FC WWN or an iSCSI target name; needs hba"`
	LUN         string `json:"lun,omitempty" jsonschema:"optional logical unit number of that target, or - for every one; needs target"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type RefreshResourceInfoInput struct {
	Path            string   `json:"path" jsonschema:"the exact canonical OpenSVC svc or vol object path; no wildcard or selector"`
	Node            string   `json:"node" jsonschema:"the exact name of the node of the instance; no wildcard, selector or the _ alias"`
	Rids            []string `json:"rids,omitempty" jsonschema:"optional exact resource ids, at most 50; omitted, every resource of the instance"`
	ConfigUpdatedAt string   `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time: leave it out unless this call follows update_object_config in this turn, then the last_modified it gave; the status or frozen times of a listing are not it. The daemon refuses with 409 a node whose configuration is older"`
	WaitSeconds     int      `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type NodeRefreshOutput = core.NodeRefreshResult

// nodeRefreshFollowUp tells the agent what the result of a node refresh
// holds.
const nodeRefreshFollowUp = "The node runs it at once: the MCP waits for the end of the execution, wait_seconds, and returns completed with its state, exit_code and error, log_tail for a failure; an execution still running returns completed false, to follow with list_daemon_executions and get_node_logs on the node, filtered on exec_id. Requires root."

// pushFollowUp says what a refresh of an inventory cache returns, and that a
// failure to send it to the collector is no failure of the refresh.
const pushFollowUp = "The node probes, writes the cache, then sends it to the collector: a node without collector, or one the collector refuses, fails the execution after the cache is written, exit_code 1, which is no failure of the refresh; the om command writes that error out of the journal, so log_tail may be empty and the cause unsaid. The daemon gives no date for the cache: cache_entries_before and cache_entries_after count its entries, unchanged when the inventory is, and the read of the cache shows its content now. To refresh the inventory of many nodes in the collector, queue_node_action of oc3-mcp acts by filter. "

// RegisterNodeRefreshTools registers the tools refreshing the caches a node
// serves, scanning its SCSI hosts and running its collector queue. They are
// registered only when actions are enabled.
func RegisterNodeRefreshTools(registrar *Registrar, service *core.Service) error {
	run := func(ctx context.Context, node string, refresh core.NodeRefresh, options core.NodeRefreshOptions, waitSeconds int) (*mcp.CallToolResult, NodeRefreshOutput, error) {
		options.Wait = time.Duration(waitSeconds) * time.Second
		result, err := service.RunNodeRefresh(ctx, node, refresh, options)
		if err != nil {
			return nil, NodeRefreshOutput{}, err
		}
		return nil, result, nil
	}
	for _, declared := range []struct {
		refresh     core.NodeRefresh
		name, title string
		description string
	}{
		{
			refresh: core.NodeRefreshCapabilities, name: "refresh_node_capabilities", title: "Refresh node capabilities",
			description: "Rescan the capabilities of one OpenSVC node, the drivers and features its system allows, and write the cache list_node_capabilities reads, which the daemon writes otherwise at its start and when the packages change: after installing a driver dependency, or when the read answers 404 waiting for cached value. Returns cache_entries_before and cache_entries_after. ",
		},
		{
			refresh: core.NodeRefreshHardware, name: "refresh_node_hardware", title: "Refresh node hardware",
			description: "Refresh the hardware and system inventory of one OpenSVC node, the cache list_node_hardware reads, when it is stale or missing, the read answering 404 waiting for cached value. " + pushFollowUp,
		},
		{
			refresh: core.NodeRefreshDisks, name: "refresh_node_disks", title: "Refresh node disks",
			description: "Refresh the disk inventory of one OpenSVC node, the cache list_node_disks reads, when it is stale or missing; after scan_node_scsi, it makes a new LUN show in list_node_disks. " + pushFollowUp,
		},
		{
			refresh: core.NodeRefreshPackages, name: "refresh_node_packages", title: "Refresh node packages",
			description: "Refresh the package inventory of one OpenSVC node, the cache list_node_packages reads, when it is stale or missing. " + pushFollowUp,
		},
	} {
		declared := declared
		if err := addTool(registrar, &mcp.Tool{
			Name: declared.name, Title: declared.title,
			Description: declared.description + nodeRefreshFollowUp,
			Annotations: actionAnnotations(false, true),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, input NodeRefreshInput) (*mcp.CallToolResult, NodeRefreshOutput, error) {
			return run(ctx, input.Node, declared.refresh, core.NodeRefreshOptions{}, input.WaitSeconds)
		}); err != nil {
			return err
		}
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "scan_node_scsi", Title: "Scan node SCSI",
		Description: "Scan the SCSI hosts of one OpenSVC node for new devices, such as a LUN a storage array just mapped: every host, or one host by its port name, an FC WWN or an iSCSI initiator name, one of its targets, one logical unit of it. A scan adds what the kernel finds and removes nothing. " +
			"list_node_disks shows a new LUN once refresh_node_disks rebuilt its cache: call it after the scan. " + nodeRefreshFollowUp,
		Annotations: actionAnnotations(false, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ScanNodeSCSIInput) (*mcp.CallToolResult, NodeRefreshOutput, error) {
		return run(ctx, input.Node, core.NodeScanSCSI, core.NodeRefreshOptions{HBA: input.HBA, Target: input.Target, LUN: input.LUN}, input.WaitSeconds)
	}); err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "dequeue_node_actions", Title: "Dequeue node actions",
		Description: "Run now the actions the collector queued for one OpenSVC node, rather than at its next read of the queue: the node reads its queue on the collector, acknowledges it, then runs each entry as om node <args> or om svc -s <service> <args>, and sends back their results. " +
			"The queue may hold any om command an administrator of the collector queued, service actions or array disk creations among them, and the MCP does not read it before running it: read it first with list_node_actions of oc3-mcp. A node without collector fails with exit_code 1, the om command writing its error, collector not configured, out of the journal: log_tail may hold only the fetch it started. " + nodeRefreshFollowUp,
		Annotations: actionAnnotations(true, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input NodeRefreshInput) (*mcp.CallToolResult, NodeRefreshOutput, error) {
		return run(ctx, input.Node, core.NodeDequeue, core.NodeRefreshOptions{}, input.WaitSeconds)
	}); err != nil {
		return err
	}
	return addTool(registrar, &mcp.Tool{
		Name: "refresh_resource_info", Title: "Refresh resource info",
		Description: "Recompute the resource information cache of the instance of one OpenSVC svc or vol object on one node, or of some of its resources, the key-values list_resource_info reads, such as the device of a disk or the image of a container, when they are stale or missing. " +
			"The node runs it at once: the MCP waits for the end of the execution, wait_seconds, and returns completed with its state, exit_code and error, log_tail for a failure. Requires operator access on the object namespace.",
		Annotations: actionAnnotations(false, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RefreshResourceInfoInput) (*mcp.CallToolResult, InstanceActionOutput, error) {
		result, err := service.RunInstanceAction(ctx, input.Path, input.Node, core.InstanceActionInfo, core.InstanceActionOptions{
			Rids: input.Rids, ConfigUpdatedAt: input.ConfigUpdatedAt, Wait: time.Duration(input.WaitSeconds) * time.Second,
		})
		if err != nil {
			return nil, InstanceActionOutput{}, err
		}
		return nil, result, nil
	})
}
