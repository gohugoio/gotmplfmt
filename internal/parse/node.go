// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Parse nodes.

package parse

import (
	"fmt"
	"strconv"
	"strings"
)

var textFormat = "%s" // Changed to "%q" in tests for better error messages.

// Directive comment markers.
const (
	directiveIgnoreAll   = "gotmplfmt-ignore-all"
	directiveIgnoreStart = "gotmplfmt-ignore-start"
	directiveIgnoreEnd   = "gotmplfmt-ignore-end"
)

// Pre-computed indent strings to avoid repeated allocation.
var indentTab [16]string

func init() {
	for i := range indentTab {
		indentTab[i] = strings.Repeat("\t", i)
	}
}

func indent(level int) string {
	if level < len(indentTab) {
		return indentTab[level]
	}
	return strings.Repeat("\t", level)
}

// A Node is an element in the parse tree. The interface is trivial.
// The interface contains an unexported method so that only
// types local to this package can satisfy it.
type Node interface {
	Type() NodeType
	String() string
	Position() Pos // byte position of start of node in full original input string
	// tree returns the containing *Tree.
	// It is unexported so all implementations of Node are in this package.
	tree() *Tree
	// writeTo writes the String output to the builder.
	writeTo(*printer)
}

// NodeType identifies the type of a parse tree node.
type NodeType int

// Pos represents a byte position in the original input text from which
// this template was parsed.
type Pos int

func (p Pos) Position() Pos {
	return p
}

type printer struct {
	*strings.Builder
	prefix      string
	depth       int
	branchDepth int
	branchBase  int // extra indent levels from whitespace context
}

func newPrinter() *printer {
	return &printer{
		Builder: new(strings.Builder),
	}
}

func (p *printer) WritePrefix() {
	p.WriteString(p.prefix)
	p.WriteString(indent(p.depth))
}

func (p *printer) writeBranchIndent() {
	if p.branchDepth == 0 && p.branchBase == 0 {
		return
	}
	s := p.String()
	if len(s) == 0 || s[len(s)-1] == '\n' {
		p.WriteString(indent(p.branchBase + p.branchDepth))
	}
}

func lineno(n Node) int {
	// TODO: common uses will be quadratic!
	// it would be easy to put in place a simple lookup structure instead at some point
	return strings.Count(n.tree().text[:n.Position()], "\n")
}

// whitespacePrefix returns the exact whitespace from the beginning of n's line to n.
// start is the token that starts n, e.g. "{{" or "(".
// If there is any non-whitespace, it returns "", false.
// For example, for a line that begins "\t\t{{ " it will return "\t\t", true,
// but "\tx\t{{ " will yield "", false.
func whitespacePrefix(n Node, ltok string) (string, bool) {
	txt := n.tree().text
	pos := n.Position()
	// Find the ltok (e.g. "{{") that precedes this node.
	tokPos := strings.LastIndex(txt[:pos], ltok)
	if tokPos < 0 {
		return "", false
	}
	// Get the whitespace from the start of ltok's line to ltok.
	start := strings.LastIndex(txt[:tokPos], "\n")
	// start is -1 on the first line; start+1 == 0 gives us the full prefix.
	line := txt[start+1 : tokPos]
	w := int(leftTrimLength(line)) // length of whitespace
	if w != len(line) {
		return "", false
	}
	return line, true
}

// Type returns itself and provides an easy default implementation
// for embedding in a Node. Embedded in all non-trivial Nodes.
func (t NodeType) Type() NodeType {
	return t
}

const (
	NodeText       NodeType = iota // Plain text.
	NodeAction                     // A non-control action such as a field evaluation.
	NodeBool                       // A boolean constant.
	NodeChain                      // A sequence of field accesses.
	NodeCommand                    // An element of a pipeline.
	NodeDot                        // The cursor, dot.
	nodeElse                       // An else action. Not added to tree.
	nodeEnd                        // An end action. Not added to tree.
	NodeField                      // A field or method name.
	NodeIdentifier                 // An identifier; always a function name.
	NodeBranch                     // A branch-y action.
	NodeList                       // A list of Nodes.
	NodeNil                        // An untyped nil constant.
	NodeNumber                     // A numerical constant.
	NodePipe                       // A pipeline of commands.
	NodeString                     // A string constant.
	NodeTemplate                   // A template invocation action.
	NodeVariable                   // A $ variable.
	NodeComment                    // A comment.
)

