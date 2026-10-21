package std

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/diff"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
)

// Fetch executes an outbound HTTP request through the sandbox's policy-enforced
// network client.
func (s *Std) Fetch(ctx context.Context, req FetchRequest) (*FetchResult, error) {
	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("%w: url is required", ErrInvalidArgument)
	}
	parsedURL, err := validateHTTPURL(rawURL)
	if err != nil {
		return nil, err
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	reqCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	var bodyReader io.Reader
	if len(req.Body) > 0 {
		bodyReader = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(reqCtx, method, parsedURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxFetchBytes
	}

	start := time.Now()
	client := s.sandbox.Net().HTTPClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	duration := time.Since(start)

	truncated := false
	if int64(len(data)) > maxBytes {
		data = data[:maxBytes]
		truncated = true
	}

	headers := make(map[string]string, len(resp.Header))
	for k, vals := range resp.Header {
		headers[strings.ToLower(k)] = strings.Join(vals, ", ")
	}

	contentType := resp.Header.Get("Content-Type")
	var text string
	if !isBinaryContent(data) {
		text = string(data)
	}

	return &FetchResult{
		URL:         parsedURL.String(),
		StatusCode:  resp.StatusCode,
		Status:      resp.Status,
		Headers:     headers,
		ContentType: contentType,
		Body:        data,
		Text:        text,
		Truncated:   truncated,
		Duration:    duration,
	}, nil
}

// Download fetches a remote resource over HTTP using the sandbox's
// policy-enforced network client, writes it to req.Path in the sandbox
// filesystem, and records the mutation in the FileTracker with SourceAgent and
// tool "download".
func (s *Std) Download(ctx context.Context, req DownloadRequest) (*DownloadResult, error) {
	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("%w: url is required", ErrInvalidArgument)
	}
	parsedURL, err := validateHTTPURL(rawURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Path) == "" {
		return nil, fmt.Errorf("%w: path is required", ErrInvalidArgument)
	}
	normPath := s.normalizePath(req.Path)
	if normPath == "." {
		return nil, fmt.Errorf("%w: path must refer to a file", ErrInvalidArgument)
	}

	var (
		diskBytes []byte
		diskHash  string
		exists    bool
		fileMode  fs.FileMode = 0o644
	)

	if info, statErr := s.sandbox.FS().Stat(ctx, normPath); statErr == nil {
		if info.IsDir() {
			return nil, fmt.Errorf("%w: %s is a directory", ErrInvalidArgument, normPath)
		}
		if perm := info.Mode().Perm(); perm != 0 {
			fileMode = perm
		}
	}

	readBytes, err := s.sandbox.FS().ReadFile(ctx, normPath)
	switch {
	case err == nil:
		exists = true
		diskBytes = readBytes
		diskHash = sha256Hex(diskBytes)
		if !req.Force {
			if err := s.verifyCAS(normPath, diskHash, req.ExpectedHash); err != nil {
				return nil, err
			}
		}
	case errors.Is(err, os.ErrNotExist), errors.Is(err, fs.ErrNotExist):
		exists = false
		if !req.Force && req.ExpectedHash != "" {
			return nil, fmt.Errorf("%w: %s (expected %s, file does not exist)", ErrFileModified, normPath, req.ExpectedHash)
		}
	default:
		return nil, err
	}

	if req.Mode != 0 {
		fileMode = req.Mode
	}

	reqCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxDownloadBytes
	}

	start := time.Now()
	client := s.sandbox.Net().HTTPClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("std: download %s failed with status %s", parsedURL.String(), resp.Status)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(bodyBytes)) > maxBytes {
		return nil, fmt.Errorf("std: download %s exceeded max size of %d bytes", parsedURL.String(), maxBytes)
	}
	duration := time.Since(start)

	parent := path.Dir(normPath)
	if parent != "." && parent != "" {
		if err := s.sandbox.FS().MkdirAll(ctx, parent, 0o755); err != nil {
			return nil, err
		}
	}
	if err := s.sandbox.FS().WriteFile(ctx, normPath, bodyBytes, fileMode); err != nil {
		return nil, err
	}

	afterHash := sha256Hex(bodyBytes)
	isBin := isBinaryContent(bodyBytes) || (exists && isBinaryContent(diskBytes))

	var add, del int
	if !isBin {
		add, del = diff.Stats(string(diskBytes), string(bodyBytes))
	}

	kind := KindCreated
	if exists {
		kind = KindModified
	}

	change := FileChange{
		Path:          normPath,
		Source:        SourceAgent,
		Tool:          "download",
		Kind:          kind,
		BeforeHash:    diskHash,
		AfterHash:     afterHash,
		BeforeContent: diskBytes,
		AfterContent:  bodyBytes,
		Additions:     add,
		Deletions:     del,
		IsBinary:      isBin,
		Timestamp:     time.Now().UTC(),
	}
	if err := s.tracker.RecordChange(ctx, &change); err != nil {
		return nil, err
	}

	s.logger.Debug("downloaded file",
		log.String("url", parsedURL.String()),
		log.String("path", normPath),
		log.Int("bytes", len(bodyBytes)),
	)

	return &DownloadResult{
		URL:          parsedURL.String(),
		Path:         normPath,
		StatusCode:   resp.StatusCode,
		ContentType:  resp.Header.Get("Content-Type"),
		BytesWritten: len(bodyBytes),
		Created:      !exists,
		BeforeHash:   diskHash,
		AfterHash:    afterHash,
		IsBinary:     isBin,
		Duration:     duration,
	}, nil
}

func validateHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid url %q: %v", ErrInvalidArgument, raw, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("%w: unsupported url scheme %q (must be http or https)", ErrInvalidArgument, u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%w: url %q is missing host", ErrInvalidArgument, raw)
	}
	return u, nil
}
