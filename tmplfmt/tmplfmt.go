package tmplfmt

import (
	"github.com/bep/gotmplfmt/internal/parse"
)

func Format(text string) (string, error) {
	root, err := parse.Parse(text)
	if err != nil {
		return "", err
	}
	return root.String(), nil
}