// Nodes.

// ListNode holds a sequence of nodes.
type ListNode struct {
	NodeType
	Pos
	tr    *Tree
	Nodes []Node // The element nodes in lexical order.
}

func (t *Tree) newList(pos Pos) *ListNode {
	return &ListNode{tr: t, NodeType: NodeList, Pos: pos}
}

// HasIgnoreAll reports whether the first comment in the list is a gotmplfmt-ignore-all directive.
func (l *ListNode) HasIgnoreAll() bool {
	for _, n := range l.Nodes {
		if c, ok := n.(*CommentNode); ok {
			return strings.Contains(c.Text, directiveIgnoreAll)
		}
		if _, ok := n.(*TextNode); ok {
			continue
		}
		break
	}
	return false
}

func (l *ListNode) append(n Node) {
	l.Nodes = append(l.Nodes, n)
}

func (l *ListNode) tree() *Tree {
	return l.tr
}

func (l *ListNode) String() string {
	p := newPrinter()
	l.writeTo(p)
	return p.String()
}

func (l *ListNode) writeTo(sb *printer) {
	if l == nil {
		return
	}
	for i := 0; i < len(l.Nodes); i++ {
		n := l.Nodes[i]
		if c, ok := n.(*CommentNode); ok && strings.Contains(c.Text, directiveIgnoreStart) {
			found := false
			for j := i + 1; j < len(l.Nodes); j++ {
				if c2, ok := l.Nodes[j].(*CommentNode); ok && strings.Contains(c2.Text, directiveIgnoreEnd) {
					src := l.tr.text
					regionStart := strings.LastIndex(src[:int(c.Position())], "{{")
					afterEnd := int(c2.Position()) + len(c2.Text)
					regionEnd := afterEnd + strings.Index(src[afterEnd:], "}}") + 2
					sb.WriteString(src[regionStart:regionEnd])
					i = j
					found = true
					break
				}
			}
			if found {
				continue
			}
		}
		n.writeTo(sb)
	}
}

// TextNode holds plain text.
type TextNode struct {
	NodeType
	Pos
	tr   *Tree
	Text string // The text; may span newlines.
}

func (t *Tree) newText(pos Pos, text string) *TextNode {
	return &TextNode{tr: t, NodeType: NodeText, Pos: pos, Text: text}
}

func (t *TextNode) String() string {
	return fmt.Sprintf(textFormat, t.Text)
}

func (t *TextNode) writeTo(sb *printer) {
	lines := strings.Split(t.Text, "\n")
	if sb.branchDepth == 0 {
		// At root level, preserve first line but strip leading
		// whitespace from subsequent lines to normalize indentation.
		for i, line := range lines {
			if i > 0 {
				sb.WriteByte('\n')
				sb.WriteString(strings.TrimLeft(line, " \t"))
			} else {
				sb.WriteString(line)
			}
		}
		return
	}
	// Inside a template block: strip and re-indent based on depth.
	for i, line := range lines {
		if i > 0 {
			sb.WriteByte('\n')
			trimmed := strings.TrimLeft(line, " \t")
			if trimmed != "" {
				sb.WriteString(indent(sb.branchBase + sb.branchDepth))
				sb.WriteString(trimmed)
			}
		} else {
			sb.WriteString(line)
		}
	}
}

func (t *TextNode) tree() *Tree {
	return t.tr
}

// CommentNode holds a comment.
type CommentNode struct {
	NodeType
	Pos
	tr   *Tree
	Text string // Comment text.
}

func (t *Tree) newComment(pos Pos, text string) *CommentNode {
	return &CommentNode{tr: t, NodeType: NodeComment, Pos: pos, Text: text}
}

func (c *CommentNode) String() string {
	sb := newPrinter()
	c.writeTo(sb)
	return sb.String()
}

