# Diseño: colapsar exit nodes Mullvad a una fila por ciudad

- **Fecha:** 2026-05-30
- **Repo:** fork personal `github.com/Nahuel-Castillo/trayscale` (upstream `DeedleFake/trayscale`)
- **Estado:** aprobado (brainstorming) — pendiente de revisión del spec por el usuario

## Problema

En la página "Mullvad Exit Nodes" de Trayscale, cada ciudad de Mullvad aparece
repetida una vez por cada nodo físico que ofrece (`us-lax-wg-001`,
`us-lax-wg-002`, `us-lax-wg-003`, …). Una ciudad como Los Angeles, CA genera
así muchas filas casi idénticas, lo que hace la lista larga y ruidosa.

El usuario quiere ver **una sola opción por ciudad** (ej. "Los Angeles, CA") y
que, al activarla, Trayscale elija internamente el mejor nodo de esa ciudad.

## Objetivo

Reemplazar las N filas por-nodo dentro de cada país por **una fila por ciudad**.
Al activar la fila de una ciudad, fijar como exit node el mejor nodo de esa
ciudad, de forma automática y transparente.

## No-objetivos (YAGNI)

- No se mantiene ninguna forma de elegir manualmente un nodo individual dentro
  de una ciudad (los nodos individuales quedan **ocultos por completo**). Si el
  mejor nodo está caído/saturado, el fallback queda fuera de alcance de este
  cambio (se usaría la CLI).
- No se hace sondeo de red (ping/latencia) para elegir el nodo.
- No se toca el grouping por país ni el resto de la app.

## Decisiones de diseño

1. **"Mejor nodo" = mayor `tailcfg.Location.Priority`.** Es el mismo criterio
   que usa Tailscale para autoseleccionar cuando una preferencia por ubicación
   coincide con más de un exit node ("the node with the highest priority wins").
   El dato ya viene en cada peer, es determinista y no requiere sondear la red.
   **Desempate:** por `Hostname` (orden estable y reproducible).

2. **Una fila por ciudad, nodos individuales ocultos.** La lista pasa a tener,
   dentro de cada expander de país, un `SwitchRow` por ciudad (key = `CityCode`,
   que Tailscale documenta como identificador único de una ubicación dentro del
   tailnet).

3. **Fork personal + PKGBUILD local.** Para que `pacman -Syu` no pise el binario
   con el paquete oficial, se empaqueta con un nombre propio que *provee* y
   *entra en conflicto* con `trayscale`.

## Comportamiento (UI)

Página "Mullvad Exit Nodes":

- Los expanders por país **no cambian** (siguen mostrando ej. "🇺🇸 USA").
- Dentro de cada país: **1 `SwitchRow` por ciudad**.
  - **Título:** nombre de la ciudad (`Location.City()`, ej. "Los Angeles, CA").
  - **Subtítulo:** hostname del nodo que representa a la ciudad:
    - si esa ciudad es el exit node activo → el hostname **real** en uso
      (cubre el caso de un nodo fijado por CLI que no sea el mejor);
    - si no → el hostname del **mejor** nodo (el que se activaría).
  - **Switch ON** si el exit node actual pertenece a esa ciudad (match por
    `CityCode`), aunque se haya fijado por CLI un nodo que no sea el mejor.
- **Activar** la fila → setea como exit node el mejor nodo de esa ciudad.
- **Desactivar** → limpia el exit node (igual que hoy).
- Orden de las ciudades dentro del país: el actual (`splitCityState`: por estado
  y luego ciudad).

## Arquitectura del código

### `internal/tsutil/tsutil.go` — lógica pura (testeable, sin GTK)

```go
// CompareMullvadNodes orders two Mullvad exit-node peers of the same city
// best-first: higher Location.Priority wins; ties are broken by hostname for a
// stable, reproducible order.
func CompareMullvadNodes(p1, p2 tailcfg.NodeView) int

// BestMullvadNode returns the highest-priority node from a non-empty slice of
// same-city Mullvad peers (the minimum under CompareMullvadNodes).
func BestMullvadNode(nodes []tailcfg.NodeView) tailcfg.NodeView
```

`BestMullvadNode` se implementa con `slices.MinFunc(nodes, CompareMullvadNodes)`
(el "mejor" ordena primero).

### `internal/ui/mullvadpage.go` — reescritura acotada

- Reemplazar `exitNodes map[tailcfg.StableNodeID]*mullvadExitNodeRow` por
  `cities map[string]*mullvadCityRow` (key = `CityCode`).
- Nueva struct:
  ```go
  type mullvadCityRow struct {
      country  string             // CountryCode, para ubicar/limpiar el expander
      cityCode string
      row      *adw.SwitchRow
      best     tailcfg.StableNodeID // mejor nodo vigente de la ciudad
  }
  ```
