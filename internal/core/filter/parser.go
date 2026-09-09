package filter

import (
	"errors"
	"fmt"
	"regexp"
)

// Precedence, lowest first. Two adjacent terms bind like AND, so an
// implicit AND sits at the AND level.
type precedence int

const (
	precLowest precedence = iota
	precOr
	precAnd
	precCompare
	precNot
)

func (o Op) precedence() precedence {
	switch o {
	case OpOr:
		return precOr
	case OpAnd:
		return precAnd
	case OpNot:
		return precNot
	default:
		return precCompare
	}
}

type parser struct {
	toks []token
	pos  int
}

// Parse compiles a query. The empty query is an error - a caller that
// wants "no filter" checks for the empty string before calling.
func Parse(input string) (Expr, error) {
	toks, err := lex(input)
	if err != nil {
		return nil, err
	}

	p := &parser{toks: toks}
	if p.cur().kind == tokEOF {
		return nil, fmt.Errorf("%w: empty query", ErrInvalid)
	}

	expr, err := p.parseExpr(precLowest)
	if err != nil {
		return nil, err
	}

	if p.cur().kind != tokEOF {
		return nil, fmt.Errorf("%w: unexpected %q at %d", ErrInvalid, p.cur().text, p.cur().pos)
	}

	return expr, nil
}

// ErrInvalid marks an expression that does not parse or names a key
// the subject does not have. Every error the package returns wraps it.
var ErrInvalid = errors.New("filter")

func (p *parser) cur() token {
	return p.toks[p.pos]
}

func (p *parser) advance() token {
	t := p.toks[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}

	return t
}

// parseExpr is a Pratt loop. After a prefix term it keeps folding
// operators of higher precedence than the caller's, and treats a term
// following a term as an implicit AND.
func (p *parser) parseExpr(floor precedence) (Expr, error) {
	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}

	for {
		t := p.cur()

		switch {
		case t.kind == tokOp && (t.op == OpAnd || t.op == OpOr):
			if t.op.precedence() <= floor {
				return left, nil
			}

			p.advance()

			right, err := p.parseExpr(t.op.precedence())
			if err != nil {
				return nil, err
			}

			left = Logical{Op: t.op, L: left, R: right}
		case t.kind == tokString || t.kind == tokParenOpen || (t.kind == tokOp && t.op == OpNot):
			// A term directly after a term: implicit AND.
			if precAnd <= floor {
				return left, nil
			}

			right, err := p.parseExpr(precAnd)
			if err != nil {
				return nil, err
			}

			left = Logical{Op: OpAnd, L: left, R: right}
		default:
			return left, nil
		}
	}
}

func (p *parser) parsePrefix() (Expr, error) {
	t := p.advance()

	switch {
	case t.kind == tokParenOpen:
		inner, err := p.parseExpr(precLowest)
		if err != nil {
			return nil, err
		}

		if p.cur().kind != tokParenClose {
			return nil, fmt.Errorf("%w: unmatched parenthesis at %d", ErrInvalid, t.pos)
		}

		p.advance()

		return inner, nil
	case t.kind == tokOp && t.op == OpNot:
		x, err := p.parseExpr(precNot)
		if err != nil {
			return nil, err
		}

		return Not{X: x}, nil
	case t.kind == tokString:
		return p.parseTerm(t)
	case t.kind == tokEOF:
		return nil, fmt.Errorf("%w: unexpected end of query", ErrInvalid)
	default:
		return nil, fmt.Errorf("%w: unexpected %q at %d", ErrInvalid, t.text, t.pos)
	}
}

// parseTerm takes a string token and, when a comparison operator
// follows, the operator and its right-hand side: a value, a list for
// `in`, nothing for `exists`.
func (p *parser) parseTerm(key token) (Expr, error) {
	next := p.cur()
	if next.kind != tokOp || next.op == OpAnd || next.op == OpOr || next.op == OpNot {
		return Text{Value: key.text}, nil
	}

	p.advance()

	switch next.op {
	case OpExists:
		return Compare{Op: OpExists, Key: Normalize(key.text)}, nil
	case OpIn:
		values, err := p.parseList()
		if err != nil {
			return nil, err
		}

		return Compare{Op: OpIn, Key: Normalize(key.text), Values: values}, nil
	}

	val := p.advance()
	if val.kind != tokString {
		return nil, fmt.Errorf("%w: %s needs a value at %d", ErrInvalid, next.text, next.pos)
	}

	c := Compare{Op: next.op, Key: Normalize(key.text), Value: val.text}

	if next.op.IsRegexp() {
		re, err := regexp.Compile(val.text)
		if err != nil {
			return nil, fmt.Errorf("%w: bad regular expression %q: %w", ErrInvalid, val.text, err)
		}

		c.Re = re
	}

	return c, nil
}

// parseList reads `(a, b, c)` after `in`. Commas are optional: `(a b)`
// reads the same, since a bare space already separates two words.
func (p *parser) parseList() ([]string, error) {
	open := p.advance()
	if open.kind != tokParenOpen {
		return nil, fmt.Errorf("%w: in needs a parenthesised list at %d", ErrInvalid, open.pos)
	}

	var values []string

	for {
		t := p.advance()

		switch t.kind {
		case tokString:
			values = append(values, t.text)
		case tokComma:
		case tokParenClose:
			if len(values) == 0 {
				return nil, fmt.Errorf("%w: empty list at %d", ErrInvalid, t.pos)
			}

			return values, nil
		case tokEOF:
			return nil, fmt.Errorf("%w: unclosed list", ErrInvalid)
		default:
			return nil, fmt.Errorf("%w: unexpected %q in list at %d", ErrInvalid, t.text, t.pos)
		}
	}
}
