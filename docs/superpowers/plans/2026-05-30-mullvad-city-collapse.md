# Mullvad city-collapse Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** En la página "Mullvad Exit Nodes" de Trayscale, mostrar una sola fila por ciudad y, al activarla, fijar como exit node el nodo de mayor `Location.Priority` de esa ciudad.

**Architecture:** Lógica pura y testeable de selección del mejor nodo en `internal/tsutil` (`CompareMullvadNodes` / `BestMullvadNode`). La página `internal/ui/mullvadpage.go` se reescribe para agrupar peers Mullvad por `CityCode` y mantener un `SwitchRow` por ciudad (en vez de uno por nodo) dentro de cada expander de país. Empaquetado vía PKGBUILD local que provee/conflicta con `trayscale`.

**Tech Stack:** Go 1.26, gotk4 (GTK4) + gotk4-adwaita (libadwaita), `tailscale.com/tailcfg` v1.96.5, makepkg/pacman.

**Workspace (ya configurado):** Fork `github.com/Nahuel-Castillo/trayscale` clonado en `~/projects/trayscale` (remotes: `origin` = fork, `upstream` = `DeedleFake/trayscale`). Rama de trabajo: `master`. Identidad git local: `nahuel <nahuelcastillo13@gmail.com>`.

**Convenciones del fork:** comentarios de código y mensajes de commit en **inglés** (estilo upstream `area: description`), distinto del repo `hyprland-custom`.

---

## Task 1: Helpers de selección del mejor nodo en `tsutil` (TDD)

**Files:**
- Create: `internal/tsutil/tsutil_test.go`
- Modify: `internal/tsutil/tsutil.go` (imports + 2 funciones nuevas al final)

- [ ] **Step 1: Escribir el test que falla**

Crear `internal/tsutil/tsutil_test.go`:

```go
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
```

- [ ] **Step 2: Correr el test y verificar que falla**

Run: `cd ~/projects/trayscale && go test ./internal/tsutil/...`
Expected: FAIL — `undefined: CompareMullvadNodes` y `undefined: BestMullvadNode`.

- [ ] **Step 3: Implementar el código mínimo**

En `internal/tsutil/tsutil.go`, agregar `"slices"` al bloque de imports (queda con `"cmp"` y `"slices"`):

```go
import (
	"cmp"
	"slices"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
)
```

Y agregar al final del archivo:

```go
// CompareMullvadNodes orders two Mullvad exit-node peers of the same city
// best-first: the node with the higher Location.Priority wins. Ties are broken
// by hostname so the order is stable and reproducible.
func CompareMullvadNodes(p1, p2 tailcfg.NodeView) int {
	l1 := p1.Hostinfo().Location()
	l2 := p2.Hostinfo().Location()
	return cmp.Or(
		-cmp.Compare(l1.Priority(), l2.Priority()),
		cmp.Compare(p1.Hostinfo().Hostname(), p2.Hostinfo().Hostname()),
	)
}

// BestMullvadNode returns the highest-priority node from a non-empty slice of
// same-city Mullvad peers (the minimum under CompareMullvadNodes). It panics if
// nodes is empty; callers must only pass cities that have at least one node.
func BestMullvadNode(nodes []tailcfg.NodeView) tailcfg.NodeView {
	return slices.MinFunc(nodes, CompareMullvadNodes)
}
```

- [ ] **Step 4: Correr los tests y verificar que pasan**

Run: `cd ~/projects/trayscale && go test ./internal/tsutil/... && go vet ./internal/tsutil/...`
Expected: PASS (ok) y sin warnings de vet.

- [ ] **Step 5: Commit**

