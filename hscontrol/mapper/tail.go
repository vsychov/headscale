package mapper

import (
	"fmt"
	"net/netip"
	"strconv"
	"time"

	"github.com/juanfont/headscale/hscontrol/policy"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/samber/lo"
	"tailscale.com/tailcfg"
)

func tailNodes(
	nodes types.Nodes,
	pol *policy.ACLPolicy,
	dnsConfig *tailcfg.DNSConfig,
	baseDomain string,
) ([]*tailcfg.Node, error) {
	tNodes := make([]*tailcfg.Node, len(nodes))

	for index, node := range nodes {
		node, err := tailNode(
			node,
			pol,
			dnsConfig,
			baseDomain,
		)
		if err != nil {
			return nil, err
		}

		tNodes[index] = node
	}

	return tNodes, nil
}

// tailNode converts a Node into a Tailscale Node. includeRoutes is false for shared nodes
// as per the expected behaviour in the official SaaS.
func tailNode(
	node *types.Node,
	pol *policy.ACLPolicy,
	dnsConfig *tailcfg.DNSConfig,
	baseDomain string,
) (*tailcfg.Node, error) {
	nodeKey, err := node.NodePublicKey()
	if err != nil {
		return nil, err
	}

	machineKey, err := node.MachinePublicKey()
	if err != nil {
		return nil, err
	}

	discoKey, err := node.DiscoPublicKey()
	if err != nil {
		return nil, err
	}

	addrs := node.IPAddresses.Prefixes()

	allowedIPs := append(
		[]netip.Prefix{},
		addrs...) // we append the node own IP, as it is required by the clients

	primaryPrefixes := []netip.Prefix{}

	for _, route := range node.Routes {
		if route.Enabled {
			if route.IsPrimary {
				allowedIPs = append(allowedIPs, netip.Prefix(route.Prefix))
				primaryPrefixes = append(primaryPrefixes, netip.Prefix(route.Prefix))
			} else if route.IsExitRoute() {
				allowedIPs = append(allowedIPs, netip.Prefix(route.Prefix))
			}
		}
	}

	var derp string
	if node.HostInfo.NetInfo != nil {
		derp = fmt.Sprintf("127.3.3.40:%d", node.HostInfo.NetInfo.PreferredDERP)
	} else {
		derp = "127.3.3.40:0" // Zero means disconnected or unknown.
	}

	var keyExpiry time.Time
	if node.Expiry != nil {
		keyExpiry = *node.Expiry
	} else {
		keyExpiry = time.Time{}
	}

	hostname, err := node.GetFQDN(dnsConfig, baseDomain)
	if err != nil {
		return nil, err
	}

	hostInfo := node.GetHostInfo()

	online := node.IsOnline()

	tags, _ := pol.TagsOfNode(node)
	tags = lo.Uniq(append(tags, node.ForcedTags...))

	tNode := tailcfg.Node{
		ID: tailcfg.NodeID(node.ID), // this is the actual ID
		StableID: tailcfg.StableNodeID(
			strconv.FormatUint(node.ID, util.Base10),
		), // in headscale, unlike tailcontrol server, IDs are permanent
		Name: hostname,

		User: tailcfg.UserID(node.UserID),

		Key:       nodeKey,
		KeyExpiry: keyExpiry,

		Machine:    machineKey,
		DiscoKey:   discoKey,
		Addresses:  addrs,
		AllowedIPs: allowedIPs,
		Endpoints:  node.Endpoints,
		DERP:       derp,
		Hostinfo:   hostInfo.View(),
		Created:    node.CreatedAt,

		Tags: tags,

		PrimaryRoutes: primaryPrefixes,

		LastSeen:          node.LastSeen,
		Online:            &online,
		KeepAlive:         true,
		MachineAuthorized: !node.IsExpired(),

		Capabilities: []string{
			tailcfg.CapabilityFileSharing,
			tailcfg.CapabilityAdmin,
			tailcfg.CapabilitySSH,
		},
	}

	return &tNode, nil
}
