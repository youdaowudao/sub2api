package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserCreationCompletionOAuthTokenJSON(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "unconfirmed"
		}
		t.Run(name, func(t *testing.T) {
			store := &completionExitStore{}
			if fail {
				store.err = errors.New("completion write acknowledgment lost")
			}
			ctx, recorder := completionExitContext(t, store)
			store.before = func() { require.Zero(t, recorder.Body.Len()) }
			writeOAuthTokenPairResponse(ctx, &service.TokenPair{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 60}, 71)
			require.Equal(t, []int64{71}, store.ids)
			var body map[string]any
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			if fail {
				require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
				require.Equal(t, "USER_CREATION_NOT_CONFIRMED", body["reason"])
				require.NotContains(t, body, "access_token")
				require.NotContains(t, body, "refresh_token")
			} else {
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Equal(t, "new-access", body["access_token"])
				require.Equal(t, "new-refresh", body["refresh_token"])
			}
		})
	}
}

func TestUserCreationCompletionOAuthTokenRedirect(t *testing.T) {
	for _, tc := range []struct {
		name     string
		callback string
		userID   int64
		storeErr bool
		wantSave bool
	}{
		{name: "new user", callback: "https://frontend.example.invalid/auth/callback", userID: 71, wantSave: true},
		{name: "existing user", callback: "https://frontend.example.invalid/auth/callback", userID: 72},
		{name: "invalid URL", callback: "%invalid", userID: 71},
		{name: "invalid scheme", callback: "javascript:callback", userID: 71},
		{name: "unconfirmed", callback: "https://frontend.example.invalid/auth/callback", userID: 71, storeErr: true, wantSave: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &completionExitStore{}
			if tc.storeErr {
				store.err = errors.New("completion write acknowledgment lost")
			}
			ctx, recorder := completionExitContext(t, store)
			store.before = func() { require.Zero(t, recorder.Body.Len()) }
			redirectOAuthTokenPair(ctx, tc.callback, &service.TokenPair{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 60}, "/dashboard", tc.userID)
			// Direct handler calls omit Gin Engine's final header flush; POST
			// redirects need that flush even when no response body is written.
			ctx.Writer.WriteHeaderNow()
			require.Equal(t, http.StatusFound, recorder.Code)
			if tc.wantSave {
				require.Equal(t, []int64{71}, store.ids)
			} else {
				require.Empty(t, store.ids)
			}
			location, err := url.Parse(recorder.Header().Get("Location"))
			require.NoError(t, err)
			fragment, err := url.ParseQuery(location.Fragment)
			require.NoError(t, err)
			if tc.storeErr {
				require.Equal(t, "USER_CREATION_NOT_CONFIRMED", fragment.Get("error"))
				require.Empty(t, fragment.Get("access_token"))
				require.Empty(t, fragment.Get("refresh_token"))
				require.NotContains(t, recorder.Body.String(), "new-access")
			} else if tc.name == "invalid URL" || tc.name == "invalid scheme" {
				require.Equal(t, linuxDoOAuthDefaultRedirectTo, recorder.Header().Get("Location"))
			} else {
				require.Equal(t, "new-access", fragment.Get("access_token"))
			}
		})
	}
}

func TestUserCreationCompletionDoesNotRecordOtherOAuthRedirects(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*gin.Context)
	}{
		{name: "error", run: func(ctx *gin.Context) {
			redirectOAuthError(ctx, "/auth/callback", "session_error", "registration did not complete", "")
		}},
		{name: "choice", run: func(ctx *gin.Context) {
			redirectToFrontendCallback(ctx, "/auth/callback")
		}},
		{name: "payment resume", run: func(ctx *gin.Context) {
			redirectWithFragment(ctx, "/auth/callback", url.Values{"payment_resume": {"resume-only"}})
		}},
		{name: "missing token", run: func(ctx *gin.Context) {
			redirectOAuthTokenPair(ctx, "/auth/callback", nil, "/dashboard", 71)
		}},
		{name: "choice payload at token helper", run: func(ctx *gin.Context) {
			redirectUserCreationWithFragment(ctx, "/auth/callback", url.Values{"access_token": {"unused"}, "step": {"choice"}}, 71)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &completionExitStore{}
			ctx, recorder := completionExitContext(t, store)
			tc.run(ctx)
			ctx.Writer.WriteHeaderNow()
			require.Equal(t, http.StatusFound, recorder.Code)
			require.Empty(t, store.ids, "a pending, failed, or payment flow must not confirm a creation")
		})
	}
}
