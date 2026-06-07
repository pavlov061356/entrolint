package microstate

import (
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/token"
	"hash/fnv"
	"sort"
)

// Duplication measures copy-pasted structure WITHIN a single Go file:
// the size-weighted amount of redundant, structurally-identical AST
// subtrees. v0.3 MVP — INTRA-FILE only. Two equal subtrees in the same
// file count; a subtree duplicated ACROSS files does not, because that
// needs a whole-tree pre-pass on both refs which breaks the per-file
// Microstate contract `check` relies on to score isolated base/head
// blobs. Cross-file detection is deferred to v0.4+, the same boundary
// Coupling draws for the full Ca/Ce graph (see coupling.go).
//
// Matching is STRUCTURAL, not textual ("hashing AST subtrees, not
// lines"): each subtree is folded Merkle-style into a 64-bit FNV-1a
// digest of its node type/operator tag plus its children's digests,
// with identifier names and literal values NORMALIZED away (every
// *ast.Ident folds to one tag, every *ast.BasicLit keeps only its
// token.Kind). So a block copy-pasted and then renamed — or with
// different constants — still collides (Type-2 clones), which a line
// matcher would miss. Operators are preserved, so `a+b` and `a*b`
// differ. Comment nodes are skipped entirely (the analyzer parses with
// ParseComments, so they are present in the AST) and never affect the
// hash. Hash equality is treated as structural equality without a
// byte-level re-check: a 64-bit collision across the few hundred
// subtrees of one file is astronomically unlikely and would only nudge
// one file's raw value, which the lognormal calibration absorbs — the
// same honesty stance Length and Coupling take.
type Duplication struct{}

// dupMinNodes is the inclusive AST-node-count floor for a subtree to be
// a clone candidate (the ROADMAP's "size threshold"). ~12 nodes ≈ a
// 3-5 line block; below it near-everything (`x := y`, `return nil`,
// `if err != nil { return err }`) collides and the signal is noise.
// Internal default for the v0.3 MVP — deliberately not a config knob.
const dupMinNodes = 12

// Name returns the microstate identifier used in config and reports.
func (Duplication) Name() string { return "duplication" }

// Measure returns the file's intra-file duplication mass: the
// size-weighted count of redundant clone instances. A class of n
// structurally-identical subtrees of size s costs (n-1)·s — the first
// occurrence is the "original" and is free, so a clean file scores 0
// and contributes ln(1+0)=0 to S, exactly like the other microstates.
// Nested clones are not double-counted: a duplicated outer block whose
// duplicated inner block would also match is counted only once, at the
// outermost subtree. Files with a nil AST (parse failure) contribute 0,
// matching Cyclomatic and Coupling.
func (Duplication) Measure(f File) float64 {
	if f.AST == nil {
		return 0
	}
	subs := dupSubtrees(f.AST)
	if len(subs) < 2 {
		return 0
	}

	counts := make(map[uint64]int, len(subs))
	for _, s := range subs {
		counts[s.hash]++
	}

	// Keep only duplicated subtrees, largest first, then drop any that
	// sit inside an already-kept (enclosing) clone so the outer block's
	// nodes are not counted again via a duplicated inner block.
	dup := make([]dupSub, 0, len(subs))
	for _, s := range subs {
		if counts[s.hash] >= 2 {
			dup = append(dup, s)
		}
	}
	sort.Slice(dup, func(i, j int) bool { return dup[i].size > dup[j].size })

	kept := make([]dupSub, 0, len(dup))
	for _, s := range dup {
		if !dupNested(s, kept) {
			kept = append(kept, s)
		}
	}

	// Re-group the outermost clones and sum the redundant mass.
	keptCount := make(map[uint64]int, len(kept))
	sizeByHash := make(map[uint64]int, len(kept))
	for _, s := range kept {
		keptCount[s.hash]++
		sizeByHash[s.hash] = s.size
	}
	mass := 0
	for h, n := range keptCount {
		if n >= 2 {
			mass += (n - 1) * sizeByHash[h]
		}
	}
	return float64(mass)
}

// dupSub is one eligible subtree: its structural digest, inclusive node
// count, and source span (used to detect nesting).
type dupSub struct {
	hash uint64
	size int
	pos  token.Pos
	end  token.Pos
}

// dupNested reports whether s is positionally contained in any already
// kept subtree. Because kept subtrees are processed largest-first, a
// container is necessarily an enclosing clone.
func dupNested(s dupSub, kept []dupSub) bool {
	for _, p := range kept {
		if p.pos <= s.pos && s.end <= p.end {
			return true
		}
	}
	return false
}

// dupSubtrees walks the file once, folding each node's structural digest
// from its tag and its children's digests (Merkle-style, so the whole
// pass is O(nodes)), and returns every statement/expression subtree
// whose node count ≥ dupMinNodes. Comment nodes are skipped so doc and
// inline comments never shape the structure.
func dupSubtrees(file *ast.File) []dupSub {
	type frame struct {
		node     ast.Node
		children []uint64
		size     int
	}
	var stack []*frame
	var out []dupSub

	ast.Inspect(file, func(n ast.Node) bool {
		if n != nil {
			switch n.(type) {
			case *ast.Comment, *ast.CommentGroup:
				return false // never let comments shape the hash or size
			}
			stack = append(stack, &frame{node: n, size: 1})
			return true
		}

		// Exit: pop the node whose children have all been folded.
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		h := dupHash(cur.node, cur.children)
		if dupEligible(cur.node) && cur.size >= dupMinNodes {
			out = append(out, dupSub{hash: h, size: cur.size, pos: cur.node.Pos(), end: cur.node.End()})
		}
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, h)
			parent.size += cur.size
		}
		return true
	})
	return out
}

// dupEligible reports whether a node may seed a clone class. Only
// statements and expressions qualify; declaration-level identity
// (*ast.FuncDecl, *ast.File) is the cross-file concern deferred to v0.4+.
func dupEligible(n ast.Node) bool {
	switch n.(type) {
	case ast.Stmt, ast.Expr:
		return true
	default:
		return false
	}
}

// dupHash folds a node's structural tag with its children's digests.
func dupHash(n ast.Node, children []uint64) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(dupTag(n)))
	var b [8]byte
	for _, c := range children {
		binary.BigEndian.PutUint64(b[:], c)
		_, _ = h.Write(b[:])
	}
	return h.Sum64()
}

// dupTag is a node's normalized structural fingerprint: the concrete
// type, plus operator/literal-kind tokens that carry meaning, but never
// identifier names or literal values — so renamed/re-valued copies
// still match (Type-2 clones).
func dupTag(n ast.Node) string {
	switch x := n.(type) {
	case *ast.Ident:
		return "id" // drop the name: renamed copies must still collide
	case *ast.BasicLit:
		return "lit:" + x.Kind.String() // drop the value, keep the kind
	case *ast.BinaryExpr:
		return "bin:" + x.Op.String()
	case *ast.UnaryExpr:
		return "un:" + x.Op.String()
	case *ast.AssignStmt:
		return "asn:" + x.Tok.String()
	case *ast.IncDecStmt:
		return "inc:" + x.Tok.String()
	case *ast.BranchStmt:
		return "br:" + x.Tok.String()
	default:
		return fmt.Sprintf("%T", n)
	}
}
