package dbutil

func Limit(value, fallback, max int) int {
	if value <= 0 {
		return fallback
	}
	return min(value, max)
}

func Offset(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
