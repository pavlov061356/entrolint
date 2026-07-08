package pipeline

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/cache"
	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
)

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func TestScan_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	result, err := Scan(ScanOptions{Root: dir, Config: config.Default()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Files) != 0 {
		t.Errorf("got %d files, want 0", len(result.Files))
	}
}

func TestScan_BasicCorpus(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"simple.go": `package p
func Simple() { _ = 1 }
`,
		"hot.go": `package p
func Hot(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 && x < 100 {
			for j := 0; j < x; j++ {
				if j%2 == 0 {
					total += j
				} else if j%3 == 0 {
					total -= j
				}
			}
		}
	}
	return total
}
`,
		"medium.go": `package p
func Medium(x int) int {
	if x > 0 {
		return x * 2
	}
	return -x
}
`,
	})
	result, err := Scan(ScanOptions{Root: dir, Config: config.Default()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Files) != 3 {
		t.Fatalf("got %d files, want 3", len(result.Files))
	}
	// Sorted by T descending — hot.go should be first.
	if result.Files[0].Path != "hot.go" {
		t.Errorf("first file = %q, want hot.go", result.Files[0].Path)
	}
	// Top (hot) file should have a dominant microstate.
	if result.Files[0].Dominant == "" {
		t.Errorf("hot file has empty Dominant")
	}
	// Contributions always sum to S.
	for _, f := range result.Files {
		var sum float64
		for _, c := range f.Contributions {
			sum += c
		}
		if absFloat(sum-f.S) > 1e-9 {
			t.Errorf("file %s: Σ contributions = %v, S = %v", f.Path, sum, f.S)
		}
	}
}

