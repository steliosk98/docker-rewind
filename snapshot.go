package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Service struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	Digest string `json:"digest,omitempty"`
}

type Volume struct {
	Name  string `json:"name"`
	File  string `json:"file"`
	Bytes int64  `json:"bytes"`
}

type Manifest struct {
	Version     int       `json:"version"`
	ID          string    `json:"id"`
	Tag         string    `json:"tag,omitempty"`
	Project     string    `json:"project"`
	Created     time.Time `json:"created"`
	ComposePath string    `json:"compose_path"`
	Services    []Service `json:"services"`
	Volumes     []Volume  `json:"volumes"`
	BindMounts  []string  `json:"bind_mounts,omitempty"`
	Warnings    []string  `json:"warnings,omitempty"`
}

func (m *Manifest) Size() int64 {
	var n int64
	for _, v := range m.Volumes {
		n += v.Bytes
	}
	return n
}

func storeDir() string {
	if v := os.Getenv("REWIND_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".docker-rewind")
}

func projDir(project string) string { return filepath.Join(storeDir(), project) }

func snapDir(project, id string) string { return filepath.Join(projDir(project), id) }

// Snapshot stops the stack, tars every named volume, records resolved image
// digests, and starts the stack again. Stop-first is deliberate: hot-copying a
// live database volume yields a backup that restores cleanly and fails later.
func Snapshot(s *Stack, tag string, log func(string, ...any)) (m *Manifest, err error) {
	cs, err := s.containers()
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, fmt.Errorf("no containers found for project %q — run `docker compose up -d` once before snapshotting", s.Project)
	}
	vols, err := s.volumeNames()
	if err != nil {
		return nil, err
	}

	m = &Manifest{
		Version:     1,
		ID:          time.Now().UTC().Format("2006-01-02T15-04-05Z"),
		Tag:         tag,
		Project:     s.Project,
		Created:     time.Now().UTC(),
		ComposePath: s.File,
	}

	seen := map[string]bool{}
	binds := map[string]bool{}
	running := false
	for _, c := range cs {
		running = running || c.Running
		for _, b := range c.Binds {
			binds[b] = true
		}
		if c.Service == "" || seen[c.Service] {
			continue
		}
		seen[c.Service] = true
		m.Services = append(m.Services, Service{
			Name:   c.Service,
			Image:  c.Image,
			Digest: repoDigest(c.ImageID, c.Image),
		})
	}
	sort.Slice(m.Services, func(i, j int) bool { return m.Services[i].Name < m.Services[j].Name })
	for b := range binds {
		m.BindMounts = append(m.BindMounts, b)
	}
	sort.Strings(m.BindMounts)

	// Rule 5: never silently skip data. Say exactly what is not in here.
	if n := len(m.BindMounts); n > 0 {
		m.Warnings = append(m.Warnings, fmt.Sprintf("%d bind mount(s) NOT captured — back these up yourself", n))
	}
	for _, sv := range m.Services {
		if sv.Digest == "" {
			m.Warnings = append(m.Warnings, fmt.Sprintf("service %q has no repo digest (locally built image?) — it will NOT be pinned on restore", sv.Name))
		}
	}
	if len(vols) == 0 {
		m.Warnings = append(m.Warnings, "no named volumes found — this snapshot contains no data")
	}

	dir := snapDir(s.Project, m.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// A half-written snapshot must never show up in `list`. Keyed on an explicit
	// flag rather than err: the deferred restart below runs first (LIFO) and can
	// set err, and a snapshot that archived fine must survive a failed restart.
	complete := false
	defer func() {
		if !complete {
			os.RemoveAll(dir)
		}
	}()

	composeSrc, err := os.ReadFile(s.File)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yml"), composeSrc, 0o600); err != nil {
		return nil, err
	}

	if running {
		log("stopping %s", s.Project)
		if _, err := s.compose("stop"); err != nil {
			return nil, err
		}
		defer func() {
			log("starting %s", s.Project)
			if _, e := s.compose("start"); e != nil && err == nil {
				err = fmt.Errorf("snapshot ok but restarting the stack failed: %w", e)
			}
		}()
	} else {
		log("stack is not running; snapshotting as-is")
	}

	for _, v := range vols {
		file := v + ".tar.gz"
		log("archiving volume %s", v)
		n, err := tarVolume(v, filepath.Join(dir, file))
		if err != nil {
			return nil, err
		}
		m.Volumes = append(m.Volumes, Volume{Name: v, File: file, Bytes: n})
	}

	if err := writeManifest(dir, m); err != nil {
		return nil, err
	}
	complete = true
	return m, nil
}

func writeManifest(dir string, m *Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(b, '\n'), 0o600)
}

// tarVolume streams the archive to the host's stdout rather than bind-mounting
// an output directory — that avoids uid and permission surprises on Windows
// and macOS entirely.
func tarVolume(vol, dest string) (int64, error) {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	cmd := exec.Command("docker", "run", "--rm", "-v", vol+":/v:ro", helperImage(), "tar", "cz", "-C", "/v", ".")
	cmd.Stdout = f
	var errb strings.Builder
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("archiving volume %s: %s", vol, strings.TrimSpace(errb.String()+" "+err.Error()))
	}
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func List(project string) ([]*Manifest, error) {
	entries, err := os.ReadDir(projDir(project))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(projDir(project), e.Name(), "manifest.json"))
		if err != nil {
			continue // not a snapshot, or half-written; skip quietly
		}
		var m Manifest
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		out = append(out, &m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// Resolve accepts "latest", a full snapshot id, or a unique id prefix.
func Resolve(snaps []*Manifest, ref string) (*Manifest, error) {
	if len(snaps) == 0 {
		return nil, fmt.Errorf("no snapshots yet — run `rewind snapshot` first")
	}
	if ref == "" || ref == "latest" {
		return snaps[0], nil
	}
	var hits []*Manifest
	for _, m := range snaps {
		if m.ID == ref || m.Tag == ref {
			return m, nil
		}
		if strings.HasPrefix(m.ID, ref) {
			hits = append(hits, m)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("no snapshot matching %q", ref)
	case 1:
		return hits[0], nil
	default:
		return nil, fmt.Errorf("%q matches %d snapshots — be more specific", ref, len(hits))
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
