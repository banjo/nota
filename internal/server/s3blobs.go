package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

// S3Config is where S3Blobs keeps its blobs: a bucket of any
// S3-compatible service (Cloudflare R2, AWS S3, MinIO, Backblaze B2, …).
type S3Config struct {
	// Endpoint is the service's URL, e.g.
	// https://<account>.r2.cloudflarestorage.com or http://localhost:9000.
	// Requests to it are path-style (<endpoint>/<bucket>/<key>). Empty
	// means AWS S3 in Region, addressed virtual-host style.
	Endpoint string
	// Region signs the requests: "auto" for R2, the bucket's region for
	// AWS (default us-east-1, or auto with an Endpoint).
	Region string
	Bucket string
	// Prefix is put before every key ("nota" keeps blobs under nota/).
	Prefix string
	// AccessKey, SecretKey and SessionToken (optional) sign the requests.
	AccessKey, SecretKey, SessionToken string
	// TmpDir holds an upload while it is hashed (default os.TempDir).
	TmpDir string
	// Client sends the requests (default: one with sane timeouts).
	Client *http.Client
	// Now is the clock for signing (default time.Now).
	Now func() time.Time
}

// S3Blobs is a content-addressed BlobStore in a bucket. A blob's key is
// <prefix>/ab/abcdef…, the same layout DiskBlobs uses on disk, so a
// directory of blobs can be copied into a bucket as it is.
//
// Every request is signed with AWS Signature Version 4. An upload is
// signed with its SHA-256, which is the blob's hash: the service checks
// that the bytes it stored are the bytes that were hashed.
type S3Blobs struct {
	cfg  S3Config
	base *url.URL
}

// NewS3Blobs returns a blob store in the configured bucket. It does not
// contact the service; Ready does.
func NewS3Blobs(cfg S3Config) (*S3Blobs, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("server: s3 blobs: no bucket")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("server: s3 blobs: no credentials (AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY)")
	}
	var base *url.URL
	if cfg.Endpoint == "" {
		if cfg.Region == "" {
			cfg.Region = "us-east-1"
		}
		base = &url.URL{Scheme: "https", Host: cfg.Bucket + ".s3." + cfg.Region + ".amazonaws.com", Path: "/"}
	} else {
		u, err := url.Parse(strings.TrimRight(cfg.Endpoint, "/"))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, fmt.Errorf("server: s3 blobs: endpoint %q: want an http(s) URL", cfg.Endpoint)
		}
		if cfg.Region == "" {
			cfg.Region = "auto"
		}
		u.Path = strings.TrimRight(u.Path, "/") + "/" + cfg.Bucket + "/"
		base = u
	}
	cfg.Prefix = strings.Trim(cfg.Prefix, "/")
	if cfg.TmpDir == "" {
		cfg.TmpDir = os.TempDir()
	}
	if err := os.MkdirAll(cfg.TmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("server: s3 blobs: %w", err)
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
		}}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &S3Blobs{cfg: cfg, base: base}, nil
}

// String names the bucket for logs: s3://bucket/prefix.
func (s *S3Blobs) String() string {
	return strings.TrimRight("s3://"+s.cfg.Bucket+"/"+s.cfg.Prefix, "/")
}

func (s *S3Blobs) key(hash string) string {
	k := hash[:2] + "/" + hash
	if s.cfg.Prefix != "" {
		k = s.cfg.Prefix + "/" + k
	}
	return k
}

