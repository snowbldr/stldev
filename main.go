// stldev watches source files, re-runs a build command on change, and
// previews the generated STL files in f3d with auto-reload and tiled windows.
//
// The build command is run via sh -c and inherits the parent environment, so
// any exported env vars (PATH, custom flags, etc.) are visible to it.
//
// Typical use:
//
//	stldev -cmd "go run ./cmd/part -res 10 -suffix _dev all" core_dev.stl socket_dev.stl
//
// Requires f3d (https://f3d.app) to be installed on $PATH.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// loadingSTL is a grid of dots written over each target STL during a rebuild,
// so f3d (which preserves camera state between reloads) still shows *something*
// regardless of how the user has zoomed. Regenerate via `cd gen && go run .`.
//
//go:embed loading.stl
var loadingSTL []byte

type stringSlice []string

func (s *stringSlice) String() string     { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

func main() {
	var (
		cmdStr     = flag.String("cmd", "go run ./...", "build command to run on change")
		watchDirs  stringSlice
		extCSV     = flag.String("ext", ".go", "comma-separated file extensions that trigger a rebuild")
		debounce   = flag.Duration("debounce", 200*time.Millisecond, "debounce interval for filesystem events")
		f3dArgsStr = flag.String("f3d-args", "", "extra arguments passed to f3d (pass --watch for live reload)")
		noTile     = flag.Bool("no-tile", false, "don't auto-tile f3d windows")
		noLoading  = flag.Bool("noloading", false, "don't overwrite STLs with a placeholder while rebuilding")
		monitor    = flag.Int("monitor", 1, "1-indexed monitor to tile on")
		screenW    = flag.Int("screen-w", 0, "override screen width for tiling (0 = autodetect)")
		screenH    = flag.Int("screen-h", 0, "override screen height for tiling (0 = autodetect)")
		screenX    = flag.Int("screen-x", 0, "override screen X origin for tiling (-monitor is ignored if set)")
		screenY    = flag.Int("screen-y", 0, "override screen Y origin for tiling")
	)
	flag.Var(&watchDirs, "watch", "directory to watch recursively (repeatable, default: current directory)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <stl-file>... [-- <f3d args>...]\n\n", filepath.Base(os.Args[0]))
		fmt.Fprintln(os.Stderr, "Watches source files, re-runs -cmd on changes, and previews STLs in f3d.")
		fmt.Fprintln(os.Stderr, "Any arguments after `--` are appended to every f3d invocation.")
		fmt.Fprintln(os.Stderr)
		flag.PrintDefaults()
	}

	// Split argv on `--` so flag.Parse only sees our flags + STL paths.
	// Anything after `--` is passed through to f3d on top of -f3d-args.
	ourArgs, passthrough := splitPassthrough(os.Args[1:])
	flag.CommandLine.Parse(ourArgs)

	stls := flag.Args()
	if len(stls) == 0 {
		fmt.Fprintln(os.Stderr, "error: provide at least one STL file to view")
		flag.Usage()
		os.Exit(2)
	}
	if len(watchDirs) == 0 {
		watchDirs = []string{"."}
	}
	exts := parseExts(*extCSV)

	f3dPath, err := exec.LookPath("f3d")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: f3d not found on $PATH. Install it from https://f3d.app")
		os.Exit(1)
	}

	done, stop := signalContext()
	defer stop()

	// Snapshot which STLs already exist before we touch anything. Existing
	// files get a viewer launched immediately against their prior output so
	// f3d's auto-fit frames the real geometry; we then overwrite them with
	// the dot-grid placeholder (after a short wait) for build-in-progress
	// feedback. Missing files get the placeholder written up front as a
	// fallback for build failure, but their viewer is delayed until after
	// the initial build — opening the tiny placeholder first would leave
	// f3d's camera fitted to it, badly framing the real part on reload.
	existed := make([]bool, len(stls))
	for i, p := range stls {
		if _, err := os.Stat(p); err == nil {
			existed[i] = true
		}
	}
	if !*noLoading {
		writeLoadingPlaceholdersIfMissing(stls)
	}

	mons := detectMonitors()
	target := pickMonitor(mons, *monitor)
	screenW2, screenH2 := resolveScreenSize(target, *screenW, *screenH, *noTile)
	tileW, tileH := tileSize(len(stls), screenW2, screenH2)
	x, y := resolveScreenOrigin(mons, target, *screenX, *screenY, *noTile)
	baseArgs := splitArgs(*f3dArgsStr)
	viewers := make([]*viewer, len(stls))
	defer killViewers(viewers)

	staggered := false
	var existingPaths []string
	for i := range stls {
		if !existed[i] {
			continue
		}
		if staggered {
			time.Sleep(200 * time.Millisecond)
		}
		launchViewer(done, viewers, i, f3dPath, baseArgs, passthrough, stls, x, y, tileW, tileH, *noTile)
		staggered = true
		existingPaths = append(existingPaths, stls[i])
	}

	// Now that f3d has had a chance to open each existing STL and fit its
	// camera to the real geometry, overwrite those files with the dot-grid
	// placeholder so the user gets immediate visual feedback that a build
	// is in flight. The 1s wait is a hedge against f3d's startup time —
	// without it, f3d races us and ends up auto-fitting to the placeholder.
	if !*noLoading && len(existingPaths) > 0 {
		time.Sleep(1 * time.Second)
		writeLoadingPlaceholders(existingPaths)
	}

	fmt.Println("[stldev] initial build:", *cmdStr)
	if err := runBuild(*cmdStr); err != nil {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "============================================================")
		fmt.Fprintln(os.Stderr, "  [stldev] INITIAL BUILD FAILED")
		fmt.Fprintln(os.Stderr, "============================================================")
		fmt.Fprintln(os.Stderr, "  command:", *cmdStr)
		fmt.Fprintln(os.Stderr, "  error:  ", err)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  Fix the build command above and re-run stldev.")
		fmt.Fprintln(os.Stderr, "============================================================")
		stop()
		killViewers(viewers)
		os.Exit(1)
	}

	for i := range stls {
		if existed[i] {
			continue
		}
		if staggered {
			time.Sleep(200 * time.Millisecond)
		}
		launchViewer(done, viewers, i, f3dPath, baseArgs, passthrough, stls, x, y, tileW, tileH, *noTile)
		staggered = true
	}

	// Start watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: create watcher:", err)
		os.Exit(1)
	}
	defer watcher.Close()

	for _, d := range watchDirs {
		if err := addWatchRecursive(watcher, d); err != nil {
			fmt.Fprintln(os.Stderr, "error: watch", d, ":", err)
			os.Exit(1)
		}
	}

	fmt.Printf("[stldev] watching %s for %s changes — ctrl-c to stop\n",
		strings.Join(watchDirs, ", "), strings.Join(exts, ","))

	var loadingPaths []string
	if !*noLoading {
		loadingPaths = stls
	}
	go runLoop(done, watcher, exts, *debounce, *cmdStr, loadingPaths)

	<-done
	fmt.Println("\n[stldev] shutting down")
}

