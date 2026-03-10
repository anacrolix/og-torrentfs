//go:build !windows

// Package ogtorrentfs implements the torrentfs.Backend interface using
// github.com/anacrolix/fuse (a fork of bazil.org/fuse), which supports
// macFUSE and fuse-t on macOS and fusermount on Linux.
package ogtorrentfs

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/anacrolix/fuse"
	fusefs "github.com/anacrolix/fuse/fs"

	"github.com/anacrolix/torrent"
	torrentfs "github.com/anacrolix/torrent/fs"
)

const defaultMode = 0o555

// Backend implements torrentfs.Backend using anacrolix/fuse.
type Backend struct {
	// MountOptions are passed to fuse.Mount.
	MountOptions []fuse.MountOption
}

type mountedFS struct {
	conn     *fuse.Conn
	mountDir string
}

func (m *mountedFS) Unmount() error {
	// Run the actual unmount in a goroutine with a timeout. On macOS with
	// fuse-t the underlying syscall.Unmount can block indefinitely when there
	// is a pending NFS read from user space. If it hasn't finished in 3s we
	// force-unmount and close the connection so callers are never blocked.
	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		err := fuse.Unmount(m.mountDir) //nolint:staticcheck
		if err != nil {
			forceUnmount(m.mountDir)
		}
		ch <- result{err}
	}()

	select {
	case r := <-ch:
		m.conn.Close()
		return r.err
	case <-time.After(3 * time.Second):
		// Force-unmount to unblock the syscall.Unmount in the goroutine, then
		// wait for it to exit so the process can terminate cleanly.
		forceUnmount(m.mountDir)
		<-ch
		m.conn.Close()
		return nil
	}
}

// Mount mounts tfs at mountDir and returns an Unmounter.
func (b *Backend) Mount(mountDir string, tfs *torrentfs.TorrentFS) (torrentfs.Unmounter, error) {
	opts := append([]fuse.MountOption{fuse.ReadOnly()}, b.MountOptions...)
	conn, err := fuse.Mount(mountDir, opts...) //nolint:staticcheck
	if err != nil {
		return nil, err
	}
	go fusefs.Serve(conn, &ogFS{tfs: tfs}) //nolint:staticcheck
	<-conn.Ready
	if err := conn.MountError; err != nil {
		conn.Close()
		return nil, err
	}
	return &mountedFS{conn: conn, mountDir: mountDir}, nil
}

// ogFS wraps TorrentFS so it implements fusefs.FS and fusefs.FSDestroyer.
type ogFS struct {
	tfs *torrentfs.TorrentFS
}

var (
	_ fusefs.FS          = (*ogFS)(nil)
	_ fusefs.FSDestroyer = (*ogFS)(nil)
)

func (ofs *ogFS) Root() (fusefs.Node, error) {
	return rootNode{node: node{tfs: ofs.tfs}}, nil
}

func (ofs *ogFS) Destroy() {
	// Destroy is managed externally via TorrentFS.Destroy; nothing to do here.
}

// --- shared node state ---

type node struct {
	path string
	tfs  *torrentfs.TorrentFS
	t    *torrent.Torrent
}

// --- rootNode ---

type rootNode struct {
	node
}

var (
	_ fusefs.NodeForgetter      = rootNode{}
	_ fusefs.HandleReadDirAller = rootNode{}
)

func (rn rootNode) Attr(ctx context.Context, attr *fuse.Attr) error {
	attr.Mode = os.ModeDir | defaultMode
	return nil
}

func (rn rootNode) Lookup(ctx context.Context, name string) (fusefs.Node, error) {
	result, ok := torrentfs.RootLookup(rn.tfs, name)
	if !ok {
		return nil, fuse.ENOENT //nolint:staticcheck
	}
	n := node{tfs: rn.tfs, t: result.Torrent}
	if !result.IsDir {
		return fileNode{node: n, f: result.File}, nil
	}
	return dirNode{node: n}, nil
}

func (rn rootNode) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	entries := torrentfs.RootEntries(rn.tfs)
	des := make([]fuse.Dirent, len(entries))
	for i, e := range entries {
		dt := fuse.DT_Dir
		if !e.IsDir {
			dt = fuse.DT_File
		}
		des[i] = fuse.Dirent{Name: e.Name, Type: dt}
	}
	return des, nil
}

func (rn rootNode) Forget() {}

// --- dirNode ---

type dirNode struct {
	node
}

var _ fusefs.HandleReadDirAller = dirNode{}

func (dn dirNode) Attr(ctx context.Context, attr *fuse.Attr) error {
	attr.Mode = os.ModeDir | defaultMode
	return nil
}

func (dn dirNode) Lookup(_ context.Context, name string) (fusefs.Node, error) {
	result, ok := torrentfs.DirLookup(dn.t, dn.path, name)
	if !ok {
		return nil, fuse.ENOENT //nolint:staticcheck
	}
	n := dn.node
	n.path = result.Path
	if !result.IsDir {
		return fileNode{node: n, f: result.File}, nil
	}
	return dirNode{node: n}, nil
}

func (dn dirNode) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	entries := torrentfs.DirEntries(dn.t, dn.path)
	des := make([]fuse.Dirent, len(entries))
	for i, e := range entries {
		dt := fuse.DT_Dir
		if !e.IsDir {
			dt = fuse.DT_File
		}
		des[i] = fuse.Dirent{Name: e.Name, Type: dt}
	}
	return des, nil
}

// --- fileNode ---

type fileNode struct {
	node
	f *torrent.File
}

var _ fusefs.NodeOpener = fileNode{}

func (fn fileNode) Attr(ctx context.Context, attr *fuse.Attr) error {
	attr.Size = uint64(fn.f.Length())
	attr.Mode = defaultMode
	return nil
}

func (fn fileNode) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fusefs.Handle, error) {
	return fileHandle{fn: fn, tf: fn.f}, nil
}

// --- fileHandle ---

type fileHandle struct {
	fn fileNode
	tf *torrent.File
}

var _ interface {
	fusefs.HandleReader
	fusefs.HandleReleaser
} = fileHandle{}

func (me fileHandle) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	if req.Dir {
		panic("read on directory")
	}
	resp.Data = resp.Data[:req.Size]
	n, err := torrentfs.ReadFile(ctx, me.fn.tfs, me.tf, resp.Data, req.Offset)
	resp.Data = resp.Data[:n]
	if err != nil {
		if errors.Is(err, torrentfs.ErrDestroyed) {
			return fuse.EIO //nolint:staticcheck
		}
		return fuse.EINTR //nolint:staticcheck
	}
	return nil
}

func (me fileHandle) Release(context.Context, *fuse.ReleaseRequest) error {
	return nil
}
