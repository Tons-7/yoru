package bt

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"iter"
	"log/slog"
	"maps"
	"os"
	"runtime/pprof"
	"slices"
	"strings"
	"time"
	"unique"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/generics/heap"
	"github.com/anacrolix/missinggo/v2/panicif"
	"yoru/internal/bt/internal/extracmp"

	"yoru/internal/bt/internal/request-strategy"
	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/webseed"
)

var webseedHostRequestConcurrency = initIntFromEnv("TORRENT_WEBSEED_HOST_REQUEST_CONCURRENCY", 25, 0)

type (
	webseedHostKey       string
	webseedHostKeyHandle = unique.Handle[webseedHostKey]
	webseedUrlKey        unique.Handle[string]
)

func (me webseedUrlKey) Value() string {
	return unique.Handle[string](me).Value()
}

func (me webseedUrlKey) String() string {
	return me.Value()
}

type webseedRequestHeapElem struct {
	webseedUniqueRequestKey
	webseedRequestOrderValue
	mightHavePartialFiles bool
}

func (cl *Client) updateWebseedRequests() {
	existingRequests := maps.Collect(cl.iterCurrentWebseedRequestsFromClient())
	panicif.False(maps.Equal(existingRequests, maps.Collect(cl.iterCurrentWebseedRequests())))

	g.MakeMapIfNil(&cl.aprioriMap)
	aprioriMap := cl.aprioriMap
	clear(aprioriMap)
	for uniqueKey, value := range cl.iterPossibleWebseedRequests() {
		if g.MapContains(existingRequests, uniqueKey) {
			continue
		}
		cur, ok := aprioriMap[uniqueKey]
		if ok {
			t := uniqueKey.t
			hasPeerConnRequest := func(reqIndex RequestIndex) bool {
				return t.requestingPeer(reqIndex) != nil
			}
			if cmp.Or(
				cmp.Compare(value.priority, cur.priority),
				extracmp.CompareBool(hasPeerConnRequest(cur.startRequest), hasPeerConnRequest(value.startRequest)),
				cmp.Compare(cur.startRequest, value.startRequest),
			) <= 0 {
				continue
			}
		}
		aprioriMap[uniqueKey] = value
	}

	heapSlice := cl.heapSlice[:0]
	requiredCap := len(aprioriMap) + len(existingRequests)
	if cap(heapSlice) < requiredCap {
		heapSlice = slices.Grow(heapSlice, requiredCap-cap(heapSlice))
	}
	defer func() {
		cl.heapSlice = heapSlice[:0]
	}()

	for key, value := range aprioriMap {
		panicif.True(g.MapContains(existingRequests, key))
		heapSlice = append(heapSlice, webseedRequestHeapElem{
			key,
			webseedRequestOrderValue{
				aprioriMapValue: value,
			},
			key.t.filesInRequestRangeMightBePartial(
				value.startRequest,
				key.t.endRequestForAlignedWebseedResponse(key.sliceIndex),
			),
		})
	}

	for key, value := range existingRequests {
		if key.t.closed.IsSet() || key.t.dataDownloadDisallowed.IsSet() {
			continue
		}
		wr := value.existingWebseedRequest
		heapSlice = append(heapSlice, webseedRequestHeapElem{
			key,
			value,
			key.t.filesInRequestRangeMightBePartial(wr.next, wr.end),
		})
	}

	aprioriHeap := heap.InterfaceForSlice(
		&heapSlice,
		func(l webseedRequestHeapElem, r webseedRequestHeapElem) bool {
			ret := cmp.Or(
				-cmp.Compare(l.priority, r.priority),
				extracmp.CompareBool(l.existingWebseedRequest == nil, r.existingWebseedRequest == nil),
				cmp.Compare(len(l.t.conns), len(r.t.conns)),
				-extracmp.CompareBool(l.mightHavePartialFiles, r.mightHavePartialFiles),
				cmp.Compare(l.t.info.Name, r.t.info.Name),
				bytes.Compare(l.t.canonicalShortInfohash()[:], r.t.canonicalShortInfohash()[:]),
				cmp.Compare(l.sliceIndex, r.sliceIndex),
			)
			if ret == 0 && l.url == r.url {
				fmt.Printf("%+v\n%+v\n", l, r)
				panic("webseed request heap ordering is not stable")
			}
			return ret < 0
		},
	)

	unwantedExistingRequests := maps.Clone(existingRequests)

	heap.Init(aprioriHeap)
	var plan webseedRequestPlan
	for aprioriHeap.Len() > 0 {
		elem := heap.Pop(aprioriHeap)
		costKey := elem.costKey
		panicif.Zero(costKey)
		if elem.existingWebseedRequest == nil {
			panicif.Eq(elem.priority, PiecePriorityNone)
		}
		panicif.True(elem.t.dataDownloadDisallowed.IsSet())
		panicif.True(elem.t.closed.IsSet())
		if len(plan.byCost[costKey]) >= webseedHostRequestConcurrency {
			continue
		}
		g.MakeMapIfNil(&plan.byCost)
		requestKey := elem.webseedUniqueRequestKey
		plan.byCost[costKey] = append(plan.byCost[costKey], plannedWebseedRequest{
			url:        elem.url,
			t:          elem.t,
			startIndex: elem.startRequest,
			sliceIndex: elem.sliceIndex,
		})
		delete(unwantedExistingRequests, requestKey)
	}

	for key, value := range unwantedExistingRequests {
		value.existingWebseedRequest.Cancel("deprioritized", key.t)
	}

	for costKey, plannedRequests := range plan.byCost {
		for _, request := range plannedRequests {
			if !cl.underWebSeedHttpRequestLimit(costKey) {
				break
			}
			existingRequestKey := request.toChunkedWebseedRequestKey()
			if g.MapContains(existingRequests, existingRequestKey) {

				panicif.True(g.MapContains(unwantedExistingRequests, existingRequestKey))
				continue
			}
			t := request.t
			peer := t.webSeeds[request.url]
			panicif.NotEq(peer.hostKey, costKey)

			debugLogger := webseedLogger.With(
				"webseedUrl", request.url,
				"webseedChunkIndex", request.sliceIndex)

			begin := request.startIndex
			end := t.getWebseedRequestEnd(begin, request.sliceIndex, debugLogger)
			panicif.LessThanOrEqual(end, begin)

			peer.spawnRequest(begin, end, debugLogger)
		}
	}
}

