package httputil

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestPrereadBodyRoundtrip(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4"}`)
	preread := NewPrereadBody(body)

	got, err := io.ReadAll(preread)
	if err != nil {
		t.Fatalf("read preread body: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body mismatch: got %q want %q", got, body)
	}
	if err := preread.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !bytes.Equal(preread.Bytes(), body) {
		t.Fatalf("Bytes() mismatch after close: %q", preread.Bytes())
	}
}

func TestPrereadBodyReadableTwiceFromFreshWrapper(t *testing.T) {
	// ResetRequestBody 每次都回填新的 PrereadBody；同一实例只被顺序消费一次。
	body := []byte("payload")
	first := NewPrereadBody(body)
	got1, _ := io.ReadAll(first)
	if !bytes.Equal(got1, body) {
		t.Fatalf("first read mismatch: %q", got1)
	}

	second := NewPrereadBody(first.Bytes())
	got2, _ := io.ReadAll(second)
	if !bytes.Equal(got2, body) {
		t.Fatalf("second read mismatch: %q", got2)
	}
}

func TestReadRequestBodyWithPreallocReturnsPrereadBodySliceWithoutCopy(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4.5"}`)
	req, err := http.NewRequest(http.MethodPost, "/v1/messages", NewPrereadBody(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Length", "0") // 故意与实际不符，验证不依赖 ContentLength

	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("ReadRequestBodyWithPrealloc: %v", err)
	}
	if &got[0] != &body[0] {
		t.Fatalf("expected zero-copy return of the same slice")
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body mismatch: got %q want %q", got, body)
	}
}

func TestReadRequestBodyWithPreallocHandlesNilPrereadBody(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/v1/messages", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil || got != nil {
		t.Fatalf("expected nil body, got (%#v, %v)", got, err)
	}
}

func TestPrereadBodyPartialConsumeThenPreallocReturnsFullBody(t *testing.T) {
	body := []byte(strings.Repeat("x", 64))
	preread := NewPrereadBody(body)

	head := make([]byte, 16)
	if _, err := io.ReadFull(preread, head); err != nil {
		t.Fatalf("partial read: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, "/v1/responses", preread)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("ReadRequestBodyWithPrealloc: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("expected full body after partial consume, got %d bytes", len(got))
	}
}

func TestPrereadBodyNilReceiverIsSafe(t *testing.T) {
	var p *PrereadBody

	if got := p.Bytes(); got != nil {
		t.Fatalf("Bytes() on nil PrereadBody: got %q want nil", got)
	}
	var buf [4]byte
	if n, err := p.Read(buf[:]); n != 0 || err != io.EOF {
		t.Fatalf("Read() on nil PrereadBody: got (%d, %v) want (0, io.EOF)", n, err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close() on nil PrereadBody: %v", err)
	}
}

func TestPrereadBodyNilInput(t *testing.T) {
	preread := NewPrereadBody(nil)
	if preread == nil {
		t.Fatal("NewPrereadBody(nil) returned nil wrapper")
	}
	if got := preread.Bytes(); got != nil {
		t.Fatalf("Bytes() with nil input: got %q want nil", got)
	}
	got, err := io.ReadAll(preread)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ReadAll with nil input: got %d bytes want 0", len(got))
	}

	req, err := http.NewRequest(http.MethodPost, "/v1/messages", preread)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	got, err = ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("ReadRequestBodyWithPrealloc: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("prealloc with nil input: got %d bytes want 0", len(got))
	}
}

func TestReadRequestBodyWithPreallocTypedNilPrereadBody(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/v1/messages", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Body = (*PrereadBody)(nil)

	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("ReadRequestBodyWithPrealloc: %v", err)
	}
	if got != nil {
		t.Fatalf("typed-nil PrereadBody: got %q want nil", got)
	}
}

func TestReadRequestBodyWithPreallocNilRequest(t *testing.T) {
	got, err := ReadRequestBodyWithPrealloc(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil body, got %q", got)
	}
}

func TestPrereadBodyZeroValueIsSafe(t *testing.T) {
	var body PrereadBody
	var buf [1]byte
	if n, err := body.Read(buf[:]); n != 0 || err != io.EOF {
		t.Fatalf("zero-value read: got (%d, %v), want (0, EOF)", n, err)
	}
	if body.Bytes() != nil {
		t.Fatal("zero-value Bytes must be nil")
	}
	if err := body.Close(); err != nil {
		t.Fatalf("zero-value Close: %v", err)
	}
	got, err := ReadRequestBodyWithPrealloc(&http.Request{Body: &body})
	if err != nil || got != nil {
		t.Fatalf("zero-value fast path: got (%q, %v)", got, err)
	}
}

func TestPrereadBodyMultipartConsumptionPreservesFullBody(t *testing.T) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	if err := writer.WriteField("model", "local-test-model"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("prompt", "local payload"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	original := buffer.Bytes()
	body := NewPrereadBody(original)
	reader := multipart.NewReader(body, writer.Boundary())
	for _, expected := range []string{"local-test-model", "local payload"} {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(part)
		if err != nil || string(got) != expected {
			t.Fatalf("multipart value: got (%q, %v), want %q", got, err, expected)
		}
		if err := part.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := reader.NextPart(); err != io.EOF {
		t.Fatalf("expected multipart EOF, got %v", err)
	}
	got, err := ReadRequestBodyWithPrealloc(&http.Request{Body: body})
	if err != nil || !bytes.Equal(got, original) || &got[0] != &original[0] {
		t.Fatalf("multipart consumption changed the complete zero-copy body: %v", err)
	}
}
