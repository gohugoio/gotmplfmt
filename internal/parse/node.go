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

// htmlScanner tracks state while scanning HTML tags in text content.
// It is embedded in the printer and shared between computeHTMLDeltas
// and splitHTMLLine to avoid duplicating scanning logic.
type htmlScanner struct {
	inTag       bool   // inside a '<' that hasn't been closed with '>'
	isClose     bool   // the current incomplete tag is a closing tag
	isVoid      bool   // the current incomplete tag is a void element
	tagName     string // tag name of the current open tag
	quoteChar   byte   // 0 if not in attr value, '"' or '\'' otherwise
	inAttrValue bool   // inside a quoted attribute value within a tag
	tagStartIdx int    // index of '<' that opened the current tag
}

// save returns a snapshot of the scanner state for later restoration.
func (s htmlScanner) save() htmlScanner { return s }

// scanText processes text character by character, calling onTagClose for each
// completed tag. Tags spanning beyond the text leave the scanner in an open
// state. Returns the index to continue scanning from (for callers that
// skip ahead, e.g. void elements).
func (s *htmlScanner) scanText(text string, onTagClose func(tag tagEvent)) {
	for i := 0; i < len(text); i++ {
		if s.inTag {
			switch text[i] {
			case '"', '\'':
				if s.quoteChar == 0 {
					s.quoteChar = text[i]
					s.inAttrValue = true
				} else if s.quoteChar == text[i] {
					s.quoteChar = 0
					s.inAttrValue = false
				}
			case '>':
				if s.inAttrValue {
					continue
				}
				selfClose := i > 0 && text[i-1] == '/'
				onTagClose(tagEvent{
					Name:      s.tagName,
					IsClose:   s.isClose,
					IsVoid:    s.isVoid,
					SelfClose: selfClose,
					StartIdx:  s.tagStartIdx,
					EndIdx:    i,
				})
				s.inTag = false
				s.isVoid = false
				s.quoteChar = 0
				s.inAttrValue = false
			}
			continue
		}
		if text[i] != '<' || i+1 >= len(text) {
			continue
		}
		next := text[i+1]
		// Skip doctype, comments, processing instructions.
		if next == '!' || next == '?' {
			if j := strings.IndexByte(text[i:], '>'); j >= 0 {
				i += j
			}
			continue
		}
		s.inTag = true
		s.isClose = next == '/'
		s.tagStartIdx = i
		// Extract tag name.
		nameStart := i + 1
		if s.isClose {
			nameStart = i + 2
		}
		nameEnd := nameStart
		for nameEnd < len(text) && isTagNameChar(text[nameEnd]) {
			nameEnd++
		}
		tagName := ""
		if nameEnd > nameStart {
			tagName = strings.ToLower(text[nameStart:nameEnd])
		}
		s.tagName = tagName
		if !s.isClose && voidElements[tagName] {
			if j := strings.IndexByte(text[i:], '>'); j >= 0 {
				// Void element closed on the same line — no depth change.
				onTagClose(tagEvent{
					Name:     tagName,
					IsVoid:   true,
					StartIdx: i,
					EndIdx:   i + j,
				})
				i += j
				s.inTag = false
				continue
			}
			// Void element spans beyond text; keep inTag open
			// so attribute lines get extra indentation.
			s.isVoid = true
		}
	}
}

// tagEvent represents a completed HTML tag found during scanning.
type tagEvent struct {
	Name      string
	IsClose   bool
	IsVoid    bool
	SelfClose bool
	StartIdx  int // index of '<' in the scanned text
	EndIdx    int // index of '>' in the scanned text
}

type printer struct {
	*strings.Builder
	prefix      string
	depth       int
	branchDepth int
	htmlDepth   int

	html htmlScanner // HTML tag tracking state

	// Signals set by template actions for cross-node communication.
	htmlTagHadActions bool   // a template action was written while an HTML tag was open
	pendingCloseTag   string // tag name to auto-close after '>'
	isLastInList      bool   // current node is the last in its parent ListNode

	// Branch-scoped formatting flags (saved/restored per branch).
	inOneLiner         bool // inside a one-liner branch (suppress newlines for else/end)
	oneLinerHTMLIndent bool // one-liner was indented as attribute in multi-line HTML tag
	rightTrimPending   bool // right trim: strip leading whitespace from the next text node
}

