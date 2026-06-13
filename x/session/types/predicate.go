package types

import (
	"fmt"
	"strings"

	"cosmossdk.io/math"
)

// Counterparty-class predicates (SPEC §6.4 rule 5 / §5.1 counterparty_class).
//
// The spec calls these "CEL predicates". Phase 0 ships a deliberately minimal,
// fully deterministic subset rather than a full CEL engine (Decision A / D-043):
// google/cel-go is a heavy consensus dependency with its own determinism
// surface, and the spec's examples ("reputation >= 50") are simple comparisons.
//
// Grammar (case-sensitive, whitespace-insignificant):
//
//	predicate  := <empty>                       // always true
//	            | orExpr
//	orExpr     := andExpr ("||" andExpr)*
//	andExpr    := factor ("&&" factor)*
//	factor     := "(" orExpr ")"
//	            | "reputation" cmpOp decimal
//	            | "has_credential" "(" "schema" ")"
//	cmpOp      := ">=" | "<=" | "==" | "!=" | ">" | "<"
//
// Determinism + safety (Decision A): an EMPTY predicate is always true; any
// non-empty predicate that fails to parse is MALFORMED and fails CLOSED — the
// caller rejects the match. Parsing never panics on any input (length- and
// depth-bounded; the parser fuzz target asserts a deterministic parse-or-error
// outcome on arbitrary bytes). malformed != empty.
const (
	// maxPredicateLen bounds predicate length so adversarial strings cannot
	// blow up parse cost (the bond/validation gate the source anyway).
	maxPredicateLen = 1024
	// maxPredicateDepth bounds parenthesis nesting (no unbounded recursion).
	maxPredicateDepth = 32
)

// PredicateEnv supplies the counterparty's Sigil state to predicate
// evaluation (SPEC §6.4 rule 5: reputation/credentials).
type PredicateEnv interface {
	Reputation() math.LegacyDec
	HasCredential(schema string) bool
}

// Predicate is a parsed, evaluable counterparty-class predicate.
type Predicate struct {
	root predNode
}

// IsEmpty reports whether the predicate imposes no constraint.
func (p Predicate) IsEmpty() bool { _, ok := p.root.(trueNode); return ok }

// Eval evaluates the predicate against a counterparty's state. A nil root
// (only via the zero value) evaluates true; constructed predicates always
// have a root.
func (p Predicate) Eval(env PredicateEnv) bool {
	if p.root == nil {
		return true
	}
	return p.root.eval(env)
}

type predNode interface{ eval(env PredicateEnv) bool }

type trueNode struct{}

func (trueNode) eval(PredicateEnv) bool { return true }

type orNode struct{ l, r predNode }

func (n orNode) eval(env PredicateEnv) bool { return n.l.eval(env) || n.r.eval(env) }

type andNode struct{ l, r predNode }

func (n andNode) eval(env PredicateEnv) bool { return n.l.eval(env) && n.r.eval(env) }

type repNode struct {
	op        string
	threshold math.LegacyDec
}

func (n repNode) eval(env PredicateEnv) bool {
	r := env.Reputation()
	switch n.op {
	case ">=":
		return r.GTE(n.threshold)
	case "<=":
		return r.LTE(n.threshold)
	case ">":
		return r.GT(n.threshold)
	case "<":
		return r.LT(n.threshold)
	case "==":
		return r.Equal(n.threshold)
	case "!=":
		return !r.Equal(n.threshold)
	default:
		return false
	}
}

type credNode struct{ schema string }

func (n credNode) eval(env PredicateEnv) bool { return env.HasCredential(n.schema) }

// ParsePredicate parses a counterparty-class string. An empty string yields an
// always-true predicate; a non-empty unparseable string returns an error
// (the caller fails closed). Never panics (Decision A).
func ParsePredicate(s string) (Predicate, error) {
	if strings.TrimSpace(s) == "" {
		return Predicate{root: trueNode{}}, nil
	}
	if len(s) > maxPredicateLen {
		return Predicate{}, fmt.Errorf("predicate exceeds %d bytes", maxPredicateLen)
	}
	toks, err := tokenize(s)
	if err != nil {
		return Predicate{}, err
	}
	p := &parser{toks: toks}
	node, err := p.parseOr(0)
	if err != nil {
		return Predicate{}, err
	}
	if !p.atEnd() {
		return Predicate{}, fmt.Errorf("unexpected token %q", p.peek().val)
	}
	return Predicate{root: node}, nil
}

// --- tokenizer ---

type tokenKind int

const (
	tkIdent tokenKind = iota
	tkNumber
	tkString
	tkOp // comparison operator
	tkAnd
	tkOr
	tkLParen
	tkRParen
)

type token struct {
	kind tokenKind
	val  string
}

