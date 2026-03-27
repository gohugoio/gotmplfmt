

The formatting is greatly inspired by [prettier-plugin-go-template](https://github.com/NiklasPor/prettier-plugin-go-template).

* Some notable differences:
  * We focus on getting the overall structure right, and not on formatting details (which is often highly subjective).
  * We use ... tabs and not spaces for indentation.
  * We don't auto-add trailing newlines to the document; see [this issue](https://github.com/prettier/prettier/issues/13036) for some context.
  * We don't read `.prettierignore`.
  * But we do support `{{/* gotmplfmt-ignore-all */}}` (must be at the top of file), `{{/* gotmplfmt-ignore-start */}}` and `{{/* gotmplfmt-ignore-end */}}`
  * And we do care about idempotency, so if you run into a example where we format differently on subsequent runs, please report it as a bug.

## License

For the license for this code, please see the LICENSE file.

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
