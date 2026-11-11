package downloader

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// timePattern accepts the time formats yt-dlp and ffmpeg take: MM:SS (01:30)
// or HH:MM:SS (00:01:30).
var timePattern = regexp.MustCompile(`^(\d{1,2}:)?\d{1,2}:\d{2}$`)

// ValidateTime checks that a time is MM:SS or HH:MM:SS.
func ValidateTime(s string) error {
	if !timePattern.MatchString(s) {
		return fmt.Errorf("invalid time %q (use MM:SS or HH:MM:SS)", s)
	}
	// Seconds, and minutes when there are hours, are clock fields: yt-dlp
	// would take 01:75 as 2:15. The minutes of an MM:SS stay free, since
	// 75:00 is a valid way to say 1h15.
	for _, p := range strings.Split(s, ":")[1:] {
		if n, _ := strconv.Atoi(p); n >= 60 {
			return fmt.Errorf("invalid time %q (minutes and seconds go from 00 to 59)", s)
		}
	}
	return nil
}

// ValidateURL checks that the URL is absolute, http or https, with a host.
func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid URL %q: it must start with http:// or https://", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("invalid URL %q: missing host", raw)
	}
	return nil
}

// parseTime turns an already validated time into seconds.
func parseTime(s string) int {
	total := 0
	for p := range strings.SplitSeq(s, ":") {
		n, _ := strconv.Atoi(p)
		total = total*60 + n
	}
	return total
}

// ValidateRange checks both times and that the start comes before the end:
// yt-dlp turns a reversed trim into an empty file.
func ValidateRange(start, end string) error {
	if err := ValidateTime(start); err != nil {
		return err
	}
	if err := ValidateTime(end); err != nil {
		return err
	}
	if parseTime(start) >= parseTime(end) {
		return fmt.Errorf("the start (%s) must come before the end (%s)", start, end)
	}
	return nil
}
