package report

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"

	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
)

//go:embed html.tmpl
var htmlTmpl string

// Canvas dimensions of the treemap SVG, in user units. CSS scales the SVG
// to the viewport, so these are an internal aspect-ratio choice, not pixels.
const (
	canvasW = 1000.0
	canvasH = 660.0
	// dirPad insets each directory's rectangle before its children are laid
	// out, so nested packages read as visually distinct regions.
	dirPad = 1.5
)

// ScanHTML renders a scan result as a single self-contained HTML page: a
// squarified treemap of the repository where each file's rectangle area is
// its structural entropy S and its colour is its temperature T (the
// churn-weighted refactoring-urgency signal). Clicking a rectangle reveals
// the per-microstate breakdown. The output embeds its own CSS, JS, and data
// — no network, no external assets — so it works offline and leaks nothing,
// matching the engine's hermetic, no-telemetry stance.
//
// The result is deterministic for a given input (no embedded timestamps),
// so it is safe to diff in tests and in CI artifacts.
func ScanHTML(files []pipeline.FileScore) ([]byte, error) {
	cells := layoutTreemap(files)

	data, err := json.Marshal(cells)
	if err != nil {
		return nil, err
	}

	t, err := template.New("html").Parse(htmlTmpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, htmlData{
		Width:       canvasW,
		Height:      canvasH,
		Cells:       cells,
		CellsJSON:   template.JS(data), // #nosec G203 -- data is our own json.Marshal, not user HTML
		Microstates: microstateOrder,
		FileCount:   len(files),
	}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// microstateOrder fixes the legend/breakdown ordering so the report reads the
// same way every run (map iteration over Contributions is otherwise random).
var microstateOrder = []string{
	"cyclomatic", "nesting", "length", "coupling", "duplication", "cross_duplication",
}

// htmlData is the template payload.
type htmlData struct {
	Width, Height float64
	Cells         []htmlCell
	CellsJSON     template.JS
	Microstates   []string
	FileCount     int
}

// htmlCell is one placed file rectangle. The JSON tags drive the in-page
// drill-down, which reads the same slice the SVG is rendered from.
type htmlCell struct {
	Path          string             `json:"path"`
	X             float64            `json:"x"`
	Y             float64            `json:"y"`
	W             float64            `json:"w"`
	H             float64            `json:"h"`
	S             float64            `json:"s"`
	T             float64            `json:"t"`
	Fill          string             `json:"fill"`
	Dominant      string             `json:"dominant"`
	Contributions map[string]float64 `json:"contributions"`
}

// rect is an axis-aligned rectangle in canvas units.
type rect struct{ x, y, w, h float64 }

// treeNode is a directory or file in the path tree. Leaves carry a score;
// directories aggregate their descendants' weight (the treemap area).
type treeNode struct {
	name     string
	score    *pipeline.FileScore
	weight   float64
	children []*treeNode
}

// layoutTreemap builds the directory tree from file paths, sizes every node
// by its entropy mass, squarifies it into the canvas, and returns one cell
// per file coloured by temperature.
func layoutTreemap(files []pipeline.FileScore) []htmlCell {
	if len(files) == 0 {
		return []htmlCell{}
	}
	root := buildTree(files)
	scale := heatScaleT(files)

	cells := make([]htmlCell, 0, len(files))
	layout(root, rect{0, 0, canvasW, canvasH}, scale, &cells)
	return cells
}

// buildTree groups files into a directory tree and computes each node's
// weight bottom-up. A file's weight is its entropy S, floored to a small
// sliver so a perfectly clean file (S=0) is still visible rather than
// collapsing to zero area.
func buildTree(files []pipeline.FileScore) *treeNode {
	root := &treeNode{}
	for i := range files {
		parts := strings.Split(files[i].Path, "/")
		cur := root
		for j, p := range parts {
			if j == len(parts)-1 {
				cur.children = append(cur.children, &treeNode{name: p, score: &files[i]})
				continue
			}
			cur = childDir(cur, p)
		}
	}
	computeWeight(root)
	return root
}

// childDir returns the named directory child of n, creating it if absent.
func childDir(n *treeNode, name string) *treeNode {
	for _, c := range n.children {
		if c.score == nil && c.name == name {
			return c
		}
	}
	c := &treeNode{name: name}
	n.children = append(n.children, c)
	return c
}

// weightFloor keeps clean files visible (a thin sliver) without distorting
// the relative areas of files that carry real entropy.
const weightFloor = 0.05

func computeWeight(n *treeNode) float64 {
	if n.score != nil {
		n.weight = math.Max(n.score.S, weightFloor)
		return n.weight
	}
	var sum float64
	for _, c := range n.children {
		sum += computeWeight(c)
	}
	n.weight = sum
	return sum
}

// layout recursively squarifies n's children into r, emitting a cell for each
// file leaf. Directories are inset by dirPad so nesting is legible.
func layout(n *treeNode, r rect, scale heatScale, out *[]htmlCell) {
	if n.score != nil {
		*out = append(*out, htmlCell{
			Path:          n.score.Path,
			X:             round(r.x),
			Y:             round(r.y),
			W:             round(r.w),
			H:             round(r.h),
			S:             n.score.S,
			T:             n.score.T,
			Fill:          scale.color(n.score.T),
			Dominant:      n.score.Dominant,
			Contributions: n.score.Contributions,
		})
		return
	}
	sort.Slice(n.children, func(i, j int) bool {
		if n.children[i].weight != n.children[j].weight {
			return n.children[i].weight > n.children[j].weight
		}
		return n.children[i].name < n.children[j].name // stable, deterministic
	})
	for _, p := range squarify(n.children, r) {
		child := p.node
		cr := p.r
		if child.score == nil {
			cr = inset(cr, dirPad)
		}
		layout(child, cr, scale, out)
	}
}

type placement struct {
	node *treeNode
	r    rect
}

// squarify lays out weighted nodes into r with near-square aspect ratios,
// following Bruls/Huizing/van Wijk. Rows are packed along the shorter side;
// a node joins the current row while doing so improves the row's worst
// aspect ratio, otherwise the row is committed and a new one begins.
func squarify(nodes []*treeNode, r rect) []placement {
	var total float64
	for _, n := range nodes {
		total += n.weight
	}
	if total <= 0 || r.w <= 0 || r.h <= 0 {
		return nil
	}
	scale := r.w * r.h / total
	areas := make([]float64, len(nodes))
	for i := range nodes {
		areas[i] = nodes[i].weight * scale
	}

	out := make([]placement, 0, len(nodes))
	free := r
	for i := 0; i < len(nodes); {
		side := math.Min(free.w, free.h)
		row := []float64{areas[i]}
		j := i + 1
		for j < len(nodes) && worst(row, areas[j], side) <= worst(row, -1, side) {
			row = append(row, areas[j])
			j++
		}
		rects, rest := layoutRow(row, free)
		for k := range rects {
			out = append(out, placement{node: nodes[i+k], r: rects[k]})
		}
		free = rest
		i = j
	}
	return out
}

// worst returns the largest aspect ratio of a row of areas packed into a strip
// of length side. extra (when ≥ 0) is a candidate area hypothetically appended.
func worst(row []float64, extra, side float64) float64 {
	mx, mn, sum := 0.0, math.Inf(1), 0.0
	consider := func(a float64) {
		sum += a
		mx = math.Max(mx, a)
		mn = math.Min(mn, a)
	}
	for _, a := range row {
		consider(a)
	}
	if extra >= 0 {
		consider(extra)
	}
	if sum == 0 {
		return math.Inf(1)
	}
	s2, sum2 := side*side, sum*sum
	return math.Max(s2*mx/sum2, sum2/(s2*mn))
}

// layoutRow places a committed row's areas as a strip along the shorter side
// of free, and returns the rectangles plus the remaining free rectangle.
func layoutRow(row []float64, free rect) ([]rect, rect) {
	var sum float64
	for _, a := range row {
		sum += a
	}
	rects := make([]rect, len(row))
	if free.w <= free.h { // horizontal strip across the top, consuming height
		h := sum / free.w
		x := free.x
		for i, a := range row {
			w := a / h
			rects[i] = rect{x, free.y, w, h}
			x += w
		}
		return rects, rect{free.x, free.y + h, free.w, free.h - h}
	}
	// vertical strip down the left, consuming width
	w := sum / free.h
	y := free.y
	for i, a := range row {
		h := a / w
		rects[i] = rect{free.x, y, w, h}
		y += h
	}
	return rects, rect{free.x + w, free.y, free.w - w, free.h}
}

// inset shrinks r by pad on every side, clamping to a non-negative size.
func inset(r rect, pad float64) rect {
	w := math.Max(0, r.w-2*pad)
	h := math.Max(0, r.h-2*pad)
	return rect{r.x + pad, r.y + pad, w, h}
}

// heatScale maps temperature to colour. The upper bound is the 95th percentile
// of T, so a single runaway-hot file does not wash out the whole gradient.
type heatScale struct{ hot float64 }

func heatScaleT(files []pipeline.FileScore) heatScale {
	ts := make([]float64, len(files))
	for i := range files {
		ts[i] = files[i].T
	}
	hot := percentile(ts, 0.95)
	if hot <= 0 {
		hot = 1 // a uniformly-cold repo: avoid divide-by-zero, everything maps to "cool"
	}
	return heatScale{hot: hot}
}

// color maps t to an HSL hue from green (cool, 120°) through yellow to red
// (hot, 0°). SVG understands hsl() directly, so the colour ships in the rect
// fill with no JS.
func (h heatScale) color(t float64) string {
	n := t / h.hot
	if n < 0 {
		n = 0
	}
	if n > 1 {
		n = 1
	}
	hue := 120 * (1 - n)
	return fmt.Sprintf("hsl(%g,70%%,45%%)", round(hue))
}

// percentile returns the p-quantile (0..1) of xs via nearest-rank on a sorted
// copy; xs is not mutated.
func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	idx := int(math.Ceil(p*float64(len(s)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx]
}

// round trims sub-pixel noise from coordinates so the emitted SVG is compact
// and stable.
func round(v float64) float64 { return math.Round(v*100) / 100 }
