package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gohugoio/gotmplfmt/internal/format"
)

var writeFlag = flag.Bool("w", false, "write result to (source) file instead of stdout")

func main() {
	log.SetFlags(0)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: gotmplfmt [flags] [path ...]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() == 0 {
		if *writeFlag {
			log.Fatal("error: cannot use -w with standard input")
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
	case ".html", ".htm", ".xml", ".svg", ".rss", ".atom", ".txt":
		return true
	}
	return false
}
