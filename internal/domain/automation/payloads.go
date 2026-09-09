package automation

import (
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/yousysadmin/ihttp/internal/models/automation"
)

// Limits on a run, so a wide range or a large count cannot fire forever.
const (
	MaxIterations  = 10000
	MaxConcurrency = 50
	MaxRandomLen   = 4096

	// MaxLine bounds one line of a list file, so a file that is not a
	// list at all is refused instead of read into memory.
	MaxLine = 1 << 20
)

// charsets a random payload can draw from, keyed by the name the console
// offers.
var charsets = map[string]string{
	"alnum":     "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789",
	"alpha":     "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz",
	"digits":    "0123456789",
	"hex":       "0123456789abcdef",
	"printable": " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~",
}

// Library is the built-in set of awkward inputs, for finding where an
// application mishandles a shape of value it did not expect. These are
// edge cases a correct handler should survive, not exploits.
var Library = []string{
	"",
	" ",
	"   ",
	"\t",
	"\n",
	"\r\n",
	"0",
	"-1",
	"1.5",
	"1e10",
	"99999999999999999999999999",
	"true",
	"false",
	"null",
	"undefined",
	"NaN",
	"Infinity",
	strings.Repeat("A", 1024),
	strings.Repeat("A", 65536),
	"a b c",
	"café",
	"日本語",
	"😀🔥",
	"\u202eabc",
	"line1\nline2",
	"\"quoted\"",
	"'quoted'",
	"back\\slash",
	"semi;colon",
	"comma,separated",
	"a&b=c",
	"a=b&c=d",
	"<tag>",
	"{\"key\":\"value\"}",
	"[1,2,3]",
	"%00",
	"%20%20",
	"a%2fb",
	"../..",
	"${var}",
	"{{tpl}}",
	"  trimmed  ",
}

// Row is one iteration's values: the columns of a list line, or a single
// value for every other kind. Raw is the line as typed, for display.
type Row struct {
	Raw    string
	Values []string
}

// Count reports how many iterations a payload expands to. Save checks it,
// so a list file that does not exist is refused when it is typed.
func Count(p automation.Payload) (int, error) {
	switch p.Kind {
	case automation.PayloadList:
		lines, err := listLines(p)
		if err != nil {
			return 0, err
		}

		return boundedLen(len(lines))
	case automation.PayloadNumbers:
		n, err := numberCount(p)
		if err != nil {
			return 0, err
		}

		return boundedLen(n)
	case automation.PayloadRandom:
		if p.Count <= 0 {
			return 0, fmt.Errorf("random needs a positive count")
		}

		return boundedLen(p.Count)
	case automation.PayloadLibrary:
		return len(Library), nil
	default:
		return 0, fmt.Errorf("unknown payload kind %q", p.Kind)
	}
}

// Expand produces the rows to try, bounded by MaxIterations. Count has
// already refused what does not fit.
func Expand(p automation.Payload) ([]Row, error) {
	if _, err := Count(p); err != nil {
		return nil, err
	}

	switch p.Kind {
	case automation.PayloadList:
		lines, err := listLines(p)
		if err != nil {
			return nil, err
		}

		out := make([]Row, 0, len(lines))
		for _, l := range lines {
			out = append(out, Row{Raw: l, Values: splitRow(l, p.Separator)})
		}

		return out, nil
	case automation.PayloadNumbers:
		return expandNumbers(p)
	case automation.PayloadRandom:
		return expandRandom(p)
	case automation.PayloadLibrary:
		return single(Library), nil
	default:
		return nil, fmt.Errorf("unknown payload kind %q", p.Kind)
	}
}

// listLines is the lines of a list payload: the file when one is named,
// else what was typed. Trailing blank lines are dropped. A blank line in
// the middle is a value a person meant to try.
func listLines(p automation.Payload) ([]string, error) {
	if strings.TrimSpace(p.File) != "" {
		return readLines(p.File)
	}

	return trimTrailingBlank(slices.Clone(p.List)), nil
}

// readLines reads a list file. A leading ~ is the home directory. A
// relative path resolves against the server's working directory, so the
// console asks for an absolute one.
func readLines(path string) ([]string, error) {
	path = strings.TrimSpace(path)

	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("list file: %w", err)
		}

		path = filepath.Join(home, rest)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("list file: %w", err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), MaxLine)

	var lines []string
	for sc.Scan() {
		if len(lines) > MaxIterations {
			return nil, fmt.Errorf("list file: more than %d lines", MaxIterations)
		}

		lines = append(lines, strings.TrimSuffix(sc.Text(), "\r"))
	}

	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("list file: a line is longer than %d bytes", MaxLine)
		}

		return nil, fmt.Errorf("list file: %w", err)
	}

	return trimTrailingBlank(lines), nil
}

// splitRow cuts a line into its columns. No separator means the whole
// line is the one value. Columns are taken as they are, spaces included.
func splitRow(line, sep string) []string {
	if sep == "" {
		return []string{line}
	}

	return strings.Split(line, sep)
}

func single(values []string) []Row {
	out := make([]Row, 0, len(values))
	for _, v := range values {
		out = append(out, Row{Raw: v, Values: []string{v}})
	}

	return out
}

func expandNumbers(p automation.Payload) ([]Row, error) {
	n, err := numberCount(p)
	if err != nil {
		return nil, err
	}

	if n > MaxIterations {
		return nil, fmt.Errorf("the range is more than %d values", MaxIterations)
	}

	step := p.Step
	if step == 0 {
		step = 1
	}

	// Counted, not walked to To: a range that ends at the edge of int
	// would wrap the counter and never get there.
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, strconv.Itoa(p.From+i*step))
	}

	return single(out), nil
}

func numberCount(p automation.Payload) (int, error) {
	step := p.Step
	if step == 0 {
		step = 1
	}

	if step > 0 && p.To < p.From {
		return 0, fmt.Errorf("to is below from but the step is positive")
	}

	if step < 0 && p.To > p.From {
		return 0, fmt.Errorf("to is above from but the step is negative")
	}

	span := p.To - p.From
	if span < 0 {
		span = -span
	}

	abs := step
	if abs < 0 {
		abs = -abs
	}

	return span/abs + 1, nil
}

func expandRandom(p automation.Payload) ([]Row, error) {
	length := p.Length
	if length <= 0 {
		length = 16
	}

	if length > MaxRandomLen {
		return nil, fmt.Errorf("random length is more than %d", MaxRandomLen)
	}

	set := charsets[p.Charset]
	if set == "" {
		set = charsets["alnum"]
	}

	out := make([]string, 0, p.Count)
	for range p.Count {
		s, err := randomString(set, length)
		if err != nil {
			return nil, err
		}

		out = append(out, s)
	}

	return single(out), nil
}

func randomString(set string, length int) (string, error) {
	var b strings.Builder
	b.Grow(length)

	limit := big.NewInt(int64(len(set)))
	for range length {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}

		b.WriteByte(set[n.Int64()])
	}

	return b.String(), nil
}

func trimTrailingBlank(list []string) []string {
	end := len(list)
	for end > 0 && list[end-1] == "" {
		end--
	}

	return list[:end]
}

func boundedLen(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("no values to try")
	}

	if n > MaxIterations {
		return 0, fmt.Errorf("more than %d values", MaxIterations)
	}

	return n, nil
}
