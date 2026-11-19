package release

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Download fetches url into dest, resuming from a partial file with a Range
// request. It returns the number of bytes written in this call.
//
// A server that ignores Range answers 200 with the whole body; that path
// restarts the file rather than appending, which is the only safe reading.
func Download(ctx context.Context, client *http.Client, url, dest string, progress func(written, total int64)) (int64, error) {
	return DownloadLimit(ctx, client, url, dest, 0, progress)
}

// MaxUpdateBytes caps a download. A release zip is a few megabytes; anything
// past this is a mistake or an attack, and the file is removed.
const MaxUpdateBytes = 512 << 20

// DownloadLimit is Download with a size cap (0 means MaxUpdateBytes) and a
// bounded number of restart attempts. The body is read through a limit so a
// server that lies about Content-Length cannot fill the disk.
func DownloadLimit(ctx context.Context, client *http.Client, url, dest string, limit int64,
	progress func(written, total int64)) (int64, error) {
	return downloadAttempt(ctx, client, url, dest, limit, progress, 0)
}

func downloadAttempt(ctx context.Context, client *http.Client, url, dest string, limit int64,
	progress func(written, total int64), attempt int) (int64, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if limit <= 0 {
		limit = MaxUpdateBytes
	}
	if attempt > 2 {
		return 0, fmt.Errorf("下载失败：服务器多次拒绝续传")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, err
	}
	var offset int64
	if st, err := os.Stat(dest); err == nil {
		offset = st.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	switch {
	case offset > 0 && resp.StatusCode == http.StatusPartialContent:
		flags |= os.O_APPEND
	case resp.StatusCode == http.StatusOK:
		offset = 0
		flags |= os.O_TRUNC
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		// The partial file is longer than the release: start over, at most
		// twice more so a server that keeps answering 416 cannot loop.
		_ = os.Remove(dest)
		return downloadAttempt(ctx, client, url, dest, limit, progress, attempt+1)
	default:
		return 0, fmt.Errorf("下载失败：HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dest, flags, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	total := offset + resp.ContentLength
	if resp.ContentLength < 0 {
		total = -1
	}
	written := int64(0)
	buf := make([]byte, 32<<10)
	last := int64(0)
	for {
		if offset+written >= limit {
			_ = os.Remove(dest)
			return written, fmt.Errorf("下载超过 %d MB，已中止", limit>>20)
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return written, werr
			}
			written += int64(n)
			if progress != nil && written-last >= 256<<10 {
				last = written
				progress(offset+written, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return written, rerr
		}
	}
	if progress != nil {
		progress(offset+written, total)
	}
	return written, nil
}

// VerifyArchive checks one file against the digest in a signed SHA256SUMS.
func VerifyArchive(path string, sums Sums) error {
	name := filepath.Base(path)
	want, ok := sums[name]
	if !ok {
		return fmt.Errorf("发布清单里没有 %s", name)
	}
	got, err := FileSHA256(path)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%s 的校验和不符：下载被截断或被篡改", name)
	}
	return nil
}

// FetchSignedSums downloads SHA256SUMS and its signature, verifies the
// signature, and returns the parsed list. Every failure here must abort the
// update: nothing has been installed yet, and the caller deletes the files.
func FetchSignedSums(ctx context.Context, client *http.Client, baseURL string, pub ed25519.PublicKey) (Sums, error) {
	read := func(name string, limit int64) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/"+name, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s：HTTP %d", name, resp.StatusCode)
		}
		return io.ReadAll(io.LimitReader(resp.Body, limit))
	}
	sums, err := read(ChecksumsName, 1<<20)
	if err != nil {
		return nil, err
	}
	sig, err := read(ChecksumsName+".sig", 4096)
	if err != nil {
		return nil, err
	}
	if err := VerifyChecksums(pub, sums, sig); err != nil {
		return nil, err
	}
	return ParseSums(sums)
}

// SHA256File exposes the digest helper under the name the updater reads.
func SHA256File(path string) (string, error) { return FileSHA256(path) }