var shortenWebseedRequests = true

func init() {
	s, ok := os.LookupEnv("TORRENT_SHORTEN_WEBSEED_REQUESTS")
	if !ok {
		return
	}
	shortenWebseedRequests = s != ""
}

func (t *Torrent) getWebseedRequestEnd(begin RequestIndex, slice webseedSliceIndex, debugLogger *slog.Logger) RequestIndex {
	chunkEnd := t.endRequestForAlignedWebseedResponse(slice)
	if !shortenWebseedRequests {
		return chunkEnd
	}
	panicif.False(t.wantReceiveChunk(begin))
	var end = begin + 1
	for ; end < chunkEnd && t.wantReceiveChunk(end); end++ {
	}
	panicif.GreaterThan(end, chunkEnd)
	if webseed.PrintDebug && end != chunkEnd {
		debugLogger.Debug(
			"shortened webseed request",
			"from", endExclusiveString(begin, chunkEnd),
			"to", endExclusiveString(begin, end))
	}
	return end
}

var webseedLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}))

var webseedRequestChunkSize = initUIntFromEnv[uint64]("TORRENT_WEBSEED_REQUEST_CHUNK_SIZE", 64<<20, 64)

func (t *Torrent) endRequestForAlignedWebseedResponse(slice webseedSliceIndex) RequestIndex {
	end := min(
		t.maxEndRequest(),
		RequestIndex(slice+1)*t.chunksPerAlignedWebseedResponse())
	return end
}

