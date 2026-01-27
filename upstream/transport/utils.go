package transport

// derefOrDefaultVal returns the dereferenced value of an *int, or a default if nil.
func derefOrDefaultVal(ptr *int, defaultValue int) int {
	if ptr != nil {
		return *ptr
	}
	return defaultValue
}
