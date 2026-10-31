package ptr

// Clone returns a shallow copy of p, or nil if p is nil.
func Clone[T any](p *T) *T {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}
