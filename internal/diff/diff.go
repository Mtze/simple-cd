// Package diff renders a small line-based unified diff for the confirmation
// dialog. Inputs are config files of a few hundred lines, so a plain LCS is enough.
package diff

import (
	"fmt"
	"strings"
)

// maxCells caps the LCS table so a huge file cannot exhaust memory.
const maxCells = 4_000_000

// Unified returns a unified diff with three lines of context, or "" if equal.
func Unified(oldName, newName, a, b string) string {
	if a == b {
		return ""
	}
	x, y := split(a), split(b)
	if len(x)*len(y) > maxCells {
		return fmt.Sprintf("--- %s\n+++ %s\n(file too large to diff: %d -> %d lines)\n", oldName, newName, len(x), len(y))
	}
	ops := lcsOps(x, y)

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", oldName, newName)
	const ctx = 3
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		// Grow a hunk around this change, merging changes closer than 2*ctx.
		start := max(0, i-ctx)
		end := i
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' {
				run++
			}
			if run == len(ops) || run-end > 2*ctx {
				end = min(len(ops), end+ctx)
				break
			}
			end = run
		}
		oldStart, newStart := ops[start].oldLine, ops[start].newLine
		var oldN, newN int
		for _, o := range ops[start:end] {
			if o.kind != '+' {
				oldN++
			}
			if o.kind != '-' {
				newN++
			}
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", oldStart+1, oldN, newStart+1, newN)
		for _, o := range ops[start:end] {
			sb.WriteByte(o.kind)
			sb.WriteString(o.text)
			sb.WriteByte('\n')
		}
		i = end
	}
	return sb.String()
}

type op struct {
	kind             byte // ' ', '-', '+'
	text             string
	oldLine, newLine int
}

func lcsOps(x, y []string) []op {
	n, m := len(x), len(y)
	// table[i][j] = LCS length of x[i:] and y[j:]
	table := make([][]int32, n+1)
	for i := range table {
		table[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else {
				table[i][j] = max(table[i+1][j], table[i][j+1])
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && x[i] == y[j]:
			ops = append(ops, op{' ', x[i], i, j})
			i++
			j++
		case i < n && (j == m || table[i+1][j] >= table[i][j+1]):
			ops = append(ops, op{'-', x[i], i, j})
			i++
		default:
			ops = append(ops, op{'+', y[j], i, j})
			j++
		}
	}
	return ops
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
