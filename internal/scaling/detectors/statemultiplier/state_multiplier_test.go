package statemultiplier

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

// fixture writes files to a t.TempDir() module. The map's keys are
// relative paths; values are the file bodies. go.mod is auto-added
// if missing.
func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module example.com/probe\n\ngo 1.21\n"
	}
	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return dir
}

// scenario sets up: head-state files on disk + N caller packages
// importing the api pkg. base and head blobs for api.go are passed
// separately; the on-disk api.go equals head (typesx reads from disk).
func scenario(t *testing.T, baseAPI, headAPI string, callerCount int, callerBody func(i int) string) (string, scaling.Input) {
	t.Helper()
	files := map[string]string{
		"go.mod":     "module example.com/probe\n\ngo 1.21\n",
		"api/api.go": headAPI,
	}
	for i := 0; i < callerCount; i++ {
		files[fmt.Sprintf("use%d/use.go", i)] = callerBody(i)
	}
	dir := fixture(t, files)
	in := scaling.Input{
		Root: dir,
		Changes: []gitx.Change{
			{Kind: gitx.ChangeModified, Path: "api/api.go"},
		},
		BaseBlobs: map[string][]byte{"api/api.go": []byte(baseAPI)},
		HeadBlobs: map[string][]byte{"api/api.go": []byte(headAPI)},
	}
	return dir, in
}

func boolCaller(i int) string {
	return fmt.Sprintf(`package use%d

import "example.com/probe/api"

func Caller() string { return api.Format("x", false) }
`, i)
}

func headBoolAPI() string {
	return `package api

func Format(s string, legacy bool) string {
	if legacy {
		return s
	}
	return s + "!"
}
`
}

func baseAPI() string {
	return `package api

func Format(s string) string { return s + "!" }
`
}

func TestStateMultiplier_BoolAddedFiresOnExternalSites(t *testing.T) {
	_, in := scenario(t, baseAPI(), headBoolAPI(), 2, boolCaller)
	hits := New().Analyze(in)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit on bool-added with 2 external sites, got %d: %+v", len(hits), hits)
	}
	h := hits[0]
	if h.Class != scaling.ClassO2N {
		t.Errorf("class = %v, want O(2ⁿ)", h.Class)
	}
	if h.Size != 2 {
		t.Errorf("size = %d, want 2 external sites", h.Size)
	}
	if h.Path != filepath.Join("api", "api.go") {
		t.Errorf("Path = %q, want api/api.go", h.Path)
	}
	if h.Detector != name {
		t.Errorf("detector = %q, want %q", h.Detector, name)
	}
}

func TestStateMultiplier_BelowMinSitesNoFire(t *testing.T) {
	_, in := scenario(t, baseAPI(), headBoolAPI(), 1, boolCaller)
	hits := New().Analyze(in)
	if len(hits) != 0 {
		t.Errorf("1 external site is below default MinExternalSites=2; got %d hits", len(hits))
	}
}

func TestStateMultiplier_NoExternalSitesNoFire(t *testing.T) {
	// Internal caller in api pkg itself — not external.
	headAPI := `package api

func Format(s string, legacy bool) string {
	if legacy {
		return s
	}
	return s + "!"
}

func internalCaller() string { return Format("x", false) }
`
	dir := fixture(t, map[string]string{
		"api/api.go": headAPI,
	})
	in := scaling.Input{
		Root:      dir,
		Changes:   []gitx.Change{{Kind: gitx.ChangeModified, Path: "api/api.go"}},
		BaseBlobs: map[string][]byte{"api/api.go": []byte(baseAPI())},
		HeadBlobs: map[string][]byte{"api/api.go": []byte(headAPI)},
	}
	hits := New().Analyze(in)
	if len(hits) != 0 {
		t.Errorf("internal callers must not count; got %d hits", len(hits))
	}
}

func TestStateMultiplier_NoNewParamNoFire(t *testing.T) {
	// Body changed but signature identical — must not fire.
	head := `package api

func Format(s string) string {
	return "[" + s + "]"
}
`
	_, in := scenario(t, baseAPI(), head, 2, func(i int) string {
		return fmt.Sprintf(`package use%d

import "example.com/probe/api"

func Caller() string { return api.Format("x") }
`, i)
	})
	hits := New().Analyze(in)
	if len(hits) != 0 {
		t.Errorf("no param added; got %d hits", len(hits))
	}
}

