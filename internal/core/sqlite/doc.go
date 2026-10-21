// Package sqlite provides a standardized SQLite connection opener and a
// scoped, version-tracked schema migration runner built on [modernc.org/sqlite].
//
// # Connection Pragmas
//
// [Open] configures every connection in the [*database/sql.DB] pool with production
// SQLite pragmas via DSN parameters:
//   - `busy_timeout = 5000` (5 seconds) to prevent transient `SQLITE_BUSY` errors.
//   - `journal_mode = WAL` for concurrent readers and writers.
//   - `foreign_keys = ON` so `ON DELETE CASCADE` and referential integrity are
//     enforced across all pooled connections.
//   - `synchronous = NORMAL` for safe, high-throughput WAL durability.
//
// # Scoped Schema Migrations
//
// [Migrate] maintains an isolated `<scope>_migrations` version table per subsystem
// (for example, `session_migrations`), allowing multiple internal packages to
// manage their own ordered DDL migration slices independently on a shared
// [*database/sql.DB] handle. Each migration step is applied atomically inside a
// transaction alongside its version record.
package sqlite
