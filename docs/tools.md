# Tools

## Sender

**Tools > Sender** is a request editor with a history. Write one from
scratch, or take one from the log with **Send to sender** and edit any
part of it.

- Method, URL, protocol (HTTP/1.0, HTTP/1.1 or HTTP/2.0), ordered
  headers with their casing kept to the wire, and the body.
- **Save** without sending, or **Send** - which saves first when there
  are unsaved changes. An indicator says when there are.
- The answer appears beside the request, read the same way as in the
  [log](request-log.md#reading-an-exchange): pretty JSON, images inline,
  hex for binary.
- **Copy as curl** and **Copy as fetch**, and **Pin as A** for a
  [comparison](#compare).

Every send is kept, and the history is searchable with the
[filter language](filter-language.md) - so `res.statusCode >= 400` over
your own attempts works too.

A send goes out the same way a proxied request does, through the
project's chosen [upstream proxy](proxies.md). A replay that left the
machine by a different route would not be a replay.

A **binary body is shown but not edited**, and stays as it was on save -
the text copy the console holds is lossy and writing it back would
corrupt the bytes.

There are no variables or environments. The placeholder mechanism in the
product is the Automation's, below.

## Automation

**Tools > Automation** repeats one request over a set of values - for
enumeration, fuzzing and rate checks.

A job is a request template with a placeholder, `AUTO` by default, which
may appear in the URL, in a header value or in the body. A payload fills
it:

|             |                                                                                                                                                       |
|-------------|-------------------------------------------------------------------------------------------------------------------------------------------------------|
| **List**    | values one per line, typed in or read from a file on disk. With a separator each line is columns, reached as `$1`, `$2` and so on, and `AUTO` is `$1` |
| **Numbers** | a range with a step                                                                                                                                   |
| **Random**  | strings of a length from a charset: `alnum`, `alpha`, `digits`, `hex` or `printable`                                                                  |
| **Library** | a built-in set of awkward inputs a correct handler should survive: empty and long values, unicode, path and template shapes                           |

A run fires up to 50 requests at once and is bounded by 10000
iterations. A random string is capped at 4096 characters.

**Stop conditions** end a run early: a status code, a response header or
a body match, with **any** or **all** of them required. The result that
tripped it is marked.

Results list the status, the size and the time per value, open into the
full exchange, copy as `curl`, and export as **HAR**.

## Decoder

**Tools > Decoder** transforms text in the browser. Nothing leaves the
page - no request is made, which matters when the text is a token.

|        |                                                                      |
|--------|----------------------------------------------------------------------|
| Base64 | encode, encode URL-safe, decode                                      |
| URL    | encode, decode                                                       |
| HTML   | encode, decode                                                       |
| Hex    | encode, decode                                                       |
| JSON   | pretty, minify                                                       |
| JWT    | decode the header and the payload. **The signature is not verified** |
| Gzip   | compress to base64, decompress from base64                           |
| Hash   | SHA-1, SHA-256, SHA-512                                              |
| Time   | Unix timestamp to a date                                             |

The result can be fed back into the input, so steps chain: base64 decode
then JSON pretty, or gzip decompress then JWT decode.

## Compare

Pin any exchange as **A** - from the log, the sender or an automation
result - then choose **Compare with A** on another.

The two are shown side by side or as a unified diff: the status line and
the headers first, then the bodies, with JSON pretty-printed on both
sides so the diff is about content rather than formatting. Binary bodies
compare by size and SHA-256, since a hex diff of two images tells you
nothing.

The pin is remembered per project for the session, so you can pin one
exchange, go looking for its counterpart, and still have it when you
find it.

Useful pairs:

- the same request before and after a deploy
- a working request beside the failing one, to find the header that
  differs
- a captured request beside your replay of it, to check you reproduced
  it faithfully
