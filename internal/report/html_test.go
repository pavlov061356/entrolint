package report

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
)

func score(path string, s, t float64) pipeline.FileScore {
	return pipeline.FileScore{
		Path: path, S: s, T: t, Dominant: "length",
		Contributions: map[string]float64{"length": s},
	}
}

func TestScanHTML_SelfContainedAndValid(t *testing.T) {
	html, err := ScanHTML([]pipeline.FileScore{
		score("internal/a/foo.go", 2, 3),
		score("internal/a/bar.go", 1, 1),
		score("cmd/main.go", 4, 8),
	})
	if err != nil {
		t.Fatalf("ScanHTML: %v", err)
	}
	s := string(html)

	if !strings.HasPrefix(s, "<!doctype html>") {
		t.Error("output is not an HTML document")
	}
	// Self-contained: no external assets to fetch (offline, leaks nothing).
	for _, bad := range []string{`src="http`, `href="http`, "//cdn", "<link"} {
		if strings.Contains(s, bad) {
			t.Errorf("report is not self-contained: found %q", bad)
		}
	}
	// One tile per file, and each path is present (drill-down + tooltip).
	if got := strings.Count(s, `<rect class="cell"`); got != 3 {
		t.Errorf("rect count = %d, want 3 (one per file)", got)
	}
	for _, p := range []string{"internal/a/foo.go", "cmd/main.go"} {
		if !strings.Contains(s, p) {
			t.Errorf("path %q missing from report", p)
		}
	}
}

func TestScanHTML_Empty(t *testing.T) {
	html, err := ScanHTML(nil)
	if err != nil {
		t.Fatalf("ScanHTML(nil): %v", err)
	}
	s := string(html)
	if !strings.HasPrefix(s, "<!doctype html>") {
		t.Error("empty input must still yield a valid document")
	}
	if strings.Contains(s, `<rect class="cell"`) {
		t.Error("empty input must render no tiles")
	}
}

func TestScanHTML_Deterministic(t *testing.T) {
	files := []pipeline.FileScore{score("a/x.go", 2, 2), score("a/y.go", 3, 1), score("b/z.go", 1, 5)}
	a, _ := ScanHTML(files)
	b, _ := ScanHTML(files)
	if string(a) != string(b) {
		t.Error("ScanHTML must be deterministic for a fixed input (no timestamps, stable ordering)")
	}
}

// TestTreemap_AreaProportionalToEntropy: in a flat layout the rectangle area
// is the file's entropy S, so a 4×-entropy file gets a 4×-area tile.
func TestTreemap_AreaProportionalToEntropy(t *testing.T) {
	cells := layoutTreemap([]pipeline.FileScore{score("big.go", 4, 1), score("small.go", 1, 1)})
	area := map[string]float64{}
	for _, c := range cells {
		area[c.Path] = c.W * c.H
	}
	if ratio := area["big.go"] / area["small.go"]; math.Abs(ratio-4) > 0.05 {
		t.Errorf("area ratio big/small = %.3f, want ~4 (area ∝ S)", ratio)
	}
}

// TestTreemap_TilesWithinBoundsAndDisjoint: the squarified layout must keep
// every tile inside the canvas and not overlap any other (no double-counted
// pixels, no off-canvas tiles).
func TestTreemap_TilesWithinBoundsAndDisjoint(t *testing.T) {
	paths := []string{
		"a/one.go", "a/two.go", "a/sub/three.go", "b/four.go", "b/five.go",
		"six.go", "c/d/seven.go", "c/d/eight.go", "c/nine.go",
	}
	files := make([]pipeline.FileScore, 0, len(paths))
	for _, p := range paths {
		files = append(files, score(p, 1+float64(len(p)%4), float64(len(p))))
	}
	cells := layoutTreemap(files)
	if len(cells) != len(files) {
		t.Fatalf("got %d cells, want %d", len(cells), len(files))
	}
	const eps = 0.05
	for _, c := range cells {
		if c.X < -eps || c.Y < -eps || c.X+c.W > canvasW+eps || c.Y+c.H > canvasH+eps {
			t.Errorf("tile %s out of bounds: x=%g y=%g w=%g h=%g", c.Path, c.X, c.Y, c.W, c.H)
		}
	}
	for i := 0; i < len(cells); i++ {
		for j := i + 1; j < len(cells); j++ {
			if a := overlapArea(cells[i], cells[j]); a > 0.5 {
				t.Errorf("tiles %s and %s overlap by %.2f", cells[i].Path, cells[j].Path, a)
			}
		}
	}
}

// TestTreemap_HotterFileIsRedder: temperature drives hue from green (cool,
// 120°) toward red (hot, 0°), so a hotter file must get a strictly lower hue.
func TestTreemap_HotterFileIsRedder(t *testing.T) {
	cells := layoutTreemap([]pipeline.FileScore{score("hot.go", 1, 10), score("cool.go", 1, 1)})
	hue := map[string]float64{}
	for _, c := range cells {
		hue[c.Path] = parseHue(t, c.Fill)
	}
	if hue["hot.go"] >= hue["cool.go"] {
		t.Errorf("hot file hue %.1f must be < cool file hue %.1f (redder)", hue["hot.go"], hue["cool.go"])
	}
}

// TestTreemap_LabelsLargeTilesOnly: a large tile carries its filename so the
// map is legible without clicking; a tiny tile omits the label (it relies on
// the hover tooltip) rather than overflow.
func TestTreemap_LabelsLargeTilesOnly(t *testing.T) {
	cells := layoutTreemap([]pipeline.FileScore{
		score("internal/engine/big_file.go", 100, 1),
		score("x.go", 0.001, 1),
	})
	var big, tiny htmlCell
	for _, c := range cells {
		switch c.Path {
		case "internal/engine/big_file.go":
			big = c
		case "x.go":
			tiny = c
		}
	}
	if big.Label != "big_file.go" {
		t.Errorf("large tile label = %q, want %q", big.Label, "big_file.go")
	}
	if tiny.Label != "" {
		t.Errorf("tiny tile must omit its label, got %q", tiny.Label)
	}
}

func TestTileLabels_FitTruncateOmit(t *testing.T) {
	s := score("a/averylongfilename.go", 2, 3)
	if n, st := tileLabels(&s, rect{0, 0, 300, 60}); n != "averylongfilename.go" || st == "" {
		t.Errorf("wide+tall tile: got name=%q stat=%q, want full name and an S·T line", n, st)
	}
	if n, st := tileLabels(&s, rect{0, 0, 70, 20}); !strings.HasSuffix(n, "…") || st != "" {
		t.Errorf("narrow tile: got name=%q stat=%q, want truncated name and no stat", n, st)
	}
	if n, _ := tileLabels(&s, rect{0, 0, 10, 10}); n != "" {
		t.Errorf("tiny tile: got name=%q, want no label", n)
	}
}

func overlapArea(a, b htmlCell) float64 {
	ox := math.Max(0, math.Min(a.X+a.W, b.X+b.W)-math.Max(a.X, b.X))
	oy := math.Max(0, math.Min(a.Y+a.H, b.Y+b.H)-math.Max(a.Y, b.Y))
	return ox * oy
}

func parseHue(t *testing.T, fill string) float64 {
	t.Helper()
	s := strings.TrimPrefix(fill, "hsl(")
	h, err := strconv.ParseFloat(strings.SplitN(s, ",", 2)[0], 64)
	if err != nil {
		t.Fatalf("bad fill %q: %v", fill, err)
	}
	return h
}
