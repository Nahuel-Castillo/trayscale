package ui

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"deedles.dev/trayscale/internal/gutil"
	"deedles.dev/trayscale/internal/tsutil"
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"tailscale.com/tailcfg"
	"tailscale.com/util/set"
)

const mullvadPageBaseName = "Mullvad Exit Nodes"

var mullvadIcon = gio.NewThemedIconWithDefaultFallbacks("network-workgroup-symbolic")

//go:embed mullvadpage.ui
var mullvadPageXML string

type MullvadPage struct {
	app *App
	row *PageRow

	Page         *adw.StatusPage
	LocationList *gtk.ListBox

	locations map[string]*adw.ExpanderRow // keyed by CountryCode
	cities    map[string]*mullvadCityRow  // keyed by CityCode
}

func NewMullvadPage(a *App, status *tsutil.IPNStatus) *MullvadPage {
	page := MullvadPage{
		app:       a,
		locations: make(map[string]*adw.ExpanderRow),
		cities:    make(map[string]*mullvadCityRow),
	}
	gutil.FillFromUI(&page, mullvadPageXML)

	page.LocationList.SetSortFunc(func(r1, r2 *gtk.ListBoxRow) int {
		e1 := r1.Cast().(*adw.ExpanderRow)
		e2 := r2.Cast().(*adw.ExpanderRow)
		return strings.Compare(e1.Title(), e2.Title())
	})

	return &page
}

func (page *MullvadPage) Widget() gtk.Widgetter {
	return page.Page
}

func (page *MullvadPage) Actions() gio.ActionGrouper {
	return nil
}

func (page *MullvadPage) Init(row *PageRow) {
	page.row = row
	row.SetTitle(mullvadPageBaseName)
	row.SetIcon(mullvadIcon)
	row.Row().AddCSSClass("mullvad")
}

func (page *MullvadPage) Update(s tsutil.Status) bool {
	status, ok := s.(*tsutil.IPNStatus)
	if !ok {
		return true
	}
	if !status.Online() {
		return false
	}

	if !tsutil.CanMullvad(status.NetMap.SelfNode) {
		return false
	}

	// Determine the active exit node and the city it belongs to.
	exitNode := status.ExitNode()
	var exitNodeCityCode string
	if exitNode.Valid() {
		exitNodeCityCode = exitNode.Hostinfo().Location().CityCode()
	}

	// Group Mullvad peers by city.
	cityNodes := make(map[string][]tailcfg.NodeView)
	for _, peer := range status.Peers {
		if tsutil.IsMullvad(peer) {
			cityCode := peer.Hostinfo().Location().CityCode()
			cityNodes[cityCode] = append(cityNodes[cityCode], peer)
		}
	}

	var subtitle string
	var exitNodeCountryCode string
	found := make(set.Set[string])
	for cityCode, nodes := range cityNodes {
		found.Add(cityCode)

		best := tsutil.BestMullvadNode(nodes)
		cityRow := page.getCityRow(best)
		cityRow.best = best.StableID()

		active := exitNode.Valid() && tsutil.IsMullvad(exitNode) && cityCode == exitNodeCityCode

		// Subtitle shows the node actually in use when active, otherwise the
		// node that would be selected.
		if active {
			cityRow.row.SetSubtitle(exitNode.Hostinfo().Hostname())
		} else {
			cityRow.row.SetSubtitle(best.Hostinfo().Hostname())
		}

		sw := cityRow.row.ActivatableWidget().(*gtk.Switch)
		sw.SetState(active)
		sw.SetActive(active)

		page.locations[cityRow.country].SetSubtitle("")
		if active {
			subtitle = mullvadLongLocationName(exitNode.Hostinfo().Location())
			exitNodeCountryCode = cityRow.country
		}
	}

	for cityCode, row := range page.cities {
		if !found.Contains(cityCode) {
			delete(page.cities, cityCode)

			locRow := page.locations[row.country]
			locRow.Remove(row.row)
			if locRow.HasCSSClass("empty") {
				delete(page.locations, row.country)
				page.LocationList.Remove(locRow)
			}
		}
	}

	page.row.SetSubtitle(subtitle)
	if exitNodeCountryCode != "" {
		page.locations[exitNodeCountryCode].SetSubtitle("Current exit node location")
	}

	return true
}

