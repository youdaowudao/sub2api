package httputil

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
)

// 保留原英文提示，兼容按原文检索的运维记录和客户端。
const (
	requestBodyReadFailure         = "Failed to read request body"
	requestBodyReadFailureTooLarge = requestBodyReadFailure + ": 请求体太大"
)

// RequestBodyReadErrorKind 把读取请求体的错误归为固定类别，不包含请求内容。
// 解压错误会包装底层的 io.ErrUnexpectedEOF，必须先于截断判断。
func RequestBodyReadErrorKind(err error) string {
	if err == nil {
		return "none"
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return "max_bytes"
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "decode content-encoding") {
		if strings.Contains(lower, "unsupported content-encoding") {
			return "unsupported_content_encoding"
		}
		return "decode_content_encoding"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return "client_disconnect"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "truncated_body"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "transport"
	}
	return "io_read"
}

// RequestBodyReadFailureMessage 返回读取请求体失败（超出大小上限除外，调用方另回 413）
// 时回给客户端的提示。上传中断（多因请求体过大）时补一句中文，其他原因保持原文。
func RequestBodyReadFailureMessage(err error) string {
	switch RequestBodyReadErrorKind(err) {
	case "client_disconnect", "truncated_body", "transport":
		return requestBodyReadFailureTooLarge
	default:
		return requestBodyReadFailure
	}
}
