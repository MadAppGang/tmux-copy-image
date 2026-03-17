// Package embedded exposes the tmux plugin files embedded into the binary at
// build time. The plugin/ directory is a symlink to ../../plugin at the project
// root; go:embed follows the symlink so the embed path contains no "..".
package embedded

import "embed"

//go:embed plugin
var PluginFS embed.FS
