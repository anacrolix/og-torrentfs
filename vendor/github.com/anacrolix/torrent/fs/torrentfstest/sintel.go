//go:build !windows

package torrentfstest

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/anacrolix/torrent"
	torrentfs "github.com/anacrolix/torrent/fs"
	"github.com/anacrolix/torrent/metainfo"
)

// sintelFileHashes are the expected MD5 hashes of files in the Sintel torrent.
var sintelFileHashes = map[string]string{
	"poster.jpg": "f9223791908131c505d7bdafa7a8aaf5",
	"Sintel.mp4": "083e808d56aa7b146f513b3458658292",
}

// testStreamSintel verifies that a large multi-file torrent can be streamed
// through the FUSE mount. It downloads the Sintel open-movie torrent via
// BitTorrent and reads a file through the filesystem, verifying its MD5.
//
// This test requires internet access and is skipped by default because it is
// slow and occasionally flaky.
func testStreamSintel(t *testing.T, mount MountFunc) {
	t.Skip("flaky")

	// Locate testdata relative to this source file so the test works both when
	// run from within the torrent module and when called from a backend repo
	// that replaces github.com/anacrolix/torrent with a local path.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller failed: cannot locate testdata")
	}
	testdataDir := filepath.Join(filepath.Dir(filename), "..", "..", "testdata")

	sintelTorrentPath := filepath.Join(testdataDir, "sintel.torrent")
	if _, err := os.Stat(sintelTorrentPath); err != nil {
		t.Skipf("sintel.torrent not found at %v: %v", sintelTorrentPath, err)
	}

	ctx := t.Context()
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	targetFile := "Sintel.mp4"
	if testing.Short() {
		targetFile = "poster.jpg"
	}

	mi, err := metainfo.LoadFromFile(sintelTorrentPath)
	require.NoError(t, err)
	m, err := mi.MagnetV2()
	require.NoError(t, err)

	cfg := torrent.NewDefaultClientConfig()
	cfg.ListenPort = 0
	cl, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	defer cl.Close()

	mountDir := t.TempDir()
	tfs := torrentfs.New(cl)
	defer tfs.Destroy()
	unmount := mount(t, tfs, mountDir)
	t.Cleanup(unmount)

	go func() {
		_, err := cl.AddTorrent(mi)
		if err != nil {
			t.Logf("AddTorrent: %v", err)
		}
		_, err = cl.AddMagnet(m.String())
		if err != nil {
			t.Logf("AddMagnet: %v", err)
		}
	}()

	f, err := openFileWhenExists(t, ctx, filepath.Join(mountDir, "Sintel", targetFile))
	require.NoError(t, err)
	t.Logf("opened %v", f.Name())
	defer f.Close()

	fi, err := f.Stat()
	require.NoError(t, err)

	var written int64
	h := md5.New()
	go func() {
		<-ctx.Done()
		f.Close()
	}()
	_, err = f.WriteTo(io.MultiWriter(h, &progressWriter{
		onWrite: func(n int) {
			written += int64(n)
			t.Logf("progress %.2f%%", 100*float64(written)/float64(fi.Size()))
		},
	}))
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
	require.NoError(t, err)

	require.Equal(t, sintelFileHashes[targetFile], hex.EncodeToString(h.Sum(nil)))
}

func openFileWhenExists(t *testing.T, ctx context.Context, name string) (*os.File, error) {
	for {
		f, err := os.Open(name)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		t.Logf("waiting for file to appear: %v", name)
		select {
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		case <-time.After(time.Second):
		}
	}
}

type progressWriter struct {
	onWrite func(n int)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.onWrite(len(p))
	return len(p), nil
}