func (c *CommentNode) writeTo(sb *printer) {
	sb.WriteString("{{")
	sb.WriteString(c.Text)
	sb.WriteString("}}")
}

func (c *CommentNode) tree() *Tree {
	return c.tr
}

// PipeNode holds a pipeline with optional declaration
type PipeNode struct {
	NodeType
	Pos
	tr       *Tree
	Line     int             // The line number in the input. Deprecated: Kept for compatibility.
	IsAssign bool            // The variables are being assigned, not declared.
	Decl     []*VariableNode // Variables in lexical order.
	Cmds     []*CommandNode  // The commands in lexical order.
}

func (t *Tree) newPipeline(pos Pos, line int, vars []*VariableNode) *PipeNode {
	return &PipeNode{tr: t, NodeType: NodePipe, Pos: pos, Line: line, Decl: vars}
}

func (p *PipeNode) append(command *CommandNode) {
	p.Cmds = append(p.Cmds, command)
}

func (p *PipeNode) String() string {
	sb := newPrinter()
	p.writeTo(sb)
	return sb.String()
}

func (p *PipeNode) writeTo(sb *printer) {
	if len(p.Decl) > 0 {
		for i, v := range p.Decl {
			if i > 0 {
				sb.WriteString(", ")
			}
			v.writeTo(sb)
		}
		if p.IsAssign {
			sb.WriteString(" = ")
		} else {
			sb.WriteString(" := ")
		}
	}
	for i, c := range p.Cmds {
		if i > 0 {
			sb.WriteString(" | ")
		}
		c.writeTo(sb)
	}
}

func (p *PipeNode) tree() *Tree {
	return p.tr
}

// ActionNode holds an action (something bounded by delimiters).
// Control actions have their own nodes; ActionNode represents simple
// ones such as field evaluations and parenthesized pipelines.
type ActionNode struct {
	NodeType
	Pos
	tr   *Tree
	Line int       // The line number in the input. Deprecated: Kept for compatibility.
	Pipe *PipeNode // The pipeline in the action.
	Trim trim
}

func (t *Tree) newAction(pos Pos, line int, pipe *PipeNode, trim trim) *ActionNode {
	return &ActionNode{tr: t, NodeType: NodeAction, Pos: pos, Line: line, Pipe: pipe, Trim: trim}
}

func (a *ActionNode) String() string {
	sb := newPrinter()
	a.writeTo(sb)
	return sb.String()
}

func (a *ActionNode) writeTo(sb *printer) {
	sb.writeBranchIndent()
	// Compute prefix from the builder output (what's on the current line
	// before this action) rather than the original text, so that the prefix
	// is stable across round trips even when text nodes strip whitespace.
	s := sb.String()
	lastNL := strings.LastIndexByte(s, '\n')
	afterNL := s
	if lastNL >= 0 {
		afterNL = s[lastNL+1:]
	}
	ok := strings.TrimLeft(afterNL, " \t") == ""
	if ok {
		sb.prefix = afterNL
	} else {
		sb.prefix = ""
	}
	sb.WriteString(a.Trim.leftDelim())
	before := strings.Count(sb.String(), "\n")
	sb.depth = 1
	a.Pipe.writeTo(sb)
	sb.depth = 0
	after := strings.Count(sb.String(), "\n")
	if before != after {
		sb.WriteString("\n")
		if ok {
			sb.WritePrefix()
		}
		sb.WriteString(a.Trim.rightDelimNoSpace())
	} else {
		sb.WriteString(a.Trim.rightDelim())
	}
}

func (a *ActionNode) tree() *Tree {
	return a.tr
}

// CommandNode holds a command (a pipeline inside an evaluating action).
type CommandNode struct {
	NodeType
	Pos
	tr   *Tree
	Args []Node // Arguments in lexical order: Identifier, field, or constant.
	Trim trim
}

func (t *Tree) newCommand(pos Pos) *CommandNode {
	return &CommandNode{tr: t, NodeType: NodeCommand, Pos: pos}
}

func (c *CommandNode) append(arg Node) {
	c.Args = append(c.Args, arg)
}

