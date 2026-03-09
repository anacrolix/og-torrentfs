github.com/anacrolix/fuse
=========================

This module supports implementing FUSE (Filesystems in Userspace) in Go. It supports MacFUSE 3.3+, 4, and FUSE-T, on MacOS, and regular FUSE on Linux and FreeBSD.

[`github.com/anacrolix/fuse`](https://github.com/anacrolix/fuse) is a fork of [`github.com/zegl/fuse`](https://github.com/zegl/fuse), which is a fork of [`bazil.org/fuse`](https://bazil.org/fuse).

`bazil.org/fuse` dropped support for FUSE on Mac when OSXFUSE [stopped being an open source project](https://github.com/bazil/fuse/issues/224).

`github.com/zegl/fuse` added support for MacFUSE 4, and restored support for MacFUSE 3.3 and newer.

`github.com/anacrolix/fuse` fixes imports and module paths so you can import this module without using Go workspaces or go.mod replace directives. It also adds support for [FUSE-T](https://www.fuse-t.org/), and Mac M1.
