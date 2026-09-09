# Rules

**Proxy > Rules** is what the proxy does to matching traffic on your
behalf, so a frontend can be worked on without the whole backend
running, or a dependency can be made to fail on purpose.

Rules apply without asking. [Intercept](intercept.md) is the one that
asks.

## Matching

A rule applies when its **method**, its **URL** regular expression and
its **filter** all agree. Each left empty is no condition, so a rule
with none matches every request.

The filter is a query in the same
[language](filter-language.md) as the log, which is what lets a rule say
what a URL pattern cannot:

```
req.header.authorization exists
req.body contains "DROP TABLE"
req.size > 1mb
```

It is decided on the request alone - a rule runs before the response
exists - so `res.*` and `ws.*` keys are refused when the rule is saved,
naming the field at fault.

Rules run **in order** and **every** matching rule applies. A rule that
answers instead of the upstream does not cut the chain: a header set by
a later rule is still on the logged request, and a delay still holds the
answer back. The first such answer wins.

Switch a rule off to keep it without applying it.

## What a rule can do

### Answer instead of the backend

|                      |                                                                                            |
|----------------------|--------------------------------------------------------------------------------------------|
| **Mock response**    | a status, headers and a body of your own. The backend is not called                        |
| **Serve local file** | a file from disk, content type from the extension                                          |
| **Block**            | a status instead of the backend, 502 when not set, with a line naming the rule that did it |

Mocked and blocked exchanges are logged like real ones and can be
intercepted like real ones - so what you arranged is visible rather than
invisible.

### Change what goes out

|                                  |                                                                                                |
|----------------------------------|------------------------------------------------------------------------------------------------|
| **Rewrite URL**                  | a regular expression over the URL with `$1`-style groups. Point a production host at localhost |
| **Set / remove request headers** | seen by the backend and in the log, not in the browser                                         |
| **Replace in request body**      | ordered regular expressions with groups                                                        |

### Change what comes back

|                                   |                                                                 |
|-----------------------------------|-----------------------------------------------------------------|
| **Set / remove response headers** |                                                                 |
| **Set response status**           | make a working endpoint answer 500, or a failing one answer 200 |
| **Replace in response body**      | ordered regular expressions with groups                         |
| **Allow CORS**                    | answers preflights and adds permissive CORS headers             |

### Carry a value from one exchange to the next

|                     |                                                           |
|---------------------|-----------------------------------------------------------|
| **Capture a value** | read a field of the exchange and remember it under a name |

Every other action writes the text you typed. This one writes what the
traffic said, and it is the only way to carry something out of one
response into later requests.

A capture reads **one field of the [filter
language](filter-language.md)** - `res.body`,
`res.header.set-cookie`, `req.query.id` - so there is one vocabulary
for naming a part of an exchange. An optional regular expression
narrows the value: its **first group** when it has one, the whole match
when it does not, the field as it stands when there is no pattern. The
name may hold letters, digits and underscores.

Then `${name}` in any action that writes text - a header value, a URL
replacement, a mock body, a body replacement - is replaced with what was
captured. **Captured values are shown on the Rules page** while the
project is open, with what read them and when, and **Forget them**
clears the lot for when a token has expired.

Some placeholders need no capture at all:

|                   |                                     |
|-------------------|-------------------------------------|
| `${uuid}`         | a fresh UUID per request            |
| `${timestamp}`    | Unix seconds                        |
| `${timestamp_ms}` | Unix milliseconds                   |
| `${isotime}`      | RFC 3339, UTC                       |
| `${random}`       | sixteen hex characters, for a nonce |

Three things worth knowing:

- **A name with no value is left as it is.** A header that quietly
  became empty costs an afternoon; a literal `${token}` arriving at the
  server is obvious the first time you look at the log.
- **Captured values are never stored.** They live while the project is
  open and go when it closes - a token has no business in a settings
  document that an export carries to somebody else.
- A capture from a **streamed** response body finds nothing: the body
  went to the client without being captured. Read a header instead.

### Change the timing

|                       |                                                                                        |
|-----------------------|----------------------------------------------------------------------------------------|
| **Delay request**     | wait before the request leaves - a slow backend                                        |
| **Delay response**    | wait before the answer reaches the client - a slow link. The backend is called at once |
| **Throttle response** | pace the answer at so many bytes a second                                              |

Throttle wraps what the client actually receives rather than waiting
before it, which is why it works on a **streamed** body too: an event
stream can be made to trickle. A delay cannot do that, because by then
the frames are already going out.

## What a rule cannot do to a streamed response

`Set response status` and `Replace in response body` are skipped on a
streamed response, with a debug line saying so. The body is on its way
to the client and re-framing it is not possible. Header rules and
throttle still apply.

A truncated body is not replaced either - the prefix is all the rule
would see, and editing that would corrupt the rest.

## Worked examples

**Sign in once, then work.** Two rules: one reads the token out of the
sign-in response, the other puts it on every API request.

|                 | Capture                   | Authorize                        |
|-----------------|---------------------------|----------------------------------|
| URL matches     | `/signin$`                | `/api/`                          |
| Action          | Capture a value           | Set request headers              |
| Read this field | `res.body`                |                                  |
| Narrow it       | `"token"\s*:\s*"([^"]+)"` |                                  |
| Remember it as  | `token`                   |                                  |
| Header          |                           | `Authorization: Bearer ${token}` |

Sign in with the client under test - or from the [Sender](tools.md) -
and every request to `/api/` after it carries the token. Before the
sign-in the header reads `Bearer ${token}` literally, which is how you
can tell the capture has not happened yet.

A session cookie is the same shape with `res.header.set-cookie` and
`session=([^;]+)`.

**Work on a frontend with no backend.** Mock, matching the API path:

```
URL matches:  ^https://api\.example\.com/v1/me
Action:       Mock response, 200, {"name":"Ada","plan":"pro"}
```

**Point one endpoint at your laptop.**

```
URL matches:  ^https://api\.example\.com/(v1/.*)$
Action:       Rewrite URL -> http://localhost:3000/$1
```

**Make analytics fail, without the noise.**

```
URL matches:  /analytics|/collect|telemetry
Action:       Block
```

**See what your app does on a slow connection.**

```
Action:       Throttle response, 50000 bytes a second
```

**Test the retry path of one authenticated write.**

```
Method:       POST
Filter:       req.header.authorization exists
Action:       Set response status, 503
```