func (c *CommandNode) String() string {
	sb := newPrinter()
	c.writeTo(sb)
	return sb.String()
}

func (c *CommandNode) writeTo(sb *printer) {
	if len(c.Args) == 0 {
		return
	}
	var prevLine int
	for i, arg := range c.Args {
		// TODO: quadratic!!!
		line := lineno(arg)
		if i > 0 {
			if line > prevLine {
				// TODO: preserve blank lines in input? That'd be: sb.WriteString(strings.Repeat("\n", line-prevLine))
				sb.WriteString("\n")
				sb.WritePrefix()
			} else {
				sb.WriteByte(' ')
			}
		}
		prevLine = line
		if arg, ok := arg.(*PipeNode); ok {
			sb.WriteByte('(')
			before := strings.Count(sb.String(), "\n")
			sb.depth++
			arg.writeTo(sb)
			sb.depth--
			after := strings.Count(sb.String(), "\n")
			if ok && before != after {
				sb.WriteString("\n")
				sb.WritePrefix()
			}
			sb.WriteByte(')')
			continue
		}
		arg.writeTo(sb)
	}
}

func (c *CommandNode) tree() *Tree {
	return c.tr
}

// IdentifierNode holds an identifier.
type IdentifierNode struct {
	NodeType
	Pos
	tr    *Tree
	Ident string // The identifier's name.
}

// NewIdentifier returns a new IdentifierNode with the given identifier name.
func NewIdentifier(ident string) *IdentifierNode {
	return &IdentifierNode{NodeType: NodeIdentifier, Ident: ident}
}

// SetPos sets the position. NewIdentifier is a public method so we can't modify its signature.
// Chained for convenience.
// TODO: fix one day?
func (i *IdentifierNode) SetPos(pos Pos) *IdentifierNode {
	i.Pos = pos
	return i
}

// SetTree sets the parent tree for the node. NewIdentifier is a public method so we can't modify its signature.
// Chained for convenience.
// TODO: fix one day?
func (i *IdentifierNode) SetTree(t *Tree) *IdentifierNode {
	i.tr = t
	return i
}

func (i *IdentifierNode) String() string {
	return i.Ident
}

func (i *IdentifierNode) writeTo(sb *printer) {
	sb.WriteString(i.String())
}

func (i *IdentifierNode) tree() *Tree {
	return i.tr
}

// VariableNode holds a list of variable names, possibly with chained field
// accesses. The dollar sign is part of the (first) name.
type VariableNode struct {
	NodeType
	Pos
	tr    *Tree
	Ident []string // Variable name and fields in lexical order.
}

func (t *Tree) newVariable(pos Pos, ident string) *VariableNode {
	return &VariableNode{tr: t, NodeType: NodeVariable, Pos: pos, Ident: strings.Split(ident, ".")}
}

func (v *VariableNode) String() string {
	sb := newPrinter()
	v.writeTo(sb)
	return sb.String()
}

func (v *VariableNode) writeTo(sb *printer) {
	for i, id := range v.Ident {
		if i > 0 {
			sb.WriteByte('.')
		}
		sb.WriteString(id)
	}
}

func (v *VariableNode) tree() *Tree {
	return v.tr
}

// DotNode holds the special identifier '.'.
type DotNode struct {
	NodeType
	Pos
	tr *Tree
}

func (t *Tree) newDot(pos Pos) *DotNode {
	return &DotNode{tr: t, NodeType: NodeDot, Pos: pos}
}

func (d *DotNode) Type() NodeType {
	// Override method on embedded NodeType for API compatibility.
	// TODO: Not really a problem; could change API without effect but
	// api tool complains.
	return NodeDot
}

func (d *DotNode) String() string {
	return "."
}

func (d *DotNode) writeTo(sb *printer) {
	sb.WriteString(d.String())
}

func (d *DotNode) tree() *Tree {
	return d.tr
}

// NilNode holds the special identifier 'nil' representing an untyped nil constant.
type NilNode struct {
	NodeType
	Pos
	tr *Tree
}

