// Package memdb provides a generic, concurrency-safe, copy-on-write in-memory
// store following the point-in-time snapshot pattern. Readers obtain lock-free
// immutable views, while writers mutate atomically through transactions or direct
// mutation methods.
package memdb
