package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// helperImage does the tar work inside a container so we never need tar, gzip,
// or root on the host. Overridable for airgapped hosts with a mirrored image.
func helperImage() string {
	if v := os.Getenv("REWIND_HELPER_IMAGE"); v != "" {
		return v
	}
	return "alpine:3"
}

// run shells out to the docker CLI. We deliberately do not use the Docker SDK:
// the CLI gives us compose v2 for free and keeps go.mod dependency-free.
func run(args ...string) (string, error) { return runIn("", args...) }

func runIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("docker %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Stack is a compose project on this host.
type Stack struct {
	Dir     string
	File    string // absolute path to the compose file
	Project string
}

var composeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

func FindStack(dir string) (*Stack, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	var file string
	for _, n := range composeNames {
		p := filepath.Join(abs, n)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			file = p
			break
		}
	}
	if file == "" {
		return nil, fmt.Errorf("no compose file found in %s (looked for %s)", abs, strings.Join(composeNames, ", "))
	}
	return &Stack{Dir: abs, File: file, Project: projectName(abs, file)}, nil
}

// projectName asks Docker rather than parsing the compose file, so a `name:`
// key or COMPOSE_PROJECT_NAME can't desync us from reality.
func projectName(dir, file string) string {
	if v := os.Getenv("COMPOSE_PROJECT_NAME"); v != "" {
		return v
	}
	if out, err := run("compose", "ls", "-a", "--format", "json"); err == nil {
		var ls []struct{ Name, ConfigFiles string }
		if json.Unmarshal([]byte(out), &ls) == nil {
			for _, e := range ls {
				for _, cf := range strings.Split(e.ConfigFiles, ",") {
					if sameFile(strings.TrimSpace(cf), file) {
						return e.Name
					}
				}
			}
		}
	}
	return sanitizeProject(filepath.Base(dir))
}

// sameFile compares by inode/file-id, which handles symlinks and Windows case
// insensitivity without us guessing at path normalisation rules.
func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// sanitizeProject mirrors compose's default: lowercase, [a-z0-9_-] only, and it
// must start alphanumeric.
func sanitizeProject(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			if b.Len() > 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func (s *Stack) compose(args ...string) (string, error) {
	return runIn(s.Dir, append([]string{"compose", "-p", s.Project, "-f", s.File}, args...)...)
}

type container struct {
	Service string
	Image   string // as written in the compose file, e.g. postgres:16
	ImageID string
	Binds   []string
	Running bool
}

func (s *Stack) label() string { return "label=com.docker.compose.project=" + s.Project }

func (s *Stack) containers() ([]container, error) {
	out, err := run("ps", "-aq", "--filter", s.label())
	if err != nil {
		return nil, err
	}
	ids := lines(out)
	if len(ids) == 0 {
		return nil, nil
	}
	out, err = run(append([]string{"inspect"}, ids...)...)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Image  string
		Config struct {
			Image  string
			Labels map[string]string
		}
		State  struct{ Running bool }
		Mounts []struct{ Type, Source string }
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parsing docker inspect: %w", err)
	}
	cs := make([]container, 0, len(raw))
	for _, r := range raw {
		c := container{
			Service: r.Config.Labels["com.docker.compose.service"],
			Image:   r.Config.Image,
			ImageID: r.Image,
			Running: r.State.Running,
		}
		for _, m := range r.Mounts {
			if m.Type == "bind" {
				c.Binds = append(c.Binds, m.Source)
			}
		}
		cs = append(cs, c)
	}
	return cs, nil
}

func (s *Stack) volumeNames() ([]string, error) {
	out, err := run("volume", "ls", "-q", "--filter", s.label())
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// repoDigest resolves the immutable repo@sha256:... reference. Empty for
// locally built images, which is a warning, not an error.
func repoDigest(imageID, imageRef string) string {
	out, err := run("image", "inspect", imageID, "--format", "{{json .RepoDigests}}")
	if err != nil {
		return ""
	}
	var digests []string
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &digests) != nil || len(digests) == 0 {
		return ""
	}
	repo := imageRef
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	for _, d := range digests {
		if strings.HasPrefix(d, repo+"@") {
			return d
		}
	}
	return digests[0]
}
