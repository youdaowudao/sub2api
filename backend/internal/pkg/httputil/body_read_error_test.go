package httputil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestBodyReadErrorKind(t *testing.T) {
	require.Equal(t, "none", RequestBodyReadErrorKind(nil))
	require.Equal(t, "unsupported_content_encoding", RequestBodyReadErrorKind(errors.New(`decode Content-Encoding "br": unsupported Content-Encoding`)))
	require.Equal(t, "decode_content_encoding", RequestBodyReadErrorKind(fmt.Errorf("decode Content-Encoding %q: %w", "gzip", io.ErrUnexpectedEOF)))
	require.Equal(t, "truncated_body", RequestBodyReadErrorKind(io.ErrUnexpectedEOF))
	require.Equal(t, "client_disconnect", RequestBodyReadErrorKind(context.Canceled))
	require.Equal(t, "client_disconnect", RequestBodyReadErrorKind(fmt.Errorf("read: %w", syscall.ECONNRESET)))
	require.Equal(t, "max_bytes", RequestBodyReadErrorKind(&http.MaxBytesError{Limit: 10}))
	require.Equal(t, "io_read", RequestBodyReadErrorKind(errors.New("malformed chunked encoding")))
}

func TestRequestBodyReadFailureMessage(t *testing.T) {
	disconnected := []error{
		io.ErrUnexpectedEOF,
		context.Canceled,
		fmt.Errorf("read: %w", syscall.ECONNRESET),
	}
	for _, err := range disconnected {
		require.Equal(t, "Failed to read request body: 请求体太大", RequestBodyReadFailureMessage(err))
	}

	// 解压错误包装了 io.ErrUnexpectedEOF，也不能说成请求体太大；其他原因保持原文，不回显原始错误。
	others := []error{
		fmt.Errorf("decode Content-Encoding %q: %w", "gzip", io.ErrUnexpectedEOF),
		fmt.Errorf("decode Content-Encoding %q: %w", "br", errors.New("unsupported Content-Encoding")),
		errors.New("malformed chunked encoding"),
	}
	for _, err := range others {
		require.Equal(t, "Failed to read request body", RequestBodyReadFailureMessage(err))
	}
}
