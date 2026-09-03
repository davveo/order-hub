package httpx

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDecodeEnvelopeAndRaw(t *testing.T) {
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"freeze_id":"fz_1"}}`))}
	var out map[string]string
	if err := Decode(resp, &out); err != nil {
		t.Fatal(err)
	}
	if out["freeze_id"] != "fz_1" {
		t.Fatalf("%v", out)
	}

	resp = &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"freeze_id":"fz_2"}`))}
	out = map[string]string{}
	if err := Decode(resp, &out); err != nil {
		t.Fatal(err)
	}
	if out["freeze_id"] != "fz_2" {
		t.Fatalf("%v", out)
	}
}
