//go:build unit

package service

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoRelayDownloadValidatesPartialMP4AndHeaders(t *testing.T) {
	mp4 := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 8, 8, 8, 8}
	request, err := parseVideoRelayRange("bytes=0-15")
	require.NoError(t, err)
	response := func(contentType, contentRange, encoding string, body []byte, length int64) *http.Response {
		header := http.Header{"Content-Type": []string{contentType}, "Content-Range": []string{contentRange}}
		if encoding != "" {
			header.Set("Content-Encoding", encoding)
		}
		return &http.Response{StatusCode: http.StatusPartialContent, Header: header, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: length}
	}
	ok := response("video/mp4", "bytes 0-15/32", "identity", mp4, 16)
	download, err := validateVideoRelayMedia(ok, request)
	require.NoError(t, err)
	require.Equal(t, int64(16), download.Length)
	require.Equal(t, "bytes 0-15/32", download.ContentRange)
	body, readErr := io.ReadAll(download.Reader)
	require.NoError(t, readErr)
	require.Equal(t, mp4, body)

	for name, res := range map[string]*http.Response{
		"incorrect length": response("video/mp4", "bytes 0-15/32", "", mp4, 15),
		"wrong range":      response("video/mp4", "bytes 10-25/32", "", mp4, 16),
		"out of bounds":    response("video/mp4", "bytes 0-15/15", "", mp4, 16),
		"compressed":       response("video/mp4", "bytes 0-15/32", "gzip", mp4, 16),
		"non-video":        response("application/json", "bytes 0-15/32", "", mp4, 16),
		"fake MP4":         response("video/mp4", "bytes 0-15/32", "", []byte("<html>fixture err"), 16),
	} {
		t.Run(name, func(t *testing.T) { _, failed := validateVideoRelayMedia(res, request); require.Error(t, failed) })
	}
	inflated := response("video/mp4", "bytes 0-15/32", "", mp4, -1)
	inflated.Uncompressed = true
	_, err = validateVideoRelayMedia(inflated, request)
	require.Error(t, err)
	_, err = parseVideoRelayRange("bytes=0-15,20-21")
	require.Error(t, err)
}
