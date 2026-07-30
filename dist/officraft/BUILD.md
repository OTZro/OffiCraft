# officraft TCC anchor build record

Source commit: `6c37523` (the source snapshot before this binary/provenance commit)

Build command:

```sh
(cd cli/officraft && go build -ldflags='-s -w' -o ../../dist/officraft/officraft ./...)
shasum -a 256 cli/officraft/go.mod cli/officraft/main.go | shasum -a 256   # -> source.sha256
(cd dist/officraft && shasum -a 256 officraft > binary.sha256)
```

Two records, because either one alone can be satisfied by a lie:

- `source.sha256` — the aggregate hash of the source that was built. CI
  recomputes it, so changing the source without refreshing this record fails.
- `binary.sha256` — the hash of the executable sitting next to it. Without it,
  refreshing `source.sha256` by hand (or replacing the executable outright)
  leaves CI perfectly green.

Verify the executable against its record at any time:

```sh
(cd dist/officraft && shasum -a 256 -c binary.sha256)
```

CI runs `bin/check-officraft-dist`, which checks both.