func newPrinter() *printer {
	return &printer{
		Builder: new(strings.Builder),
	}
}

// totalIndent returns the combined HTML + template nesting depth.
func (p *printer) totalIndent() int {
	n := p.htmlDepth + p.branchDepth
	if n < 0 {
		return 0
	}
	return n
}

func (p *printer) WritePrefix() {
	p.WriteString(p.prefix)
	p.WriteString(indent(p.depth))
}

// writeAction writes a template action: {{ keyword pipe }} or {{ pipe }}.
// It handles multi-line pipes by computing a prefix from the current output
// line and placing the closing delimiter on its own indented line.
// keyword is empty for plain actions (ActionNode) and "if"/"with"/etc. for branches.
func (p *printer) writeAction(keyword string, pipe *PipeNode, tr trim) {
	// Compute prefix for multi-line pipe formatting.
	s := p.String()
	lastNL := strings.LastIndexByte(s, '\n')
	afterNL := s
	if lastNL >= 0 {
		afterNL = s[lastNL+1:]
	}
	onOwnLine := strings.TrimLeft(afterNL, " \t") == ""
	if onOwnLine {
		p.prefix = afterNL
	} else {
		p.prefix = ""
	}

	p.WriteString(tr.leftDelim())
	if keyword != "" {
		p.WriteString(keyword)
	}
	if len(pipe.Cmds) > 0 {
		if keyword != "" {
			p.WriteByte(' ')
		}
		before := strings.Count(p.String(), "\n")
		p.depth = 1
		pipe.writeTo(p)
		p.depth = 0
		cur := p.String()
		after := strings.Count(cur, "\n")
		if before != after && cur[len(cur)-1] != '`' {
			p.WriteString("\n")
			if onOwnLine {
				p.WritePrefix()
			}
			p.WriteString(tr.rightDelimNoSpace())
		} else {
			p.WriteString(tr.rightDelim())
		}
	} else {
		p.WriteString(tr.rightDelim())
	}
}

func (p *printer) writeBranchIndent() {
	if p.totalIndent() == 0 {
		return
	}
	s := p.String()
	if len(s) == 0 || s[len(s)-1] == '\n' {
		p.WriteString(indent(p.totalIndent()))
	}
}

// writeControlIndent is like writeBranchIndent but forces a newline if the
// output doesn't already end with one. Used for template control structures
// (end, else, if, range, etc.) which must always start on their own line.
// When inside an HTML tag's quoted attribute value, newlines and indentation
// are suppressed to keep the attribute on a single line. When inside a
// multi-line HTML tag but outside quotes, template actions are indented
// one level deeper than the tag itself (like attribute continuation lines).
func (p *printer) writeControlIndent() {
	if p.inOneLiner {
		return
	}
	if p.html.inTag {
		if p.html.inAttrValue {
			return
		}
		// Multi-line tag: template control on its own line,
		// indented one level deeper than the tag.
		s := p.String()
		if len(s) > 0 && s[len(s)-1] != '\n' {
			p.WriteByte('\n')
		}
		n := p.totalIndent() + 1
		if n > 0 {
			p.WriteString(indent(n))
		}
		return
	}
	s := p.String()
	if len(s) > 0 && s[len(s)-1] != '\n' {
		p.WriteByte('\n')
	}
	if p.totalIndent() > 0 {
		p.WriteString(indent(p.totalIndent()))
	}
}