func (page *MullvadPage) getLocationRow(loc tailcfg.LocationView) *adw.ExpanderRow {
	if row, ok := page.locations[loc.CountryCode()]; ok {
		return row
	}

	row := adw.NewExpanderRow()
	row.SetTitle(mullvadLocationName(loc))
	gutil.ExpanderRowListBox(row).SetSortFunc(func(r1, r2 *gtk.ListBoxRow) int {
		sw1 := r1.Cast().(*adw.SwitchRow)
		sw2 := r2.Cast().(*adw.SwitchRow)
		c1, s1 := splitCityState(sw1.Title())
		c2, s2 := splitCityState(sw2.Title())
		return cmp.Or(
			strings.Compare(s1, s2),
			strings.Compare(c1, c2),
			strings.Compare(sw1.Subtitle(), sw2.Subtitle()),
		)
	})

	page.locations[loc.CountryCode()] = row
	page.LocationList.Append(row)
	return row
}

func (page *MullvadPage) getCityRow(peer tailcfg.NodeView) *mullvadCityRow {
	loc := peer.Hostinfo().Location()
	cityCode := loc.CityCode()
	if row, ok := page.cities[cityCode]; ok {
		return row
	}

	row := adw.NewSwitchRow()
	row.SetTitle(loc.City())
	row.SetSubtitle(peer.Hostinfo().Hostname())

	cityRow := &mullvadCityRow{
		country:  loc.CountryCode(),
		cityCode: cityCode,
		row:      row,
	}

	sw := row.ActivatableWidget().(*gtk.Switch)
	sw.SetMarginTop(12)
	sw.SetMarginBottom(12)
	sw.ConnectStateSet(func(s bool) bool {
		if s == sw.State() {
			return false
		}

		if s {
			err := tsutil.AdvertiseExitNode(context.TODO(), false)
			if err != nil {
				slog.Error("disable exit node advertisement", "err", err)
				// Continue anyways.
			}
		}

		// Read the current best node at click time; Update keeps cityRow.best
		// fresh on every poll.
		var node tailcfg.StableNodeID
		if s {
			node = cityRow.best
		}
		err := tsutil.ExitNode(context.TODO(), node)
		if err != nil {
			slog.Error("set exit node", "err", err)
			sw.SetActive(!s)
			return true
		}
		return true
	})

	page.getLocationRow(loc).AddRow(row)
	page.cities[cityCode] = cityRow
	return cityRow
}

type mullvadCityRow struct {
	country  string // CountryCode, used to locate/clean up the country expander
	cityCode string
	row      *adw.SwitchRow
	best     tailcfg.StableNodeID // best (highest-priority) node currently in the city
}

func mullvadLongLocationName(loc tailcfg.LocationView) string {
	return fmt.Sprintf(
		"%v %v, %v",
		countryCodeToFlag(loc.CountryCode()),
		loc.City(),
		loc.Country(),
	)
}

func mullvadLocationName(loc tailcfg.LocationView) string {
	return fmt.Sprintf(
		"%v %v",
		countryCodeToFlag(loc.CountryCode()),
		loc.Country(),
	)
}

func countryCodeToFlag(code string) string {
	var raw [2]rune
	for i, c := range code {
		raw[i] = 127397 + c
	}

	return string(raw[:])
}

var cityStateRE = regexp.MustCompile(`^(.*),?\s+([A-Z]{2})$`)

func splitCityState(str string) (city, state string) {
	parts := cityStateRE.FindStringSubmatch(str)
	if len(parts) == 0 {
		return str, ""
	}
	return parts[1], parts[2]
}
