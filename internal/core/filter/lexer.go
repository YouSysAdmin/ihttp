package filter

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokString
	tokParenOpen
	tokParenClose
	tokComma
	tokOp
)

type token struct {
	kind tokenKind
	text string
	op   Op
	pos  int
}

// lex turns input into tokens in one pass. A slice rather than a
// channel: a query is never long enough to need streaming.
func lex(input string) ([]token, error) {
	var toks []token
	pos := 0

	emit := func(kind tokenKind, text string, op Op, at int) {
		toks = append(toks, token{kind: kind, text: text, op: op, pos: at})
	}

	for pos < len(input) {
		r, width := utf8.DecodeRuneInString(input[pos:])

		switch {
		case unicode.IsSpace(r):
			pos += width
		case r == '(':
			emit(tokParenOpen, "(", 0, pos)
			pos += width
		case r == ')':
			emit(tokParenClose, ")", 0, pos)
			pos += width
		case r == ',':
			emit(tokComma, ",", 0, pos)
			pos += width
		case r == '"':
			end := strings.IndexByte(input[pos+1:], '"')
			if end < 0 {
				return nil, fmt.Errorf("%w: unclosed quote at %d", ErrInvalid, pos)
			}

			emit(tokString, input[pos+1:pos+1+end], 0, pos)
			pos += end + 2
		case r == '=' || r == '!' || r == '<' || r == '>':
			op, n, err := lexOperator(input[pos:])
			if err != nil {
				return nil, fmt.Errorf("%w: %w at %d", ErrInvalid, err, pos)
			}

			emit(tokOp, input[pos:pos+n], op, pos)
			pos += n
		default:
			start := pos
			for pos < len(input) {
				r, width := utf8.DecodeRuneInString(input[pos:])
				if unicode.IsSpace(r) || strings.ContainsRune(`(),="!<>`, r) {
					break
				}

				pos += width
			}

			word := input[start:pos]
			switch strings.ToUpper(word) {
			case "AND":
				emit(tokOp, word, OpAnd, start)
			case "OR":
				emit(tokOp, word, OpOr, start)
			case "NOT":
				emit(tokOp, word, OpNot, start)
			case "CONTAINS":
				emit(tokOp, word, OpContains, start)
			case "IN":
				emit(tokOp, word, OpIn, start)
			case "EXISTS":
				emit(tokOp, word, OpExists, start)
			default:
				emit(tokString, word, 0, start)
			}
		}
	}

	emit(tokEOF, "", 0, pos)

	return toks, nil
}

// lexOperator reads the one or two character operator at the head of s.
func lexOperator(s string) (Op, int, error) {
	two := ""
	if len(s) >= 2 {
		two = s[:2]
	}

	switch two {
	case "=~":
		return OpRe, 2, nil
	case "!~":
		return OpNotRe, 2, nil
	case "!=":
		return OpNotEq, 2, nil
	case ">=":
		return OpGtEq, 2, nil
	case "<=":
		return OpLtEq, 2, nil
	}

	switch s[0] {
	case '=':
		return OpEq, 1, nil
	case '>':
		return OpGt, 1, nil
	case '<':
		return OpLt, 1, nil
	}

	return 0, 0, fmt.Errorf("invalid operator %q", s[:1])
}