// computeHTMLDeltas scans text for HTML tags, updates the printer's tag-tracking
// state (for tags split across TextNodes), and returns depth adjustments:
//   - pre: depth change to apply BEFORE indenting (from leading closing tags)
//   - post: depth change to apply AFTER indenting (from opening tags)
func (p *printer) computeHTMLDeltas(text string) (pre, post int) {
	delta := 0
	minDelta := 0
	p.html.scanText(text, func(tag tagEvent) {
		if tag.SelfClose || tag.IsVoid {
			// no depth change
		} else if tag.IsClose {
			delta--
			if delta < minDelta {
				minDelta = delta
			}
		} else if p.htmlTagHadActions {
			// Structural tag with template actions inside.
			rest := strings.TrimSpace(text[tag.EndIdx+1:])
			closeTag := "</" + tag.Name + ">"
			if rest == "" {
				// No content after >, auto-close for depth balance.
				p.pendingCloseTag = tag.Name
			} else if strings.HasPrefix(strings.ToLower(rest), closeTag) {
				// Close tag already present (idempotent re-format).
			} else {
				// Tag has actual content, treat as normal opening.
				delta++
			}
		} else {
			delta++
		}
		p.htmlTagHadActions = false
	})
	return minDelta, delta - minDelta
}

func isTagNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-'
}

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true,
	"embed": true, "hr": true, "img": true, "input": true,
	"link": true, "meta": true, "param": true, "source": true,
	"track": true, "wbr": true,
}

var rawContentElements = map[string]bool{
	"script": true,
	"style":  true,
}

// liner is implemented by nodes that store their source line number.
type liner interface {
	lineNumber() int
}

