package microstate

import (
	"go/token"
	"sort"
)

// CrossDuplication measures copy-pasted structure ACROSS files: the
// size-weighted redundant mass attributed to a file from clone classes
// whose structurally-identical subtrees appear in more than one file.
// It is the cross-file complement of Duplication, which stays intra-file;
// the two partition the clone space with no overlap (see CrossDupMassByFile).
//
// It uses the same structural Merkle digest as Duplication, so a block
// copy-pasted between files — even after renaming identifiers — still
// collides. Unlike Duplication it cannot derive its value from a single
// File: it reads the pre-computed per-file mass from File.Corpus, which
// the corpus pre-pass populated by running CrossDupMassByFile over the
// whole tree at this file's ref. A File with no Corpus contributes 0,
// so isolated scoring (unit tests, corpus-less paths) never panics.
type CrossDuplication struct{}

// Name returns the microstate identifier used in config and reports.
func (CrossDuplication) Name() string { return "cross_duplication" }

// Measure returns the file's cross-file duplication mass from the
// pre-computed corpus context, or 0 when no corpus is attached or the
// file has no cross-file clones.
func (CrossDuplication) Measure(f File) float64 {
	if f.Corpus == nil {
		return 0
	}
	return f.Corpus.CrossDupMass(f.Path)
}

// CloneOccurrence is one appearance of a clone class in a corpus file.
// pos/end are token.Pos in that file's own FileSet and are only ever
// compared against occurrences in the SAME file (nesting suppression);
// they are meaningless across files.
type CloneOccurrence struct {
	Path string
	pos  token.Pos
	end  token.Pos
}

// CloneClass is a set of structurally-identical subtrees (one Merkle
// digest) found across a corpus, each Size AST nodes large.
type CloneClass struct {
	Hash uint64
	Size int
	Occ  []CloneOccurrence
}

// CloneIndex groups every eligible AST subtree (≥ dupMinNodes nodes)
// across the corpus by its structural digest, keeping only digests that
// occur at least twice (a clone needs two copies). Files with a nil AST
// are skipped. It drives the same private kernel Duplication uses
// (dupSubtrees, dupHash, dupMinNodes), so intra- and cross-file detection
// stay structurally identical. Exposed for reporting (which files share
// a block) and tests.
func CloneIndex(files []File) map[uint64]CloneClass {
	occ := make(map[uint64][]CloneOccurrence)
	size := make(map[uint64]int)
	for _, f := range files {
		if f.AST == nil {
			continue
		}
		for _, s := range dupSubtrees(f.AST) {
			occ[s.hash] = append(occ[s.hash], CloneOccurrence{Path: f.Path, pos: s.pos, end: s.end})
			size[s.hash] = s.size
		}
	}
	out := make(map[uint64]CloneClass, len(occ))
	for h, os := range occ {
		if len(os) < 2 {
			continue
		}
		out[h] = CloneClass{Hash: h, Size: size[h], Occ: os}
	}
	return out
}

// CrossDupMassByFile computes, per file path, the cross-file duplication
// mass attributed to that file. The attribution cleanly partitions clone
// redundancy with intra-file Duplication: a clone class appearing N times
// across D distinct files carries (N-1)·s total redundancy; the intra-file
// part Σ_file (count_file-1)·s = (N-D)·s is Duplication's, and the remaining
// (D-1)·s cross-file part is charged here — one of the D files (the
// lexicographically lowest path, deterministically the "original") pays
// nothing, every other file pays the class size s once. Per-file nesting
// suppression then drops a charged inner clone sitting inside a charged
// outer clone, mirroring Duplication's outermost-only rule.
//
// The deterministic original (computed independently at each ref) keeps
// ΔS honest: extracting a shared helper removes a file from the class,
// lowering that file's mass and yielding the intended negative ΔS.
func CrossDupMassByFile(files []File) map[string]float64 {
	charges := crossFileCharges(CloneIndex(files))
	out := make(map[string]float64, len(charges))
	for path, cs := range charges {
		out[path] = outermostMass(cs)
	}
	return out
}

// crossFileCharges maps each non-original file to every occurrence of
// each cross-file class (≥2 distinct files) it participates in. ALL
// occurrences are recorded — not one representative — so the nesting
// suppression in outermostMass sees real positions and the result is
// independent of source layout. The lexicographically-lowest file in a
// class is its free "original". Intra-file-only classes are skipped —
// they belong to the Duplication microstate.
func crossFileCharges(idx map[uint64]CloneClass) map[string][]dupSub {
	charges := make(map[string][]dupSub)
	for _, cc := range idx {
		files, original := distinctFilesAndOriginal(cc)
		if len(files) < 2 {
			continue
		}
		for _, o := range cc.Occ {
			if o.Path != original {
				charges[o.Path] = append(charges[o.Path], dupSub{hash: cc.Hash, size: cc.Size, pos: o.pos, end: o.end})
			}
		}
	}
	return charges
}

// distinctFilesAndOriginal returns the set of distinct files a class
// appears in and the lexicographically lowest one (the free "original").
func distinctFilesAndOriginal(cc CloneClass) (files map[string]bool, original string) {
	files = make(map[string]bool)
	for _, o := range cc.Occ {
		files[o.Path] = true
		if original == "" || o.Path < original {
			original = o.Path
		}
	}
	return files, original
}

// outermostMass charges one class size per cross-file clone class the
// file participates in, after dropping occurrences nested inside an
// already-counted larger clone — mirroring Duplication's outermost-only
// rule so an inner clone is not double-counted under its enclosing clone.
// Occurrences are ordered by (size desc, pos asc, end asc) — a total
// order, since each AST node has a unique span within its file — so the
// charge is independent of source layout and map iteration order. A class
// appearing several non-nested times in one file is still charged once
// (each non-original file pays its class size a single time).
func outermostMass(cs []dupSub) float64 {
	sort.Slice(cs, func(i, j int) bool {
		switch {
		case cs[i].size != cs[j].size:
			return cs[i].size > cs[j].size
		case cs[i].pos != cs[j].pos:
			return cs[i].pos < cs[j].pos
		default:
			return cs[i].end < cs[j].end
		}
	})
	kept := make([]dupSub, 0, len(cs))
	charged := make(map[uint64]bool, len(cs))
	mass := 0
	for _, c := range cs {
		if dupNested(c, kept) {
			continue
		}
		kept = append(kept, c)
		if !charged[c.hash] {
			charged[c.hash] = true
			mass += c.size
		}
	}
	return float64(mass)
}
