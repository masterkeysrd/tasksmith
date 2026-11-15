// Package models provides a single, effective description of a model within a
// workspace.
//
// A model is named by one [ID] ("provider/model") and described by one
// immutable [Model], built once when the workspace loads (and on reload). The
// [Model] merges the workspace manifest layer (a workspace.ProviderModel) with
// the built-in provider profile layer (a loom profile.Profile), applying a single
// set of merge rules. After it is built, every lookup is a map read.
//
// Call preferences are validated against a [Model] by [Configure], a pure
// function that returns the effective loom configuration. Instances of
// loom models are cheap to construct, so they are built on demand by the
// gateway and never cached.
//
// The [Catalog] holds the built models of one workspace and is the only place
// resolution happens; clients receive resolved model data and never re-implement
// resolution.
package models
