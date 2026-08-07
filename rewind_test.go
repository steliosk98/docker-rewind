package main

import (
	"flag"
	"strings"
	"testing"
	"time"
)

// Resolve decides which snapshot gets written over live volumes. Picking the
// wrong one restores the wrong data, so ambiguity must be an error, never a
// guess.
func TestResolve(t *testing.T) {
	snaps := []*Manifest{
		{ID: "2026-08-07T12-00-00Z", Tag: "pre-upgrade"},
		{ID: "2026-08-06T09-30-00Z"},
		{ID: "2026-08-06T08-00-00Z"},
	}
	for _, tc := range []struct{ ref, want string }{
		{"", "2026-08-07T12-00-00Z"},
		{"latest", "2026-08-07T12-00-00Z"},
		{"pre-upgrade", "2026-08-07T12-00-00Z"},
		{"2026-08-06T08-00-00Z", "2026-08-06T08-00-00Z"},
		{"2026-08-07", "2026-08-07T12-00-00Z"}, // unique prefix
	} {
		m, err := Resolve(snaps, tc.ref)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", tc.ref, err)
		}
		if m.ID != tc.want {
			t.Errorf("Resolve(%q) = %s, want %s", tc.ref, m.ID, tc.want)
		}
	}
	if _, err := Resolve(snaps, "2026-08-06"); err == nil {
		t.Error("ambiguous prefix must be an error, not an arbitrary pick")
	}
	if _, err := Resolve(snaps, "nope"); err == nil {
		t.Error("unknown ref must be an error")
	}
	if _, err := Resolve(nil, "latest"); err == nil {
		t.Error("empty snapshot list must be an error")
	}
}

// Digest pinning is the reason restore works at all. A service that lost its
// digest must be left unpinned rather than pinned to a moving tag.
func TestComposeOverride(t *testing.T) {
	got := composeOverride(&Manifest{Services: []Service{
		{Name: "db", Image: "postgres:16", Digest: "postgres@sha256:aaa"},
		{Name: "web", Image: "myapp:dev"}, // locally built, no digest
	}})
	if !strings.Contains(got, "  db:\n    image: postgres@sha256:aaa\n") {
		t.Errorf("db not pinned to its digest:\n%s", got)
	}
	if strings.Contains(got, "web") {
		t.Errorf("service without a digest must be omitted, not pinned to a tag:\n%s", got)
	}
	if !strings.HasPrefix(got, "services:\n") {
		t.Errorf("missing services key:\n%s", got)
	}
	if composeOverride(&Manifest{Services: []Service{{Name: "web"}}}) != "" {
		t.Error("no digests at all must yield no override file")
	}
}

// originOK is the CSRF check on endpoints that delete volumes.
func TestOriginOK(t *testing.T) {
	const addr = "127.0.0.1:7654"
	for _, o := range []string{"http://127.0.0.1:7654", "http://localhost:7654"} {
		if !originOK(o, addr) {
			t.Errorf("own origin %q rejected", o)
		}
	}
	for _, o := range []string{
		"http://evil.test",
		"https://127.0.0.1:7654",
		"http://127.0.0.1:7655",
		"http://127.0.0.1.evil.test:7654",
		"null",
	} {
		if originOK(o, addr) {
			t.Errorf("foreign origin %q accepted", o)
		}
	}
}

// --yes skips the "are you sure you want to wipe these volumes" prompt, so it
// must survive argument reordering exactly — never swallowing the snapshot id,
// never getting silently dropped.
func TestHoistFlags(t *testing.T) {
	newFS := func() *flag.FlagSet {
		fs := flag.NewFlagSet("restore", flag.ContinueOnError)
		fs.String("C", ".", "")
		fs.Bool("yes", false, "")
		return fs
	}
	for _, tc := range []struct {
		args []string
		id   string
		yes  bool
		dir  string
	}{
		{[]string{"latest", "--yes"}, "latest", true, "."},
		{[]string{"--yes", "latest"}, "latest", true, "."},
		{[]string{"latest"}, "latest", false, "."},
		{[]string{"-C", "/srv/app", "latest", "--yes"}, "latest", true, "/srv/app"},
		{[]string{"latest", "-C", "/srv/app"}, "latest", false, "/srv/app"},
		{[]string{"--yes=true", "latest"}, "latest", true, "."},
	} {
		fs := newFS()
		if err := fs.Parse(hoistFlags(fs, tc.args)); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if got := fs.Arg(0); got != tc.id {
			t.Errorf("%v: id = %q, want %q", tc.args, got, tc.id)
		}
		if got := fs.Lookup("yes").Value.String(); got != boolStr(tc.yes) {
			t.Errorf("%v: --yes = %s, want %v", tc.args, got, tc.yes)
		}
		if got := fs.Lookup("C").Value.String(); got != tc.dir {
			t.Errorf("%v: -C = %q, want %q", tc.args, got, tc.dir)
		}
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestSanitizeProject(t *testing.T) {
	for in, want := range map[string]string{
		"My Stack":   "mystack",
		"immich":     "immich",
		"_leading":   "leading",
		"a-b_c":      "a-b_c",
		"Foo.Bar!99": "foobar99",
	} {
		if got := sanitizeProject(in); got != want {
			t.Errorf("sanitizeProject(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	for in, want := range map[int64]string{
		0: "0 B", 512: "512 B", 1024: "1.0 KB", 1536: "1.5 KB",
		1048576: "1.0 MB", 1073741824: "1.0 GB",
	} {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestManifestSize(t *testing.T) {
	m := &Manifest{Volumes: []Volume{{Bytes: 100}, {Bytes: 23}}, Created: time.Now()}
	if m.Size() != 123 {
		t.Errorf("Size() = %d, want 123", m.Size())
	}
}
