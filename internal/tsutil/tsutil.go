package tsutil

import (
	"cmp"
	"slices"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
)

const AdminDashboardURL = "https://tailscale.com/admin"

// IsMullvad returns true if peer is a Mullvad exit node.
func IsMullvad(peer tailcfg.NodeView) bool {
	return peer.Tags().ContainsFunc(func(tag string) bool {
		return tag == "tag:mullvad-exit-node"
	})
}

// CanMullvad returns true if peer is allowed to access Mullvad exit
// nodes.
func CanMullvad(peer tailcfg.NodeView) bool {
	return peer.HasCap("mullvad")
}

// CompareLocations alphabestically compares the countries and then,
// if necessary, cities of two Locations.
func CompareLocations(loc1, loc2 tailcfg.LocationView) int {
	return cmp.Or(
		cmp.Compare(loc1.Country(), loc2.Country()),
		cmp.Compare(loc1.City(), loc2.City()),
	)
}

// ComparePeers compares two peers. It does so by location if
// available, then by hostname. It returns the peers in a
// deterministic order if their locations or hostnames are identical,
// so the result of calling this is never 0. To determine if peers are
// the same, compare their IDs manually.
func ComparePeers(p1, p2 tailcfg.NodeView) int {
	i1 := p1.Hostinfo()
	i2 := p2.Hostinfo()

	loc := 0
	if i1.Location().Valid() && i2.Location().Valid() {
		loc = CompareLocations(i1.Location(), i2.Location())
	}
	return cmp.Or(
		loc,
		cmp.Compare(i1.Hostname(), i2.Hostname()),
		cmp.Compare(p1.ID(), p2.ID()),
	)
}

// CompareWaitingFiles compares two incoming files first by name and
// then by size.
func CompareWaitingFiles(f1, f2 apitype.WaitingFile) int {
	return cmp.Or(
		cmp.Compare(f1.Name, f2.Name),
		cmp.Compare(f1.Size, f2.Size),
	)
}

// CompareMullvadNodes orders two Mullvad exit-node peers of the same city
// best-first: the node with the higher Location.Priority wins. Ties are broken
// by hostname so the order is stable and reproducible.
func CompareMullvadNodes(p1, p2 tailcfg.NodeView) int {
	h1 := p1.Hostinfo()
	h2 := p2.Hostinfo()
	return cmp.Or(
		-cmp.Compare(h1.Location().Priority(), h2.Location().Priority()),
		cmp.Compare(h1.Hostname(), h2.Hostname()),
	)
}

// BestMullvadNode returns the highest-priority node from a non-empty slice of
// same-city Mullvad peers (the minimum under CompareMullvadNodes). It panics if
// nodes is empty; callers must only pass cities that have at least one node.
func BestMullvadNode(nodes []tailcfg.NodeView) tailcfg.NodeView {
	return slices.MinFunc(nodes, CompareMullvadNodes)
}
