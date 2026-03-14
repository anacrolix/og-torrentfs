# og-torrentfs

A [torrentfs](https://pkg.go.dev/github.com/anacrolix/torrent/fs) backend that
mounts a torrent client as a read-only FUSE filesystem using
[anacrolix/fuse](https://github.com/anacrolix/fuse).

`anacrolix/fuse` is a fork of `bazil.org/fuse` that supports macFUSE and
fuse-t on macOS and fusermount on Linux.

## Why does this exist?

The `torrent/fs` package defines a FUSE-library-agnostic `Backend` interface.
This module provides one concrete implementation.  A second implementation,
[hanwen-torrentfs](https://github.com/anacrolix/hanwen-torrentfs), uses
`hanwen/go-fuse/v2` instead.  Having both lets users pick the FUSE library
that works best on their platform.

## Usage

```go
import (
    ogtorrentfs "github.com/anacrolix/og-torrentfs"
    torrentfs   "github.com/anacrolix/torrent/fs"
)

tfs := torrentfs.New(cl)
defer tfs.Destroy()

b := &ogtorrentfs.Backend{}
u, err := b.Mount("/mnt/torrents", tfs)
if err != nil {
    log.Fatal(err)
}
defer u.Unmount()
```

See `cmd/torrentfs` for a ready-to-run command.
