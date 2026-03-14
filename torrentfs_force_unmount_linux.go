//go:build linux

package ogtorrentfs

import "os/exec"

func forceUnmount(dir string) {
	exec.Command("fusermount", "-u", "-z", dir).Run() //nolint:errcheck
}
