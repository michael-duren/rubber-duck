package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// contentTypes pins the types we serve for known extensions.
// mime.TypeByExtension consults the host's mime tables (/etc/mime.types), so
// its answer varies across machines — e.g. .js can come back as
// application/javascript. Pinning keeps responses identical everywhere.
var contentTypes = map[string]string{
	".css": "text/css; charset=utf-8",
	".js":  "text/javascript; charset=utf-8",
	".svg": "image/svg+xml",
}

// staticAsset is one embedded file, gzipped once at startup.
type staticAsset struct {
	raw         []byte
	gz          []byte // nil when gzip didn't pay off (e.g. PNGs)
	contentType string
	etag        string
}

// staticHandler serves embedded static assets with gzip content-negotiation
// and ETag revalidation. http.FileServerFS does neither, so the ~800KB editor
// bundle went out uncompressed on every load — and because embed.FS files have
// a zero modtime, it couldn't send Last-Modified either, defeating caching. We
// compress each asset once at startup and keep both copies in memory (total a
// couple MB); requests then negotiate encoding and revalidate cheaply via ETag.
type staticHandler struct {
	assets map[string]staticAsset
}

// newStaticHandler precompresses every file under root in fsys. root is the
// embed path ("static"); the resulting map is keyed by that same path so a
// request for "/static/cm6.js" looks up "static/cm6.js".
func newStaticHandler(fsys fs.FS, root string) (*staticHandler, error) {
	h := &staticHandler{assets: map[string]staticAsset{}}
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		ct := contentTypes[path.Ext(p)]
		if ct == "" {
			ct = mime.TypeByExtension(path.Ext(p))
		}
		if ct == "" {
			ct = http.DetectContentType(raw)
		}
		sum := sha256.Sum256(raw)
		a := staticAsset{
			raw:         raw,
			contentType: ct,
			etag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
		}
		// Only keep the gzip copy when it actually shrinks the file, so
		// already-compressed assets (images) don't pay a pointless re-encode.
		if gz := gzipBytes(raw); gz != nil && len(gz) < len(raw)*9/10 {
			a.gz = gz
		}
		h.assets[p] = a
		return nil
	})
	if err != nil {
		return nil, err
	}
	return h, nil
}

func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if _, err := zw.Write(b); err != nil {
		return nil
	}
	if err := zw.Close(); err != nil {
		return nil
	}
	return buf.Bytes()
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a, ok := h.assets[strings.TrimPrefix(r.URL.Path, "/")]
	if !ok {
		http.NotFound(w, r)
		return
	}

	hdr := w.Header()
	hdr.Set("Content-Type", a.contentType)
	hdr.Set("ETag", a.etag)
	// Filenames aren't content-hashed, so keep freshness short and lean on
	// ETag revalidation (a matching If-None-Match yields a tiny 304) rather
	// than caching a stale asset across a deploy.
	hdr.Set("Cache-Control", "public, max-age=300")
	hdr.Add("Vary", "Accept-Encoding")

	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, a.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	body := a.raw
	if a.gz != nil && acceptsGzip(r) {
		hdr.Set("Content-Encoding", "gzip")
		body = a.gz
	}
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// acceptsGzip reports whether the client will take a gzip-encoded body: a
// gzip token in Accept-Encoding whose q parameter, if present, is nonzero.
// Wildcards and the x-gzip alias are ignored — every real browser sends a
// plain "gzip" token.
func acceptsGzip(r *http.Request) bool {
	for tok := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
		enc, param, hasParam := strings.Cut(tok, ";")
		if strings.TrimSpace(enc) != "gzip" {
			continue
		}
		if !hasParam {
			return true
		}
		if v, ok := strings.CutPrefix(strings.TrimSpace(param), "q="); ok {
			q, err := strconv.ParseFloat(v, 64)
			return err != nil || q > 0
		}
		return true
	}
	return false
}

// etagMatches reports whether an If-None-Match header covers etag. It accepts
// "*", a comma-separated list, and weak validators (W/ prefix), which is all
// browsers send for a conditional GET.
func etagMatches(header, etag string) bool {
	if strings.TrimSpace(header) == "*" {
		return true
	}
	for tok := range strings.SplitSeq(header, ",") {
		if strings.TrimPrefix(strings.TrimSpace(tok), "W/") == etag {
			return true
		}
	}
	return false
}