func TestScan_SortedByTDescending(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go": `package p
func A() {}
`,
		"b.go": `package p
func B(x int) {
	if x > 0 { _ = x }
	if x < 0 { _ = x }
	if x == 0 { _ = x }
}
`,
		"c.go": `package p
func C(x int) {
	if x > 0 { _ = x }
}
`,
	})
	result, err := Scan(ScanOptions{Root: dir, Config: config.Default()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	temps := make([]float64, len(result.Files))
	for i, f := range result.Files {
		temps[i] = f.T
	}
	// Check that temps is non-increasing.
	if !sort.SliceIsSorted(temps, func(i, j int) bool { return temps[i] > temps[j] }) {
		t.Errorf("T not sorted desc: %v", temps)
	}
}

func TestScan_CachePersistsAndRoundtrips(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go": `package p
func A(x int) { if x > 0 { _ = x } }
`,
		"b.go": `package p
func B(x int) { for i := 0; i < x; i++ { _ = i } }
`,
	})
	cachePath := filepath.Join(dir, ".entrolint.cache.json")

	first, err := Scan(ScanOptions{Root: dir, Config: config.Default(), CachePath: cachePath})
	if err != nil {
		t.Fatalf("first Scan: %v", err)
	}
	// Cache file should exist now.
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file not created: %v", err)
	}
	state, err := cache.Load(cachePath)
	if err != nil {
		t.Fatalf("cache.Load: %v", err)
	}
	if state.K == 0 {
		t.Error("cached K is zero")
	}

	// Second scan should reuse cache and yield identical scores.
	second, err := Scan(ScanOptions{Root: dir, Config: config.Default(), CachePath: cachePath})
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(first.Files) != len(second.Files) {
		t.Fatalf("file count differs: %d vs %d", len(first.Files), len(second.Files))
	}
	for i := range first.Files {
		if first.Files[i].T != second.Files[i].T {
			t.Errorf("T differs at %d: %v vs %v", i, first.Files[i].T, second.Files[i].T)
		}
	}
}

func TestScan_RecalibrateBypassesCache(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"simple.go": `package p
func A(x int) { if x > 0 { _ = x } }
`,
		"complex.go": `package p
func B(x int) {
	if x > 0 { _ = x }
	for i := 0; i < x; i++ { _ = i }
	switch x {
	case 1, 2, 3:
	default:
	}
}
`,
	})
	cachePath := filepath.Join(dir, ".entrolint.cache.json")

	// Plant a cache with inflated K and valid lognormal params so
	// normalize() returns non-zero values — scores will be ~999× the
	// fresh-calibration scores.
	bogus := cache.State{
		Version:   cache.SchemaVersion,
		Signature: cacheSignature(structuralMicrostates(), config.Default()),
		K:         999.0,
		Alpha:     0.5,
		Microstates: map[string]thermo.LogNormalParams{
			"cyclomatic":        {Mu: 0, Sigma: 1, Valid: true},
			"nesting":           {Mu: 0, Sigma: 1, Valid: true},
			"length":            {Mu: 0, Sigma: 1, Valid: true},
			"coupling":          {Mu: 0, Sigma: 1, Valid: true},
			"duplication":       {Mu: 0, Sigma: 1, Valid: true},
			"cross_duplication": {Mu: 0, Sigma: 1, Valid: true},
		},
	}
	if err := cache.Save(cachePath, bogus); err != nil {
		t.Fatalf("Save: %v", err)
	}

	withCache, err := Scan(ScanOptions{Root: dir, Config: config.Default(), CachePath: cachePath})
	if err != nil {
		t.Fatalf("Scan with cache: %v", err)
	}
	recal, err := Scan(ScanOptions{Root: dir, Config: config.Default(), CachePath: cachePath, Recalibrate: true})
	if err != nil {
		t.Fatalf("Scan recalibrate: %v", err)
	}
	if len(withCache.Files) == 0 || len(recal.Files) == 0 {
		t.Fatal("expected at least one file")
	}
	if withCache.Files[0].S == recal.Files[0].S {
		t.Errorf("recalibrate produced same S (%v) as bogus cache — cache was not bypassed", recal.Files[0].S)
	}
	// Bogus K=999 should dominate over a normal calibration.
	if withCache.Files[0].S <= recal.Files[0].S*10 {
		t.Errorf("expected bogus cache S (%v) to be much larger than recalibrated S (%v)",
			withCache.Files[0].S, recal.Files[0].S)
	}
}

// TestScan_StaleCacheRecalibratesOnNewMicrostate pins the v0.3
// regression where adding a new microstate left old caches valid but
// missing the new lognormal — thermo.normalize would silently return 0
// and the new microstate's weighted contribution would vanish from S.
// resolveEngine now treats incomplete cache as a miss.
func TestScan_StaleCacheRecalibratesOnNewMicrostate(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go": `package p

import "fmt"

func A() { fmt.Println() }
`,
	})
	cachePath := filepath.Join(dir, ".entrolint.cache.json")

	// Cache without coupling — emulates a v0.2 cache after upgrading to v0.3.
	stale := cache.State{
		Version: cache.SchemaVersion,
		K:       1.0,
		Alpha:   0.5,
		Microstates: map[string]thermo.LogNormalParams{
			"cyclomatic": {Mu: 0, Sigma: 1, Valid: true},
			"nesting":    {Mu: 0, Sigma: 1, Valid: true},
			"length":     {Mu: 0, Sigma: 1, Valid: true},
		},
	}
	if err := cache.Save(cachePath, stale); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := Scan(ScanOptions{Root: dir, Config: config.Default(), CachePath: cachePath}); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	reloaded, err := cache.Load(cachePath)
	if err != nil {
		t.Fatalf("Load after scan: %v", err)
	}
	if _, ok := reloaded.Microstates["coupling"]; !ok {
		t.Error("expected fresh calibration to add coupling lognormal — stale cache survived")
	}
}

func TestScan_CacheRecalibratesOnWeightChange(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go": `package p
func A(x int) { if x > 0 { _ = x } }
`,
		"b.go": `package p
func B(x int) {
	for i := 0; i < x; i++ { _ = i }
}
`,
	})
	cachePath := filepath.Join(dir, ".entrolint.cache.json")

	if _, err := Scan(ScanOptions{Root: dir, Config: config.Default(), CachePath: cachePath}); err != nil {
		t.Fatalf("initial Scan: %v", err)
	}
	initial, err := cache.Load(cachePath)
	if err != nil {
		t.Fatalf("Load initial cache: %v", err)
	}
	if got := initial.Signature.Weights["length"]; got != 0.5 {
		t.Fatalf("initial length weight = %v, want 0.5", got)
	}

	changed := config.Default()
	changed.Weights["length"] = 2.0
	if _, err := Scan(ScanOptions{Root: dir, Config: changed, CachePath: cachePath}); err != nil {
		t.Fatalf("Scan after weight change: %v", err)
	}
	reloaded, err := cache.Load(cachePath)
	if err != nil {
		t.Fatalf("Load changed cache: %v", err)
	}
	if got := reloaded.Signature.Weights["length"]; got != 2.0 {
		t.Errorf("cache signature length weight = %v, want 2.0", got)
	}
}

func TestScan_NonExistentRoot(t *testing.T) {
	_, err := Scan(ScanOptions{Root: "/nonexistent/entrolint-scan-test", Config: config.Default()})
	if err == nil {
		t.Error("expected error for non-existent root")
	}
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
