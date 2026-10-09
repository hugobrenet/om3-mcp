package tools

import (
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestNodeDiagnosticToolsRequireNode(t *testing.T) {
	schemas := map[string]func() (*jsonschema.Schema, error){
		"get_node_config":            func() (*jsonschema.Schema, error) { return jsonschema.For[GetNodeConfigInput](nil) },
		"list_node_properties":       func() (*jsonschema.Schema, error) { return jsonschema.For[ListNodePropertiesInput](nil) },
		"list_node_hardware":         func() (*jsonschema.Schema, error) { return jsonschema.For[ListNodeHardwareInput](nil) },
		"list_node_packages":         func() (*jsonschema.Schema, error) { return jsonschema.For[ListNodePackagesInput](nil) },
		"list_node_capabilities":     func() (*jsonschema.Schema, error) { return jsonschema.For[ListNodeCapabilitiesInput](nil) },
		"list_node_drivers":          func() (*jsonschema.Schema, error) { return jsonschema.For[ListNodeDriversInput](nil) },
		"get_node_daemon_metrics":    func() (*jsonschema.Schema, error) { return jsonschema.For[GetNodeDaemonMetricsInput](nil) },
		"list_node_disks":            func() (*jsonschema.Schema, error) { return jsonschema.For[ListNodeDisksInput](nil) },
		"get_node_san_topology":      func() (*jsonschema.Schema, error) { return jsonschema.For[GetNodeSANTopologyInput](nil) },
		"list_node_ip_addresses":     func() (*jsonschema.Schema, error) { return jsonschema.For[ListNodeIPAddressesInput](nil) },
		"probe_node_reachability":    func() (*jsonschema.Schema, error) { return jsonschema.For[ProbeNodeReachabilityInput](nil) },
		"list_daemon_executions":     func() (*jsonschema.Schema, error) { return jsonschema.For[ListDaemonExecutionsInput](nil) },
		"list_daemon_orchestrations": func() (*jsonschema.Schema, error) { return jsonschema.For[ListDaemonOrchestrationsInput](nil) },
		"list_dns_records":           func() (*jsonschema.Schema, error) { return jsonschema.For[ListDNSRecordsInput](nil) },
	}
	for name, schemaFor := range schemas {
		schema, err := schemaFor()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.Contains(schema.Required, "node") {
			t.Errorf("%s does not require node: %v", name, schema.Required)
		}
	}
}
