package main

import (
	"embed"
	"io/fs"
)

// The TCC identity anchor, staged in by bin/build-bindist before ocwarden is
// built (the same .gitkeep-placeholder staging contract the server uses for
// bindist/seedsdist/webdist, so a plain `go build` on a clean checkout still
// compiles — it just carries no anchor).
//
// WHY ocwarden carries a copy at all: the cockpit's one-liner for a new machine
// downloads ocwarden ALONE and runs `ocwarden install`, and install refuses to
// proceed without an anchor beside it. Without this the anchor would be
// reachable only from the unpacked release tarball, and every remote onboarding
// would fail at the last step. The bytes are identical to dist/officraft and to
// the tarball's copy — they have to be, because those bytes ARE the TCC
// identity, and three different copies would mean three different identities
// and three separate authorization prompts.
//
//go:embed all:anchordist
var anchorEmbed embed.FS

// embeddedAnchor returns the staged anchor bytes, or nil when this ocwarden was
// built without one (a plain dev build). A package var so tests can supply
// their own; production never reassigns it.
var embeddedAnchor = func() []byte {
	b, err := fs.ReadFile(anchorEmbed, "anchordist/officraft")
	if err != nil {
		return nil
	}
	return b
}
