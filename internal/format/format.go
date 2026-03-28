package format

import (
	"strings"

	"github.com/gohugoio/gotmplfmt/internal/parse"
)

func Format(text string) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	root, err := parse.Parse(text)
	if err != nil {
		return "", err
	}
	if list, ok := root.(*parse.ListNode); ok && list.HasIgnoreAll() {
		return text, nil
	}
	return root.String(), nil
}
