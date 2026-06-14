package microstate

import (
	"go/token"
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
	// Two passes over the parsed ASTs. Pass 1 counts eligible subtrees per
	// structural digest; pass 2 materializes a CloneOccurrence slice ONLY for
	// digests that recur — the singleton subtrees, the vast majority of any
	// tree, never cost an occurrence. This trades a second O(nodes) walk over
	// the already-parsed ASTs for the peak memory of holding every unique
	// subtree's occurrence on a large corpus.
	count, size := countCloneCandidates(files)
	out := make(map[uint64]CloneClass)
	for h, n := range count {
		if n >= 2 {
			out[h] = CloneClass{Hash: h, Size: size[h]}
		}
	}
	if len(out) > 0 { // skip the second walk entirely when there are no clones
		collectCloneOccurrences(files, out)
	}
	return out
}

// countCloneCandidates counts eligible subtrees per structural digest and
// records each digest's node count. size[hash] is last-write-wins, which is
// exact: every subtree sharing a digest is structurally identical, so it has
// the same inclusive node count by construction — every write stores the same
// value, whatever the file or traversal order. Pass 1 of CloneIndex.
func countCloneCandidates(files []File) (count, size map[uint64]int) {
	count = make(map[uint64]int)
	size = make(map[uint64]int)
	for _, f := range files {
		if f.AST == nil {
			continue
		}
		for _, s := range dupSubtrees(f.AST) {
			count[s.hash]++
			size[s.hash] = s.size
		}
	}
	return count, size
}

// collectCloneOccurrences appends every occurrence of a surviving (recurring)
// digest to its class. Pass 2 of CloneIndex.
func collectCloneOccurrences(files []File, classes map[uint64]CloneClass) {
	for _, f := range files {
		if f.AST == nil {
			continue
		}
		for _, s := range dupSubtrees(f.AST) {
			if cc, ok := classes[s.hash]; ok {
				cc.Occ = append(cc.Occ, CloneOccurrence{Path: f.Path, pos: s.pos, end: s.end})
				classes[s.hash] = cc
			}
		}
	}
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
// The original is deterministic WITHIN one corpus (lowest path among the
// files present), so extracting a shared helper removes a file from the
// class, lowers that file's mass, and yields the intended negative ΔS.
// Cross-ref ΔS honesty needs one more guarantee the microstate cannot give
// alone: the same clone class must have the SAME member set at base and
// head. A class member that exists on both refs but parses on only one
// (a mid-edit syntax error, an unreadable blob) would be present in one
// corpus and absent from the other, shifting the original and perturbing a
// sibling's ΔS. `check` enforces the missing guarantee by holding such
// files out of both corpora (pipeline.asymmetricParseExclusions, issue #68).
// The only residual is a file that fails to parse at BOTH refs — it is in
// no corpus, which is correct — or a blob that flakes on fetch, an I/O
// anomaly outside this layer's control.
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

// outermostMass charges one class size per cross-file clone class the file
// participates in, after dropping occurrences nested inside an already-counted
// larger clone — the same outermost-only rule Duplication uses, via the shared
// outermostSubtrees helper, so an inner clone is not double-counted under its
// enclosing clone. The kept set is independent of source layout and map
// iteration order (see outermostSubtrees). A class appearing several non-nested
// times in one file is still charged once (each non-original file pays its
// class size a single time).
func outermostMass(cs []dupSub) float64 {
	kept := outermostSubtrees(cs)
	charged := make(map[uint64]bool, len(kept))
	mass := 0
	for _, c := range kept {
		if !charged[c.hash] {
			charged[c.hash] = true
			mass += c.size
		}
	}
	return float64(mass)
}
