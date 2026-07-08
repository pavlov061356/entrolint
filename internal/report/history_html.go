package report

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"

	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
)

//go:embed history_html.tmpl
var historyHTMLTmpl string

const (
	historyCanvasW = 1000.0
	historyCanvasH = 520.0
	historyLeft    = 74.0
	historyRight   = 30.0
	historyTop     = 36.0
	historyBottom  = 82.0
)

// HistoryHTML renders the phase-portrait data as a self-contained HTML page.
// It is deterministic for a fixed HistoryResult and embeds no external assets.
func HistoryHTML(res pipeline.HistoryResult) ([]byte, error) {
	data := buildHistoryHTMLData(res)
	t, err := template.New("history").Parse(historyHTMLTmpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type historyHTMLData struct {
	Ref        string
	Width      float64
	Height     float64
	PlotLeft   float64
	PlotRight  float64
	PlotTop    float64
	PlotBottom float64
	XTickY     float64
	XLabelX    float64
	XLabelY    float64
	YLabelX    float64
	YLabelY    float64
	LegendX    float64
	LegendY    float64
	CenterX    float64
	CenterY    float64
	PointCount int
	MaxS       string
	Polyline   string
	Points     []historyHTMLPoint
	XTicks     []historyHTMLXTick
	YTicks     []historyHTMLTick
}

type historyHTMLPoint struct {
	X, Y      float64
	ShortSHA  string
	Date      string
	Subject   string
	S         float64
	FileCount int
	Title     string
}

type historyHTMLTick struct {
	Y     float64
	Label string
}

type historyHTMLXTick struct {
	X      float64
	Label  string
	Anchor string
}

func buildHistoryHTMLData(res pipeline.HistoryResult) historyHTMLData {
	maxS := maxHistoryS(res.Points)
	if maxS <= 0 {
		maxS = 1
	}
	plotLeft := historyLeft
	plotRight := historyCanvasW - historyRight
	plotTop := historyTop
	plotBottom := historyCanvasH - historyBottom
	plotW := plotRight - plotLeft
	plotH := plotBottom - plotTop
	commitTimes := historyCommitTimes(res.Points)
	minTime, maxTime := historyTimeBounds(commitTimes)

	points := make([]historyHTMLPoint, 0, len(res.Points))
	poly := make([]string, 0, len(res.Points))
	for i, p := range res.Points {
		x := historyTimeX(commitTimes[i], minTime, maxTime, plotLeft, plotW)
		y := plotBottom - (p.S/maxS)*plotH
		date := shortDate(p.CommitTime)
		hp := historyHTMLPoint{
			X:         round(x),
			Y:         round(y),
			ShortSHA:  p.ShortSHA,
			Date:      date,
			Subject:   p.Subject,
			S:         p.S,
			FileCount: p.FileCount,
			Title:     fmt.Sprintf("%s %s\nS=%.2f  files=%d\n%s", p.ShortSHA, date, p.S, p.FileCount, p.Subject),
		}
		points = append(points, hp)
		poly = append(poly, fmt.Sprintf("%.2f,%.2f", hp.X, hp.Y))
	}

	return historyHTMLData{
		Ref:        res.Ref,
		Width:      historyCanvasW,
		Height:     historyCanvasH,
		PlotLeft:   plotLeft,
		PlotRight:  plotRight,
		PlotTop:    plotTop,
		PlotBottom: plotBottom,
		CenterX:    historyCanvasW / 2,
		CenterY:    historyCanvasH / 2,
		PointCount: len(res.Points),
		MaxS:       fmt.Sprintf("%.2f", maxHistoryS(res.Points)),
		Polyline:   strings.Join(poly, " "),
		Points:     points,
		XTicks:     historyXTicks(minTime, maxTime, plotLeft, plotRight),
		YTicks:     historyTicks(maxS, plotBottom, plotH),
		XTickY:     plotBottom + 24,
		XLabelX:    historyCanvasW / 2,
		XLabelY:    historyCanvasH - 18,
		YLabelX:    plotLeft,
		YLabelY:    plotTop - 16,
		LegendX:    plotRight - 220,
		LegendY:    plotTop - 18,
	}
}

func maxHistoryS(points []pipeline.HistoryPoint) float64 {
	var maxS float64
	for _, p := range points {
		if p.S > maxS {
			maxS = p.S
		}
	}
	return maxS
}

func historyTicks(maxS, bottomY, plotH float64) []historyHTMLTick {
	values := []float64{0, maxS / 2, maxS}
	ticks := make([]historyHTMLTick, 0, len(values))
	for _, v := range values {
		y := bottomY - (v/maxS)*plotH
		ticks = append(ticks, historyHTMLTick{
			Y:     round(y),
			Label: fmt.Sprintf("%.1f", math.Round(v*10)/10),
		})
	}
	return ticks
}

func historyCommitTimes(points []pipeline.HistoryPoint) []time.Time {
	out := make([]time.Time, len(points))
	for i, p := range points {
		t, err := time.Parse(time.RFC3339, p.CommitTime)
		if err != nil {
			t = time.Unix(int64(i), 0).UTC()
		}
		out[i] = t
	}
	return out
}

func historyTimeBounds(times []time.Time) (time.Time, time.Time) {
	if len(times) == 0 {
		return time.Time{}, time.Time{}
	}
	minT, maxT := times[0], times[0]
	for _, t := range times[1:] {
		if t.Before(minT) {
			minT = t
		}
		if t.After(maxT) {
			maxT = t
		}
	}
	return minT, maxT
}

func historyTimeX(t, minT, maxT time.Time, left, width float64) float64 {
	if maxT.IsZero() || !maxT.After(minT) {
		return round(left + width/2)
	}
	ratio := float64(t.Sub(minT)) / float64(maxT.Sub(minT))
	return round(left + ratio*width)
}

func historyXTicks(minT, maxT time.Time, left, right float64) []historyHTMLXTick {
	if minT.IsZero() {
		return nil
	}
	if !maxT.After(minT) {
		return []historyHTMLXTick{{
			X:      round((left + right) / 2),
			Label:  historyTimeLabel(minT, 0),
			Anchor: "middle",
		}}
	}
	const tickCount = 5
	span := maxT.Sub(minT)
	ticks := make([]historyHTMLXTick, 0, tickCount)
	for i := 0; i < tickCount; i++ {
		ratio := float64(i) / float64(tickCount-1)
		t := minT.Add(time.Duration(float64(span) * ratio))
		ticks = append(ticks, historyHTMLXTick{
			X:      round(left + (right-left)*ratio),
			Label:  historyTimeLabel(t, span),
			Anchor: historyOrdinalAnchor(i, tickCount),
		})
	}
	return ticks
}

func historyOrdinalAnchor(idx, n int) string {
	switch idx {
	case 0:
		return "start"
	case n - 1:
		return "end"
	default:
		return "middle"
	}
}

func historyTimeLabel(t time.Time, span time.Duration) string {
	if span > 0 && span < 72*time.Hour {
		return t.Format("2006-01-02 15:04")
	}
	return t.Format("2006-01-02")
}

func shortDate(authorTime string) string {
	if len(authorTime) >= len("2006-01-02") {
		return authorTime[:len("2006-01-02")]
	}
	return authorTime
}
