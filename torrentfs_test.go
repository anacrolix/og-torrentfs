//go:build !windows

package ogtorrentfs_test

import (
	"sync"
	"testing"

	ogtorrentfs "github.com/anacrolix/og-torrentfs"
	torrentfs "github.com/anacrolix/torrent/fs"
	"github.com/anacrolix/torrent/fs/tfstest"
)

func testMountFunc(t testing.TB, tfs *torrentfs.TorrentFS, mountDir string) (unmount func()) {
	t.Helper()
	b := &ogtorrentfs.Backend{}
	u, err := b.Mount(mountDir, tfs)
	if err != nil {
		t.Skipf("mount: %v", err)
		return func() {}
	}
	var once sync.Once
	return func() { once.Do(func() { u.Unmount() }) }
}

func TestTorrentFS(t *testing.T) {
	tfstest.RunTestSuite(t, testMountFunc)
}