- `Update()`:
  1. Agrupar los peers Mullvad por `CityCode` → `map[string][]tailcfg.NodeView`.
  2. Por cada ciudad: calcular `best := BestMullvadNode(nodos)`; obtener/crear su
     `SwitchRow` bajo el expander de su país (`getLocationRow`, sin cambios);
     refrescar en la struct el `best`, el subtítulo y el estado del switch.
  3. El estado ON/OFF se calcula comparando el `CityCode` del exit node actual
     (`status.ExitNode()`) con el de la ciudad.
  4. Limpiar las ciudades que ya no estén presentes y los expanders de país que
     queden vacíos (misma mecánica que hoy usa con los nodos).
- El handler `ConnectStateSet` del switch lee `cityRow.best` **al momento del
  click** (no captura un peer fijo), así siempre usa el mejor nodo vigente que
  `Update` dejó actualizado en la struct.
- Renombrar `getExitNodeRow` → `getCityRow`; eliminar la struct
  `mullvadExitNodeRow`. Se conservan `getLocationRow`, `splitCityState`,
  `mullvadLocationName`, `mullvadLongLocationName`, `countryCodeToFlag`.

### Convenciones del fork

- **Comentarios de código y mensajes de commit en inglés**, para coincidir con
  el estilo del repo upstream (ej. commits `internal/ui: …`). Esto difiere de la
  convención en español del repo `hyprland-custom`; aplica solo a este fork.

## Casos borde

- Ciudad con un solo nodo → 1 fila, `best` = ese nodo.
- Prioridades iguales o en 0 → desempate por hostname (determinista).
- Exit node fijado por CLI a un nodo no-óptimo de una ciudad → la fila igual
  figura ON y el subtítulo muestra el hostname real en uso.

## Testing

- **`internal/tsutil/tsutil_test.go`** (nuevo): table tests para
  `CompareMullvadNodes` y `BestMullvadNode`:
  - gana el de mayor `Priority`;
  - empate de prioridad → gana el de hostname menor;
  - slice de un solo nodo → lo devuelve;
  - prioridades en 0 → desempate por hostname.
  - Los `NodeView` se construyen con
    `(&tailcfg.Node{Hostinfo: (&tailcfg.Hostinfo{Location: &tailcfg.Location{...}}).View()}).View()`.
- La UI GTK no tiene tests en el repo (consistente con el proyecto) → se valida
  manualmente (ver "Verificación").
- `go build ./...` y `go test ./internal/tsutil/...` deben pasar.

## Fork, build y empaquetado

### Workspace
- Fork: `github.com/Nahuel-Castillo/trayscale` (ya creado).
- Clon local: `~/projects/trayscale` (remotes `origin` = fork,
  `upstream` = `DeedleFake/trayscale`).
- Rama de trabajo: **`master`** del fork (single-branch, sin feature branch).
- Docs de spec/plan: dentro del fork en `docs/superpowers/`.

### Build
Replica de `dist.sh build`:
```bash
go build -v -trimpath \
  -ldflags="-w -s -X 'deedles.dev/trayscale/internal/metadata.version=$version'" \
  -o trayscale ./cmd/trayscale
```
Deps de build: `go`, `git`, `gtk4`, `libadwaita` (presentes; la app ya corre).
Go 1.26.3 local cumple el `go.mod` (`go 1.26.2`).

### PKGBUILD (`packaging/PKGBUILD` dentro del fork)
```bash
pkgname=trayscale-mullvadcity
provides=('trayscale')
conflicts=('trayscale')
makedepends=('go' 'git')
depends=('gtk4' 'libadwaita>=1:1.7')
source=("git+https://github.com/Nahuel-Castillo/trayscale.git#branch=master")
# pkgver() vía `git describe --tags --always` → ej. 0.18.9.rXX.gHASH
```
- `build()` → el `go build` de arriba, con `version` de `git describe`.
- `package()` → replica `dist.sh install` a `$pkgdir/usr`: binario, icono
  256x256, `.desktop`, `metainfo.xml` y `gschema.xml` (el hook de glib de pacman
  compila el schema al instalar).
- Instalación: `makepkg -si` desde `packaging/`.
- Al tener `pkgname` propio + `provides`/`conflicts=trayscale`, `pacman -Syu`
  nunca lo reemplaza (no está en ningún repo) y desinstala el oficial.

## Verificación end-to-end

1. `go test ./internal/tsutil/...` y `go build ./...` pasan.
2. `makepkg -si` instala `trayscale-mullvadcity` y desplaza al oficial.
3. Abrir Trayscale → la página Mullvad muestra **1 fila por ciudad**.
4. Activar "Los Angeles, CA" → `tailscale status` muestra como exit node el nodo
   de mayor `Priority` de LAX; el subtítulo de la fila muestra ese hostname.
5. Desactivar → sin exit node.
6. Pasada de aceptación humana (visual) del usuario.
