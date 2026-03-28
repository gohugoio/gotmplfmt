package main

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testscripts",
		// UpdateScripts: true, // Uncomment to rewrite the test scripts with
		// TestWork: true, // Uncomment to keep the test work dir.
		Setup: func(env *testscript.Env) error {
			return nil
		},
	})
}

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"gotmplfmt": main,
	})
}