func (t *Tree) newNil(pos Pos) *NilNode {
	return &NilNode{tr: t, NodeType: NodeNil, Pos: pos}
}

func (n *NilNode) Type() NodeType {
	// Override method on embedded NodeType for API compatibility.
	// TODO: Not really a problem; could change API without effect but
	// api tool complains.
	return NodeNil
}

func (n *NilNode) String() string {
	return "nil"
}

func (n *NilNode) writeTo(sb *printer) {
	sb.WriteString(n.String())
}

func (n *NilNode) tree() *Tree {
	return n.tr
}

// FieldNode holds a field (identifier starting with '.').
// The names may be chained ('.x.y').
// The period is dropped from each ident.
type FieldNode struct {
	NodeType
	Pos
	tr    *Tree
	Ident []string // The identifiers in lexical order.
}

func (t *Tree) newField(pos Pos, ident string) *FieldNode {
	return &FieldNode{tr: t, NodeType: NodeField, Pos: pos, Ident: strings.Split(ident[1:], ".")} // [1:] to drop leading period
}

func (f *FieldNode) String() string {
	sb := newPrinter()
	f.writeTo(sb)
	return sb.String()
}

func (f *FieldNode) writeTo(sb *printer) {
	for _, id := range f.Ident {
		sb.WriteByte('.')
		sb.WriteString(id)
	}
}

func (f *FieldNode) tree() *Tree {
	return f.tr
}

// ChainNode holds a term followed by a chain of field accesses (identifier starting with '.').
// The names may be chained ('.x.y').
// The periods are dropped from each ident.
type ChainNode struct {
	NodeType
	Pos
	tr    *Tree
	Node  Node
	Field []string // The identifiers in lexical order.
}

func (t *Tree) newChain(pos Pos, node Node) *ChainNode {
	return &ChainNode{tr: t, NodeType: NodeChain, Pos: pos, Node: node}
}

// Add adds the named field (which should start with a period) to the end of the chain.
func (c *ChainNode) Add(field string) {
	if len(field) == 0 || field[0] != '.' {
		panic("no dot in field")
	}
	field = field[1:] // Remove leading dot.
	if field == "" {
		panic("empty field")
	}
	c.Field = append(c.Field, field)
}

func (c *ChainNode) String() string {
	sb := newPrinter()
	c.writeTo(sb)
	return sb.String()
}

func (c *ChainNode) writeTo(sb *printer) {
	if _, ok := c.Node.(*PipeNode); ok {
		sb.WriteByte('(')
		c.Node.writeTo(sb)
		sb.WriteByte(')')
	} else {
		c.Node.writeTo(sb)
	}
	for _, field := range c.Field {
		sb.WriteByte('.')
		sb.WriteString(field)
	}
}

func (c *ChainNode) tree() *Tree {
	return c.tr
}

// BoolNode holds a boolean constant.
type BoolNode struct {
	NodeType
	Pos
	tr   *Tree
	True bool // The value of the boolean constant.
}

func (t *Tree) newBool(pos Pos, true bool) *BoolNode {
	return &BoolNode{tr: t, NodeType: NodeBool, Pos: pos, True: true}
}

func (b *BoolNode) String() string {
	if b.True {
		return "true"
	}
	return "false"
}

func (b *BoolNode) writeTo(sb *printer) {
	sb.WriteString(b.String())
}

func (b *BoolNode) tree() *Tree {
	return b.tr
}

// NumberNode holds a number: signed or unsigned integer, float, or complex.
// The value is parsed and stored under all the types that can represent the value.
// This simulates in a small amount of code the behavior of Go's ideal constants.
type NumberNode struct {
	NodeType
	Pos
	tr         *Tree
	IsInt      bool       // Number has an integral value.
	IsUint     bool       // Number has an unsigned integral value.
	IsFloat    bool       // Number has a floating-point value.
	IsComplex  bool       // Number is complex.
	Int64      int64      // The signed integer value.
	Uint64     uint64     // The unsigned integer value.
	Float64    float64    // The floating-point value.
	Complex128 complex128 // The complex value.
	Text       string     // The original textual representation from the input.
}