// Put implements BlobStore. The bytes are hashed into a temp file first
// (the hash is the key and signs the upload), then uploaded. Content the
// bucket already has is uploaded again: that is one request either way,
// and it renews the object's time, so garbage collection's grace covers
// a re-upload that has not been committed yet.
func (s *S3Blobs) Put(ctx context.Context, r io.Reader, limit int64) (string, int64, error) {
	f, err := os.CreateTemp(s.cfg.TmpDir, "s3put-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	h := sha256.New()
	src := r
	if limit > 0 {
		src = io.LimitReader(r, limit+1)
	}
	n, err := io.Copy(io.MultiWriter(f, h), src)
	if err != nil {
		return "", 0, err
	}
	if limit > 0 && n > limit {
		return "", 0, ErrTooLarge
	}
	hash := hex.EncodeToString(h.Sum(nil))
	res, err := s.do(ctx, http.MethodPut, s.key(hash), nil, f, n, hash)
	if err != nil {
		return "", 0, err
	}
	defer drain(res)
	if res.StatusCode/100 != 2 {
		return "", 0, s3Error("put", hash, res)
	}
	return hash, n, nil
}

// Open implements BlobStore.
func (s *S3Blobs) Open(ctx context.Context, hash string) (io.ReadCloser, error) {
	if !protocol.ValidHash(hash) {
		return nil, &fs.PathError{Op: "open", Path: hash, Err: fs.ErrNotExist}
	}
	res, err := s.do(ctx, http.MethodGet, s.key(hash), nil, nil, 0, "")
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		defer drain(res)
		return nil, s3Error("open", hash, res)
	}
	return res.Body, nil
}

// Stat implements BlobStore.
func (s *S3Blobs) Stat(ctx context.Context, hash string) (BlobInfo, error) {
	if !protocol.ValidHash(hash) {
		return BlobInfo{}, &fs.PathError{Op: "stat", Path: hash, Err: fs.ErrNotExist}
	}
	res, err := s.do(ctx, http.MethodHead, s.key(hash), nil, nil, 0, "")
	if err != nil {
		return BlobInfo{}, err
	}
	defer drain(res)
	if res.StatusCode != http.StatusOK {
		return BlobInfo{}, s3Error("stat", hash, res)
	}
	mod, _ := http.ParseTime(res.Header.Get("Last-Modified"))
	return BlobInfo{Hash: hash, Size: res.ContentLength, Modified: mod}, nil
}

// Delete implements BlobStore.
func (s *S3Blobs) Delete(ctx context.Context, hash string) error {
	if !protocol.ValidHash(hash) {
		return nil
	}
	res, err := s.do(ctx, http.MethodDelete, s.key(hash), nil, nil, 0, "")
	if err != nil {
		return err
	}
	defer drain(res)
	if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
		return s3Error("delete", hash, res)
	}
	return nil
}

type listResult struct {
	Contents []struct {
		Key          string
		Size         int64
		LastModified time.Time
	}
	IsTruncated           bool
	NextContinuationToken string
}

// Walk implements BlobStore with ListObjectsV2, a thousand keys a page.
// Keys that are not <prefix>/ab/<hash> are skipped.
func (s *S3Blobs) Walk(ctx context.Context, fn func(BlobInfo) error) error {
	prefix := ""
	if s.cfg.Prefix != "" {
		prefix = s.cfg.Prefix + "/"
	}
	token := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		page, err := s.list(ctx, q)
		if err != nil {
			return err
		}
		for _, c := range page.Contents {
			rest, ok := strings.CutPrefix(c.Key, prefix)
			dir, name, _ := strings.Cut(rest, "/")
			if !ok || !protocol.ValidHash(name) || dir != name[:2] {
				continue
			}
			if err := fn(BlobInfo{Hash: name, Size: c.Size, Modified: c.LastModified}); err != nil {
				return err
			}
		}
		if !page.IsTruncated || page.NextContinuationToken == "" {
			return nil
		}
		token = page.NextContinuationToken
	}
}

// Ready checks that the bucket answers and the credentials may list it.
func (s *S3Blobs) Ready(ctx context.Context) error {
	_, err := s.list(ctx, url.Values{"list-type": {"2"}, "max-keys": {"1"}})
	return err
}

func (s *S3Blobs) list(ctx context.Context, q url.Values) (*listResult, error) {
	res, err := s.do(ctx, http.MethodGet, "", q, nil, 0, "")
	if err != nil {
		return nil, err
	}
	defer drain(res)
	if res.StatusCode != http.StatusOK {
		return nil, s3Error("list", s.String(), res)
	}
	var page listResult
	if err := xml.NewDecoder(res.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("s3 list %s: %w", s, err)
	}
	return &page, nil
}

