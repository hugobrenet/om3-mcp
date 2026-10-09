package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type ListNetworksInput struct {
	Name string `json:"name,omitempty" jsonschema:"optional exact network name; omit for every network"`
}

type ListNetworksOutput = core.NetworkList

type ListNetworkIPsInput struct {
	Network    string `json:"network,omitempty" jsonschema:"optional exact network name; omit for the addresses of every network"`
	Node       string `json:"node,omitempty" jsonschema:"optional exact OpenSVC node name; no wildcard, selector or underscore alias"`
	Path       string `json:"path,omitempty" jsonschema:"optional exact OpenSVC object path; no wildcard or selector"`
	SharedOnly bool   `json:"shared_only,omitempty" jsonschema:"optional; true lists only the addresses reported by more than one object resource"`
	Limit      int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 100"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListNetworkIPsOutput = core.NetworkIPList

func RegisterNetworkTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_networks",
			Title: "List networks",
			Description: "Read the OpenSVC cluster backend networks: name, driver type, address range, and the number of addresses used, in total and still free. " +
				"Used counts the node, object and resource entries reporting an address in the range: an address held by the instances of several nodes counts once per node. " +
				"Address counts are decimal strings, since an IPv6 range exceeds 64-bit integers. Reports daemon facts and errors without verdicts. Requires root access to the daemon endpoint.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNetworksInput) (*mcp.CallToolResult, ListNetworksOutput, error) {
			networks, err := service.ListNetworks(ctx, core.ListNetworksOptions{Name: input.Name})
			if err != nil {
				return nil, ListNetworksOutput{}, err
			}
			return nil, networks, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_network_ips",
			Title: "List network IPs",
			Description: "List the addresses the OpenSVC instances report in the cluster backend networks, with the node, object, resource and network of each. " +
				"Each entry counts the other object resources reporting the same address; use shared_only to list only those addresses, which may reveal a conflict. " +
				"The instances of a failover object on several nodes each report its address without sharing it. Filter by network, node or object, and use the cursor to continue a page. " +
				"Requires root access to the daemon endpoint.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNetworkIPsInput) (*mcp.CallToolResult, ListNetworkIPsOutput, error) {
			ips, err := service.ListNetworkIPs(ctx, core.ListNetworkIPsOptions{
				Network: input.Network, Node: input.Node, Path: input.Path, SharedOnly: input.SharedOnly, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListNetworkIPsOutput{}, err
			}
			return nil, ips, nil
		},
	); err != nil {
		return err
	}
	return nil
}
