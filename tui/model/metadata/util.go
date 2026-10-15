package metadata

import "charm.land/lipgloss/v2"

func jH(strs ...string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, insertSpacesInbetween(strs)...)
}

func jV(strs ...string) string {
	return lipgloss.JoinVertical(lipgloss.Left, insertSpacesInbetween(strs)...)
}

func insertSpacesInbetween(strs []string) []string {
	size := len(strs)
	newStrs := make([]string, 2*size-1)
	for i := range strs {
		newStrs[2*i] = strs[i]
		if i < size-1 {
			newStrs[2*i+1] = " "
		}
	}
	return newStrs
}
