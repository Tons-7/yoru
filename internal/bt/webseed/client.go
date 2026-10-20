package webseed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime/pprof"
	"strings"
	"sync"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/anacrolix/missinggo/v2/panicif"
	"github.com/dustin/go-humanize"
	"golang.org/x/time/rate"

	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/segments"
)

// The most of a response that may be skipped, as archive.org sends small files whole instead of the range
// asked. TODO: configurable.
const MaxDiscardBytes = 48 << 10

// Output debug information to stdout.
var PrintDebug = false

func init() {
	_, PrintDebug = os.LookupEnv("TORRENT_WEBSEED_DEBUG")
}

type RequestSpec = segments.Extent

type requestPart struct {
	req        *http.Request
	fileRange  segments.Extent
	fileLength int64
	do         func() (*http.Response, error)
	fileIndex  int
}

type Request struct {
	// So you can view it from externally.
	ctx    context.Context
	cancel context.CancelCauseFunc
	Body   io.Reader
	// Closed with error to unstick copy routine when context isn't checked.
	bodyPipe *io.PipeReader
}

func (r *Request) Context() context.Context {
	return r.ctx
}

func (r *Request) Cancel(cause error) {
	r.cancel(cause)
}

func (r *Request) Close() {
	// We aren't cancelling because we want to know if we can keep receiving buffered data after
	// cancellation. PipeReader.Close always returns nil.
	_ = r.bodyPipe.Close()
}

type Client struct {
	Logger     *slog.Logger
	HttpClient *http.Client
	Url        string
	// Max concurrent requests to a WebSeed for a given torrent. TODO: Unused.
	MaxRequests int

	fileIndex *segments.Index
	info      *metainfo.Info
	// The pieces the Url can serve, where file-level blocks would map to the Client's pieces.
	// TODO: unused; it belongs in webseedPeer.
	Pieces roaring.Bitmap
	// This wraps http.Response bodies, for example to limit the download rate.
	ResponseBodyWrapper     ResponseBodyWrapper
	ResponseBodyRateLimiter *rate.Limiter
	PathEscaper             PathEscaper
}

type ResponseBodyWrapper func(r io.Reader, interrupt func()) io.Reader

func (me *Client) SetInfo(info *metainfo.Info, fileIndex *segments.Index) {
	if !strings.HasSuffix(me.Url, "/") && info.IsDir() {
		// In my experience, this is a non-conforming webseed. For example the
		// http://ia600500.us.archive.org/1/items URLs in archive.org torrents.
		me.Logger.Warn("webseed URL does not end with / and torrent is a directory")
		return
	}
	me.fileIndex = fileIndex
	me.info = info
	me.Pieces.AddRange(0, uint64(info.NumPieces()))
}

// Returns the URL for the given file index. This is assumed to be globally unique.
func (ws *Client) UrlForFileIndex(fileIndex int) string {
	return urlForFileIndex(ws.Url, fileIndex, ws.info, ws.PathEscaper)
}

func (ws *Client) StartNewRequest(ctx context.Context, r RequestSpec, debugLogger *slog.Logger) Request {
	ctx, cancel := context.WithCancelCause(ctx)
	var requestParts []requestPart
	panicif.Nil(ws.fileIndex)
	panicif.Nil(ws.info)
	for i, e := range ws.fileIndex.LocateIter(r) {
		req, err := newRequest(
			ctx,
			ws.Url, i, ws.info, e.Start, e.Length,
			ws.PathEscaper,
		)
		panicif.Err(err)
		part := requestPart{
			req:        req,
			fileRange:  e,
			fileLength: ws.fileIndex.Index(i).Length,
			fileIndex:  i,
		}
		part.do = func() (resp *http.Response, err error) {
			resp, err = ws.HttpClient.Do(req)
			if PrintDebug {
				if err == nil {
					debugLogger.Debug(
						"request for part",
						"url", req.URL,
						"part-length", humanize.IBytes(uint64(e.Length)),
						"part-file-offset", humanize.IBytes(uint64(e.Start)),
						"file-length", humanize.IBytes(uint64(part.fileLength)),
						"CF-Cache-Status", resp.Header.Get("CF-Cache-Status"),
					)
				}
			}
			return
		}
		requestParts = append(requestParts, part)
	}
	// The parts should run on from each other: data that doesn't is piece aligned and needs no several
	// requests. TODO: assert it.
	panicif.Zero(len(requestParts))
	body, w := io.Pipe()
	req := Request{
		ctx:      ctx,
		cancel:   cancel,
		Body:     body,
		bodyPipe: body,
	}
	go ws.requestPartResponsesReader(ctx, w, requestParts)
	return req
}

