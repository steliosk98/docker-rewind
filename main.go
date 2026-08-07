package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
)

const version = "0.1.0"

const usage = `rewind ` + version + ` — snapshot a Docker Compose stack, roll it back when an update breaks it.

Run from a directory containing a compose file.

  rewind snapshot [--tag NAME]     stop the stack, archive volumes + image digests, start it
  rewind list                      show snapshots for this project
  rewind restore <id|tag|latest>   WIPE volumes and restore a snapshot
  rewind ui [--port 7654]          local web GUI
  rewind version

Flags:
  -C DIR    run against the stack in DIR instead of the current directory
  --yes     skip the restore confirmation prompt (for scripts)

Snapshots live in ~/.docker-rewind/<project>/ as plain .tar.gz plus a JSON
manifest — restorable with tar alone if this tool ever goes away.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "snapshot", "snap":
		err = cmdSnapshot(os.Args[2:])
	case "list", "ls":
		err = cmdList(os.Args[2:])
	case "restore":
		err = cmdRestore(os.Args[2:])
	case "ui", "gui":
		err = cmdUI(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q (try `rewind help`)", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rewind: "+err.Error())
		os.Exit(1)
	}
}

func logf(format string, a ...any) {
	if format == "" {
		fmt.Println()
		return
	}
	fmt.Printf(format+"\n", a...)
}

// stackFrom parses the shared -C flag and locates the compose project.
func stackFrom(fs *flag.FlagSet, dir *string, args []string) (*Stack, error) {
	if err := fs.Parse(hoistFlags(fs, args)); err != nil {
		return nil, err
	}
	return FindStack(*dir)
}

// hoistFlags moves positional arguments to the back. Go's flag package stops
// parsing at the first positional, so `rewind restore latest --yes` would
// otherwise drop --yes and sit waiting on a prompt in a script.
func hoistFlags(fs *flag.FlagSet, args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") || i+1 >= len(args) {
			continue
		}
		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			continue // --yes takes no value; don't swallow the snapshot id
		}
		i++
		flags = append(flags, args[i])
	}
	return append(flags, pos...)
}

func cmdSnapshot(args []string) error {
	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	dir := fs.String("C", ".", "stack directory")
	tag := fs.String("tag", "", "label for this snapshot, e.g. pre-upgrade")
	s, err := stackFrom(fs, dir, args)
	if err != nil {
		return err
	}
	logf("project %s (%s)", s.Project, s.File)
	m, err := Snapshot(s, *tag, logf)
	if err != nil {
		return err
	}
	logf("")
	logf("snapshot %s  %d volume(s), %s", m.ID, len(m.Volumes), humanBytes(m.Size()))
	for _, w := range m.Warnings {
		logf("  ! %s", w)
	}
	for _, b := range m.BindMounts {
		logf("    bind mount not captured: %s", b)
	}
	return nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	dir := fs.String("C", ".", "stack directory")
	s, err := stackFrom(fs, dir, args)
	if err != nil {
		return err
	}
	snaps, err := List(s.Project)
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		logf("no snapshots for %s yet — run `rewind snapshot`", s.Project)
		return nil
	}
	fmt.Printf("%-22s %-16s %8s %10s  %s\n", "ID", "TAG", "SIZE", "AGE", "")
	for _, m := range snaps {
		note := ""
		if len(m.Warnings) > 0 {
			note = fmt.Sprintf("! %d warning(s)", len(m.Warnings))
		}
		fmt.Printf("%-22s %-16s %8s %10s  %s\n", m.ID, m.Tag, humanBytes(m.Size()), humanAge(m.Created), note)
	}
	return nil
}

func cmdRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	dir := fs.String("C", ".", "stack directory")
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	s, err := stackFrom(fs, dir, args)
	if err != nil {
		return err
	}
	snaps, err := List(s.Project)
	if err != nil {
		return err
	}
	m, err := Resolve(snaps, fs.Arg(0))
	if err != nil {
		return err
	}

	logf("restoring %s (%s, %s) into project %s", m.ID, m.Tag, humanAge(m.Created), s.Project)
	logf("this DELETES the current contents of %d volume(s):", len(m.Volumes))
	for _, v := range m.Volumes {
		logf("  %s", v.Name)
	}
	for _, b := range m.BindMounts {
		logf("  bind mount %s is NOT restored and NOT backed up", b)
	}
	if !*yes {
		fmt.Printf("\nType the project name (%s) to confirm: ", s.Project)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != s.Project {
			return fmt.Errorf("aborted")
		}
	}
	if err := Restore(s, m, logf); err != nil {
		return err
	}
	logf("")
	logf("restored %s", m.ID)
	return nil
}
