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

const MaxDiscardBytes = 48 << 10

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
	ctx      context.Context
	cancel   context.CancelCauseFunc
	Body     io.Reader
	bodyPipe *io.PipeReader
}

func (r *Request) Context() context.Context {
	return r.ctx
}

func (r *Request) Cancel(cause error) {
	r.cancel(cause)
}

func (r *Request) Close() {
	_ = r.bodyPipe.Close()
}

type Client struct {
	Logger      *slog.Logger
	HttpClient  *http.Client
	Url         string
	MaxRequests int

	fileIndex               *segments.Index
	info                    *metainfo.Info
	Pieces                  roaring.Bitmap
	ResponseBodyWrapper     ResponseBodyWrapper
	ResponseBodyRateLimiter *rate.Limiter
	PathEscaper             PathEscaper
}

type ResponseBodyWrapper func(r io.Reader, interrupt func()) io.Reader

func (me *Client) SetInfo(info *metainfo.Info, fileIndex *segments.Index) {
	if !strings.HasSuffix(me.Url, "/") && info.IsDir() {
		me.Logger.Warn("webseed URL does not end with / and torrent is a directory")
		return
	}
	me.fileIndex = fileIndex
	me.info = info
	me.Pieces.AddRange(0, uint64(info.NumPieces()))
}

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
	return new(make([]byte, 128<<10))
}}

func (me *Client) recvPartResult(ctx context.Context, w io.Writer, part requestPart, resp *http.Response) error {
	defer resp.Body.Close()
	var body io.Reader = resp.Body
	if a := me.ResponseBodyWrapper; a != nil {
		body = a(body, func() { resp.Body.Close() })
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	switch resp.StatusCode {
	case http.StatusPartialContent:
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
		me.checkContentLength(resp, part, part.fileLength)
		discard := part.fileRange.Start
		if discard != 0 {
			me.Logger.Debug("resp status ok but requested range",
				"url", part.req.URL.String(),
				"range", part.req.Header.Get("Range"))
		}
		if discard > MaxDiscardBytes {
			return ErrStatusOkForRangeRequest{}
		}
		discarded, err := io.CopyN(io.Discard, body, discard)
		if err != nil {
			return fmt.Errorf("error discarding bytes from http ok response: %w", err)
		}
		panicif.NotEq(discarded, discard)
		_, err = io.CopyN(w, body, part.fileRange.Length)
		return err
	case http.StatusServiceUnavailable:
		return ErrTooFast
	default:
		return ErrBadResponse{
			fmt.Sprintf("unhandled response status code (%v)", resp.Status),
			resp,
		}
	}
}

var ErrTooFast = errors.New("making requests too fast")

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
