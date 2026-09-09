package reqlog_test

import (
	"bytes"
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	models "github.com/yousysadmin/ihttp/internal/models/reqlog"
)

func TestWriteHAR(t *testing.T) {
	when := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	answered := models.Entry{
		ID: "a", CreatedAt: when, Method: "POST", URL: "https://x.test/p?q=1&r=two", Proto: "HTTP/1.1",
		Headers: httpmsg.Headers{{Name: "Cookie", Value: "sid=1"}, {Name: "Content-Type", Value: "application/json"}},
		Body:    httpmsg.Body(`{"a":1}`),
		Response: &models.Response{
			Proto: "HTTP/2.0", StatusCode: 200, Status: "OK", DurationMS: 42,
			Headers: httpmsg.Headers{{Name: "Content-Type", Value: "application/octet-stream"}, {Name: "Set-Cookie", Value: "t=2; Path=/"}},
			Body:    httpmsg.Body{0x00, 0x01, 0x02},
		},
	}
	pending := models.Entry{ID: "b", CreatedAt: when, Method: "GET", URL: "https://x.test/", Proto: "HTTP/1.1", Headers: httpmsg.Headers{}}

	var buf bytes.Buffer

	err := reqlog.WriteHAR(&buf, func(emit func(models.Entry) error) error {
		for _, e := range []models.Entry{answered, pending} {
			if err := emit(e); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Log struct {
			Version string `json:"version"`
			Creator struct {
				Name string `json:"name"`
			} `json:"creator"`
			Entries []struct {
				Time    int64 `json:"time"`
				Request struct {
					Method      string              `json:"method"`
					Cookies     []map[string]string `json:"cookies"`
					QueryString []map[string]string `json:"queryString"`
					PostData    *struct {
						Text string `json:"text"`
					} `json:"postData"`
				} `json:"request"`
				Response struct {
					Status  int `json:"status"`
					Content struct {
						Size     int    `json:"size"`
						Text     string `json:"text"`
						Encoding string `json:"encoding"`
					} `json:"content"`
					Cookies []map[string]string `json:"cookies"`
				} `json:"response"`
				Meta struct {
					ID      string `json:"id"`
					Pending bool   `json:"pending"`
				} `json:"_ihttp"`
			} `json:"entries"`
		} `json:"log"`
	}

	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("%v in %s", err, buf.String())
	}

	if doc.Log.Version != "1.2" || doc.Log.Creator.Name != "ihttp" || len(doc.Log.Entries) != 2 {
		t.Fatalf("envelope: %+v", doc.Log)
	}

	a := doc.Log.Entries[0]
	if a.Time != 42 || a.Meta.ID != "a" || a.Meta.Pending || a.Request.PostData == nil || a.Request.PostData.Text != `{"a":1}` {
		t.Fatalf("answered entry: %+v", a)
	}

	if len(a.Request.Cookies) != 1 || a.Request.Cookies[0]["name"] != "sid" || len(a.Request.QueryString) != 2 || a.Request.QueryString[1]["value"] != "two" {
		t.Fatalf("cookies %v query %v", a.Request.Cookies, a.Request.QueryString)
	}

	if a.Response.Status != 200 || a.Response.Content.Encoding != "base64" || a.Response.Content.Text != "AAEC" || a.Response.Content.Size != 3 || len(a.Response.Cookies) != 1 {
		t.Fatalf("answered response: %+v", a.Response)
	}

	b := doc.Log.Entries[1]
	if b.Response.Status != 0 || !b.Meta.Pending || b.Request.PostData != nil {
		t.Fatalf("pending entry: %+v", b)
	}
}
