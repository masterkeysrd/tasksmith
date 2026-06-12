// Package colorscheme provides a robust, Neovim-inspired system for defining
// and resolving terminal color themes. It supports highlight groups,
// inheritance via linking, and topological resolution.
//
// Builtin themes are stored as JSON files in the builtin/ directory and
// are embedded into the binary. User themes can be loaded from the filesystem.
package colorscheme