func (t *Torrent) chunksPerAlignedWebseedResponse() RequestIndex {
	return RequestIndex(webseedRequestChunkSize / t.chunkSize.Uint64())
}

func (t *Torrent) requestIndexToWebseedSliceIndex(requestIndex RequestIndex) webseedSliceIndex {
	return webseedSliceIndex(requestIndex / t.chunksPerAlignedWebseedResponse())
}

func (cl *Client) dumpCurrentWebseedRequests() {
	if webseed.PrintDebug {
		fmt.Println("current webseed requests:")
		for key, value := range cl.iterCurrentWebseedRequests() {
			fmt.Printf("\t%v: %v, priority %v\n", key, value.existingWebseedRequest, value.priority)
		}
	}
}

type webseedRequestPlan struct {
	byCost map[webseedHostKeyHandle][]plannedWebseedRequest
}

type plannedWebseedRequest struct {
	url        webseedUrlKey
	t          *Torrent
	sliceIndex webseedSliceIndex
	startIndex RequestIndex
}

func (me *plannedWebseedRequest) toChunkedWebseedRequestKey() webseedUniqueRequestKey {
	return webseedUniqueRequestKey{
		url:        me.url,
		t:          me.t,
		sliceIndex: me.sliceIndex,
	}
}

func (me webseedRequestPlan) String() string {
	var sb strings.Builder
	for costKey, requestKeys := range me.byCost {
		fmt.Fprintf(&sb, "%v\n", costKey.Value())
		for _, requestKey := range requestKeys {
			fmt.Fprintf(&sb, "\t%v\n", requestKey)
		}
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

type webseedUniqueRequestKey struct {
	url        webseedUrlKey
	t          *Torrent
	sliceIndex webseedSliceIndex
}

type aprioriMapValue struct {
	costKey      webseedHostKeyHandle
	priority     PiecePriority
	startRequest RequestIndex
}

func (me webseedUniqueRequestKey) String() string {
	return fmt.Sprintf("torrent %v: webseed %v: slice %v", me.t, me.url, me.sliceIndex)
}

type webseedRequestOrderValue struct {
	aprioriMapValue
	existingWebseedRequest *webseedRequest
}

func (me webseedRequestOrderValue) String() string {
	return fmt.Sprintf("%#v", me)
}

func (cl *Client) iterPossibleWebseedRequests() iter.Seq2[webseedUniqueRequestKey, aprioriMapValue] {
	return func(yield func(webseedUniqueRequestKey, aprioriMapValue) bool) {
		for key, value := range cl.pieceRequestOrder {
			input := key.getRequestStrategyInput(cl)
			if !requestStrategy.GetRequestablePieces(
				input,
				value.pieces,
				func(ih metainfo.Hash, pieceIndex int, orderState requestStrategy.PieceRequestOrderState) bool {
					t := cl.torrentsByShortHash[ih]
					if len(t.webSeeds) == 0 {
						return true
					}
					p := t.piece(pieceIndex)
					cleanOpt := p.firstCleanChunk()
					if !cleanOpt.Ok {
						return true
					}
					priority := p.effectivePriority()
					firstRequest := p.requestIndexBegin() + cleanOpt.Value
					panicif.GreaterThanOrEqual(firstRequest, t.maxEndRequest())
					webseedSliceIndex := t.requestIndexToWebseedSliceIndex(firstRequest)
					for url, ws := range t.webSeeds {
						if ws.suspended() {
							continue
						}
						if !ws.peer.peerHasPiece(pieceIndex) {
							continue
						}
						if !yield(
							webseedUniqueRequestKey{
								t:          t,
								sliceIndex: webseedSliceIndex,
								url:        url,
							},
							aprioriMapValue{
								priority:     priority,
								costKey:      ws.hostKey,
								startRequest: firstRequest,
							},
						) {
							return false
						}
					}
					return true
				},
			) {
				break
			}
		}
	}
}

func (cl *Client) updateWebseedRequestsWithReason(reason updateRequestReason) {
	cl.scheduleImmediateWebseedRequestUpdate(reason)
}

func (cl *Client) yieldKeyAndValue(
	yield func(webseedUniqueRequestKey, webseedRequestOrderValue) bool,
	key webseedUniqueRequestKey,
	ar *webseedRequest,
) bool {
	t := key.t
	url := key.url
	hostKey := t.webSeeds[url].hostKey
	if false {
		if ar.cancelled.Load() {
			cl.slogger.Debug("iter current webseed requests: skipped cancelled webseed request")
			return true
		}
	}
	priority := PiecePriorityNone
	if ar.next < ar.end {
		p := t.piece(t.pieceIndexOfRequestIndex(ar.next))
		priority = p.effectivePriority()
	}
	return yield(
		webseedUniqueRequestKey{
			t:          t,
			sliceIndex: t.requestIndexToWebseedSliceIndex(ar.begin),
			url:        url,
		},
		webseedRequestOrderValue{
			aprioriMapValue{
				priority:     priority,
				costKey:      hostKey,
				startRequest: ar.next,
			},
			ar,
		},
	)
}

func (cl *Client) iterCurrentWebseedRequestsFromClient() iter.Seq2[webseedUniqueRequestKey, webseedRequestOrderValue] {
	return func(yield func(webseedUniqueRequestKey, webseedRequestOrderValue) bool) {
		for key, ar := range cl.activeWebseedRequests {
			if !cl.yieldKeyAndValue(yield, key, ar) {
				return
			}
		}
	}
}

func (cl *Client) iterCurrentWebseedRequests() iter.Seq2[webseedUniqueRequestKey, webseedRequestOrderValue] {
	return func(yield func(webseedUniqueRequestKey, webseedRequestOrderValue) bool) {
		for t := range cl.torrents {
			for url, ws := range t.webSeeds {
				for ar := range ws.activeRequests {
					key := webseedUniqueRequestKey{
						t:          t,
						sliceIndex: t.requestIndexToWebseedSliceIndex(ar.begin),
						url:        url,
					}
					if !cl.yieldKeyAndValue(yield, key, ar) {
						return
					}
				}
			}
		}
	}
}

func (cl *Client) scheduleImmediateWebseedRequestUpdate(reason updateRequestReason) {
	if !cl.webseedRequestTimer.Stop() {
		return
	}
	cl.webseedUpdateReason = cmp.Or(cl.webseedUpdateReason, reason)
	panicif.True(cl.webseedRequestTimer.Reset(0))
}

func (cl *Client) updateWebseedRequestsTimerFunc() {
	if cl.closed.IsSet() {
		return
	}
	cl.webseedUpdateReason = cmp.Or(cl.webseedUpdateReason, "timer")
	cl.lock()
	defer cl.unlock()
	cl.updateWebseedRequestsAndResetTimer()
}

func (cl *Client) updateWebseedRequestsAndResetTimer() {
	pprof.Do(context.Background(), pprof.Labels(
		"reason", string(cl.webseedUpdateReason),
	), func(_ context.Context) {
		started := time.Now()
		reason := cl.webseedUpdateReason
		cl.webseedUpdateReason = ""
		cl.updateWebseedRequests()
		panicif.NotZero(cl.webseedUpdateReason)
		if webseed.PrintDebug {
			now := time.Now()
			fmt.Printf("%v: updateWebseedRequests took %v (reason: %v)\n", now, now.Sub(started), reason)
		}
	})
	panicif.True(cl.webseedRequestTimer.Reset(webseedRequestUpdateTimerInterval))

}

type endExclusive[T any] struct {
	start, end T
}

func (me endExclusive[T]) String() string {
	return fmt.Sprintf("[%v-%v)", me.start, me.end)
}

func endExclusiveString[T any](start, end T) string {
	return endExclusive[T]{start, end}.String()
}
