package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	videoRangePattern            = regexp.MustCompile(`^bytes=([0-9]*)-([0-9]*)$`)
	videoContentRangePattern     = regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+)$`)
	videoUnsatisfiedRangePattern = regexp.MustCompile(`^bytes \*/([0-9]+)$`)
)

type videoRelayRange struct {
	start int64
	end   int64
}

func parseVideoRelayRange(header string) (*videoRelayRange, error) {
	if header == "" {
		return nil, nil
	}
	parts := videoRangePattern.FindStringSubmatch(strings.TrimSpace(header))
	if len(parts) == 0 || parts[1] == "" && parts[2] == "" {
		return nil, errors.New("invalid video byte range")
	}
	var err error
	span := &videoRelayRange{start: -1, end: -1}
	if parts[1] != "" {
		span.start, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, err
		}
	}
	if parts[2] != "" {
		span.end, err = strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, err
		}
	}
	if span.start < 0 && span.end == 0 || span.start >= 0 && span.end >= 0 && span.end < span.start {
		return nil, errors.New("invalid video byte limits")
	}
	return span, nil
}

func (span *videoRelayRange) forLength(total int64) (int64, int64, bool) {
	if span == nil || total <= 0 || span.start >= total {
		return 0, 0, false
	}
	if span.start < 0 {
		first := total - span.end
		if first < 0 {
			first = 0
		}
		return first, total - 1, true
	}
	end := total - 1
	if span.end >= 0 {
		end = min(end, span.end)
	}
	return span.start, end, true
}

type VideoRelayDownload struct {
	TaskID       string
	Status       int
	Length       int64
	ContentRange string
	Reader       io.Reader
	Close        func()
}

func validateVideoRelayMedia(resp *http.Response, wanted *videoRelayRange) (*VideoRelayDownload, error) {
	invalid := errors.New("upstream response does not contain the requested MP4 data")
	types := resp.Header.Values("Content-Type")
	if resp.Uncompressed || len(types) != 1 {
		return nil, invalid
	}
	for _, raw := range resp.Header.Values("Content-Encoding") {
		if encoding := strings.ToLower(strings.TrimSpace(raw)); encoding != "" && encoding != "identity" {
			return nil, invalid
		}
	}
	if mediaType, _, err := mime.ParseMediaType(types[0]); err != nil || mediaType != "video/mp4" {
		return nil, invalid
	}
	download := &VideoRelayDownload{Status: resp.StatusCode, Length: resp.ContentLength, Reader: resp.Body}
	ranges := resp.Header.Values("Content-Range")
	checkSignature := true
	switch resp.StatusCode {
	case http.StatusOK:
		if len(ranges) != 0 {
			return nil, invalid
		}
	case http.StatusPartialContent:
		if len(ranges) != 1 {
			return nil, invalid
		}
		fields := videoContentRangePattern.FindStringSubmatch(ranges[0])
		if len(fields) == 0 {
			return nil, invalid
		}
		start, a := strconv.ParseInt(fields[1], 10, 64)
		end, b := strconv.ParseInt(fields[2], 10, 64)
		total, c := strconv.ParseInt(fields[3], 10, 64)
		if a != nil || b != nil || c != nil || start > end || end >= total {
			return nil, invalid
		}
		first, last, ok := wanted.forLength(total)
		if !ok || first != start || last != end {
			return nil, invalid
		}
		length := end - start + 1
		if resp.ContentLength >= 0 && resp.ContentLength != length {
			return nil, invalid
		}
		download.Length = length
		download.ContentRange = fmt.Sprintf("bytes %d-%d/%d", start, end, total)
		checkSignature = start == 0 && length >= 12
	default:
		return nil, invalid
	}
	if checkSignature {
		reader := bufio.NewReader(resp.Body)
		download.Reader = reader
		prefix, err := reader.Peek(12)
		if err != nil || string(prefix[4:8]) != "ftyp" {
			return download, invalid
		}
	}
	return download, nil
}

func videoRelayRangeError() *VideoRelayError {
	return &VideoRelayError{Status: http.StatusRequestedRangeNotSatisfiable, Code: "invalid_request_error", Message: "Request a single valid byte range for this video. 下载范围无效，请只请求一段有效范围。"}
}

func (s *VideoRelayService) Download(c *gin.Context, key *APIKey, id string, ranges []string) (*VideoRelayDownload, *VideoRelayError) {
	ctx := c.Request.Context()
	task, err := s.GetOwned(ctx, key, id)
	if err != nil {
		return nil, err
	}
	if !task.CanDownload() {
		return nil, &VideoRelayError{Status: http.StatusConflict, Code: "invalid_request_error", Message: "The video is not available for download. Check the task status first. 视频还不能下载，请先查询任务状态。"}
	}
	var rangeHeader string
	if len(ranges) > 1 || len(ranges) == 1 && strings.TrimSpace(ranges[0]) == "" {
		return nil, videoRelayRangeError()
	}
	if len(ranges) == 1 {
		rangeHeader = strings.TrimSpace(ranges[0])
	}
	wanted, rangeErr := parseVideoRelayRange(rangeHeader)
	if rangeErr != nil {
		return nil, videoRelayRangeError()
	}
	unavailable := &VideoRelayError{Status: http.StatusBadGateway, Code: "upstream_error", Message: videoRelayDownloadMessage}
	route, release := s.originalRoute(ctx, task)
	if route == nil {
		return nil, unavailable
	}
	req, buildErr := route.taskRequest(ctx, task.UpstreamTaskID, true)
	if buildErr != nil {
		releaseVideoRelaySlot(release)
		return nil, unavailable
	}
	deleteHeaderAllForms(req.Header, "range")
	if wanted != nil {
		req.Header.Set("Range", rangeHeader)
	}
	req.Header.Set("Accept", "video/mp4")
	req.Header.Set("Accept-Encoding", "identity")
	resp, roundTripErr := s.gateway.httpUpstream.Do(req, route.proxyURL, route.account.ID, route.account.Concurrency)
	if roundTripErr != nil {
		releaseVideoRelaySlot(release)
		_ = s.gateway.handleOpenAIUpstreamTransportError(ctx, c, route.account, roundTripErr, false)
		unavailable.UpstreamStatus = http.StatusBadGateway
		return nil, unavailable
	}
	var once sync.Once
	closeBody := func() {
		once.Do(func() {
			_ = resp.Body.Close()
			releaseVideoRelaySlot(release)
		})
	}
	stop := context.AfterFunc(ctx, closeBody)
	closeDownload := func() {
		stop()
		closeBody()
	}
	reader := io.Reader(resp.Body)
	kind := "http_error"
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
		download, err := validateVideoRelayMedia(resp, wanted)
		if err == nil {
			download.TaskID, download.Close = task.TaskID, closeDownload
			return download, nil
		}
		kind = "invalid_response"
		if download != nil {
			reader = download.Reader
		}
	}
	// Preserve the full bounded error for account state and Ops, including
	// structured errors that need to be parsed as complete JSON.
	limit := int64(maxBodyMatchLen)
	if kind == "http_error" {
		limit = videoRelayResponseLimit + 1
	}
	readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	stopRead := context.AfterFunc(readCtx, closeBody)
	body, readErr := io.ReadAll(io.LimitReader(reader, limit))
	stopRead()
	cancel()
	closeDownload()
	accountBody := body
	if kind == "http_error" && (readErr != nil || len(body) > videoRelayResponseLimit) {
		accountBody = nil
	}
	s.recordUpstreamError(ctx, c, route, resp, accountBody, task.UpstreamModel, kind)
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && wanted != nil {
		if ranges := resp.Header.Values("Content-Range"); len(ranges) == 1 {
			if fields := videoUnsatisfiedRangePattern.FindStringSubmatch(ranges[0]); len(fields) != 0 {
				if total, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					if _, _, satisfiable := wanted.forLength(total); !satisfiable {
						failure := videoRelayRangeError()
						failure.ContentRange = "bytes */" + strconv.FormatInt(total, 10)
						return nil, failure
					}
				}
			}
		}
	}
	// Rules can use the prefix from this same read, even if the body was
	// truncated or larger than the limit for account-state handling.
	ruleBody := body[:min(len(body), maxBodyMatchLen)]
	return nil, videoRelayUpstreamError(c, route.account, resp.StatusCode, ruleBody, videoRelayDownloadMessage)
}
