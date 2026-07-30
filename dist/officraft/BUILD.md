# officraft TCC anchor build record

Source commit: `6c37523` (the source snapshot before this binary/provenance commit)

Build command:

```sh
(cd cli/officraft && go build -ldflags='-s -w' -o ../../dist/officraft/officraft ./...)
shasum -a 256 cli/officraft/go.mod cli/officraft/main.go | shasum -a 256
```

The first line of `source.sha256` is the resulting aggregate source hash. CI
recomputes it and refuses a source/binary record that was not refreshed.