func tokenize(s string) ([]token, error) {
	var toks []token
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		c := runes[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{tkLParen, "("})
			i++
		case c == ')':
			toks = append(toks, token{tkRParen, ")"})
			i++
		case c == '&':
			if i+1 < len(runes) && runes[i+1] == '&' {
				toks = append(toks, token{tkAnd, "&&"})
				i += 2
			} else {
				return nil, fmt.Errorf("unexpected '&' at %d", i)
			}
		case c == '|':
			if i+1 < len(runes) && runes[i+1] == '|' {
				toks = append(toks, token{tkOr, "||"})
				i += 2
			} else {
				return nil, fmt.Errorf("unexpected '|' at %d", i)
			}
		case c == '>' || c == '<' || c == '=' || c == '!':
			op, n, err := scanOp(runes[i:])
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{tkOp, op})
			i += n
		case c == '"':
			str, n, err := scanString(runes[i:])
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{tkString, str})
			i += n
		case isDigit(c):
			num, n := scanWhile(runes[i:], func(r rune) bool { return isDigit(r) || r == '.' })
			toks = append(toks, token{tkNumber, num})
			i += n
		case isIdentStart(c):
			id, n := scanWhile(runes[i:], isIdentChar)
			toks = append(toks, token{tkIdent, id})
			i += n
		default:
			return nil, fmt.Errorf("unexpected character %q at %d", c, i)
		}
	}
	if len(toks) == 0 {
		return nil, fmt.Errorf("empty token stream")
	}
	return toks, nil
}

func scanOp(r []rune) (string, int, error) {
	if len(r) >= 2 {
		two := string(r[:2])
		switch two {
		case ">=", "<=", "==", "!=":
			return two, 2, nil
		}
	}
	switch r[0] {
	case '>':
		return ">", 1, nil
	case '<':
		return "<", 1, nil
	default:
		return "", 0, fmt.Errorf("invalid operator near %q", string(r[0]))
	}
}

func scanString(r []rune) (string, int, error) {
	// r[0] == '"'
	for j := 1; j < len(r); j++ {
		if r[j] == '"' {
			return string(r[1:j]), j + 1, nil
		}
	}
	return "", 0, fmt.Errorf("unterminated string literal")
}

func scanWhile(r []rune, keep func(rune) bool) (string, int) {
	j := 0
	for j < len(r) && keep(r[j]) {
		j++
	}
	return string(r[:j]), j
}

func isDigit(r rune) bool      { return r >= '0' && r <= '9' }
func isIdentStart(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' }
func isIdentChar(r rune) bool  { return isIdentStart(r) || isDigit(r) }

// --- recursive-descent parser ---

type parser struct {
	toks []token
	pos  int
}

func (p *parser) atEnd() bool    { return p.pos >= len(p.toks) }
func (p *parser) peek() token    { return p.toks[p.pos] }
func (p *parser) advance() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) parseOr(depth int) (predNode, error) {
	node, err := p.parseAnd(depth)
	if err != nil {
		return nil, err
	}
	for !p.atEnd() && p.peek().kind == tkOr {
		p.advance()
		rhs, err := p.parseAnd(depth)
		if err != nil {
			return nil, err
		}
		node = orNode{l: node, r: rhs}
	}
	return node, nil
}

func (p *parser) parseAnd(depth int) (predNode, error) {
	node, err := p.parseFactor(depth)
	if err != nil {
		return nil, err
	}
	for !p.atEnd() && p.peek().kind == tkAnd {
		p.advance()
		rhs, err := p.parseFactor(depth)
		if err != nil {
			return nil, err
		}
		node = andNode{l: node, r: rhs}
	}
	return node, nil
}

func (p *parser) parseFactor(depth int) (predNode, error) {
	if depth > maxPredicateDepth {
		return nil, fmt.Errorf("predicate nesting exceeds %d", maxPredicateDepth)
	}
	if p.atEnd() {
		return nil, fmt.Errorf("unexpected end of predicate")
	}
	t := p.peek()
	switch {
	case t.kind == tkLParen:
		p.advance()
		node, err := p.parseOr(depth + 1)
		if err != nil {
			return nil, err
		}
		if p.atEnd() || p.peek().kind != tkRParen {
			return nil, fmt.Errorf("missing ')'")
		}
		p.advance()
		return node, nil
	case t.kind == tkIdent && t.val == "reputation":
		return p.parseReputation()
	case t.kind == tkIdent && t.val == "has_credential":
		return p.parseCredential()
	default:
		return nil, fmt.Errorf("unexpected token %q", t.val)
	}
}

func (p *parser) parseReputation() (predNode, error) {
	p.advance() // reputation
	if p.atEnd() || p.peek().kind != tkOp {
		return nil, fmt.Errorf("reputation: expected comparison operator")
	}
	op := p.advance().val
	if p.atEnd() || p.peek().kind != tkNumber {
		return nil, fmt.Errorf("reputation: expected number")
	}
	numTok := p.advance().val
	dec, err := math.LegacyNewDecFromStr(numTok)
	if err != nil {
		return nil, fmt.Errorf("reputation: invalid number %q", numTok)
	}
	return repNode{op: op, threshold: dec}, nil
}

func (p *parser) parseCredential() (predNode, error) {
	p.advance() // has_credential
	if p.atEnd() || p.peek().kind != tkLParen {
		return nil, fmt.Errorf("has_credential: expected '('")
	}
	p.advance()
	if p.atEnd() || p.peek().kind != tkString {
		return nil, fmt.Errorf("has_credential: expected quoted schema")
	}
	schema := p.advance().val
	if schema == "" {
		return nil, fmt.Errorf("has_credential: empty schema")
	}
	if p.atEnd() || p.peek().kind != tkRParen {
		return nil, fmt.Errorf("has_credential: expected ')'")
	}
	p.advance()
	return credNode{schema: schema}, nil
}
