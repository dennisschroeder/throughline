package dashboard

import "strings"

// lineDiff computes a minimal added/removed/context line diff between two texts using a
// classic LCS backtrack. Texts here are plan summaries and item titles — a handful of short
// lines at most — so the O(n*m) table is trivially cheap; this exists only to answer "what
// changed since the last accepted revision" in the drawer's Evidence section without
// shipping a diff library.
func lineDiff(oldText, newText string) []DiffLine {
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)
	n, m := len(oldLines), len(newLines)

	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var out []DiffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case oldLines[i] == newLines[j]:
			out = append(out, DiffLine{Kind: "context", Text: oldLines[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, DiffLine{Kind: "removed", Text: oldLines[i]})
			i++
		default:
			out = append(out, DiffLine{Kind: "added", Text: newLines[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, DiffLine{Kind: "removed", Text: oldLines[i]})
	}
	for ; j < m; j++ {
		out = append(out, DiffLine{Kind: "added", Text: newLines[j]})
	}
	return out
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
