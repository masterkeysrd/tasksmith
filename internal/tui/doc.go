// Package tui provides the terminal user interface for TaskSmith.
//
// It is built using the kite framework and follows a component-based architecture
// inspired by React. The package orchestrates the application's lifecycle within
// the terminal, managing engine initialization, rendering, and global providers.
//
// The root of the interface is the App component, which wraps the entire application
// in necessary context providers for API access (via the api package) and
// semantic styling (via the highlight package).
//
// Key sub-packages include:
//   - api: Reactive client context for workspace interactions.
//   - colorscheme: Theme definition and resolution logic.
//   - highlight: Semantic group registry and style propagation.
//   - keymap: Mode-aware input handling and sequence resolution.
//   - mode: TUI-specific input state management (Normal, Insert, Command).
//   - command: Global action registry and execution.
//   - queries: Reactive data hooks for fetching workspace resources.
//   - styles: Utilities for building Kite styles from colorschemes.
package tui
