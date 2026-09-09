// Package upstream is the stored shape of the proxies this instance can
// go out through.
package upstream

import "time"

// Server is one upstream proxy the operator has named.
//
// The URL is stored as it was typed, credentials and all. This is a
// local tool: the database already holds every captured request, so a
// password beside them changes nothing about who can read what. What
// does matter is not putting it in front of people who did not ask, so
// the list and every picker carry Name and Host, and the full URL is
// only ever handed to the editor.
type Server struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// URL is the proxy, with a scheme: http://, https://, socks5://.
	// Credentials may be in it as userinfo.
	URL string `json:"url"`

	// Bypass are host globs reached directly, on top of localhost.
	Bypass []string `json:"bypass,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Summary is a list row. It carries no credential at all - not even a
// masked one - because Host is the URL with any userinfo removed. A
// list, a project picker and a log line all read this.
type Summary struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Host   string   `json:"host"`
	Bypass []string `json:"bypass,omitempty"`

	// HasCredentials says the stored URL carries a user name, so the
	// editor can say so without showing it.
	HasCredentials bool `json:"has_credentials,omitzero"`
}

// MaxServers bounds the list, so a picker stays a picker.
const MaxServers = 50

// MaxNameLen bounds a name.
const MaxNameLen = 60
