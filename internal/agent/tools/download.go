package tools

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
)

const (
	defaultDialTimeout         = 10 * time.Second
	defaultMaxIdleConns        = 100
	defaultMaxIdleConnsPerHost = 10
	defaultIdleConnTimeout     = 90 * time.Second
	defaultBufferSizeBytes     = 32 * 1024
	minWatchdogTickerPeriod    = 10 * time.Millisecond
	watchdogCheckInterval      = 1 * time.Second
	progressLogInterval        = 2 * time.Second
	defaultSyncWaitMs          = 5000
	directoryPermMask          = 0755
	filePermMask               = 0644

	chromeUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var defaultIdleTimeout = 30 * time.Second

// DownloadRunner implements TaskRunner and StateReporter for downloading files.
type DownloadRunner struct {
	URL         string
	DestPath    string
	IdleTimeout time.Duration

	mu         sync.Mutex
	downloaded int64
	total      int64
	isStuck    bool
	cancelFunc context.CancelFunc
}

// Start executes the download and streams data to DestPath.
func (r *DownloadRunner) Start(ctx context.Context, stdout io.Writer, stderr io.Writer) error {
	runCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancelFunc = cancel
	r.mu.Unlock()
	defer cancel()

	// 1. Configure uTLS client (same chrome handshake spoofing as web_fetch)
	transport := &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout: defaultDialTimeout,
			}
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				host = addr
			}

			config := &utls.Config{
				ServerName:         host,
				InsecureSkipVerify: false,
			}
			uconn := utls.UClient(conn, config, utls.HelloCustom)

			spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
			if err != nil {
				conn.Close()
				return nil, err
			}

			// Force HTTP/1.1 ALPN
			for _, ext := range spec.Extensions {
				if alpn, ok := ext.(*utls.ALPNExtension); ok {
					alpn.AlpnProtocols = []string{"http/1.1"}
				}
			}

			err = uconn.ApplyPreset(&spec)
			if err != nil {
				conn.Close()
				return nil, err
			}

			err = uconn.Handshake()
			if err != nil {
				conn.Close()
				return nil, err
			}
			return uconn, nil
		},
		TLSNextProto:        make(map[string]func(string, *tls.Conn) http.RoundTripper),
		MaxIdleConns:        defaultMaxIdleConns,
		MaxIdleConnsPerHost: defaultMaxIdleConnsPerHost,
		IdleConnTimeout:     defaultIdleConnTimeout,
	}

	client := &http.Client{
		Timeout:   0, // We control timeouts via context and the watchdog
		Transport: transport,
	}

	req, err := http.NewRequestWithContext(runCtx, "GET", r.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Browser spoof headers
	req.Header.Set("User-Agent", chromeUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "keep-alive")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned status: %s", resp.Status)
	}

	r.mu.Lock()
	r.total = resp.ContentLength
	r.mu.Unlock()

	// 2. Open destination file
	outFile, err := os.OpenFile(r.DestPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePermMask)
	if err != nil {
		return fmt.Errorf("failed to create target file: %w", err)
	}
	defer outFile.Close()

	// 3. Initialize progress tracking & watchdog
	var lastReadMu sync.Mutex
	lastReadAt := time.Now()

	updateLastRead := func() {
		lastReadMu.Lock()
		lastReadAt = time.Now()
		lastReadMu.Unlock()
	}

	idleTimeout := r.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = defaultIdleTimeout
	}

	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		tickerPeriod := watchdogCheckInterval
		if idleTimeout < tickerPeriod {
			tickerPeriod = idleTimeout / 2
			if tickerPeriod < minWatchdogTickerPeriod {
				tickerPeriod = minWatchdogTickerPeriod
			}
		}
		ticker := time.NewTicker(tickerPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				lastReadMu.Lock()
				idleTime := time.Since(lastReadAt)
				lastReadMu.Unlock()
				if idleTime > idleTimeout {
					r.mu.Lock()
					r.isStuck = true
					r.mu.Unlock()
					fmt.Fprintf(stderr, "error: download stalled: no data received for %s\n", idleTime.String())
					cancel()
					return
				}
			}
		}
	}()

	// 4. Read loop
	buf := make([]byte, defaultBufferSizeBytes)
	var totalBytes int64
	var lastLogTime time.Time

	fmt.Fprintln(stdout, "Starting download...")
	for {
		if err := runCtx.Err(); err != nil {
			return err
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			updateLastRead()

			wn, writeErr := outFile.Write(buf[:n])
			if writeErr != nil {
				return fmt.Errorf("failed to write data: %w", writeErr)
			}
			totalBytes += int64(wn)

			r.mu.Lock()
			r.downloaded = totalBytes
			r.mu.Unlock()

			if time.Since(lastLogTime) > progressLogInterval {
				lastLogTime = time.Now()
				fmt.Fprintln(stdout, r.State())
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return readErr
		}
	}

	fmt.Fprintf(stdout, "Successfully downloaded %d bytes to %s\n", totalBytes, r.DestPath)

	// Block until watchdog exits cleanly to avoid leak
	cancel()
	<-watchdogDone

	return nil
}