```bash
cd ~/projects/trayscale
git add internal/tsutil/tsutil.go internal/tsutil/tsutil_test.go
git commit -m "internal/tsutil: add Mullvad best-node selection helpers

CompareMullvadNodes orders same-city Mullvad peers best-first by
Location.Priority (tie-break by hostname); BestMullvadNode returns the
top one.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Reescribir `mullvadpage.go` a una fila por ciudad

**Files:**
- Modify: `internal/ui/mullvadpage.go`

No hay test unitario (UI GTK; el repo no testea UI). Se valida con `go build`/`go vet` y, opcionalmente, corriendo el binario.

- [ ] **Step 1: Cambiar el campo del map en el struct `MullvadPage`**

Reemplazar (líneas ~28-37):

```go
type MullvadPage struct {
	app *App
	row *PageRow

	Page         *adw.StatusPage
	LocationList *gtk.ListBox

	locations map[string]*adw.ExpanderRow
	exitNodes map[tailcfg.StableNodeID]*mullvadExitNodeRow
}
```

por:

```go
type MullvadPage struct {
	app *App
	row *PageRow

	Page         *adw.StatusPage
	LocationList *gtk.ListBox

	locations map[string]*adw.ExpanderRow // keyed by CountryCode
	cities    map[string]*mullvadCityRow  // keyed by CityCode
}
```

- [ ] **Step 2: Actualizar la inicialización en `NewMullvadPage`**

Reemplazar (líneas ~39-44):

```go
	page := MullvadPage{
		app:       a,
		locations: make(map[string]*adw.ExpanderRow),
		exitNodes: make(map[tailcfg.StableNodeID]*mullvadExitNodeRow),
	}
```

por:

```go
	page := MullvadPage{
		app:       a,
		locations: make(map[string]*adw.ExpanderRow),
		cities:    make(map[string]*mullvadCityRow),
	}
