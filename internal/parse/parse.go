// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package parse builds parse trees for templates as defined by text/template
// and html/template. Clients should use those packages to construct templates
// rather than this one, which provides shared internal data structures not
// intended for general use.
package parse

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

// Tree is the representation of a single parsed template.
type Tree struct {
	Root *ListNode // top-level root of the tree.
	text string    // text parsed to create the template (or its parent)
	// Parsing only; cleared after parse.
	lex        *lexer
	token      [3]item // three-token lookahead for parser.
	peekCount  int
	actionLine int // line of left delim starting action
}

// A mode value is a set of flags (or 0). Modes control parser behavior.
type Mode uint

const (
	ParseComments Mode = 1 << iota // parse comments and add them to AST
	SkipFuncCheck                  // do not check that functions are defined
)

// Parse returns a map from template name to parse.Tree, created by parsing the
// templates described in the argument string. The top-level template will be
// given the specified name. If an error is encountered, parsing stops and an
// empty map is returned with the error.
func Parse(text string) (Node, error) {
	t := new(Tree)
	err := t.Parse(text)
	if err != nil {
		return nil, err
	}
	return t.Root, nil
}

// next returns the next token.
func (t *Tree) next() item {
	if t.peekCount > 0 {
		t.peekCount--
	} else {
		t.token[0] = t.lex.nextItem()
	}
	return t.token[t.peekCount]
}

// backup backs the input stream up one token.
func (t *Tree) backup() {
	t.peekCount++
}

// backup2 backs the input stream up two tokens.
// The zeroth token is already there.
func (t *Tree) backup2(t1 item) {
	t.token[1] = t1
	t.peekCount = 2
}

// backup3 backs the input stream up three tokens
// The zeroth token is already there.
func (t *Tree) backup3(t2, t1 item) { // Reverse order: we're pushing back.
	t.token[1] = t1
	t.token[2] = t2
	t.peekCount = 3
}

// peek returns but does not consume the next token.
func (t *Tree) peek() item {
	if t.peekCount > 0 {
		return t.token[t.peekCount-1]
	}
	t.peekCount = 1
	t.token[0] = t.lex.nextItem()
	return t.token[0]
}

// nextNonSpace returns the next non-space token.
func (t *Tree) nextNonSpace() (token item) {
	for {
		token = t.next()
		if token.typ != itemSpace {
			break
		}
	}
	return token
}

// peekNonSpace returns but does not consume the next non-space token.
func (t *Tree) peekNonSpace() item {
	token := t.nextNonSpace()
	t.backup()
	return token
}

// Parsing.

// ErrorContext returns a textual representation of the location of the node in the input text.
// The receiver is only used when the node does not have a pointer to the tree inside,
// which can occur in old code.
func (t *Tree) ErrorContext(n Node) (location, context string) {
	pos := int(n.Position())
	tree := n.tree()
	if tree == nil {
		tree = t
	}
	text := tree.text[:pos]
	byteNum := strings.LastIndex(text, "\n")
	if byteNum == -1 {
		byteNum = pos // On first line.
	} else {
		byteNum++ // After the newline.
		byteNum = pos - byteNum
	}
	lineNum := 1 + strings.Count(text, "\n")
	context = n.String()
	return fmt.Sprintf("%d:%d", lineNum, byteNum), context
}

// errorf formats the error and terminates processing.
func (t *Tree) errorf(format string, args ...any) {
	t.Root = nil
	format = fmt.Sprintf("template: %d: %s", t.token[0].line, format)
	panic(fmt.Errorf(format, args...))
}

// error terminates processing.
func (t *Tree) error(err error) {
	t.errorf("%s", err)
}

// expect consumes the next token and guarantees it has the required type.
func (t *Tree) expect(expected itemType, context string) item {
	token := t.nextNonSpace()
	if token.typ != expected {
		t.unexpected(token, context)
	}
	return token
}

// unexpected complains about the token and terminates processing.
func (t *Tree) unexpected(token item, context string) {
	if token.typ == itemError {
		extra := ""
		if t.actionLine != 0 && t.actionLine != token.line {
			extra = fmt.Sprintf(" in action started at :%d", t.actionLine)
			if strings.HasSuffix(token.val, " action") {
				extra = extra[len(" in action"):] // avoid "action in action"
			}
		}
		t.errorf("%s%s", token, extra)
	}
	t.errorf("unexpected %s in %s", token, context)
}

// Parse parses the template definition string to construct a representation of
// the template for formatting.
func (t *Tree) Parse(text string) (err error) {
	defer func() {
		e := recover()
		if e == nil {
			return
		}
		if _, ok := e.(runtime.Error); ok {
			panic(e)
		}
		err = e.(error)
	}()
	t.lex = lex(text)
	t.text = text
	t.parse()
	return nil
}

