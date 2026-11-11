package downloader

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ErrorKind classifies the known yt-dlp failures, so callers can react to
// them without matching strings.
type ErrorKind int

const (
	ErrUnknown ErrorKind = iota
	ErrLiveNoDVR
	ErrPrivate
	ErrUnavailable
	ErrUnsupportedURL

	// ErrLoginRequired: the site wants a session, which cookies provide.
	ErrLoginRequired

	// ErrRateLimited: the site answered 429, too many requests from this IP.
	ErrRateLimited

	// ErrCanceled: the download was canceled (Ctrl+C). Not a failure.
	ErrCanceled
)

// maxDetailLen caps the yt-dlp error line shown to the user.
const maxDetailLen = 300

// DownloadError wraps a download failure with its classification.
type DownloadError struct {
	Kind   ErrorKind
	Output string
	Err    error
}

func (e *DownloadError) Error() string {
	switch e.Kind {
	case ErrLiveNoDVR:
		return "this live stream has no DVR or has not started yet, so it cannot be trimmed"
	case ErrPrivate:
		return "the video is private"
	case ErrUnavailable:
		return "the video is unavailable or was removed"
	case ErrUnsupportedURL:
		return "yt-dlp does not support this URL"
	case ErrLoginRequired:
		return "the site asked for a login or a bot check"
	case ErrRateLimited:
		return "the site is rate limiting requests (HTTP 429)"
	case ErrCanceled:
		return "download canceled"
	}
	if detail := e.Detail(); detail != "" {
		return detail
	}
	if e.Err != nil {
		return fmt.Sprintf("yt-dlp failed: %v", e.Err)
	}
	return "the download failed"
}

func (e *DownloadError) Unwrap() error { return e.Err }

// Hint suggests how to fix the failure, or "" when there is nothing to
// suggest.
func (e *DownloadError) Hint() string {
	switch e.Kind {
	case ErrLoginRequired:
		return "export cookies from a browser where you are logged in and pass them with --cookies " +
			"(or set them once with: dotdl config cookies <file>)"
	case ErrRateLimited:
		return "wait a few minutes, or pass cookies with --cookies"
	case ErrUnknown:
		return "if a site that used to work stopped working, yt-dlp is probably outdated (dotdl doctor)"
	}
	return ""
}

// Detail returns yt-dlp's last "ERROR:" line, without the prefix and capped
// at maxDetailLen characters: the real reason for an unclassified failure,
// which the user needs instead of "exit status 1". It is empty when the
// output has none.
func (e *DownloadError) Detail() string {
	lines := strings.Split(e.Output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), "ERROR:"); ok {
			return truncateRunes(strings.TrimSpace(rest), maxDetailLen)
		}
	}
	return ""
}

// truncateRunes cuts s to n characters (not bytes, so a multi-byte character
// is never split), ending with "…" when it cuts.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

// classify turns yt-dlp's output and the run and context errors into a
// DownloadError with the right Kind.
func classify(output string, ctxErr, runErr error) *DownloadError {
	lower := strings.ToLower(output)
	kind := ErrUnknown

	switch {
	case errors.Is(ctxErr, context.Canceled):
		kind = ErrCanceled
	case strings.Contains(lower, "does not allow dvr"),
		strings.Contains(lower, "is not a video"),
		strings.Contains(lower, "this live event will begin"):
		kind = ErrLiveNoDVR
	// Before the login checks: YouTube's private-video message also says
	// "Sign in if you've been granted access".
	case strings.Contains(lower, "private video"):
		kind = ErrPrivate
	case strings.Contains(lower, "sign in to confirm"),
		strings.Contains(lower, "login required"),
		strings.Contains(lower, "cookies for the authentication"):
		kind = ErrLoginRequired
	case strings.Contains(lower, "http error 429"),
		strings.Contains(lower, "too many requests"):
		kind = ErrRateLimited
	case strings.Contains(lower, "video unavailable"):
		kind = ErrUnavailable
	case strings.Contains(lower, "unsupported url"):
		kind = ErrUnsupportedURL
	}

	return &DownloadError{Kind: kind, Output: output, Err: runErr}
}