// Stop cancels the running download operation.
func (r *DownloadRunner) Stop() error {
	r.mu.Lock()
	cancel := r.cancelFunc
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// State returns the current human-readable download progress.
func (r *DownloadRunner) State() string {
	r.mu.Lock()
	downloaded := r.downloaded
	total := r.total
	isStuck := r.isStuck
	r.mu.Unlock()

	if isStuck {
		return "Download stalled: idle timeout reached"
	}

	if total > 0 {
		pct := float64(downloaded) / float64(total) * 100
		return fmt.Sprintf("Downloading: %.1f MB / %.1f MB (%.1f%%)", float64(downloaded)/(1024*1024), float64(total)/(1024*1024), pct)
	}
	return fmt.Sprintf("Downloading: %.1f MB (unknown size)", float64(downloaded)/(1024*1024))
}

// WriteStdin writes standard input to the download runner. It is not supported for downloads.
func (r *DownloadRunner) WriteStdin(data string) error {
	return fmt.Errorf("stdin write not supported for downloads")
}

// Download handles tool execution for downloading a file.
func (h *ToolHandlers) Download(ctx context.Context, in DownloadArgs) (DownloadOutput, error) {
	if h.TaskManager == nil {
		return DownloadOutput{}, fmt.Errorf("task manager is not initialized")
	}

	parsedURL, err := url.Parse(in.Url)
	if err != nil {
		return DownloadOutput{}, fmt.Errorf("invalid URL: %w", err)
	}

	dest := in.Destination
	if dest == "" {
		base := path.Base(parsedURL.Path)
		if base == "" || base == "." || base == "/" {
			base = "downloaded_file"
		}
		dest = base
	}

	var absDest string
	if filepath.IsAbs(dest) {
		absDest = dest
	} else {
		absDest = filepath.Join(h.CWD, dest)
	}

	if err := os.MkdirAll(filepath.Dir(absDest), directoryPermMask); err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to create destination directory: %w", err)
	}

	runner := &DownloadRunner{
		URL:         in.Url,
		DestPath:    absDest,
		IdleTimeout: defaultIdleTimeout,
	}

	waitMs := in.WaitMs
	if waitMs <= 0 {
		waitMs = defaultSyncWaitMs
	}

	task, err := h.TaskManager.Submit(ctx, SubmitOptions{
		SessionID: h.SessionID,
		TaskType:  "download",
		Name:      fmt.Sprintf("Download %s to %s", in.Url, dest),
		Runner:    runner,
		WaitMs:    waitMs,
	})
	if err != nil {
		return DownloadOutput{
			Success: false,
			Message: fmt.Sprintf("Failed to submit download task: %v", err),
		}, nil
	}

	h.TaskManager.mu.RLock()
	status := task.Status
	isBg := task.IsBackground
	h.TaskManager.mu.RUnlock()

	relDest, relErr := filepath.Rel(h.CWD, absDest)
	var finalPath string
	if relErr == nil && !strings.HasPrefix(relDest, "..") {
		finalPath = relDest
	} else {
		finalPath = absDest
	}

	if isBg {
		return DownloadOutput{
			Success: true,
			TaskId:  task.ID,
			Message: fmt.Sprintf("Download is running in the background (Task ID: %s).", task.ID),
			Path:    finalPath,
		}, nil
	}

	if status == StatusCompleted {
		info, err := os.Stat(absDest)
		var size int64
		if err == nil {
			size = info.Size()
		}
		return DownloadOutput{
			Success:   true,
			Path:      finalPath,
			SizeBytes: int(size),
			Message:   "Download completed successfully.",
		}, nil
	}

	// Task failed or was killed
	errMsg := task.Error
	if errMsg == "" {
		errMsg = string(status)
	}
	return DownloadOutput{
		Success: false,
		Message: fmt.Sprintf("Download failed: %s", errMsg),
		Path:    finalPath,
	}, nil
}

// TextContent returns a human-readable representation of DownloadOutput.
func (o DownloadOutput) TextContent() string {
	return o.Message
}