func parseExts(csv string) []string {
	raw := strings.Split(csv, ",")
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		out = append(out, e)
	}
	return out
}

func signalContext() (<-chan struct{}, func()) {
	ch := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	var once sync.Once
	stop := func() { once.Do(func() { close(ch) }) }
	go func() {
		<-sig
		stop()
	}()
	return ch, stop
}

// writeLoadingPlaceholders overwrites each target path with the embedded
// dot-grid STL. f3d's --watch picks up the mtime change and reloads; the
// user's build command will overwrite each file again with the real output
// when it finishes. If the build fails, the placeholder stays visible.
func writeLoadingPlaceholders(paths []string) {
	for _, p := range paths {
		if err := os.WriteFile(p, loadingSTL, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "[stldev] write loading placeholder:", err)
		}
	}
}

// writeLoadingPlaceholdersIfMissing writes the dot-grid STL only for paths
// that don't exist yet. Used at startup so f3d has something to open without
// clobbering an existing build output that's about to be regenerated.
func writeLoadingPlaceholdersIfMissing(paths []string) {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			continue
		}
		if err := os.WriteFile(p, loadingSTL, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "[stldev] write loading placeholder:", err)
		}
	}
}

func runBuild(cmdStr string) error {
	c := exec.Command("sh", "-c", cmdStr)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func addWatchRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		name := info.Name()
		if name != "." && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}

func runLoop(done <-chan struct{}, w *fsnotify.Watcher, exts []string, debounce time.Duration, cmdStr string, loadingPaths []string) {
	// Builds go through a dedicated worker so only one runs at a time.
	// Sending a trigger while a build is in flight pre-empts it — the running
	// build is killed and a fresh one starts. That keeps rapid-fire edits from
	// piling up CPU work or producing two f3d reloads for the same change.
	triggerCh := make(chan struct{}, 1)
	go buildWorker(done, triggerCh, cmdStr, loadingPaths)

	sendTrigger := func() {
		select {
		case triggerCh <- struct{}{}:
		default:
			// A trigger is already queued; either the worker is about to pick
			// it up or is already building and will see it as a pre-empt.
		}
	}

	var timer *time.Timer
	for {
		select {
		case <-done:
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			// Pick up newly-created directories so the recursive watch keeps up.
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					_ = addWatchRecursive(w, ev.Name)
					continue
				}
			}
			if !matchesExt(ev.Name, exts) {
				continue
			}
			// Only content changes trigger a rebuild. Many editors emit a
			// Chmod (or Remove/Rename on swap/backup files) shortly after
			// Write — if that tail event lands just past the debounce window,
			// it queues a second trigger for what was really one save.
			if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, sendTrigger)
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			fmt.Fprintln(os.Stderr, "[stldev] watcher error:", err)
		}
	}
}

