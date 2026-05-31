package tsutil

import (
	"testing"

	"tailscale.com/tailcfg"
)

// mullvadNode builds a Mullvad-style NodeView for tests. All helpers operate on
// nodes of a single city, so CityCode/City are fixed and only hostname/priority
// vary.
func mullvadNode(stableID, hostname string, priority int) tailcfg.NodeView {
	return (&tailcfg.Node{
		StableID: tailcfg.StableNodeID(stableID),
		Hostinfo: (&tailcfg.Hostinfo{
			Hostname: hostname,
			Location: &tailcfg.Location{
				City:     "Los Angeles, CA",
				CityCode: "LAX",
				Priority: priority,
			},
		}).View(),
	}).View()
}

func TestCompareMullvadNodes_HigherPriorityFirst(t *testing.T) {
	high := mullvadNode("n1", "us-lax-wg-001", 10)
	low := mullvadNode("n2", "us-lax-wg-002", 5)

	if CompareMullvadNodes(high, low) >= 0 {
		t.Errorf("higher priority node should sort first (got >= 0)")
	}
	if CompareMullvadNodes(low, high) <= 0 {
		t.Errorf("lower priority node should sort after (got <= 0)")
	}
}

func TestCompareMullvadNodes_TieBreakByHostname(t *testing.T) {
	a := mullvadNode("n1", "us-lax-wg-001", 5)
	b := mullvadNode("n2", "us-lax-wg-002", 5)

	if CompareMullvadNodes(a, b) >= 0 {
		t.Errorf("equal priority: lower hostname should sort first (got >= 0)")
	}
	if CompareMullvadNodes(b, a) <= 0 {
		t.Errorf("equal priority: higher hostname should sort after (got <= 0)")
	}
}

func TestBestMullvadNode_HighestPriority(t *testing.T) {
	nodes := []tailcfg.NodeView{
		mullvadNode("n1", "us-lax-wg-003", 5),
		mullvadNode("n2", "us-lax-wg-001", 10), // highest priority
		mullvadNode("n3", "us-lax-wg-002", 5),
	}

	best := BestMullvadNode(nodes)
	if got := best.Hostinfo().Hostname(); got != "us-lax-wg-001" {
		t.Errorf("BestMullvadNode = %q, want us-lax-wg-001", got)
	}
}

func TestBestMullvadNode_TieBreakByHostname(t *testing.T) {
	nodes := []tailcfg.NodeView{
		mullvadNode("n1", "us-lax-wg-002", 0),
		mullvadNode("n2", "us-lax-wg-001", 0),
	}

	best := BestMullvadNode(nodes)
	if got := best.Hostinfo().Hostname(); got != "us-lax-wg-001" {
		t.Errorf("BestMullvadNode tie-break = %q, want us-lax-wg-001", got)
	}
}

func TestBestMullvadNode_Single(t *testing.T) {
	nodes := []tailcfg.NodeView{mullvadNode("n1", "us-lax-wg-001", 0)}

	best := BestMullvadNode(nodes)
	if got := best.StableID(); got != "n1" {
		t.Errorf("BestMullvadNode single = %q, want n1", got)
	}
}
