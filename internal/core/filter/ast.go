// Package filter is the search language the console offers over request
// logs, sender history and the intercept queue.
//
// A query is free text, comparisons, or both:
//
//	login                             any field contains "login"
//	req.method = POST                 a field equals a value
//	req.body contains password        a field contains a word, any case
//	req.method in (POST, PUT)         one of several values
//	req.header.authorization exists   the field is present
//	res.statusCode >= 400             numbers compare as numbers
//	res.size > 1mb  res.duration > 2s units for sizes and times
//	req.url =~ "/api/v[0-9]+"         a regular expression
//	req.headers =~ "^Cookie:"         every header as "Name: value"
//	NOT req.url =~ "\.png$" AND res.statusCode != 304
//
// Two adjacent terms are ANDed. Parentheses group. Which keys exist is
// decided by the Subject being matched, not here - the language is the
// same over a stored log entry and a live request in the proxy.
//
// The expression is kept as SOURCE in a project's settings and reparsed
// on load: a compiled tree is a snapshot of one version of this package,
// and a string is not.
package filter

import (
	"regexp"
	"strconv"
	"strings"
)

// Expr is a parsed query. String renders it back as a query that parses
// to the same tree, which is how a test proves the parser's shape.
type Expr interface {
	String() string
}

// Not negates its operand.
type Not struct {
	X Expr
}

// String implements Expr.
func (n Not) String() string {
	return "(NOT " + n.X.String() + ")"
}

// Logical is AND or OR over two operands.
type Logical struct {
	Op   Op
	L, R Expr
}

// String implements Expr.
func (l Logical) String() string {
	return "(" + l.L.String() + " " + l.Op.String() + " " + l.R.String() + ")"
}

// Compare tests one field against a value. Re is set for the two regexp
// operators, Values for OpIn, and neither for OpExists.
type Compare struct {
	Op     Op
	Key    string
	Value  string
	Values []string
	Re     *regexp.Regexp
}

// String implements Expr.
func (c Compare) String() string {
	switch c.Op {
	case OpExists:
		return "(" + c.Key + " exists)"
	case OpIn:
		quoted := make([]string, len(c.Values))
		for i, v := range c.Values {
			quoted[i] = strconv.Quote(v)
		}

		return "(" + c.Key + " in (" + strings.Join(quoted, ", ") + "))"
	default:
		return "(" + c.Key + " " + c.Op.String() + " " + strconv.Quote(c.Value) + ")"
	}
}

// Text is a bare word or quoted string with no operator: a substring
// search over everything the subject exposes, case-insensitively.
type Text struct {
	Value string
}

// String implements Expr.
func (t Text) String() string {
	return strconv.Quote(t.Value)
}

// Op is an operator.
type Op int

// The operators. The comparison ones are what Compare carries, the
// logical ones what Logical carries.
const (
	OpEq Op = iota
	OpNotEq
	OpGt
	OpLt
	OpGtEq
	OpLtEq
	OpRe
	OpNotRe
	OpContains
	OpIn
	OpExists
	OpAnd
	OpOr
	OpNot
)

var opStrings = map[Op]string{
	OpEq: "=", OpNotEq: "!=", OpGt: ">", OpLt: "<", OpGtEq: ">=", OpLtEq: "<=",
	OpRe: "=~", OpNotRe: "!~", OpContains: "contains", OpIn: "in", OpExists: "exists",
	OpAnd: "AND", OpOr: "OR", OpNot: "NOT",
}

// String renders the operator as it is written in a query.
func (o Op) String() string {
	if s, ok := opStrings[o]; ok {
		return s
	}

	return "?"
}

// IsRegexp reports whether the operator takes a regular expression.
func (o Op) IsRegexp() bool {
	return o == OpRe || o == OpNotRe
}

// Keys lists the field names a Compare in e refers to, once each. The
// console uses it to warn about a key the subject does not know.
func Keys(e Expr) []string {
	var out []string
	seen := map[string]bool{}

	var walk func(Expr)
	walk = func(e Expr) {
		switch v := e.(type) {
		case Not:
			walk(v.X)
		case Logical:
			walk(v.L)
			walk(v.R)
		case Compare:
			if !seen[v.Key] {
				seen[v.Key] = true
				out = append(out, v.Key)
			}
		}
	}

	if e != nil {
		walk(e)
	}

	return out
}

// Normalize lower-cases a key for comparison, so `req.URL` and `req.url`
// name the same field.
func Normalize(key string) string {
	return strings.ToLower(key)
}
