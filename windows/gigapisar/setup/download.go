// Скачивание больших файлов с докачкой и автоповтором; распаковка архивов.
package setup

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

var ErrNotFound = errors.New("404")
var ErrCancelled = errors.New("отменено")

type Progress func(done, total int64, speedMBs float64)

// Download качает url в dest (через dest.part), докачивая после обрыва.
func Download(url, dest string, cancel *int32, p Progress) error {
	attempt := 0
	for {
		err := downloadOnce(url, dest, cancel, p)
		if err == nil || err == ErrNotFound || err == ErrCancelled {
			return err
		}
		if cancel != nil && atomic.LoadInt32(cancel) != 0 {
			return ErrCancelled
		}
		attempt++
		if attempt > 40 {
			return fmt.Errorf("связь рвётся раз за разом (%v); попробуйте позже — докачается с этого места", err)
		}
		d := time.Duration(attempt) * 2 * time.Second
		if d > 15*time.Second {
			d = 15 * time.Second
		}
		time.Sleep(d)
	}
}

func downloadOnce(url, dest string, cancel *int32, p Progress) error {
	part := dest + ".part"
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "giga-pisar-windows")
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := (&http.Client{Timeout: 0}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
		have = 0
	case 206:
	case 404:
		return ErrNotFound
	default:
		return fmt.Errorf("сервер ответил %s", resp.Status)
	}
	os.MkdirAll(filepath.Dir(dest), 0o755)
	flags := os.O_CREATE | os.O_WRONLY
	if have > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	total := have + resp.ContentLength
	if resp.ContentLength < 0 {
		total = 0
	}
	buf := make([]byte, 1<<16)
	done := have
	start, last := time.Now(), time.Time{}
	for {
		if cancel != nil && atomic.LoadInt32(cancel) != 0 {
			f.Close()
			return ErrCancelled
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			done += int64(n)
			if p != nil && time.Since(last) > 300*time.Millisecond {
				last = time.Now()
				p(done, total, float64(done-have)/1e6/time.Since(start).Seconds())
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	f.Close()
	if p != nil {
		p(done, total, 0)
	}
	if total > 0 && done < total {
		return errors.New("оборвалось")
	}
	return os.Rename(part, dest)
}

// ExtractTarGz кладёт нужные файлы архива в dest по именам (без путей).
func ExtractTarGz(src, dest string, wanted map[string]bool) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	os.MkdirAll(dest, 0o755)
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		base := filepath.Base(h.Name)
		if h.Typeflag != tar.TypeReg || (wanted != nil && !wanted[base]) {
			continue
		}
		out := filepath.Join(dest, base)
		w, err := os.Create(out + ".part")
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, tr); err != nil {
			w.Close()
			return err
		}
		w.Close()
		os.Rename(out+".part", out)
	}
}

// ExtractZip раскладывает файлы архива в dest; если задан anchor — берётся папка, где он лежит.
func ExtractZip(src, dest, anchor string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	base := ""
	if anchor != "" {
		for _, f := range r.File {
			if filepath.Base(f.Name) == anchor {
				base = strings.TrimSuffix(f.Name, filepath.Base(f.Name))
			}
		}
	}
	os.MkdirAll(dest, 0o755)
	for _, f := range r.File {
		if f.FileInfo().IsDir() || (base != "" && !strings.HasPrefix(f.Name, base)) {
			continue
		}
		rel := strings.TrimPrefix(f.Name, base)
		if base == "" {
			rel = filepath.Base(f.Name)
		}
		out := filepath.Join(dest, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(out), 0o755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.Create(out)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(w, rc)
		w.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