func (t *Tree) newNumber(pos Pos, text string, typ itemType) (*NumberNode, error) {
	n := &NumberNode{tr: t, NodeType: NodeNumber, Pos: pos, Text: text}
	switch typ {
	case itemCharConstant:
		rune, _, tail, err := strconv.UnquoteChar(text[1:], text[0])
		if err != nil {
			return nil, err
		}
		if tail != "'" {
			return nil, fmt.Errorf("malformed character constant: %s", text)
		}
		n.Int64 = int64(rune)
		n.IsInt = true
		n.Uint64 = uint64(rune)
		n.IsUint = true
		n.Float64 = float64(rune) // odd but those are the rules.
		n.IsFloat = true
		return n, nil
	case itemComplex:
		// fmt.Sscan can parse the pair, so let it do the work.
		if _, err := fmt.Sscan(text, &n.Complex128); err != nil {
			return nil, err
		}
		n.IsComplex = true
		n.simplifyComplex()
		return n, nil
	}
	// Imaginary constants can only be complex unless they are zero.
	if len(text) > 0 && text[len(text)-1] == 'i' {
		f, err := strconv.ParseFloat(text[:len(text)-1], 64)
		if err == nil {
			n.IsComplex = true
			n.Complex128 = complex(0, f)
			n.simplifyComplex()
			return n, nil
		}
	}
	// Do integer test first so we get 0x123 etc.
	u, err := strconv.ParseUint(text, 0, 64) // will fail for -0; fixed below.
	if err == nil {
		n.IsUint = true
		n.Uint64 = u
	}
	i, err := strconv.ParseInt(text, 0, 64)
	if err == nil {
		n.IsInt = true
		n.Int64 = i
		if i == 0 {
			n.IsUint = true // in case of -0.
			n.Uint64 = u
		}
	}
	// If an integer extraction succeeded, promote the float.
	if n.IsInt {
		n.IsFloat = true
		n.Float64 = float64(n.Int64)
	} else if n.IsUint {
		n.IsFloat = true
		n.Float64 = float64(n.Uint64)
	} else {
		f, err := strconv.ParseFloat(text, 64)
		if err == nil {
			// If we parsed it as a float but it looks like an integer,
			// it's a huge number too large to fit in an int. Reject it.
			if !strings.ContainsAny(text, ".eEpP") {
				return nil, fmt.Errorf("integer overflow: %q", text)
			}
			n.IsFloat = true
			n.Float64 = f
			// If a floating-point extraction succeeded, extract the int if needed.
			if !n.IsInt && float64(int64(f)) == f {
				n.IsInt = true
				n.Int64 = int64(f)
			}
			if !n.IsUint && float64(uint64(f)) == f {
				n.IsUint = true
				n.Uint64 = uint64(f)
			}
		}
	}
	if !n.IsInt && !n.IsUint && !n.IsFloat {
		return nil, fmt.Errorf("illegal number syntax: %q", text)
	}
	return n, nil
}

// simplifyComplex pulls out any other types that are represented by the complex number.
// These all require that the imaginary part be zero.
func (n *NumberNode) simplifyComplex() {
	n.IsFloat = imag(n.Complex128) == 0
	if n.IsFloat {
		n.Float64 = real(n.Complex128)
		n.IsInt = float64(int64(n.Float64)) == n.Float64
		if n.IsInt {
			n.Int64 = int64(n.Float64)
		}
		n.IsUint = float64(uint64(n.Float64)) == n.Float64
		if n.IsUint {
			n.Uint64 = uint64(n.Float64)
		}
	}
}

func (n *NumberNode) String() string {
	return n.Text
}

func (n *NumberNode) writeTo(sb *printer) {
	sb.WriteString(n.String())
}

func (n *NumberNode) tree() *Tree {
	return n.tr
}

// StringNode holds a string constant. The value has been "unquoted".
type StringNode struct {
	NodeType
	Pos
	tr     *Tree
	Quoted string // The original text of the string, with quotes.
	Text   string // The string, after quote processing.
}