// parse is the top-level parser for a template, essentially the same
// as itemList except it also parses {{define}} actions.
// It runs to EOF.
func (t *Tree) parse() {
	t.Root = t.newList(t.peek().pos)
	for t.peek().typ != itemEOF {
		if t.peek().typ == itemLeftDelim {
			delim := t.next()
			t.nextNonSpace()
			t.backup2(delim)
		}
		switch n := t.textOrAction(); n.Type() {
		case nodeEnd, nodeElse:
			t.errorf("unexpected %s", n)
		default:
			t.Root.append(n)
		}
	}
}

// itemList:
//
//	textOrAction*
//
// Terminates at {{end}} or {{else}}, returned separately.
func (t *Tree) itemList() (list *ListNode, next Node) {
	list = t.newList(t.peekNonSpace().pos)
	for t.peekNonSpace().typ != itemEOF {
		n := t.textOrAction()
		switch n.Type() {
		case nodeEnd, nodeElse:
			return list, n
		}
		list.append(n)
	}
	t.errorf("unexpected EOF")
	return
}

// textOrAction:
//
//	text | comment | action
func (t *Tree) textOrAction() (n Node) {
	token := t.nextNonSpace()
	switch token.typ {
	case itemText:
		return t.newText(token.pos, token.val, token.line)
	case itemLeftDelim:
		t.actionLine = token.line
		defer t.clearActionLine()
		return t.action(token.trim)
	case itemComment:
		return t.newComment(token.pos, token.val, token.trim, token.line)
	default:
		t.unexpected(token, "input")
	}
	return nil
}

func (t *Tree) clearActionLine() {
	t.actionLine = 0
}

// Action:
//
//	control
//	command ("|" command)*
//
// Left delim is past. Now get actions.
// First word could be a keyword such as range.
func (t *Tree) action(trim trim) (n Node) {
	switch token := t.nextNonSpace(); token.typ {
	case itemElse:
		return t.elseControl(trim)
	case itemEnd:
		return t.endControl(trim)
	case itemIf, itemBranch:
		return t.branchControl(token.val, trim)
	}
	t.backup()
	token := t.peek()
	pipe, endtok := t.pipeline("command", itemRightDelim)
	trim.right = endtok.trim.right
	return t.newAction(token.pos, token.line, pipe, trim)
}

// Pipeline:
//
//	declarations? command ('|' command)*
func (t *Tree) pipeline(context string, end itemType) (*PipeNode, item) {
	token := t.peekNonSpace()
	pipe := t.newPipeline(token.pos, token.line, nil)
	// Are there declarations or assignments?
decls:
	if v := t.peekNonSpace(); v.typ == itemVariable {
		t.next()
		// Since space is a token, we need 3-token look-ahead here in the worst case:
		// in "$x foo" we need to read "foo" (as opposed to ":=") to know that $x is an
		// argument variable rather than a declaration. So remember the token
		// adjacent to the variable so we can push it back if necessary.
		tokenAfterVariable := t.peek()
		next := t.peekNonSpace()
		switch {
		case next.typ == itemAssign, next.typ == itemDeclare:
			pipe.IsAssign = next.typ == itemAssign
			t.nextNonSpace()
			pipe.Decl = append(pipe.Decl, t.newVariable(v.pos, v.val, v.line))
		case next.typ == itemChar && next.val == ",":
			t.nextNonSpace()
			pipe.Decl = append(pipe.Decl, t.newVariable(v.pos, v.val, v.line))
			if context == "range" && len(pipe.Decl) < 2 {
				switch t.peekNonSpace().typ {
				case itemVariable, itemRightDelim, itemRightParen:
					// second initialized variable in a range pipeline
					goto decls
				default:
					t.errorf("range can only initialize variables")
				}
			}
			t.errorf("too many declarations in %s", context)
		case tokenAfterVariable.typ == itemSpace:
			t.backup3(v, tokenAfterVariable)
		default:
			t.backup2(v)
		}
	}
	for {
		switch token := t.nextNonSpace(); token.typ {
		case end:
			// At this point, the pipeline is complete
			t.checkPipeline(pipe, context)
			return pipe, token
		case itemBool, itemCharConstant, itemComplex, itemDot, itemField, itemIdentifier,
			itemNumber, itemNil, itemRawString, itemString, itemVariable, itemLeftParen:
			t.backup()
			pipe.append(t.command())
		default:
			t.unexpected(token, context)
		}
	}
}

func (t *Tree) checkPipeline(pipe *PipeNode, context string) {
	// Allow empty pipelines — this is a formatter, not a validator.
	if len(pipe.Cmds) == 0 {
		return
	}
	// Only the first command of a pipeline can start with a non executable operand
	for i, c := range pipe.Cmds[1:] {
		switch c.Args[0].Type() {
		case NodeBool, NodeDot, NodeNil, NodeNumber, NodeString:
			// With A|B|C, pipeline stage 2 is B
			t.errorf("non executable command in pipeline stage %d", i+2)
		}
	}
}