// emptyHash is the SHA-256 of no bytes: the payload hash of a request
// without a body.
const emptyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// do sends a signed request for key (the bucket itself when ""), with
// body (size bytes, SHA-256 bodyHash) when not nil. Network errors and
// 5xx or 429 answers are retried with backoff; a body is read again from
// its start each time.
func (s *S3Blobs) do(ctx context.Context, method, key string, q url.Values, body io.ReadSeeker, size int64, bodyHash string) (*http.Response, error) {
	u := *s.base
	u.Path += key
	u.RawPath = s.base.EscapedPath() + awsEscape(key, true)
	u.RawQuery = canonicalQuery(q)
	if bodyHash == "" {
		bodyHash = emptyHash
	}
	const attempts = 4
	var last error
	for i := range attempts {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(100<<i) * time.Millisecond):
			}
		}
		var rd io.Reader
		if body != nil {
			if _, err := body.Seek(0, io.SeekStart); err != nil {
				return nil, err
			}
			rd = io.NopCloser(body) // keep the client from closing the temp file
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
		if err != nil {
			return nil, err
		}
		if body != nil {
			req.ContentLength = size
			req.Header.Set("Content-Type", "application/octet-stream")
		}
		req.Header.Set("X-Amz-Content-Sha256", bodyHash)
		if s.cfg.SessionToken != "" {
			req.Header.Set("X-Amz-Security-Token", s.cfg.SessionToken)
		}
		signV4(req, bodyHash, s.cfg.AccessKey, s.cfg.SecretKey, s.cfg.Region, s.cfg.Now())
		res, err := s.cfg.Client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			last = fmt.Errorf("s3 %s %s: %w", method, s, err)
			continue
		}
		if (res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests) && i < attempts-1 {
			last = s3Error(strings.ToLower(method), key, res)
			drain(res)
			continue
		}
		return res, nil
	}
	return nil, last
}

func drain(res *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	res.Body.Close()
}

// s3Error is the error of an unsuccessful answer: one wrapping
// fs.ErrNotExist for 404, else the service's code and message.
func s3Error(op, name string, res *http.Response) error {
	if res.StatusCode == http.StatusNotFound {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}
	var e struct{ Code, Message string }
	if res.Request == nil || res.Request.Method != http.MethodHead {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		_ = xml.Unmarshal(data, &e)
	}
	if e.Code == "" {
		e.Code = res.Status
	}
	if e.Message != "" {
		return fmt.Errorf("s3 %s %s: %s: %s", op, name, e.Code, e.Message)
	}
	return fmt.Errorf("s3 %s %s: %s", op, name, e.Code)
}

// signV4 adds an AWS Signature Version 4 Authorization header for
// service s3. It signs the Host and every header the request has, so set
// them all first. See
// https://docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-header-based-auth.html
func signV4(req *http.Request, payloadHash, accessKey, secretKey, region string, now time.Time) {
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)

	headers := map[string]string{"host": req.Host}
	if req.Host == "" {
		headers["host"] = req.URL.Host
	}
	for k, v := range req.Header {
		headers[strings.ToLower(k)] = strings.Join(v, ",")
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canon bytes.Buffer
	for _, k := range names {
		canon.WriteString(k + ":" + strings.TrimSpace(headers[k]) + "\n")
	}
	signed := strings.Join(names, ";")

	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	creq := strings.Join([]string{req.Method, path, req.URL.RawQuery, canon.String(), signed, payloadHash}, "\n")
	scope := day + "/" + region + "/s3/aws4_request"
	sum := sha256.Sum256([]byte(creq))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])

	key := hmacSHA256([]byte("AWS4"+secretKey), day)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(key, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+
		", SignedHeaders="+signed+", Signature="+sig)
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// canonicalQuery encodes q sorted by key, each key and value escaped as
// SigV4 wants; the same string is sent and signed.
func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, awsEscape(k, false)+"="+awsEscape(v, false))
		}
	}
	return strings.Join(parts, "&")
}

// awsEscape percent-encodes everything but the unreserved characters
// (A-Z a-z 0-9 - _ . ~), and "/" when keepSlash.
func awsEscape(s string, keepSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9',
			c == '-', c == '_', c == '.', c == '~', c == '/' && keepSlash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
