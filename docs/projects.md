# Projects

Everything belongs to a project: the request log, the WebSocket
messages, the sender history, the automation jobs and their results, the
gRPC schemas, and the settings that decide what the proxy does while it
is open - scope, rules, intercept switches and filters, views,
retention, and which [proxy](proxies.md) it goes out through.

**With no project open the proxy forwards without logging.** That is not
an error state, it is the honest default: a log has to belong to
something.

Only one project is open at a time. Which one is remembered in the
database and reopened at start, so a restart puts you back where you
were.

## Managing them

**Workspace > Projects** creates, opens, closes and deletes them, and
shows which upstream proxy each goes out through.

Creating and opening are **two steps**. Creating a project while another
is open must not silently switch what is being recorded.

The open project cannot be deleted - close it first.

## Retention

By default a project's log grows until you clear it. **Keep at most**, in
the log's [Filter options](request-log.md#filter-options), caps it: past
the number the oldest entries go as new ones arrive.

- **Saved entries are never dropped.** They count towards the cap but
  are not evicted, which is what saving one is for.
- An evicted entry takes its WebSocket messages with it.
- The count is checked every so often rather than on every request, so
  the log sits a little over the number between passes.

A cap below the number of saved entries simply stops evicting rather
than deleting what was kept on purpose.

## Where the bytes are

The database is `ihttp.db` in the data directory, and **a body of a
megabyte or more is kept in a file beside it**, under `bodies/<project
id>/`, rather than in it. Fifteen megabytes of captured responses leave
the database at a couple of hundred kilobytes instead of twenty
megabytes that never shrink again - bbolt does not give space back.

Nothing about using the tool changes: the log, the filter language, the
reader and every export see whole bodies, because the store reads the
file back on every read. `--body-blob-mb` moves the threshold, and `0`
keeps every body in the database as before.

The files are removed with the entries that point at them - a delete, a
Clear, a retention pass, a deleted project. A body file left behind by
a crash between the two writes is removed at the next start, which
reports how many went.

## Export and import

**Export** offers two files.

**Everything** is one JSON file with the settings, the log, the
WebSocket messages, the sender history, the automation jobs with their
results and the gRPC schemas. It is the whole project, for moving to
another machine or keeping as evidence.

**Settings only** is the same file with the settings and no traffic - for
handing a team a set of rules, a scope and the filters without handing
over anybody's captured exchanges. It imports through the same path.

Importing always **makes a new project** and never touches the open one.
A name that is taken gets a suffix.

Two things worth knowing before you share a file:

- **Filter text can itself be telling.** A view called "the checkout
  bug" with a query naming an internal host says more than you may
  intend. Read the file before sending it.
- A project's chosen upstream proxy travels as a **reference**, never a
  URL, so none of your credentials go with it. A reference that means
  nothing on the other machine falls back to that machine's default.

For traffic that another tool has to read, use
[HAR export](request-log.md#har) instead - the project file is ours
alone.