// branch-y thing:
//
//	{{if pipeline}} itemList {{end}}
//	{{if pipeline}} itemList {{else}} itemList {{end}}
//
// If keyword is past.
func (t *Tree) branchControl(keyword string, trim trim) Node {
	pipe, tok := t.pipeline(keyword, itemRightDelim)
	trim.right = tok.trim.right
	b := &BranchNode{
		tr:      t,
		Keyword: keyword,
		Pos:     pipe.Position(),
		Line:    pipe.Line,
		Pipe:    pipe,
		Trim:    trim,
	}
	var next Node
	b.List, next = t.itemList() // TODO: use next
Elses:
	for {
		switch n := next.(type) {
		case *ElseNode:
			n.List, next = t.itemList()
			b.Elses = append(b.Elses, n)
		case *EndNode:
			b.End = n
			break Elses
		}
	}
	return b
}

// End:
//
//	{{end}}
//
// End keyword is past.
func (t *Tree) endControl(trim trim) Node {
	token := t.expect(itemRightDelim, "end")
	trim.right = token.trim.right
	return t.newEnd(token.pos, trim, token.line)
}

// Else:
//
//	{{else}}
//
// Else keyword is past.
func (t *Tree) elseControl(trim trim) Node {
	var token item
	var pipe *PipeNode
	var keyword string
	peek := t.peekNonSpace()
	if peek.typ == itemIf || peek.typ == itemBranch {
		keyword = peek.val
		token = t.next() // Consume the "if"/"with"/etc. token.
		var eoptok item
		pipe, eoptok = t.pipeline("else "+keyword, itemRightDelim)
		trim.right = eoptok.trim.right
	} else {
		token = t.expect(itemRightDelim, "else")
		trim.right = token.trim.right
	}
	return t.newElse(token.pos, token.line, keyword, pipe, trim)
}

// command:
//
//	operand (space operand)*
//
// space-separated arguments up to a pipeline character or right delimiter.
// we consume the pipe character but leave the right delim to terminate the action.
func (t *Tree) command() *CommandNode {
	tok := t.peekNonSpace()
	cmd := t.newCommand(tok.pos, tok.line)
	for {
		t.peekNonSpace() // skip leading spaces.
		operand := t.operand()
		if operand != nil {
			cmd.append(operand)
		}
		switch token := t.next(); token.typ {
		case itemSpace:
			continue
		case itemRightDelim, itemRightParen:
			t.backup()
		case itemPipe:
			// nothing here; break loop below
		default:
			t.unexpected(token, "operand")
		}
		break
	}
	if len(cmd.Args) == 0 {
		t.errorf("empty command")
	}
	return cmd
}

// operand:
//
//	term .Field*
//
// An operand is a space-separated component of a command,
// a term possibly followed by field accesses.
// A nil return means the next item is not an operand.
func (t *Tree) operand() Node {
	node := t.term()
	if node == nil {
		return nil
	}
	if t.peek().typ == itemField {
		chainTok := t.peek()
		chain := t.newChain(chainTok.pos, node, chainTok.line)
		for t.peek().typ == itemField {
			chain.Add(t.next().val)
		}
		// Compatibility with original API: If the term is of type NodeField
		// or NodeVariable, just put more fields on the original.
		// Otherwise, keep the Chain node.
		// Obvious parsing errors involving literal values are detected here.
		// More complex error cases will have to be handled at execution time.
		switch node.Type() {
		case NodeField:
			node = t.newField(chain.Position(), chain.String(), chainTok.line)
		case NodeVariable:
			node = t.newVariable(chain.Position(), chain.String(), chainTok.line)
		case NodeBool, NodeString, NodeNumber, NodeNil, NodeDot:
			t.errorf("unexpected . after term %q", node.String())
		default:
			node = chain
		}
	}
	return node
}

// term:
//
//	literal (number, string, nil, boolean)
//	function (identifier)
//	.
//	.Field
//	$
//	'(' pipeline ')'
//
// A term is a simple "expression".
// A nil return means the next item is not a term.
func (t *Tree) term() Node {
	switch token := t.nextNonSpace(); token.typ {
	case itemIdentifier:
		return NewIdentifier(token.val).SetTree(t).SetPos(token.pos).SetLine(token.line)
	case itemDot:
		return t.newDot(token.pos, token.line)
	case itemNil:
		return t.newNil(token.pos, token.line)
	case itemVariable:
		return t.useVar(token.pos, token.val, token.line)
	case itemField:
		return t.newField(token.pos, token.val, token.line)
	case itemBool:
		return t.newBool(token.pos, token.val == "true", token.line)
	case itemCharConstant, itemComplex, itemNumber:
		number, err := t.newNumber(token.pos, token.val, token.typ, token.line)
		if err != nil {
			t.error(err)
		}
		return number
	case itemLeftParen:
		pipe, _ := t.pipeline("parenthesized pipeline", itemRightParen)
		return pipe
	case itemString, itemRawString:
		s, err := strconv.Unquote(token.val)
		if err != nil {
			t.error(err)
		}
		return t.newString(token.pos, token.val, s, token.line)
	}
	t.backup()
	return nil
}

// useVar returns a node for a variable reference. It errors if the
// variable is not defined.
func (t *Tree) useVar(pos Pos, name string, line int) Node {
	return t.newVariable(pos, name, line)
}
