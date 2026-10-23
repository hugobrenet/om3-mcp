package core

import (
	"context"
	"fmt"
	"net/url"
	"sort"
)

const maxNetworkReservations = 1000

type NetworkReservation struct {
	Network string `json:"network" jsonschema:"the cluster backend network"`
	IP      string `json:"ip" jsonschema:"the address reserved"`
	Path    string `json:"path" jsonschema:"the object the address is reserved for"`
	RID     string `json:"rid" jsonschema:"the resource the address is reserved for"`
}

type NodeNetworkReservationList struct {
	Provenance    Provenance           `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node          string               `json:"node" jsonschema:"the node read"`
	NetworkFilter string               `json:"network_filter,omitempty" jsonschema:"the network read, empty for every network"`
	Total         int                  `json:"total" jsonschema:"the number of reservations the node holds"`
	Count         int                  `json:"count" jsonschema:"the number of reservations returned"`
	Reservations  []NetworkReservation `json:"reservations" jsonschema:"the reservations, by network then address"`
	Truncated     bool                 `json:"truncated" jsonschema:"whether reservations were left out after 1000"`
}

type daemonNetworkReservationList struct {
	Items []struct {
		Network string `json:"network"`
		IP      string `json:"ip"`
		Path    string `json:"path"`
		RID     string `json:"rid"`
	} `json:"items"`
}

// ListNodeNetworkReservations reads the addresses one node holds reserved on
// the cluster backend networks, the daemon relaying the read to it. They are
// read from the reservation store: an address shows as soon as it is drawn,
// before the status of its object does.
func (s *Service) ListNodeNetworkReservations(ctx context.Context, node string, network string) (NodeNetworkReservationList, error) {
	if err := validateNodeTarget(node); err != nil {
		return NodeNetworkReservationList{}, err
	}
	query := url.Values{}
	if network != "" {
		name, err := validateNetworkName(network)
		if err != nil {
			return NodeNetworkReservationList{}, err
		}
		network = name
		query.Set("name", network)
	}
	var response daemonNetworkReservationList
	if err := s.client.GetJSON(ctx, fmt.Sprintf("/api/node/name/%s/network/reservation", node), query, &response); err != nil {
		return NodeNetworkReservationList{}, fmt.Errorf("list the network reservations of %s: %w", node, err)
	}
	reservations := make([]NetworkReservation, 0, len(response.Items))
	for _, item := range response.Items {
		reservations = append(reservations, NetworkReservation{Network: item.Network, IP: item.IP, Path: item.Path, RID: item.RID})
	}
	sort.SliceStable(reservations, func(i, j int) bool {
		if reservations[i].Network != reservations[j].Network {
			return reservations[i].Network < reservations[j].Network
		}
		return reservations[i].IP < reservations[j].IP
	})
	result := NodeNetworkReservationList{Node: node, NetworkFilter: network, Total: len(reservations), Reservations: reservations}
	if len(reservations) > maxNetworkReservations {
		result.Reservations, result.Truncated = reservations[:maxNetworkReservations], true
	}
	result.Count = len(result.Reservations)
	result.Provenance = s.newProvenance()
	return result, nil
}
