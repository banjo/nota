package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The examples of the AWS documentation, "Signature Calculations for the
// Authorization Header": GET Object and GET Bucket (List Objects). The
// keys and signatures are AWS's published examples, not secrets.
func TestSignV4(t *testing.T) {
	at := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	ak, sk := "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" // ggignore
	sig := func(req *http.Request) string {
		req.Header.Set("X-Amz-Content-Sha256", emptyHash)
		signV4(req, emptyHash, ak, sk, "us-east-1", at)
		auth := req.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential="+ak+"/20130524/us-east-1/s3/aws4_request, ") {
			t.Errorf("credential: %s", auth)
		}
		return auth[strings.Index(auth, "SignedHeaders="):]
	}

	req, _ := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	if got, want := sig(req), "SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, Signature="+
		"f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"; got != want { // ggignore
		t.Errorf("GET object:\n got %s\nwant %s", got, want)
	}

	req, _ = http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/?"+
		canonicalQuery(map[string][]string{"prefix": {"J"}, "max-keys": {"2"}}), nil)
	if got, want := sig(req), "SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature="+
		"34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7"; got != want { // ggignore
		t.Errorf("list objects:\n got %s\nwant %s", got, want)
	}
}

func TestAWSEscape(t *testing.T) {
	for in, want := range map[string]string{
		"nota/ab/abc": "nota/ab/abc",
		"a b+c=ö~._-": "a%20b%2Bc%3D%C3%B6~._-",
		"\x01":        "%01",
	} {
		if got := awsEscape(in, true); got != want {
			t.Errorf("awsEscape(%q) = %q, want %q", in, got, want)
		}
	}
	if got := awsEscape("a/b", false); got != "a%2Fb" {
		t.Errorf("awsEscape(a/b, false) = %q", got)
	}
}

// An upload is retried after a 503 or a dropped connection, with its
// whole body again; a 403 is not retried and says why.
func TestS3BlobsRetries(t *testing.T) {
	f := newFakeS3(t)
	b := f.store(t, "")
	f.fail(http.StatusServiceUnavailable, 2)
	h, _, err := b.Put(context.Background(), strings.NewReader("survives\n"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.object(h[:2] + "/" + h); got != "survives\n" {
		t.Fatalf("stored %q", got)
	}
	f.fail(http.StatusForbidden, 1)
	if _, err := b.Stat(context.Background(), h); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("403: %v", err)
	}
	f.fail(http.StatusForbidden, 1)
	if err := b.Delete(context.Background(), h); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("403 with a body: %v", err)
	}
}

// fakeS3 is an in-memory S3 with path-style addressing, bucket "b". It
// checks every request's signature and an upload's SHA-256, and lists
// two keys a page so Walk has to follow continuation tokens.
type fakeS3 struct {
	t   *testing.T
	srv *httptest.Server
	mu  sync.Mutex
	obj map[string]fakeObject
	// failNext answers the next requests with failCode.
	failNext, failCode int
}

type fakeObject struct {
	data []byte
	mod  time.Time
}

const fakeKey, fakeSecret = "test-key", "test-secret"

func newFakeS3(t *testing.T) *fakeS3 {
	f := &fakeS3{t: t, obj: map[string]fakeObject{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeS3) store(t *testing.T, prefix string) *S3Blobs {
	b, err := NewS3Blobs(S3Config{Endpoint: f.srv.URL, Bucket: "b", Prefix: prefix,
		AccessKey: fakeKey, SecretKey: fakeSecret, TmpDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (f *fakeS3) fail(code, n int) {
	f.mu.Lock()
	f.failCode, f.failNext = code, n
	f.mu.Unlock()
}

func (f *fakeS3) object(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.obj[key].data)
}

func (f *fakeS3) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext > 0 {
		f.failNext--
		w.WriteHeader(f.failCode)
		if r.Method != http.MethodHead {
			fmt.Fprintf(w, "<Error><Code>AccessDenied</Code><Message>no</Message></Error>")
		}
		return
	}
	// The signature, recomputed from what arrived.
	auth := r.Header.Get("Authorization")
	date, _ := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	check, _ := http.NewRequest(r.Method, "http://"+r.Host+r.URL.RequestURI(), nil)
	signed := auth[strings.Index(auth, "SignedHeaders=")+len("SignedHeaders=") : strings.Index(auth, ", Signature=")]
	for _, h := range strings.Split(signed, ";") {
		if h != "host" && h != "x-amz-date" {
			check.Header.Set(h, r.Header.Get(h))
		}
	}
	signV4(check, r.Header.Get("X-Amz-Content-Sha256"), fakeKey, fakeSecret, "auto", date)
	if check.Header.Get("Authorization") != auth {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "<Error><Code>SignatureDoesNotMatch</Code></Error>")
		return
	}
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if bucket != "b" {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "<Error><Code>NoSuchBucket</Code></Error>")
		return
	}
	o, ok := f.obj[key]
	switch {
	case key == "" && r.Method == http.MethodGet:
		f.list(w, r)
	case r.Method == http.MethodPut:
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != r.Header.Get("X-Amz-Content-Sha256") ||
			r.ContentLength != int64(len(body)) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "<Error><Code>XAmzContentSHA256Mismatch</Code></Error>")
			return
		}
		f.obj[key] = fakeObject{data: body, mod: time.Now()}
	case r.Method == http.MethodDelete:
		delete(f.obj, key)
		w.WriteHeader(http.StatusNoContent)
	case !ok:
		w.WriteHeader(http.StatusNotFound)
	case r.Method == http.MethodHead, r.Method == http.MethodGet:
		w.Header().Set("Content-Length", strconv.Itoa(len(o.data)))
		w.Header().Set("Last-Modified", o.mod.UTC().Format(http.TimeFormat))
		if r.Method == http.MethodGet {
			_, _ = w.Write(o.data)
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var keys []string
	for k := range f.obj {
		if strings.HasPrefix(k, q.Get("prefix")) && k > q.Get("continuation-token") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	max := 2
	if m, err := strconv.Atoi(q.Get("max-keys")); err == nil && m < max {
		max = m
	}
	type content struct {
		Key          string
		Size         int
		LastModified string
	}
	var out struct {
		XMLName               xml.Name `xml:"ListBucketResult"`
		Contents              []content
		IsTruncated           bool
		NextContinuationToken string `xml:",omitempty"`
	}
	for i, k := range keys {
		if i == max {
			out.IsTruncated, out.NextContinuationToken = true, keys[i-1]
			break
		}
		out.Contents = append(out.Contents, content{k, len(f.obj[k].data), f.obj[k].mod.UTC().Format(time.RFC3339Nano)})
	}
	var buf bytes.Buffer
	_ = xml.NewEncoder(&buf).Encode(out)
	_, _ = w.Write(buf.Bytes())
}
