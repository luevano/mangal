package base

// history is the stack of the States under the shown one, the oldest first.
type history struct {
	states []State
}

// Push appends a new State to the history.
func (h *history) Push(state State) {
	h.states = append(h.states, state)
}

// Pop returns the last State from the history and removes it.
// If there are no more items, returns nil.
func (h *history) Pop() State {
	last := h.Peek()
	if last != nil {
		h.states = h.states[:h.Size()-1]
	}
	return last
}

// Peek returns the last State from the history.
// If there are no more items, returns nil.
func (h *history) Peek() State {
	if h.Size() == 0 {
		return nil
	}
	return h.states[h.Size()-1]
}

// Size returns the amount of States in the history.
func (h *history) Size() int {
	return len(h.states)
}

// Find is the index of the State that is state or the same window (its key), -1 if none.
func (h *history) Find(state State) int {
	key := keyOf(state)
	for i := len(h.states) - 1; i >= 0; i-- {
		if s := h.states[i]; s == state || (key != "" && keyOf(s) == key) {
			return i
		}
	}
	return -1
}
