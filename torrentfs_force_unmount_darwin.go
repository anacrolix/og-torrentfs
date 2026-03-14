//go:build darwin

package ogtorrentfs

import "os/exec"

func forceUnmount(dir string) {
	exec.Command("diskutil", "unmount", "force", dir).Run() //nolint:errcheck
}
