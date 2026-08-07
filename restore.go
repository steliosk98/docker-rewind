package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// composeOverride pins every service to the exact image digest recorded at
// snapshot time. This is the whole reason Rewind works where a volume backup
// script doesn't: :latest moved, and restoring old data onto a new image is how
// you get a database that won't start.
func composeOverride(m *Manifest) string {
	var b strings.Builder
	for _, s := range m.Services {
		if s.Digest == "" {
			continue // warned about at snapshot time
		}
		fmt.Fprintf(&b, "  %s:\n    image: %s\n", s.Name, s.Digest)
	}
	if b.Len() == 0 {
		return ""
	}
	return "services:\n" + b.String()
}

// Restore wipes every named volume and unpacks the snapshot over it. There is
// no undo for this.
func Restore(s *Stack, m *Manifest, log func(string, ...any)) error {
	dir := snapDir(s.Project, m.ID)
	stored := filepath.Join(dir, "compose.yml")
	if _, err := os.Stat(stored); err != nil {
		return fmt.Errorf("snapshot %s is missing its compose file: %w", m.ID, err)
	}
	for _, v := range m.Volumes {
		if _, err := os.Stat(filepath.Join(dir, v.File)); err != nil {
			return fmt.Errorf("snapshot %s is missing %s: %w", m.ID, v.File, err)
		}
	}

	// Tear down using the *snapshotted* compose file, not the current one. The
	// whole reason you're here may be that the current file is broken, and a
	// rollback tool that needs a working config to roll back is useless.
	base := []string{"compose", "-p", s.Project, "--project-directory", s.Dir, "-f", stored}

	log("stopping and removing containers for %s", s.Project)
	if _, err := runIn(s.Dir, append(base, "down")...); err != nil {
		return err
	}

	for _, v := range m.Volumes {
		log("restoring volume %s (%s)", v.Name, humanBytes(v.Bytes))
		if err := ensureVolume(v.Name, s.Project); err != nil {
			return err
		}
		if err := wipeVolume(v.Name); err != nil {
			return err
		}
		if err := untarVolume(v.Name, filepath.Join(dir, v.File)); err != nil {
			return err
		}
	}

	// The project directory stays the user's so relative paths still resolve.
	args := append([]string{}, base...)
	tmp, err := os.MkdirTemp("", "rewind-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if ov := composeOverride(m); ov != "" {
		p := filepath.Join(tmp, "docker-compose.override.yml")
		if err := os.WriteFile(p, []byte(ov), 0o600); err != nil {
			return err
		}
		args = append(args, "-f", p)
	}

	log("starting %s with images pinned to snapshot digests", s.Project)
	if _, err := runIn(s.Dir, append(args, "up", "-d")...); err != nil {
		return err
	}

	// Rule 5 again: the running stack and the file on disk may now disagree.
	current, err := os.ReadFile(s.File)
	snapped, err2 := os.ReadFile(stored)
	if err == nil && err2 == nil && !bytes.Equal(current, snapped) {
		log("")
		log("NOTE: your compose file differs from the snapshot. The stack is running")
		log("from the snapshotted copy. To make that permanent:")
		log("  cp %q %q", stored, s.File)
	}
	return nil
}

func ensureVolume(name, project string) error {
	if _, err := run("volume", "inspect", name); err == nil {
		return nil
	}
	short := strings.TrimPrefix(name, project+"_")
	_, err := run("volume", "create",
		"--label", "com.docker.compose.project="+project,
		"--label", "com.docker.compose.volume="+short,
		name)
	return err
}

func wipeVolume(vol string) error {
	// -mindepth 1 keeps the mountpoint itself; -exec rm -rf {} + is portable
	// across busybox builds in a way that find -delete is not.
	_, err := run("run", "--rm", "-v", vol+":/v", helperImage(),
		"find", "/v", "-mindepth", "1", "-exec", "rm", "-rf", "{}", "+")
	if err != nil {
		return fmt.Errorf("emptying volume %s: %w", vol, err)
	}
	return nil
}

func untarVolume(vol, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.Command("docker", "run", "--rm", "-i", "-v", vol+":/v", helperImage(), "tar", "xz", "-C", "/v")
	cmd.Stdin = f
	var errb strings.Builder
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("unpacking volume %s: %s", vol, strings.TrimSpace(errb.String()+" "+err.Error()))
	}
	return nil
}