// buildWorker runs one build at a time, pre-empting the in-flight build when a
// new trigger arrives. Exits when done is closed (killing any in-flight build).
func buildWorker(done <-chan struct{}, triggerCh <-chan struct{}, cmdStr string, loadingPaths []string) {
	for {
		select {
		case <-done:
			return
		case <-triggerCh:
		}
		writeLoadingPlaceholders(loadingPaths)
		fmt.Println("[stldev] rebuild:", cmdStr)
		runAndWatch(done, triggerCh, cmdStr)
	}
}

// runAndWatch execs the build and blocks until it finishes, shutdown is
// requested, or a new trigger arrives. On pre-empt, kills the build and loops
// with a fresh exec — the loading placeholder is already on disk from the
// outer caller, so we don't re-flash it.
func runAndWatch(done <-chan struct{}, triggerCh <-chan struct{}, cmdStr string) {
	for {
		// `exec` inside sh replaces the shell with the user's command in-place,
		// so killing the process group doesn't produce the "Terminated: 15"
		// message sh would otherwise print when its foreground child dies.
		cmd := exec.Command("sh", "-c", "exec "+cmdStr)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = detachedProcAttr()
		if err := cmd.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "[stldev] build start failed:", err)
			return
		}
		exitCh := make(chan error, 1)
		go func() { exitCh <- cmd.Wait() }()

		select {
		case <-done:
			killBuildGroup(cmd)
			<-exitCh
			return
		case <-triggerCh:
			fmt.Println("[stldev] edits during build — restarting")
			killBuildGroup(cmd)
			<-exitCh
			// loop: fresh build
		case err := <-exitCh:
			if err != nil {
				fmt.Fprintln(os.Stderr, "[stldev] build failed:", err)
			} else {
				fmt.Println("[stldev] build ok")
			}
			return
		}
	}
}

func matchesExt(name string, exts []string) bool {
	e := strings.ToLower(filepath.Ext(name))
	for _, want := range exts {
		if e == strings.ToLower(want) {
			return true
		}
	}
	return false
}

// tileSize returns per-window tile dimensions given a screen size and the
// number of tiles. 0 inputs pass through as 0 (meaning "don't pass geometry
// flags to f3d"). The -80/-60 fudge factors are chrome/title-bar allowances.
func tileSize(n, screenW, screenH int) (int, int) {
	if screenW == 0 || screenH == 0 {
		return 0, 0
	}
	switch {
	case n <= 1:
		return screenW, screenH - 80
	case n == 2:
		return screenW / 2, screenH - 80
	default:
		return screenW / 2, screenH/2 - 60
	}
}

// monitor describes one detected display. Positions are not reported by
// system_profiler; we compute X assuming a left-to-right horizontal layout
// (widths summed in the order the monitors are listed).
type monitor struct {
	w, h int
}

// detectMonitors enumerates attached displays in system_profiler's order.
// Returns an empty slice off macOS or on parse failure — callers fall back
// to passing no geometry flags to f3d.
func detectMonitors() []monitor {
	if runtime.GOOS != "darwin" {
		return nil
	}
	out, err := exec.Command("system_profiler", "SPDisplaysDataType").Output()
	if err != nil {
		return nil
	}
	var mons []monitor
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Resolution:") {
			continue
		}
		parts := strings.Fields(strings.TrimPrefix(line, "Resolution:"))
		if len(parts) < 3 {
			continue
		}
		var w, h int
		fmt.Sscanf(parts[0], "%d", &w)
		fmt.Sscanf(parts[2], "%d", &h)
		if w > 0 && h > 0 {
			mons = append(mons, monitor{w, h})
		}
	}
	return mons
}

// pickMonitor picks a 1-indexed monitor from the detected set, clamping to
// the detected range. 0 (or off-macOS where no monitors are detected) means
// "don't pass geometry flags to f3d".
func pickMonitor(mons []monitor, n int) int {
	if len(mons) == 0 || n <= 0 {
		return 0
	}
	if n > len(mons) {
		return len(mons)
	}
	return n
}

