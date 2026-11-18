package format

import "fmt"

// Bytes formats a byte count concisely (e.g., "512 B", "142 KB", "2.4 MB").
func Bytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		kb := float64(n) / 1024
		if kb >= 10 || n%1024 == 0 {
			return fmt.Sprintf("%.0f KB", kb)
		}
		return fmt.Sprintf("%.1f KB", kb)
	default:
		mb := float64(n) / (1024 * 1024)
		if mb >= 10 || n%(1024*1024) == 0 {
			return fmt.Sprintf("%.0f MB", mb)
		}
		return fmt.Sprintf("%.1f MB", mb)
	}
}
