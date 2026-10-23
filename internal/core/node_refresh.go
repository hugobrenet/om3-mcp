package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"time"
)

// NodeRefresh is a node action the daemon runs as an execution to refresh a
// cache the reads of the MCP serve, or to scan or dequeue.
type NodeRefresh string

const (
	// NodeRefreshCapabilities rescans the capabilities of the node, the
	// cache list_node_capabilities reads.
	NodeRefreshCapabilities NodeRefresh = "scan/capabilities"
	// NodeRefreshHardware, NodeRefreshDisks and NodeRefreshPackages probe the
	// node, write the cache list_node_hardware, list_node_disks or
	// list_node_packages reads, then send it to the collector.
	NodeRefreshHardware NodeRefresh = "push/asset"
	NodeRefreshDisks    NodeRefresh = "push/disk"
	NodeRefreshPackages NodeRefresh = "push/pkg"
	// NodeScanSCSI scans the SCSI hosts of the node for new devices.
	NodeScanSCSI NodeRefresh = "scsi/scan"
	// NodeDequeue runs the actions the collector queued for the node.
	NodeDequeue NodeRefresh = "dequeue"
)

// scsiPortName is a port name the SCSI scan resolves a host or a target
// from: an FC WWN, with or without 0x, or an iSCSI name.
var scsiPortName = regexp.MustCompile(`^((0x)?[0-9A-Fa-f]{16}|iqn\.[0-9]{4}-[0-9]{2}\.[A-Za-z0-9.:_-]{1,200})$`)

// scsiLUN is a logical unit number, or - for every one.
var scsiLUN = regexp.MustCompile(`^([0-9]{1,10}|-)$`)

type NodeRefreshOptions struct {
	// HBA, Target and LUN narrow a SCSI scan: the host by its port name, a
	// target of that host by its port name, a logical unit of that target.
	HBA    string
	Target string
	LUN    string
	Wait   time.Duration
}

type NodeRefreshResult struct {
	NodeExecActionResult
	// CacheEntriesBefore and CacheEntriesAfter count the entries of the
	// cache the action refreshes, read before and after it.
	CacheEntriesBefore *int   `json:"cache_entries_before,omitempty" jsonschema:"the entries of the cache refreshed, read before the action; absent when the cache was missing or could not be read"`
	CacheEntriesAfter  *int   `json:"cache_entries_after,omitempty" jsonschema:"the entries of the cache refreshed, read again once the execution ended"`
	CacheError         string `json:"cache_error,omitempty" jsonschema:"why the cache could not be read again; the action ran all the same"`
}

// RunNodeRefresh runs one refresh, scan or dequeue on one node and waits a
// bounded time for its execution; for a refresh of a cache, it counts the
// entries of the cache before and after.
func (s *Service) RunNodeRefresh(ctx context.Context, node string, refresh NodeRefresh, options NodeRefreshOptions) (NodeRefreshResult, error) {
	switch refresh {
	case NodeRefreshCapabilities, NodeRefreshHardware, NodeRefreshDisks, NodeRefreshPackages, NodeScanSCSI, NodeDequeue:
	default:
		return NodeRefreshResult{}, fmt.Errorf("unsupported node refresh %q", refresh)
	}
	wait, err := validateNodeAction(node, options.Wait)
	if err != nil {
		return NodeRefreshResult{}, err
	}
	var query url.Values
	if options.HBA != "" || options.Target != "" || options.LUN != "" {
		if refresh != NodeScanSCSI {
			return NodeRefreshResult{}, fmt.Errorf("hba, target and lun apply to the SCSI scan only")
		}
		if query, err = scsiScanQuery(options); err != nil {
			return NodeRefreshResult{}, err
		}
	}
	result := NodeRefreshResult{}
	if before, err := s.cacheEntries(ctx, node, refresh); err == nil && before >= 0 {
		result.CacheEntriesBefore = &before
	}
	exec, err := s.runNodeExec(ctx, node, string(refresh), query, wait)
	if err != nil {
		return NodeRefreshResult{}, err
	}
	result.NodeExecActionResult = exec
	if exec.Completed {
		after, err := s.cacheEntries(ctx, node, refresh)
		switch {
		case err != nil:
			result.CacheError = err.Error()
		case after >= 0:
			result.CacheEntriesAfter = &after
		}
	}
	return result, nil
}

// scsiScanQuery checks the narrowing of a SCSI scan: a target needs its
// host, and a logical unit its target, om3 scanning every host otherwise.
func scsiScanQuery(options NodeRefreshOptions) (url.Values, error) {
	query := url.Values{}
	if options.HBA != "" {
		if !scsiPortName.MatchString(options.HBA) {
			return nil, fmt.Errorf("hba must be the port name of a host: an FC WWN, such as 0x500143802426baf4, or an iSCSI initiator name, iqn.…")
		}
		query.Set("hba", options.HBA)
	}
	if options.Target != "" {
		if options.HBA == "" {
			return nil, fmt.Errorf("target needs hba: without it, om3 scans every host and ignores the target")
		}
		if !scsiPortName.MatchString(options.Target) {
			return nil, fmt.Errorf("target must be the port name of a target: an FC WWN or an iSCSI target name, iqn.…")
		}
		query.Set("target", options.Target)
	}
	if options.LUN != "" {
		if options.Target == "" {
			return nil, fmt.Errorf("lun needs target: without it, om3 scans every logical unit")
		}
		if !scsiLUN.MatchString(options.LUN) {
			return nil, fmt.Errorf("lun must be a logical unit number, or - for every one")
		}
		query.Set("lun", options.LUN)
	}
	return query, nil
}

// cacheEntries counts the entries of the cache a refresh writes, as the
// read serving it reports them; -1 for an action refreshing no cache.
func (s *Service) cacheEntries(ctx context.Context, node string, refresh NodeRefresh) (int, error) {
	switch refresh {
	case NodeRefreshCapabilities:
		list, err := s.ListNodeCapabilities(ctx, ListNodeCapabilitiesOptions{Node: node, Limit: 1})
		return list.ReportedTotal, err
	case NodeRefreshHardware:
		list, err := s.ListNodeHardware(ctx, ListNodeHardwareOptions{Node: node, Limit: 1})
		return list.ReportedTotal, err
	case NodeRefreshDisks:
		list, err := s.ListNodeDisks(ctx, ListNodeDisksOptions{Node: node, Limit: 1})
		return list.ReportedTotal, err
	case NodeRefreshPackages:
		list, err := s.ListNodePackages(ctx, ListNodePackagesOptions{Node: node, Limit: 1})
		return list.ReportedTotal, err
	}
	return -1, nil
}