// resolveScreenSize returns the per-window tile size for the chosen monitor,
// respecting any explicit overrides. A 0 return means "don't pass --resolution".
func resolveScreenSize(target, overrideW, overrideH int, noTile bool) (int, int) {
	if noTile {
		return 0, 0
	}
	mons := detectMonitors()
	w, h := overrideW, overrideH
	if target > 0 && target <= len(mons) {
		if w == 0 {
			w = mons[target-1].w
		}
		if h == 0 {
			h = mons[target-1].h
		}
	}
	if w == 0 || h == 0 {
		return 0, 0
	}
	// Chrome-adjusted tile sizes used by launchViewer's tilePosition layout.
	return w, h
}

// resolveScreenOrigin returns the X/Y origin for tiling. Explicit -screen-x/
// -screen-y win; otherwise X is the sum of earlier monitors' widths (assumes
// left-to-right horizontal layout), Y is 0.
func resolveScreenOrigin(mons []monitor, target, overrideX, overrideY int, noTile bool) (int, int) {
	if noTile {
		return overrideX, overrideY
	}
	x, y := 0, 0
	if target > 0 && target <= len(mons) {
		for i := 0; i < target-1; i++ {
			x += mons[i].w
		}
	}
	if flagSet("screen-x") {
		x = overrideX
	}
	if flagSet("screen-y") {
		y = overrideY
	}
	return x, y
}

// flagSet reports whether the given flag was explicitly passed on the CLI.
func flagSet(name string) bool {
	seen := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}

// viewer keeps one f3d window alive by relaunching it whenever it exits,
// until the parent signals shutdown via the done channel.
type viewer struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

func (v *viewer) current() *exec.Cmd {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cmd
}

func (v *viewer) set(cmd *exec.Cmd) {
	v.mu.Lock()
	v.cmd = cmd
	v.mu.Unlock()
}

// launchViewer starts a single f3d viewer for stls[i], writing it into
// viewers[i]. Caller is responsible for staggering successive launches: on
// macOS, launching multiple GUI apps in the same tick causes a focus race
// where some windows end up behind others.
func launchViewer(done <-chan struct{}, viewers []*viewer, i int, f3dPath string, baseArgs, passthrough, stls []string, x, y, w, h int, noTile bool) {
	args := append([]string{}, baseArgs...)
	args = append(args, passthrough...)
	if !noTile && w > 0 && h > 0 {
		px, py := tilePosition(i, len(stls), x, y, w, h)
		args = append(args, fmt.Sprintf("--resolution=%d,%d", w, h))
		args = append(args, fmt.Sprintf("--position=%d,%d", px, py))
	}
	args = append(args, stls[i])
	v := &viewer{}
	viewers[i] = v
	go keepAlive(done, v, f3dPath, args)
}

// keepAlive runs f3d in a loop, relaunching on exit (matching the old
// f3d-watch.sh behavior) until the done channel is closed.
func keepAlive(done <-chan struct{}, v *viewer, f3dPath string, args []string) {
	for {
		select {
		case <-done:
			return
		default:
		}
		cmd := exec.Command(f3dPath, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = detachedProcAttr()
		if err := cmd.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "[stldev] failed to launch f3d:", err)
			return
		}
		v.set(cmd)
		_ = cmd.Wait()
		v.set(nil)
		// Back off briefly so a crash-looping f3d doesn't spin the CPU.
		select {
		case <-done:
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func tilePosition(i, n, x, y, w, h int) (int, int) {
	switch {
	case n <= 1:
		return x, y
	case n == 2:
		return x + i*w, y
	default:
		col := i % 2
		row := i / 2
		return x + col*w, y + row*(h+30)
	}
}

// splitPassthrough splits argv on the first standalone `--`. Everything before
// is returned as our args; everything after is the passthrough for f3d.
func splitPassthrough(argv []string) ([]string, []string) {
	for i, a := range argv {
		if a == "--" {
			return argv[:i], argv[i+1:]
		}
	}
	return argv, nil
}

// splitArgs splits on whitespace, respecting simple double-quoted segments.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for _, r := range s {
		switch {
		case r == '"':
			inQ = !inQ
		case !inQ && (r == ' ' || r == '\t'):
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func killViewers(viewers []*viewer) {
	// Send SIGTERM to whatever's currently running in each viewer loop.
	for _, v := range viewers {
		if v == nil {
			continue
		}
		if cmd := v.current(); cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
	}
	// Wait briefly, then hard-kill any stragglers. The keepAlive goroutines
	// won't relaunch because they observe the closed done channel.
	time.Sleep(300 * time.Millisecond)
	for _, v := range viewers {
		if v == nil {
			continue
		}
		if cmd := v.current(); cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}
