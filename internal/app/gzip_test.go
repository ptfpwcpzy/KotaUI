package app

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"testing"
)

func readGzipBody(t *testing.T, body []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer reader.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	return raw
}

func TestWriteStaticBodyGzip(t *testing.T) {
	payload := []byte("<html><body>hello panel</body></html>")

	// 接受 gzip：压过、能解回原文。
	req := httptest.NewRequest("GET", "/ptf", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	rec := httptest.NewRecorder()
	writeStaticBody(rec, req, payload, "text/html; charset=utf-8")
	if rec.Header().Get("content-encoding") != "gzip" {
		t.Fatalf("expected gzip content-encoding, got %q", rec.Header().Get("content-encoding"))
	}
	if !bytes.Equal(readGzipBody(t, rec.Body.Bytes()), payload) {
		t.Fatalf("gunzipped body differs from payload")
	}

	// 不接受 gzip：原样输出。
	req2 := httptest.NewRequest("GET", "/ptf", nil)
	rec2 := httptest.NewRecorder()
	writeStaticBody(rec2, req2, payload, "text/html; charset=utf-8")
	if rec2.Header().Get("content-encoding") != "" {
		t.Fatalf("expected no content-encoding, got %q", rec2.Header().Get("content-encoding"))
	}
	if !bytes.Equal(rec2.Body.Bytes(), payload) {
		t.Fatalf("plain body differs from payload")
	}

	// 图片即使接受 gzip 也不压（本身已压缩）。
	req3 := httptest.NewRequest("GET", "/assets/kotaui-logo.png", nil)
	req3.Header.Set("Accept-Encoding", "gzip")
	rec3 := httptest.NewRecorder()
	writeStaticBody(rec3, req3, payload, "image/png")
	if rec3.Header().Get("content-encoding") != "" {
		t.Fatalf("image should not be gzipped, got %q", rec3.Header().Get("content-encoding"))
	}
}
