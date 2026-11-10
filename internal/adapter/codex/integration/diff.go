package integration

import (
	"fmt"
	"strings"
)

// diffContext is the number of unchanged lines around a change.
const diffContext = 3

// maxDiffCells bounds the table of the line diff; beyond it the changed middle shows as removed and added.
const maxDiffCells = 1 << 22

type diffOp struct {
	kind byte // ' ', '-' or '+'
	line string
}

// unifiedDiff is the unified diff of one file; a missing file reads as /dev/null. It is empty when nothing
// changes.
func unifiedDiff(path string, old, new []byte, oldExists, newExists bool) string {
	if oldExists == newExists && string(old) == string(new) {
		return ""
	}
	ops := diffLines(splitLines(string(old)), splitLines(string(new)))
	from, to := mask(path), mask(path)
	if !oldExists {
		from = "/dev/null"
	}
	if !newExists {
		to = "/dev/null"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", from, to)
	writeHunks(&b, ops)
	return b.String()
}

// splitLines splits text after each line break; the last line may lack one.
func splitLines(s string) []string {
	var out []string
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return append(out, s)
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

func diffLines(a, b []string) []diffOp {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var ops []diffOp
	for _, l := range a[:pre] {
		ops = append(ops, diffOp{' ', l})
	}
	am, bm := a[pre:len(a)-suf], b[pre:len(b)-suf]
	n, m := len(am), len(bm)
	if n*m > maxDiffCells {
		for _, l := range am {
			ops = append(ops, diffOp{'-', l})
		}
		for _, l := range bm {
			ops = append(ops, diffOp{'+', l})
		}
	} else {
		// lcs[i][j] is the length of the longest common subsequence of am[i:] and bm[j:].
		lcs := make([][]int32, n+1)
		for i := range lcs {
			lcs[i] = make([]int32, m+1)
		}
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if am[i] == bm[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < n || j < m {
			switch {
			case i < n && j < m && am[i] == bm[j]:
				ops = append(ops, diffOp{' ', am[i]})
				i, j = i+1, j+1
			case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
				ops = append(ops, diffOp{'-', am[i]})
				i++
			default:
				ops = append(ops, diffOp{'+', bm[j]})
				j++
			}
		}
	}
	for _, l := range a[len(a)-suf:] {
		ops = append(ops, diffOp{' ', l})
	}
	return ops
}

func writeHunks(b *strings.Builder, ops []diffOp) {
	// oldAt[k] and newAt[k] count the old and new lines before ops[k].
	oldAt, newAt := make([]int, len(ops)+1), make([]int, len(ops)+1)
	for k, op := range ops {
		oldAt[k+1], newAt[k+1] = oldAt[k], newAt[k]
		if op.kind != '+' {
			oldAt[k+1]++
		}
		if op.kind != '-' {
			newAt[k+1]++
		}
	}
	for i := 0; i < len(ops); {
		for i < len(ops) && ops[i].kind == ' ' {
			i++
		}
		if i == len(ops) {
			return
		}
		start, end := max(0, i-diffContext), i
		for j := i; j < len(ops); {
			if ops[j].kind != ' ' {
				j++
				end = j
				continue
			}
			k := j
			for k < len(ops) && ops[k].kind == ' ' {
				k++
			}
			if k == len(ops) || k-j > 2*diffContext {
				break
			}
			j = k
		}
		stop := min(len(ops), end+diffContext)
		fmt.Fprintf(b, "@@ -%s +%s @@\n", hunkRange(oldAt[start], oldAt[stop]-oldAt[start]),
			hunkRange(newAt[start], newAt[stop]-newAt[start]))
		for _, op := range ops[start:stop] {
			line, broken := strings.CutSuffix(op.line, "\n")
			b.WriteByte(op.kind)
			b.WriteString(mask(line))
			b.WriteByte('\n')
			if !broken {
				b.WriteString("\\ No newline at end of file\n")
			}
		}
		i = stop
	}
}

func hunkRange(before, count int) string {
	if count == 0 {
		return fmt.Sprintf("%d,0", before)
	}
	return fmt.Sprintf("%d,%d", before+1, count)
}