// Concatenates request part responses and sends them over the pipe.
func (ws *Client) requestPartResponsesReader(ctx context.Context, w *io.PipeWriter, requestParts []requestPart) {
	pprof.SetGoroutineLabels(context.Background())
	err := ws.readRequestPartResponses(ctx, w, requestParts)
	panicif.Err(w.CloseWithError(err))
}

type ErrStatusOkForRangeRequest struct{}

func (ErrStatusOkForRangeRequest) Error() string {
	return "resp status ok but requested range"
}

type ErrBadResponse struct {
	Msg      string
	Response *http.Response
}

func (me ErrBadResponse) Error() string {
	return me.Msg
}

// Warn about bad content-lengths.
func (me *Client) checkContentLength(resp *http.Response, part requestPart, expectedLen int64) {
	if resp.ContentLength == -1 {
		return
	}
	switch resp.Header.Get("Content-Encoding") {
	case "identity", "":
	default:
		return
	}
	if resp.ContentLength != expectedLen {
		me.Logger.Warn("unexpected identity response Content-Length value",
			"actual", resp.ContentLength,
			"expected", expectedLen,
			"url", part.req.URL.String())
	}
}

var bufPool = &sync.Pool{New: func() any {
	return new(make([]byte, 128<<10)) // 128 KiB. 4x the default.
}}

// Reads the part in full. All expected bytes must be returned or there will an error returned.
func (me *Client) recvPartResult(ctx context.Context, w io.Writer, part requestPart, resp *http.Response) error {
	defer resp.Body.Close()
	var body io.Reader = resp.Body
	if a := me.ResponseBodyWrapper; a != nil {
		body = a(body, func() { panicif.Err(resp.Body.Close()) })
	}
	// resp.Body stays set: niling it might upset the HTTP machinery.
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	switch resp.StatusCode {
	case http.StatusPartialContent:
		// The response should be just as long as we requested.
		me.checkContentLength(resp, part, part.fileRange.Length)
		buf := bufPool.Get().(*[]byte)
		defer bufPool.Put(buf)
		copied, err := io.CopyBuffer(w, body, *buf)
		if err != nil {
			return err
		}
		if copied != part.fileRange.Length {
			return fmt.Errorf("got %v bytes, expected %v", copied, part.fileRange.Length)
		}
		return nil
	case http.StatusOK:
		// The response is from the beginning of the file.
		me.checkContentLength(resp, part, part.fileLength)
		discard := part.fileRange.Start
		if discard != 0 {
			me.Logger.Debug("resp status ok but requested range",
				"url", part.req.URL.String(),
				"range", part.req.Header.Get("Range"))
		}
		if discard > MaxDiscardBytes {
			// TODO: So I think this can happen if the webseed host is caching and needs to pull
			// from the origin. If you try again later it will probably work.
			return ErrStatusOkForRangeRequest{}
		}
		// Skipped rather than taken as chunks: several requests answered by one whole file would need a
		// smarter request algorithm.
		discarded, err := io.CopyN(io.Discard, body, discard)
		if err != nil {
			return fmt.Errorf("error discarding bytes from http ok response: %w", err)
		}
		panicif.NotEq(discarded, discard)
		// Not a ranged reply, so the body is cut to the part.
		_, err = io.CopyN(w, body, part.fileRange.Length)
		return err
	case http.StatusServiceUnavailable:
		// TODO: Include all of Erigon's cases here?
		return ErrTooFast
	default:
		// TODO: Could we have a slog.Valuer or something to allow callers to unpack reasonable values?
		return ErrBadResponse{
			fmt.Sprintf("unhandled response status code (%v)", resp.Status),
			resp,
		}
	}
}

var ErrTooFast = errors.New("making requests too fast")

// Contains info for callers to act (like ignoring particular files or rate limiting).
type ReadRequestPartError struct {
	FileIndex int
	Err       error
}

func (me ReadRequestPartError) Unwrap() error {
	return me.Err
}

func (r ReadRequestPartError) Error() string {
	return fmt.Sprintf("reading request part for file index %v: %v", r.FileIndex, r.Err)
}

func (me *Client) readRequestPartResponses(ctx context.Context, w io.Writer, parts []requestPart) (err error) {
	for _, part := range parts {
		var resp *http.Response
		resp, err = part.do()
		// TODO: Does debugging caching belong here?
		if err == nil {
			err = me.recvPartResult(ctx, w, part, resp)
		}
		if err != nil {
			err = fmt.Errorf("reading %q at %q: %w", part.req.URL, part.req.Header.Get("Range"), err)
			err = ReadRequestPartError{
				FileIndex: part.fileIndex,
				Err:       err,
			}
			break
		}
	}
	return
}
