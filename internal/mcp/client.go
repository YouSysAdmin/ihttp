package mcp

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to a running ihttp's console API.
//
// Over HTTP rather than into the database on purpose: the instance may
// be in a container or on another host, and two processes opening one
// bbolt file is a lock fight, not a feature.
type Client struct {
	base string
	http *http.Client
}

// NewClient builds a client for the console at addr, which may be a
// host:port or a full URL.
func NewClient(addr string) *Client {
	base := strings.TrimSuffix(strings.TrimSpace(addr), "/")
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}

	return &Client{
		base: base,
		http: &http.Client{
			// Never through a proxy: the console is ours, and an
			// HTTP_PROXY in the agent's environment would otherwise
			// send these calls through whatever it names - possibly
			// through the very proxy we are asking about.
			Transport: &http.Transport{Proxy: nil},
			Timeout:   30 * time.Second,
		},
	}
}

// ErrUnreachable is a console that is not answering, which almost always
// means no ihttp is running.
type ErrUnreachable struct {
	Base string
	Err  error
}

func (e *ErrUnreachable) Error() string {
	return fmt.Sprintf("no ihttp is answering at %s - start one with `ihttp serve`, or pass --addr: %v", e.Base, e.Err)
}

func (e *ErrUnreachable) Unwrap() error { return e.Err }

// get reads a JSON document from the API into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

// post sends body, when it is not nil, and reads the answer into out,
// when that is not nil.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, nil, body, out)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.base + "/api" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reader io.Reader

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}

		reader = strings.NewReader(string(data))
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return &ErrUnreachable{Base: c.base, Err: err}
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= 400 {
		return apiError(res)
	}

	if out == nil {
		return nil
	}

	return json.UnmarshalRead(res.Body, out)
}

// raw reads a body endpoint, which serves bytes rather than JSON, up to
// limit. The media type comes back too, since a hex dump and a page read
// differently.
func (c *Client) raw(ctx context.Context, path string, limit int64) (media string, data []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api"+path, nil)
	if err != nil {
		return "", nil, err
	}

	res, err := c.http.Do(req)
	if err != nil {
		return "", nil, &ErrUnreachable{Base: c.base, Err: err}
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= 400 {
		return "", nil, apiError(res)
	}

	data, err = io.ReadAll(io.LimitReader(res.Body, limit))

	return res.Header.Get("Content-Type"), data, err
}

// apiError turns the API's error envelope into the message it carries,
// so an agent reads "no project is open" rather than "status 409".
func apiError(res *http.Response) error {
	var env struct {
		Error string `json:"error"`
	}

	data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	_ = json.Unmarshal(data, &env)

	if env.Error != "" {
		return fmt.Errorf("%s", env.Error)
	}

	return fmt.Errorf("the API answered %s", res.Status)
}

// decodeArgs reads a tool's arguments. Absent arguments are an empty
// object, which every tool here tolerates.
//
// Unknown members are refused rather than ignored: a misspelt argument
// that is silently dropped looks to an agent like a tool that ignored
// it, and it will try again the same way. The message names the member
// and nothing about Go, since the reader is a model reading a tool
// result.
func decodeArgs(args jsontext.Value, out any) error {
	if len(args) == 0 || string(args) == "null" {
		return nil
	}

	err := json.Unmarshal(args, out, json.RejectUnknownMembers(true))
	if err == nil {
		return nil
	}

	var semantic *json.SemanticError
	if errors.As(err, &semantic) && errors.Is(semantic.Err, json.ErrUnknownName) {
		return fmt.Errorf("%s is not an argument this tool takes - the tool's inputSchema lists the ones it does",
			semantic.JSONPointer)
	}

	return fmt.Errorf("the arguments were not understood: %w", err)
}

// maxBody bounds what a body tool reads back, so one call cannot fill an
// agent's context with a video.
const maxBody = 256 << 10
