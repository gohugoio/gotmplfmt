package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/gohugoio/gotmplfmt/internal/format"
	"github.com/rogpeppe/go-internal/diff"
)

const develTag = "(devel)"

var (
	commit = "none"
	tag    = develTag
	date   = "unknown"
)

var (
	writeFlag   = flag.Bool("w", false, "write result to (source) file instead of stdout")
	listFlag    = flag.Bool("l", false, "list files whose formatting differs from gotmplfmt's")
	diffFlag    = flag.Bool("d", false, "display diffs instead of rewriting files")
	versionFlag = flag.Bool("version", false, "print version information and exit")
)

func main() {
	log.SetFlags(0)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: gotmplfmt [flags] [path ...]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *versionFlag {
		initVersionInfo()
		fmt.Printf("gotmplfmt %s (commit: %s, date: %s)\n", tag, commit, date)
		return
	}

	if flag.NArg() == 0 {
		if *writeFlag {
			log.Fatal("error: cannot use -w with standard input")
		}
		if *listFlag {
			log.Fatal("error: cannot use -l with standard input")
		}
		if err := processReader(os.Stdin, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}

	for _, arg := range flag.Args() {
		if err := processPath(arg); err != nil {
			log.Fatal(err)
		}
	}
}

func processPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if isTemplateFile(p) {
				return processFile(p)
			}
			return nil
		})
	}
	return processFile(path)
}

func processFile(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := format.Format(string(src))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if *listFlag {
		if out != string(src) {
			fmt.Println(path)
		}
		return nil
	}
	if *diffFlag {
		if out != string(src) {
			d := diff.Diff(path+".orig", src, path, []byte(out))
			os.Stdout.Write(d)
		}
		return nil
	}
	if *writeFlag {
		if out == string(src) {
			return nil
		}
		return os.WriteFile(path, []byte(out), 0o644)
	}
	_, err = os.Stdout.WriteString(out)
	return err
}

func processReader(r io.Reader, w io.Writer) error {
	src, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	out, err := format.Format(string(src))
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, out)
	return err
}

func isTemplateFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".html", ".htm", ".xml", ".svg", ".rss", ".atom", ".gotmpl", ".txt":
		return true
	}
	return false
}

func initVersionInfo() {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	tag = resolveTag(tag, bi)

	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs":
		case "vcs.revision":
			commit = s.Value
		case "vcs.time":
			date = s.Value
		case "vcs.modified":
		}
	}
}

// resolveTag returns the tag set via ldflags if any, otherwise the module
// version recorded in the build info. The latter is what is available when
// installed with e.g. go install github.com/gohugoio/gotmplfmt@v0.4.1.
func resolveTag(tag string, bi *debug.BuildInfo) string {
	if tag != "" && tag != develTag {
		return tag
	}
	if bi != nil && bi.Main.Version != "" && bi.Main.Version != develTag {
		return bi.Main.Version
	}
	return tag
}
