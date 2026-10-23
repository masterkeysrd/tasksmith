// Package timeline manages the persistent, append-only chronological log of session events
// (messages, model/effort/agent/mode transitions, and permission resolutions) stored in
// the timeline SQLite table, ordered by UUIDv7 id ASC.
package timeline
