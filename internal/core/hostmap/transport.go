package hostmap

import (
	"net/http"
)

// WrapTransport returns a RoundTripper that speaks to an overridden host
// the way its override said to: an entry written http://127.0.0.1:3000
// reaches a plain dev server even though the client asked for https, and
// the transport then dials that scheme's default port, which Addr keeps.
//
// The flip is made on a COPY and res.Request is put back, so everything
// above this - the hooks, the log, the intercept - still sees the URL
// the client asked for. What actually went over the wire is in the
// response's own protocol, as it is for everything else.
//
// overrides is read per request rather than held, so an edit takes
// effect on the next one.
func WrapTransport(next http.RoundTripper, overrides func() *Map) http.RoundTripper {
	if overrides == nil {
		return next
	}

	return &schemeSwitcher{next: next, overrides: overrides}
}

type schemeSwitcher struct {
	next      http.RoundTripper
	overrides func() *Map
}

func (s *schemeSwitcher) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		return s.next.RoundTrip(req)
	}

	scheme := s.overrides().Scheme(req.URL.Hostname())
	if scheme == "" || scheme == req.URL.Scheme {
		return s.next.RoundTrip(req)
	}

	// Only the scheme moves. A URL carrying no port now takes the new
	// scheme's default, which is the point: https://host with an
	// http:// override dials :80, and Addr then sends :80 to the
	// entry's address. A URL that named its own port keeps it.
	out := req.Clone(req.Context())
	out.URL.Scheme = scheme

	res, err := s.next.RoundTrip(out)
	if res != nil {
		res.Request = req
	}

	return res, err
}
