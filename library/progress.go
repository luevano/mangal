package library

import "fmt"

// Progress is where a long library operation is, for the OnProgress callbacks: library
// code never prints.
type Progress struct {
	// Phase is what's being done, e.g. "scanning library".
	Phase string
	// Done is the number of the current item of Total, Total is 0 when there's no count.
	Done, Total int
	// Item is the current item (e.g. a manga title), may be empty.
	Item string
}

func (p Progress) String() string {
	s := p.Phase
	if p.Total > 0 {
		s += fmt.Sprintf(": %d/%d", p.Done, p.Total)
	}
	if p.Item != "" {
		s += " (" + p.Item + ")"
	}
	return s
}

// report calls onProgress, if set.
func report(onProgress func(Progress), p Progress) {
	if onProgress != nil {
		onProgress(p)
	}
}
