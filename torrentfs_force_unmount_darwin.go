//go:build darwin

package ogtorrentfs

import "syscall"

// forceUnmount force-unmounts dir using MNT_FORCE. We intentionally avoid
// "diskutil unmount force" here: that command sends SIGKILL to processes
// with open file descriptors on the mount (a fuse-t/NFS behaviour), which
// would kill the calling test binary. The kernel-level MNT_FORCE flag
// performs a forced unmount without signalling user processes.
func forceUnmount(dir string) {
	const mntForce = 0x80000 // MNT_FORCE on macOS/darwin (sys/mount.h)
	syscall.Unmount(dir, mntForce) //nolint:errcheck
}