func (t *Tree) newString(pos Pos, orig, text string) *StringNode {
	return &StringNode{tr: t, NodeType: NodeString, Pos: pos, Quoted: orig, Text: text}
}

func (s *StringNode) String() string {
	return s.Quoted
}

func (s *StringNode) writeTo(sb *printer) {
	sb.WriteString(s.String())
}

func (s *StringNode) tree() *Tree {
	return s.tr
}

// EndNode represents an {{end}} action.
type EndNode struct {
	NodeType
	Pos
	tr   *Tree
	Trim trim
}

func (t *Tree) newEnd(pos Pos, trim trim) *EndNode {
	return &EndNode{tr: t, NodeType: nodeEnd, Pos: pos, Trim: trim}
}

func (e *EndNode) String() string {
	sb := newPrinter()
	e.writeTo(sb)
	return sb.String()
}

func (e *EndNode) writeTo(sb *printer) {
	sb.writeBranchIndent()
	sb.WriteString(e.Trim.leftDelim())
	sb.WriteString("end")
	sb.WriteString(e.Trim.rightDelim())
}

func (e *EndNode) tree() *Tree {
	return e.tr
}

// ElseNode represents an {{else}} or {{else if}} action. Does not appear in the final tree.
type ElseNode struct {
	NodeType
	Pos
	tr   *Tree
	Pipe *PipeNode // guard check, may be nil for bare {{ else }}
	List *ListNode // stuff to execute if pipe holds
	Line int       // The line number in the input. Deprecated: Kept for compatibility.
	Trim trim
}

func (t *Tree) newElse(pos Pos, line int, pipe *PipeNode, trim trim) *ElseNode {
	return &ElseNode{tr: t, NodeType: nodeElse, Pos: pos, Line: line, Pipe: pipe, Trim: trim}
}

func (e *ElseNode) Type() NodeType {
	return nodeElse
}

func (e *ElseNode) String() string {
	sb := newPrinter()
	e.writeTo(sb)
	return sb.String()
}

func (e *ElseNode) writeTo(sb *printer) {
	sb.writeBranchIndent()
	sb.WriteString(e.Trim.leftDelim())
	sb.WriteString("else")
	if e.Pipe != nil {
		sb.WriteString(" if")
		if len(e.Pipe.Cmds) > 0 {
			sb.WriteByte(' ')
			e.Pipe.writeTo(sb)
		}
	}
	sb.WriteString(e.Trim.rightDelim())
	sb.branchDepth++
	e.List.writeTo(sb)
	sb.branchDepth--
}

func (e *ElseNode) tree() *Tree {
	return e.tr
}

// BranchNode is the common representation of if, range, and with.
type BranchNode struct {
	NodeType
	Keyword string
	Pos
	tr    *Tree
	Line  int         // The line number in the input. Deprecated: Kept for compatibility.
	Pipe  *PipeNode   // The pipeline to be evaluated.
	List  *ListNode   // What to execute if the value is non-empty.
	Elses []*ElseNode // all else / else if lists
	End   *EndNode
	Trim  trim
}

func (b *BranchNode) String() string {
	sb := newPrinter()
	b.writeTo(sb)
	return sb.String()
}

func (b *BranchNode) writeTo(sb *printer) {
	prevBase := sb.branchBase
	if sb.branchDepth == 0 {
		if ws, ok := whitespacePrefix(b, "{{"); ok && len(ws) > 0 {
			sb.branchBase = strings.Count(ws, "\t") // count tab indent levels
		}
	}
	sb.writeBranchIndent()
	sb.WriteString(b.Trim.leftDelim())
	sb.WriteString(b.Keyword)
	if len(b.Pipe.Cmds) > 0 {
		sb.WriteByte(' ')
		b.Pipe.writeTo(sb)
	}
	sb.WriteString(b.Trim.rightDelim())
	sb.branchDepth++
	b.List.writeTo(sb)
	sb.branchDepth--
	for _, e := range b.Elses {
		e.writeTo(sb)
	}
	b.End.writeTo(sb)
	sb.branchBase = prevBase
}

func (b *BranchNode) tree() *Tree {
	return b.tr
}
