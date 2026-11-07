package middleware

import (
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/gin-gonic/gin"
)

// Recovery converts panics into the project's standard JSON error envelope.
//
// It preserves Gin's broken-pipe handling by not attempting to write a response
// when the client connection is already gone.
func Recovery() gin.HandlerFunc {
	writer := gin.DefaultErrorWriter
	if writer != nil {
		writer = pluginAssetRedactingWriter{writer: writer}
	}
	return gin.CustomRecoveryWithWriter(writer, func(c *gin.Context, recovered any) {
		recoveredErr, _ := recovered.(error)

		if isBrokenPipe(recoveredErr) {
			if recoveredErr != nil {
				_ = c.Error(recoveredErr)
			}
			c.Abort()
			return
		}

		if c.Writer.Written() {
			c.Abort()
			return
		}

		response.ErrorWithDetails(
			c,
			http.StatusInternalServerError,
			infraerrors.UnknownMessage,
			infraerrors.UnknownReason,
			nil,
		)
		c.Abort()
	})
}

type pluginAssetRedactingWriter struct {
	writer io.Writer
}

func (w pluginAssetRedactingWriter) Write(input []byte) (int, error) {
	redacted := []byte(logredact.RedactPluginAssetPaths(string(input)))
	n, err := w.writer.Write(redacted)
	if n != len(redacted) {
		if err == nil {
			err = io.ErrShortWrite
		}
		return 0, err
	}
	return len(input), err
}

func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}

	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return false
	}

	var syscallErr *os.SyscallError
	if !errors.As(opErr.Err, &syscallErr) {
		return false
	}

	msg := strings.ToLower(syscallErr.Error())
	return strings.Contains(msg, "broken pipe") || strings.Contains(msg, "connection reset by peer")
}