```

- [ ] **Step 3: Reescribir `Update` para agrupar por ciudad**

Reemplazar todo el método `Update` (líneas ~71-132) por:

```go
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

		active := exitNode.Valid() && cityCode == exitNodeCityCode

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

		loc := best.Hostinfo().Location()
		page.locations[loc.CountryCode()].SetSubtitle("")
		if active {
			subtitle = mullvadLongLocationName(exitNode.Hostinfo().Location())
			exitNodeCountryCode = loc.CountryCode()
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
```

- [ ] **Step 4: Reemplazar `getExitNodeRow` por `getCityRow` y el struct de fila**

Reemplazar `getExitNodeRow` (líneas ~158-206) y el struct `mullvadExitNodeRow` (líneas ~208-211) por:

```go
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
```

Nota: `getLocationRow` toma una `tailcfg.LocationView`; acá se le pasa `loc`. El resto del archivo (`getLocationRow`, `splitCityState`, `mullvadLocationName`, `mullvadLongLocationName`, `countryCodeToFlag`, imports) queda igual.

- [ ] **Step 5: Compilar y verificar**

Run: `cd ~/projects/trayscale && go build ./... && go vet ./...`
Expected: compila sin errores ni warnings. (Si `go vet` se queja de un import sin usar, revisar que `tailcfg` y `set` sigan usándose — lo están: `tailcfg.NodeView`/`StableNodeID` y `set.Set[string]`.)

- [ ] **Step 6 (opcional, recomendado): Sanity visual del binario local**

La app es single-instance (GApplication), así que hay que cerrar la instancia en ejecución para probar la nueva:

```bash
cd ~/projects/trayscale
go build -o /tmp/trayscale-dev ./cmd/trayscale
killall trayscale            # cierra la instancia oficial en el tray
/tmp/trayscale-dev           # correr la nueva; abrir la página Mullvad
```
Verificar a ojo: 1 fila por ciudad; activar una conecta al mejor nodo. Cerrar con Ctrl-C. (La aceptación visual formal la hace el usuario en Task 4.)

- [ ] **Step 7: Commit**

```bash
cd ~/projects/trayscale
git add internal/ui/mullvadpage.go
git commit -m "internal/ui: collapse Mullvad exit nodes to one row per city

Each country expander now shows one SwitchRow per city instead of one
per node. Activating a city sets the highest-priority node (via
tsutil.BestMullvadNode) as the exit node. The row is on when the active
exit node is in that city, and its subtitle shows the node in use.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: PKGBUILD local

**Files:**
- Create: `packaging/PKGBUILD`

- [ ] **Step 1: Crear `packaging/PKGBUILD`**

```bash
# Maintainer: Nahuel Castillo <nahuelcastillo13@gmail.com>
# Fork of trayscale: collapses Mullvad exit nodes to one row per city.
pkgname=trayscale-mullvadcity
pkgver=0.18.9
pkgrel=1
pkgdesc="Trayscale fork: Mullvad exit nodes collapsed to one row per city (auto-selects best node)"
arch=('x86_64')
url="https://github.com/Nahuel-Castillo/trayscale"
license=('MIT')
depends=('gtk4' 'libadwaita>=1:1.7')
makedepends=('go' 'git' 'pkgconf')
provides=('trayscale')
conflicts=('trayscale')
source=("git+https://github.com/Nahuel-Castillo/trayscale.git#branch=master")
sha256sums=('SKIP')

pkgver() {
	cd "$srcdir/trayscale"
	git describe --tags --always | sed 's/^v//;s/-/./g'
}

build() {
	cd "$srcdir/trayscale"
	export CGO_ENABLED=1
	export GOFLAGS="-mod=readonly -modcacherw"
	local version
	version="$(git describe --tags --always)"
	go build -v -trimpath \
		-ldflags="-w -s -X 'deedles.dev/trayscale/internal/metadata.version=$version'" \
		-o trayscale ./cmd/trayscale
}

package() {
	cd "$srcdir/trayscale"
	install -Dm755 trayscale "$pkgdir/usr/bin/trayscale"
	install -Dm644 dev.deedles.Trayscale.png "$pkgdir/usr/share/icons/hicolor/256x256/apps/dev.deedles.Trayscale.png"
	install -Dm644 dev.deedles.Trayscale.desktop "$pkgdir/usr/share/applications/dev.deedles.Trayscale.desktop"
	install -Dm644 dev.deedles.Trayscale.metainfo.xml "$pkgdir/usr/share/metainfo/dev.deedles.Trayscale.metainfo.xml"
	install -Dm644 dev.deedles.Trayscale.gschema.xml "$pkgdir/usr/share/glib-2.0/schemas/dev.deedles.Trayscale.gschema.xml"
}
```

- [ ] **Step 2: Chequeo de sintaxis bash del PKGBUILD**

Run: `bash -n ~/projects/trayscale/packaging/PKGBUILD`
Expected: sin salida (sintaxis válida). (La validación real ocurre en Task 4 con `makepkg`.)

- [ ] **Step 3: Commit**

```bash
cd ~/projects/trayscale
git add packaging/PKGBUILD
git commit -m "packaging: add local PKGBUILD for trayscale-mullvadcity

Builds from this fork's master and installs as a package that
provides/conflicts trayscale, so pacman -Syu never replaces it with the
official package.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Push, build del paquete, instalación y verificación end-to-end

**Files:** ninguno (acciones de build/install/verify).

- [ ] **Step 1: Push de la rama master al fork**

El PKGBUILD compila desde `git+...#branch=master`, así que el código debe estar en el fork antes de empaquetar.

```bash
cd ~/projects/trayscale
git push origin master
```
Expected: push OK (incluye los commits de Task 1–3 y los docs).

- [ ] **Step 2: Build e instalación con makepkg**

```bash
cd ~/projects/trayscale/packaging
makepkg -si
```
Expected: clona el fork, compila, y pide sudo para instalar `trayscale-mullvadcity`, removiendo el `trayscale` oficial (por `conflicts`). Confirmar el reemplazo cuando lo pregunte.

- [ ] **Step 3: Verificar la instalación**

Run: `pacman -Qi trayscale-mullvadcity | grep -E 'Version|Provides|Conflicts'`
Expected: el paquete instalado provee y conflicta con `trayscale`.

- [ ] **Step 4: Reiniciar Trayscale y aceptación visual (usuario)**

```bash
killall trayscale 2>/dev/null
trayscale &
```
Verificar a ojo en la página "Mullvad Exit Nodes":
1. Hay **1 fila por ciudad** (ej. "Los Angeles, CA" una sola vez), no una por nodo.
2. Activar "Los Angeles, CA" → la fila queda ON y el subtítulo muestra el hostname elegido.
3. En otra terminal: `tailscale status | head` — el exit node activo es el nodo LAX de mayor `Priority`.
4. Desactivar → sin exit node.

- [ ] **Step 5: Finalizar la rama de desarrollo**

Invocar la skill `superpowers:finishing-a-development-branch` para decidir el cierre (en este flujo single-branch: los commits ya están en `master` y pusheados; confirmar estado limpio con `git -C ~/projects/trayscale status`).

---

## Notas de mantenimiento (futuras actualizaciones del upstream)

Para traer cambios de upstream al fork más adelante:

```bash
cd ~/projects/trayscale
git fetch upstream
git rebase upstream/master     # reaplica los commits del feature
git push --force-with-lease origin master
cd packaging && makepkg -si    # reinstala la versión actualizada
```
Si hay conflictos en `mullvadpage.go`, resolverlos preservando la lógica de agrupado por ciudad descrita en el spec.
