

* I have used [prettier-plugin-go-template](https://github.com/NiklasPor/prettier-plugin-go-template) for many years, to the formatting here is greatly influenced by that output.
* Some notable differences:
  * We use ... tabs and not spaces for indentation.
  * We don't auto-add trailing newlines; see [this issue](https://github.com/prettier/prettier/issues/13036) for some context.
  * We don't read `.prettierignore`.
  * But we do support `{{/* gotmplfmt-ignore-all */}}` (must be at the top of file), `{{/* gotmplfmt-ignore-start */}}` and `{{/* gotmplfmt-ignore-end */}}`
   

----

This is an EXPERIMENTAL [Go template](https://pkg.go.dev/html/template) formatter.

It is designed with HTML templates in mind, but it may later support other types of whitespace-insensitive templates.

It is derived from the text/template/parse package in Go 1.20.4 (see license note below). However, it has been substantively modified, not entirely gracefully.

## State

Current abilities:

* does not alter final rendered output
* adjusts whitespace inside some nodes, e.g. converts `{{end}}` to `{{ end }}` and does some indentation of multiline nodes

Future things to work on:

* lots of unit tests (i know, i know)
* smarter gofmt-like opinions about organization, line wrapping, etc.
* interline alignment using whitespace
* whitespace-only changes to text to automatically indent an entire document

Feedback about the current state and what you'd like out of a future state is welcome. But to set expectations, this tool may or may not get abandoned, and comments will be almost certainly be replied to slowly.

Note that the parser contained herein is more lax than the actual template parser are used for rendering.

## Code

I do not recommend looking at the code right now. It has not gone through any effort at cleanup, and is very messy and disorganized and contains the seeds of several false starts.

A few notes follow for anyone foolish enough to go spelunking. Or for future me.

The standard library parser has a few shortcomings when it comes to re-emitting formatted code from the AST:

- trimming (`{{-` and `-}}`) are handled in the lexer, rather than represented explicitly in the AST
- `{{ else if X }}` is rewritten during parsing into `{{ else }}{{ if X }}`
- `define` and `block` generate new templates rather than being integrated into the parent AST
- `:=` is not distinguished from `=` once variable-related paperwork is done
- `{{ end }}` is not represented explicitly

It also does a bunch of extra work that we don't need:

- checking functions resolve correctly
- ensuring correctness if `break` or `continue` are function names
- variable stack tracking

This hacked up parser tracks more of the original input state. It also simplifies the parser: It treats all control-like structures identically. This is any node that has a corresponding end node: `range`, `if`, `define`, `with`, `block`, etc. As a result, it will accept and formats semantically invalid templates. Oh well; gofmt will format code that doesn't type check.

## License

For the license for this code, please see the LICENSE file (spoiler: BSD).

This code is based on code from the Go standard library. The BSD-ish license for that code is:

```
Copyright (c) 2009 The Go Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google Inc. nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