func TestStateMultiplier_NewIntParamNoFire(t *testing.T) {
	// New param is int — not state-multiplying. Spec says bool or enum.
	head := `package api

func Format(s string, n int) string {
	return s
}
`
	_, in := scenario(t, baseAPI(), head, 3, func(i int) string {
		return fmt.Sprintf(`package use%d

import "example.com/probe/api"

func Caller() string { return api.Format("x", 0) }
`, i)
	})
	hits := New().Analyze(in)
	if len(hits) != 0 {
		t.Errorf("new int param is not state-multiplying; got %d hits", len(hits))
	}
}

func TestStateMultiplier_EnumParamFires(t *testing.T) {
	// New param is a named enum type with ≥2 consts of its type.
	files := map[string]string{
		"go.mod":     "module example.com/probe\n\ngo 1.21\n",
		"api/api.go": "package api\n\ntype Mode int\n\nconst (\n\tModeA Mode = iota\n\tModeB\n)\n\nfunc Format(s string, m Mode) string { return s }\n",
		"use0/use.go": `package use0

import "example.com/probe/api"

func Caller() string { return api.Format("x", api.ModeA) }
`,
		"use1/use.go": `package use1

import "example.com/probe/api"

func Caller() string { return api.Format("x", api.ModeB) }
`,
	}
	dir := fixture(t, files)
	base := "package api\n\ntype Mode int\n\nconst (\n\tModeA Mode = iota\n\tModeB\n)\n\nfunc Format(s string) string { return s }\n"
	in := scaling.Input{
		Root:      dir,
		Changes:   []gitx.Change{{Kind: gitx.ChangeModified, Path: "api/api.go"}},
		BaseBlobs: map[string][]byte{"api/api.go": []byte(base)},
		HeadBlobs: map[string][]byte{"api/api.go": []byte(files["api/api.go"])},
	}
	hits := New().Analyze(in)
	if len(hits) != 1 {
		t.Fatalf("expected 1 enum-param hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].Class != scaling.ClassO2N {
		t.Errorf("class = %v, want O(2ⁿ)", hits[0].Class)
	}
}

func TestStateMultiplier_UnexportedFunctionIgnored(t *testing.T) {
	// Lowercase fn — even with cross-pkg refs (impossible in Go, but
	// the export check should reject before refs are counted).
	base := `package api

func format(s string) string { return s }
`
	head := `package api

func format(s string, legacy bool) string { return s }
`
	dir := fixture(t, map[string]string{"api/api.go": head})
	in := scaling.Input{
		Root:      dir,
		Changes:   []gitx.Change{{Kind: gitx.ChangeModified, Path: "api/api.go"}},
		BaseBlobs: map[string][]byte{"api/api.go": []byte(base)},
		HeadBlobs: map[string][]byte{"api/api.go": []byte(head)},
	}
	hits := New().Analyze(in)
	if len(hits) != 0 {
		t.Errorf("unexported fn must not fire; got %d", len(hits))
	}
}

func TestStateMultiplier_MissingBaseBlobNoFire(t *testing.T) {
	dir := fixture(t, map[string]string{"api/api.go": headBoolAPI()})
	in := scaling.Input{
		Root:      dir,
		Changes:   []gitx.Change{{Kind: gitx.ChangeModified, Path: "api/api.go"}},
		BaseBlobs: nil,
		HeadBlobs: map[string][]byte{"api/api.go": []byte(headBoolAPI())},
	}
	hits := New().Analyze(in)
	if len(hits) != 0 {
		t.Errorf("missing base blob → soft-skip; got %d hits", len(hits))
	}
}

func TestStateMultiplier_EmptyRootSkipsSilently(t *testing.T) {
	in := scaling.Input{
		Changes:   []gitx.Change{{Kind: gitx.ChangeModified, Path: "api/api.go"}},
		BaseBlobs: map[string][]byte{"api/api.go": []byte(baseAPI())},
		HeadBlobs: map[string][]byte{"api/api.go": []byte(headBoolAPI())},
	}
	if hits := New().Analyze(in); len(hits) != 0 {
		t.Errorf("empty Root must soft-skip, got %d hits", len(hits))
	}
}

func TestStateMultiplier_MethodReceiverFires(t *testing.T) {
	// Method on exported type gains a bool param; ≥2 external callers.
	files := map[string]string{
		"go.mod":     "module example.com/probe\n\ngo 1.21\n",
		"api/api.go": "package api\n\ntype Client struct{}\n\nfunc (c *Client) Send(msg string, async bool) error { return nil }\n",
		"use0/use.go": `package use0

import "example.com/probe/api"

func Caller(c *api.Client) error { return c.Send("x", false) }
`,
		"use1/use.go": `package use1

import "example.com/probe/api"

func Caller(c *api.Client) error { return c.Send("x", true) }
`,
	}
	dir := fixture(t, files)
	base := "package api\n\ntype Client struct{}\n\nfunc (c *Client) Send(msg string) error { return nil }\n"
	in := scaling.Input{
		Root:      dir,
		Changes:   []gitx.Change{{Kind: gitx.ChangeModified, Path: "api/api.go"}},
		BaseBlobs: map[string][]byte{"api/api.go": []byte(base)},
		HeadBlobs: map[string][]byte{"api/api.go": []byte(files["api/api.go"])},
	}
	hits := New().Analyze(in)
	if len(hits) != 1 {
		t.Fatalf("method+bool fires with 2 ext sites; got %d hits", len(hits))
	}
	if hits[0].Class != scaling.ClassO2N {
		t.Errorf("class = %v, want O(2ⁿ)", hits[0].Class)
	}
}

func TestStateMultiplier_RegisteredInRegistry(t *testing.T) {
	found := false
	for _, d := range scaling.Registry {
		if d.Name() == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("state_multiplier not registered; got %d entries", len(scaling.Registry))
	}
}

func TestStateMultiplier_MiddleInsertedParamMissed(t *testing.T) {
	// v0.2 MVP only catches "appended at end" case. A middle-inserted
	// bool slips through. This test pins the documented limitation —
	// fixing it goes to v0.3.
	base := `package api

func Format(s string, n int) string { return s }
`
	head := `package api

func Format(s string, legacy bool, n int) string { return s }
`
	_, in := scenario(t, base, head, 3, func(i int) string {
		return fmt.Sprintf(`package use%d

import "example.com/probe/api"

func Caller() string { return api.Format("x", false, 0) }
`, i)
	})
	hits := New().Analyze(in)
	if len(hits) != 0 {
		t.Errorf("middle-inserted param is documented v0.2 miss; got %d hits", len(hits))
	}
}

func TestStateMultiplier_ParamRenamedNotRetypedNoFire(t *testing.T) {
	// Same arity, same types, just renamed Names — must not fire.
	base := `package api

func Format(s string) string { return s }
`
	head := `package api

func Format(payload string) string { return payload }
`
	_, in := scenario(t, base, head, 2, func(i int) string {
		return fmt.Sprintf(`package use%d

import "example.com/probe/api"

func Caller() string { return api.Format("x") }
`, i)
	})
	hits := New().Analyze(in)
	if len(hits) != 0 {
		t.Errorf("rename without retype must not fire; got %d", len(hits))
	}
}

func TestStateMultiplier_PointerToBoolNoFire(t *testing.T) {
	// `*bool` parameter models tri-state (nil / false / true) — by
	// the v0.2 policy that's an option, not a state-multiplying
	// branch. Pin the no-fire so a future refactor of
	// stateMultiplierKind doesn't accidentally flip it.
	base := `package api

func Format(s string) string { return s }
`
	head := `package api

func Format(s string, legacy *bool) string { return s }
`
	_, in := scenario(t, base, head, 3, func(i int) string {
		return fmt.Sprintf(`package use%d

import "example.com/probe/api"

func Caller() string { return api.Format("x", nil) }
`, i)
	})
	if hits := New().Analyze(in); len(hits) != 0 {
		t.Errorf("*bool must not be state-multiplying; got %d hits", len(hits))
	}
}

func TestStateMultiplier_UnparseableBaseSoftSkips(t *testing.T) {
	// Legit-broken Go in the base blob — parser fails, analyzeFile
	// returns nil for the file, no panic, no fire.
	root := fixture(t, map[string]string{
		"api/api.go": "package api\n\nfunc Format(s string, legacy bool) string { return s }\n",
		"use0/use.go": `package use0

import "example.com/probe/api"

func Caller() string { return api.Format("x", false) }
`,
		"use1/use.go": `package use1

import "example.com/probe/api"

func Caller() string { return api.Format("x", true) }
`,
	})
	broken := []byte("package api\nfunc Format(") // unparseable
	in := scaling.Input{
		Root:      root,
		Changes:   []gitx.Change{{Kind: gitx.ChangeModified, Path: "api/api.go"}},
		BaseBlobs: map[string][]byte{"api/api.go": broken},
		HeadBlobs: map[string][]byte{"api/api.go": []byte("package api\n\nfunc Format(s string, legacy bool) string { return s }\n")},
	}
	if hits := New().Analyze(in); len(hits) != 0 {
		t.Errorf("unparseable base blob must soft-skip; got %d hits", len(hits))
	}
}

func TestStateMultiplier_CrossPackageFuncNameCollisionResolved(t *testing.T) {
	// Two packages both declare `func Format(s string) string`. Only
	// pkg api gets +bool. countExternalSites must anchor on the file
	// path (api/api.go) and count only uses of api.Format, NOT uses
	// of util.Format (which exists in other pkg but didn't change).
	files := map[string]string{
		"go.mod":     "module example.com/probe\n\ngo 1.21\n",
		"api/api.go": "package api\n\nfunc Format(s string, legacy bool) string { return s }\n",
		"util/util.go": `package util

func Format(s string) string { return s + "!" }
`,
		"use0/use.go": `package use0

import "example.com/probe/api"

func Caller() string { return api.Format("x", false) }
`,
		"use1/use.go": `package use1

import (
	"example.com/probe/api"
	"example.com/probe/util"
)

func Caller() string { return api.Format("x", false) + util.Format("y") }
`,
		"use2/use.go": `package use2

import "example.com/probe/util"

// Calls util.Format ONLY — must NOT count toward state_multiplier's
// hit on api.Format.
func Caller() string { return util.Format("z") }
`,
	}
	dir := fixture(t, files)
	in := scaling.Input{
		Root:      dir,
		Changes:   []gitx.Change{{Kind: gitx.ChangeModified, Path: "api/api.go"}},
		BaseBlobs: map[string][]byte{"api/api.go": []byte("package api\n\nfunc Format(s string) string { return s }\n")},
		HeadBlobs: map[string][]byte{"api/api.go": []byte(files["api/api.go"])},
	}
	hits := New().Analyze(in)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit anchored on api.Format, got %d: %+v", len(hits), hits)
	}
	if hits[0].Size != 2 {
		t.Errorf("Size = %d, want 2 (use0 + use1; use2 calls util.Format only)", hits[0].Size)
	}
}

func TestStateMultiplier_InternalCallersExcluded(t *testing.T) {
	// 2 external callers + 1 internal caller. The internal caller
	// MUST NOT count: Size == 2, not 3.
	files := map[string]string{
		"go.mod": "module example.com/probe\n\ngo 1.21\n",
		"api/api.go": `package api

func Format(s string, legacy bool) string { return s }

func internalCaller() string { return Format("x", false) }
`,
		"use0/use.go": `package use0

import "example.com/probe/api"

func Caller() string { return api.Format("x", false) }
`,
		"use1/use.go": `package use1

import "example.com/probe/api"

func Caller() string { return api.Format("x", true) }
`,
	}
	dir := fixture(t, files)
	in := scaling.Input{
		Root:      dir,
		Changes:   []gitx.Change{{Kind: gitx.ChangeModified, Path: "api/api.go"}},
		BaseBlobs: map[string][]byte{"api/api.go": []byte("package api\n\nfunc Format(s string) string { return s }\n")},
		HeadBlobs: map[string][]byte{"api/api.go": []byte(files["api/api.go"])},
	}
	hits := New().Analyze(in)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].Size != 2 {
		t.Errorf("Size = %d, want 2 (internal caller excluded)", hits[0].Size)
	}
}
