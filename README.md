# stldev

A live dev loop for code-generated STL files — watches your source, runs your build command, and reloads tiled [f3d](https://f3d.app) preview windows.

`stldev` is build-system-agnostic: point it at any shell command that produces STLs (Go, Python/CadQuery, JavaScript, OpenSCAD…) and it handles the watch / rebuild / reload / window-tiling plumbing around it.

## Install

```bash
go install github.com/snowbldr/stldev@latest
```

Requires [f3d](https://f3d.app) on `$PATH`. On macOS: `brew install f3d`.

## Usage

```bash
stldev -cmd "go run ." obj1.stl obj2.stl
```

- Watches the current directory for `.go` changes (configurable)
- Runs `-cmd` on every debounced change
- Overwrites each target STL with a small dot-grid placeholder at the start of every build — so f3d briefly flashes a "build in progress" indicator (visible at any zoom) and then auto-reloads the real output when your command finishes writing the STL. Disable with `-noloading`.
- Launches a tiled `f3d` window per STL, kept alive across manual close
- Ctrl-C cleanly shuts down the watcher and all viewers

Pass extra args through to every f3d invocation using either `-f3d-args` or a `--` separator (both work, and can be combined — `--` args are appended on top of `-f3d-args`):

```bash
stldev -cmd "go run ." part.stl -f3d-args "--up=+Z --roughness=0.8"
stldev -cmd "go run ." part.stl -- --up=+Z --roughness=0.8
```

## Flags

| Flag | Description |
|---|---|
| `-cmd` | build command to run on change (default `go run ./...`) |
| `-watch` | directory to watch recursively (repeatable, default `.`) |
| `-ext` | extensions that trigger a rebuild (default `.go`) |
| `-debounce` | debounce interval for events (default `200ms`) |
| `-f3d-args` | extra arguments passed to every f3d invocation (pass `--watch` for live reload) |
| `-no-tile` | don't auto-tile f3d windows |
| `-noloading` | disable the placeholder shown while rebuilding |
| `-monitor N` | 1-indexed monitor to tile on (default: 1) |
| `-screen-w` / `-screen-h` / `-screen-x` / `-screen-y` | override tiling geometry |

Multi-monitor support works on macOS by enumerating displays via `system_profiler` and computing each monitor's X origin as the sum of earlier monitors' widths — matching a standard left-to-right layout whether you have 2, 3, or more monitors. For vertical stacks or unusual arrangements, override with `-screen-x` / `-screen-y`.

## Suggested workflow: drive it from a Makefile

The cleanest way to adopt stldev is to wire it into a Makefile so you get `make dev`, `make dev <subset>`, and `make build` for free. See [`examples/nutsandbolts/`](examples/nutsandbolts/) for a working copy — renders four STLs (bolts + nuts, inch + metric) and lets you watch any subset:

```bash
cd examples/nutsandbolts
make dev          # watch + preview all 4
make dev bolts    # watch + preview just the bolts
make dev inch     # watch + preview just the inch pair
make build        # one-shot full-quality render
```

Copy that `Makefile` into your own project, point `STLS` at your outputs, adjust the subset branches.

## Regenerating the loading placeholder

The placeholder STL that shows while a build runs is a 3D grid of dots, committed at `loading.stl` and embedded into the binary. To tweak it (dot size, spacing, grid density):

```bash
cd gen
go run .                                # defaults
go run . -radius 20 -step 100 -count 7  # bigger / denser
go run . -keep 0.02                     # smaller file (2% of triangles kept)
```

The generator uses [fluent-sdfx](https://github.com/snowbldr/fluent-sdfx), so the main `stldev` binary stays dependency-light.

## License

MIT
