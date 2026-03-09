//go:build !windows

// Package ogtorrentfs implements the torrentfs.Backend interface using
// github.com/anacrolix/fuse (a fork of bazil.org/fuse), which supports
// macFUSE and fuse-t on macOS and fusermount on Linux.
package ogtorrentfs

import (
	"context"
	"io"
	"strings"

	"github.com/anacrolix/fuse"
	fusefs "github.com/anacrolix/fuse/fs"
	"github.com/anacrolix/missinggo/v2"

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
	err := fuse.Unmount(m.mountDir) //nolint:staticcheck
	m.conn.Close()
	return err
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
	attr.Mode = 0o40000 | defaultMode // S_IFDIR
	return nil
}

func (rn rootNode) Lookup(ctx context.Context, name string) (fusefs.Node, error) {
	for _, t := range rn.tfs.Client.Torrents() {
		info := t.Info()
		if t.Name() != name || info == nil {
			continue
		}
		n := node{tfs: rn.tfs, t: t}
		if !info.IsDir() {
			return fileNode{node: n, f: t.Files()[0]}, nil
		}
		return dirNode{node: n}, nil
	}
	return nil, fuse.ENOENT //nolint:staticcheck
}

func (rn rootNode) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	var des []fuse.Dirent
	for _, t := range rn.tfs.Client.Torrents() {
		info := t.Info()
		if info == nil {
			continue
		}
		dt := fuse.DT_Dir
		if !info.IsDir() {
			dt = fuse.DT_File
		}
		des = append(des, fuse.Dirent{Name: info.BestName(), Type: dt})
	}
	return des, nil
}

func (rn rootNode) Forget() {
	rn.tfs.Destroy()
}

// --- dirNode ---

type dirNode struct {
	node
}

var _ fusefs.HandleReadDirAller = dirNode{}

func (dn dirNode) Attr(ctx context.Context, attr *fuse.Attr) error {
	attr.Mode = 0o40000 | defaultMode
	return nil
}

func (dn dirNode) Lookup(_ context.Context, name string) (fusefs.Node, error) {
	var fullPath string
	if dn.path != "" {
		fullPath = dn.path + "/" + name
	} else {
		fullPath = name
	}
	dir := false
	var file *torrent.File
	for _, f := range dn.t.Files() {
		if f.DisplayPath() == fullPath {
			file = f
		}
		if torrentfs.IsSubPath(fullPath, f.DisplayPath()) {
			dir = true
		}
	}
	n := dn.node
	n.path = fullPath
	if dir && file != nil {
		panic("both dir and file")
	}
	if file != nil {
		return fileNode{node: n, f: file}, nil
	}
	if dir {
		return dirNode{node: n}, nil
	}
	return nil, fuse.ENOENT //nolint:staticcheck
}

func (dn dirNode) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	info := dn.t.Info()
	names := map[string]bool{}
	var des []fuse.Dirent
	for _, fi := range info.UpvertedFiles() {
		filePathname := strings.Join(fi.BestPath(), "/")
		if !torrentfs.IsSubPath(dn.path, filePathname) {
			continue
		}
		var name string
		if dn.path == "" {
			name = fi.BestPath()[0]
		} else {
			dirPathname := strings.Split(dn.path, "/")
			name = fi.BestPath()[len(dirPathname)]
		}
		if names[name] {
			continue
		}
		names[name] = true
		de := fuse.Dirent{Name: name}
		if len(fi.BestPath()) == len(dn.path)+1 {
			de.Type = fuse.DT_File
		} else {
			de.Type = fuse.DT_Dir
		}
		des = append(des, de)
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
	r := me.tf.NewReader()
	defer r.Close()
	pos, err := r.Seek(req.Offset, io.SeekStart)
	if err != nil {
		panic(err)
	}
	if pos != req.Offset {
		panic("seek failed")
	}
	resp.Data = resp.Data[:req.Size]
	readDone := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	var readErr error
	doneFn := me.fn.tfs.TrackBlockedRead()
	go func() {
		defer close(readDone)
		cr := missinggo.ContextedReader{R: r, Ctx: ctx}
		var n int
		n, readErr = io.ReadFull(cr, resp.Data)
		if readErr == io.ErrUnexpectedEOF {
			readErr = nil
		}
		resp.Data = resp.Data[:n]
	}()
	defer func() {
		<-readDone
		doneFn()
	}()
	defer cancel()

	select {
	case <-readDone:
		return readErr
	case <-me.fn.tfs.Destroyed():
		return fuse.EIO //nolint:staticcheck
	case <-ctx.Done():
		return fuse.EINTR //nolint:staticcheck
	}
}

func (me fileHandle) Release(context.Context, *fuse.ReleaseRequest) error {
	return nil
}