func lineno(n Node) int {
	return n.(liner).lineNumber()
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
		sb.isLastInList = i == len(l.Nodes)-1
		n := l.Nodes[i]
		if c, ok := n.(*CommentNode); ok && strings.Contains(c.Text, directiveIgnoreStart) {
			found := false
			for j := i + 1; j < len(l.Nodes); j++ {
				if c2, ok := l.Nodes[j].(*CommentNode); ok && strings.Contains(c2.Text, directiveIgnoreEnd) {
					src := l.tr.text
					// Format the ignore-start comment with proper indentation.
					c.writeTo(sb)
					// Copy raw source between the two directive comments.
					afterStart := int(c.Position()) + len(c.Text)
					ignoreStartEnd := afterStart + strings.Index(src[afterStart:], "}}") + 2
					ignoreEndStart := strings.LastIndex(src[:int(c2.Position())], "{{")
					sb.WriteString(strings.TrimRight(src[ignoreStartEnd:ignoreEndStart], " \t"))
					// Format the ignore-end comment with proper indentation.
					c2.writeTo(sb)
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
	Line int
	Text string // The text; may span newlines.
}

func (t *Tree) newText(pos Pos, text string, line int) *TextNode {
	return &TextNode{tr: t, NodeType: NodeText, Pos: pos, Line: line, Text: text}
}

func (t *TextNode) lineNumber() int {
	return t.Line
}

func (t *TextNode) String() string {
	return fmt.Sprintf(textFormat, t.Text)
}

type rawLineType int

const (
	rawNone    rawLineType = iota
	rawTagLine             // the <script>/<style> or </script>/</style> line itself
	rawContent             // content between the open and close tags
)

type rawContentRange struct {
	openLine  int
	closeLine int
}

// findRawContentRanges finds <script> and <style> blocks where both
// opening and closing tags are in the same TextNode (no template actions).
func findRawContentRanges(lines []string) []rawContentRange {
	var ranges []rawContentRange
	skip := -1
	for i, line := range lines {
		if i <= skip {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		lower := strings.ToLower(trimmed)
		for tag := range rawContentElements {
			if strings.HasPrefix(lower, "<"+tag) && !strings.HasPrefix(lower, "</"+tag) {
				closeTag := "</" + tag + ">"
				for j := i + 1; j < len(lines); j++ {
					if strings.Contains(strings.ToLower(lines[j]), closeTag) {
						ranges = append(ranges, rawContentRange{openLine: i, closeLine: j})
						skip = j
						break
					}
				}
				break
			}
		}
	}
	return ranges
}

func rawLineKind(lineIdx int, ranges []rawContentRange) rawLineType {
	for _, r := range ranges {
		if lineIdx == r.openLine || lineIdx == r.closeLine {
			return rawTagLine
		}
		if lineIdx > r.openLine && lineIdx < r.closeLine {
			return rawContent
		}
	}
	return rawNone
}

func (t *TextNode) writeTo(sb *printer) {
	text := t.Text
	// Right trim: strip leading whitespace from text following a -}} delimiter.
	if sb.rightTrimPending {
		text = strings.TrimLeft(text, " \t\n\r")
		sb.rightTrimPending = false
	}
	lines := strings.Split(text, "\n")

	// Find raw content element ranges (<script>, <style>) where both
	// opening and closing tags are in this TextNode. These have no
	// template actions inside, so their content is preserved verbatim.
	rawRanges := findRawContentRanges(lines)

	for i, line := range lines {
		if i == 0 {
			sb.writeTextFirstLine(line)
			// Handle pending auto-close tag. Only auto-close when this text
			// is the last node in its list (meaning the tag closes right
			// before {{ end }}, with no content after). If more nodes follow,
			// the tag has actual content and a proper closing tag.
			if sb.pendingCloseTag != "" {
				if sb.isLastInList {
					sb.writePendingCloseTag()
				} else {
					sb.htmlDepth++ // Count as normal opening tag.
					sb.pendingCloseTag = ""
				}
			}
		} else {
			sb.writeTextLine(line, rawLineKind(i, rawRanges), i == len(lines)-1)
		}
	}
}

// writeTextFirstLine writes the first line of a TextNode, which continues
// on the same line as the previous node (no newline or indent is added).
func (p *printer) writeTextFirstLine(line string) {
	pre, post := p.computeHTMLDeltas(line)
	p.htmlDepth += pre + post
	if p.htmlDepth < 0 {
		p.htmlDepth = 0
	}
	p.WriteString(line)
	// Note: pendingCloseTag is handled by TextNode.writeTo after this call,
	// where it can check the full source for a proper closing tag.
}

// writeTextLine writes a subsequent (non-first) line of a TextNode.
func (p *printer) writeTextLine(line string, rawKind rawLineType, isLast bool) {
	// Raw content inside <script>/<style>: write verbatim.
	if rawKind == rawContent {
		p.WriteByte('\n')
		p.WriteString(line)
		return
	}

	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		p.WriteByte('\n')
		return
	}

	// Strip trailing whitespace from the last line of the TextNode,
	// which sits between content and the next template action.
	// Preserve a trailing space when it looks like inline content
	// spacing (e.g. "* {{ .Name }}") rather than indentation
	// between tags and actions (e.g. "</ul>\t\t{{ end }}").
	if isLast {
		t := strings.TrimRight(trimmed, " \t")
		if t == "" {
			return
		}
		tail := trimmed[len(t):]
		if strings.ContainsRune(tail, '\t') {
			trimmed = t
		} else if len(tail) > 0 {
			trimmed = t + " "
		}
	}

	// Split the line at tag boundaries where depth drops,
	// so closing tags that end a nesting level get their own line.
	for _, seg := range p.splitHTMLLine(trimmed) {
		p.WriteByte('\n')
		p.writeHTMLSegment(seg, rawKind == rawTagLine)
	}
}

// writeHTMLSegment writes a single segment of an HTML line, computing
// indentation from the HTML tag depth.
func (p *printer) writeHTMLSegment(seg string, suppressDepth bool) {
	wasInTag := p.html.inTag
	pre, post := p.computeHTMLDeltas(seg)
	if suppressDepth {
		pre = 0
		post = 0
	}
	p.htmlDepth += pre
	if p.htmlDepth < 0 {
		p.htmlDepth = 0
	}
	extra := 0
	if wasInTag && len(seg) > 0 && seg[0] != '>' && !strings.HasPrefix(seg, "/>") {
		// Continuation line inside a multiline HTML tag with attribute
		// content — indent one level deeper. Lines that are just the
		// closing bracket (> or />) stay at the tag's indent level.
		extra = 1
	}
	p.WriteString(indent(p.totalIndent() + extra))
	p.WriteString(seg)
	p.writePendingCloseTag()
	p.htmlDepth += post
	if p.htmlDepth < 0 {
		p.htmlDepth = 0
	}
}

// writePendingCloseTag appends an auto-closing tag if one is pending.
func (p *printer) writePendingCloseTag() {
	if p.pendingCloseTag != "" {
		p.WriteString("</" + p.pendingCloseTag + ">")
		p.pendingCloseTag = ""
	}
}

// splitHTMLLine splits a line into segments where a closing tag would decrease
// depth below the line's starting depth. For example, "<div></div></div>" becomes
// ["<div></div>", "</div>"] because the second </div> closes a tag from a previous line.
func (p *printer) splitHTMLLine(line string) []string {
	// Peek ahead with a temporary scanner copy — computeHTMLDeltas will
	// process the actual segments and update the real scanner state.
	tmp := p.html.save()

	depth := 0
	var splits []int
	tmp.scanText(line, func(tag tagEvent) {
		if tag.SelfClose || tag.IsVoid || rawContentElements[tag.Name] {
			return
		}
		if tag.IsClose {
			depth--
		} else {
			depth++
		}
		if depth < 0 {
			// Find the '<' that started this tag within the current line.
			// We can't use tag.StartIdx because it may refer to a previous
			// scanText call if the tag opened on a prior line.
			lt := strings.LastIndex(line[:tag.EndIdx], "<")
			if lt >= 0 && (len(splits) == 0 || lt > splits[len(splits)-1]) {
				splits = append(splits, lt)
			}
			depth = 0
		}
	})

	if len(splits) == 0 {
		return []string{line}
	}
	var segments []string
	start := 0
	for _, pos := range splits {
		if pos > start {
			segments = append(segments, line[start:pos])
		}
		start = pos
	}
	segments = append(segments, line[start:])
	return segments
}

func (t *TextNode) tree() *Tree {
	return t.tr
}

// CommentNode holds a comment.
type CommentNode struct {
	NodeType
	Pos
	tr   *Tree
	Line int
	Text string // Comment text.
	Trim trim   // Trim markers.
}

func (t *Tree) newComment(pos Pos, text string, tr trim, line int) *CommentNode {
	return &CommentNode{tr: t, NodeType: NodeComment, Pos: pos, Line: line, Text: text, Trim: tr}
}

func (c *CommentNode) lineNumber() int {
	return c.Line
}

func (c *CommentNode) String() string {
	sb := newPrinter()
	c.writeTo(sb)
	return sb.String()
}

func (c *CommentNode) writeTo(sb *printer) {
	sb.writeBranchIndent()
	if c.Trim.left {
		sb.WriteString("{{- ")
	} else {
		sb.WriteString("{{")
	}
	sb.WriteString(c.Text)
	if c.Trim.right {
		sb.WriteString(" -}}")
	} else {
		sb.WriteString("}}")
	}
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

func (p *PipeNode) lineNumber() int {
	return p.Line
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

func (a *ActionNode) lineNumber() int {
	return a.Line
}

func (a *ActionNode) String() string {
	sb := newPrinter()
	a.writeTo(sb)
	return sb.String()
}

func (a *ActionNode) writeTo(sb *printer) {
	sb.writeBranchIndent()
	sb.writeAction("", a.Pipe, a.Trim)
}

func (a *ActionNode) tree() *Tree {
	return a.tr
}

// CommandNode holds a command (a pipeline inside an evaluating action).
type CommandNode struct {
	NodeType
	Pos
	tr   *Tree
	Line int
	Args []Node // Arguments in lexical order: Identifier, field, or constant.
	Trim trim
}

func (t *Tree) newCommand(pos Pos, line int) *CommandNode {
	return &CommandNode{tr: t, NodeType: NodeCommand, Pos: pos, Line: line}
}

func (c *CommandNode) lineNumber() int {
	return c.Line
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
		line := lineno(arg)
		if i > 0 {
			if line > prevLine {
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
			arg.writeTo(sb)
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
	Line  int
	Ident string // The identifier's name.
}

// NewIdentifier returns a new IdentifierNode with the given identifier name.
func NewIdentifier(ident string) *IdentifierNode {
	return &IdentifierNode{NodeType: NodeIdentifier, Ident: ident}
}

// SetPos sets the position. NewIdentifier is a public method so we can't modify its signature.
// Chained for convenience.
func (i *IdentifierNode) SetPos(pos Pos) *IdentifierNode {
	i.Pos = pos
	return i
}

// SetTree sets the parent tree for the node. NewIdentifier is a public method so we can't modify its signature.
// Chained for convenience.
func (i *IdentifierNode) SetTree(t *Tree) *IdentifierNode {
	i.tr = t
	return i
}

// SetLine sets the line number. NewIdentifier is a public method so we can't modify its signature.
// Chained for convenience.
func (i *IdentifierNode) SetLine(line int) *IdentifierNode {
	i.Line = line
	return i
}

func (i *IdentifierNode) lineNumber() int {
	return i.Line
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
	Line  int
	Ident []string // Variable name and fields in lexical order.
}

func (t *Tree) newVariable(pos Pos, ident string, line int) *VariableNode {
	return &VariableNode{tr: t, NodeType: NodeVariable, Pos: pos, Line: line, Ident: strings.Split(ident, ".")}
}

func (v *VariableNode) lineNumber() int {
	return v.Line
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
	tr   *Tree
	Line int
}

func (t *Tree) newDot(pos Pos, line int) *DotNode {
	return &DotNode{tr: t, NodeType: NodeDot, Pos: pos, Line: line}
}

func (d *DotNode) lineNumber() int {
	return d.Line
}

func (d *DotNode) Type() NodeType {
	// Override method on embedded NodeType for API compatibility.

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
	tr   *Tree
	Line int
}

func (t *Tree) newNil(pos Pos, line int) *NilNode {
	return &NilNode{tr: t, NodeType: NodeNil, Pos: pos, Line: line}
}

func (n *NilNode) lineNumber() int {
	return n.Line
}

func (n *NilNode) Type() NodeType {
	// Override method on embedded NodeType for API compatibility.
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
	Line  int
	Ident []string // The identifiers in lexical order.
}

func (t *Tree) newField(pos Pos, ident string, line int) *FieldNode {
	return &FieldNode{tr: t, NodeType: NodeField, Pos: pos, Line: line, Ident: strings.Split(ident[1:], ".")} // [1:] to drop leading period
}

func (f *FieldNode) lineNumber() int {
	return f.Line
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
	Line  int
	Node  Node
	Field []string // The identifiers in lexical order.
}

func (t *Tree) newChain(pos Pos, node Node, line int) *ChainNode {
	return &ChainNode{tr: t, NodeType: NodeChain, Pos: pos, Line: line, Node: node}
}

func (c *ChainNode) lineNumber() int {
	return c.Line
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
	Line int
	True bool // The value of the boolean constant.
}

func (t *Tree) newBool(pos Pos, true bool, line int) *BoolNode {
	return &BoolNode{tr: t, NodeType: NodeBool, Pos: pos, Line: line, True: true}
}

func (b *BoolNode) lineNumber() int {
	return b.Line
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
	Line       int
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

func (n *NumberNode) lineNumber() int {
	return n.Line
}

func (t *Tree) newNumber(pos Pos, text string, typ itemType, line int) (*NumberNode, error) {
	n := &NumberNode{tr: t, NodeType: NodeNumber, Pos: pos, Line: line, Text: text}
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
	Line   int
	Quoted string // The original text of the string, with quotes.
	Text   string // The string, after quote processing.
}

func (t *Tree) newString(pos Pos, orig, text string, line int) *StringNode {
	return &StringNode{tr: t, NodeType: NodeString, Pos: pos, Line: line, Quoted: orig, Text: text}
}

func (s *StringNode) lineNumber() int {
	return s.Line
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
	Line int
	Trim trim
}

func (t *Tree) newEnd(pos Pos, trim trim, line int) *EndNode {
	return &EndNode{tr: t, NodeType: nodeEnd, Pos: pos, Line: line, Trim: trim}
}

func (e *EndNode) lineNumber() int {
	return e.Line
}

func (e *EndNode) String() string {
	sb := newPrinter()
	e.writeTo(sb)
	return sb.String()
}

func (e *EndNode) writeTo(sb *printer) {
	sb.writeControlIndent()
	sb.WriteString(e.Trim.leftDelim())
	sb.WriteString("end")
	sb.WriteString(e.Trim.rightDelim())
	if e.Trim.right && sb.html.inTag {
		sb.rightTrimPending = true
	}
}

func (e *EndNode) tree() *Tree {
	return e.tr
}

// ElseNode represents an {{else}}, {{else if}}, or {{else with}} action. Does not appear in the final tree.
type ElseNode struct {
	NodeType
	Pos
	tr      *Tree
	Keyword string    // "if" or "with"; empty for bare {{ else }}
	Pipe    *PipeNode // guard check, may be nil for bare {{ else }}
	List    *ListNode // stuff to execute if pipe holds
	Line    int       // The line number in the input. Deprecated: Kept for compatibility.
	Trim    trim
}

func (t *Tree) newElse(pos Pos, line int, keyword string, pipe *PipeNode, trim trim) *ElseNode {
	return &ElseNode{tr: t, NodeType: nodeElse, Pos: pos, Line: line, Keyword: keyword, Pipe: pipe, Trim: trim}
}

func (e *ElseNode) lineNumber() int {
	return e.Line
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
	inAttrValue := sb.html.inTag && sb.html.inAttrValue
	sb.writeControlIndent()
	sb.WriteString(e.Trim.leftDelim())
	sb.WriteString("else")
	if e.Pipe != nil {
		sb.WriteString(" ")
		sb.WriteString(e.Keyword)
		if len(e.Pipe.Cmds) > 0 {
			sb.WriteByte(' ')
			e.Pipe.writeTo(sb)
		}
	}
	sb.WriteString(e.Trim.rightDelim())
	if sb.html.inTag && !sb.html.inAttrValue && !sb.inOneLiner {
		sb.htmlTagHadActions = true
	}
	savedHTMLDepth := sb.htmlDepth
	if !inAttrValue {
		sb.branchDepth++
	}
	e.List.writeTo(sb)
	if !inAttrValue {
		sb.branchDepth--
	}
	sb.htmlDepth = savedHTMLDepth
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

func (b *BranchNode) lineNumber() int {
	return b.Line
}

func (b *BranchNode) String() string {
	sb := newPrinter()
	b.writeTo(sb)
	return sb.String()
}

func (b *BranchNode) writeTo(sb *printer) {
	inAttrValue := sb.html.inTag && sb.html.inAttrValue

	// Detect one-liner branches (start and end on the same source line).
	savedOneLiner := sb.inOneLiner
	if lineno(b) == lineno(b.End) {
		sb.inOneLiner = true
	}

	if sb.inOneLiner {
		if sb.html.inTag && !sb.html.inAttrValue {
			// One-liner inside HTML tag: indent as attribute if at start of line.
			s := sb.String()
			if len(s) == 0 || s[len(s)-1] == '\n' {
				n := sb.totalIndent() + 1
				if n > 0 {
					sb.WriteString(indent(n))
				}
				sb.oneLinerHTMLIndent = true
			}
		} else {
			// One-liner: add indent if at start of line, but don't force a newline.
			sb.writeBranchIndent()
		}
	} else {
		sb.writeControlIndent()
	}

	sb.writeAction(b.Keyword, b.Pipe, b.Trim)

	if sb.html.inTag && !sb.html.inAttrValue && !sb.inOneLiner {
		sb.htmlTagHadActions = true
	}

	savedHTMLDepth := sb.htmlDepth
	indentBody := !inAttrValue && !sb.inOneLiner && b.Keyword != "define"
	if indentBody {
		sb.branchDepth++
	}
	b.List.writeTo(sb)
	if indentBody {
		sb.branchDepth--
	}
	for _, e := range b.Elses {
		sb.htmlDepth = savedHTMLDepth
		e.writeTo(sb)
	}
	sb.htmlDepth = savedHTMLDepth
	b.End.writeTo(sb)
	sb.inOneLiner = savedOneLiner
	sb.oneLinerHTMLIndent = false
}

func (b *BranchNode) tree() *Tree {
	return b.tr
}
