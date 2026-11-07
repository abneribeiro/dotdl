package downloader

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Phase is what yt-dlp is doing.
type Phase int

const (
	// PhaseInfo: fetching the video information, before downloading.
	PhaseInfo Phase = iota
	// PhaseDownload: downloading a file (video, audio or one format).
	PhaseDownload
	// PhaseMerge: merging video and audio with ffmpeg.
	PhaseMerge
	// PhaseExtract: extracting or converting the audio (audio mode).
	PhaseExtract
)

// Progress is a yt-dlp event: a download progress line or a phase change.
type Progress struct {
	Phase Phase
	// Downloaded and Total in bytes; Total is 0 when the site does not say.
	Downloaded int64
	Total      int64
	// Speed in bytes/s (0 = unknown) and ETA (-1 = unknown).
	Speed float64
	ETA   time.Duration
	// File is the file yt-dlp is working on, when the line says so.
	File string
	// Index and Count: position in a playlist (0 outside playlists).
	Index int
	Count int
}

// Fraction returns the progress between 0 and 1, or -1 when the total is unknown.
func (p Progress) Fraction() float64 {
	if p.Total <= 0 {
		return -1
	}
	return min(float64(p.Downloaded)/float64(p.Total), 1)
}

// progressMarker tags the --progress-template lines so they are not mistaken
// for the rest of the output.
const progressMarker = "dotdl-progress"

// progressTemplate makes yt-dlp print an easy-to-parse line on every update:
// bytes, total, estimated total, speed and ETA ("NA" when unknown).
const progressTemplate = "download:" + progressMarker +
	" %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s" +
	" %(progress.speed)s %(progress.eta)s"

var (
	destinationLine = regexp.MustCompile(`^\[download\] Destination: (.+)$`)
	alreadyLine     = regexp.MustCompile(`^\[download\] (.+) has already been (?:downloaded|recorded in the archive)`)
	itemLine        = regexp.MustCompile(`^\[download\] Downloading item (\d+) of (\d+)`)
	mergeLine       = regexp.MustCompile(`^\[Merger\] Merging formats into "(.+)"$`)
	extractLine     = regexp.MustCompile(`^\[ExtractAudio\] Destination: (.+)$`)
)

// ParseLine reads one line of yt-dlp's stdout. It returns ok=false for lines
// that are neither progress nor a phase change.
func ParseLine(line string) (Progress, bool) {
	line = strings.TrimSpace(line)

	if rest, ok := strings.CutPrefix(line, progressMarker+" "); ok {
		f := strings.Fields(rest)
		if len(f) != 5 {
			return Progress{}, false
		}
		p := Progress{Phase: PhaseDownload, ETA: -1}
		p.Downloaded = int64(parseNum(f[0]))
		p.Total = int64(parseNum(f[1]))
		if p.Total <= 0 {
			// Sites with fragmented streams (HLS/DASH) only give an estimate.
			p.Total = int64(parseNum(f[2]))
		}
		p.Speed = parseNum(f[3])
		if eta := parseNum(f[4]); eta >= 0 && f[4] != "NA" {
			p.ETA = time.Duration(eta * float64(time.Second))
		}
		return p, true
	}

	if m := destinationLine.FindStringSubmatch(line); m != nil {
		return Progress{Phase: PhaseDownload, File: m[1], ETA: -1}, true
	}
	if m := alreadyLine.FindStringSubmatch(line); m != nil {
		return Progress{Phase: PhaseDownload, File: m[1], ETA: -1}, true
	}
	if m := itemLine.FindStringSubmatch(line); m != nil {
		index, _ := strconv.Atoi(m[1])
		count, _ := strconv.Atoi(m[2])
		return Progress{Phase: PhaseInfo, Index: index, Count: count, ETA: -1}, true
	}
	if m := mergeLine.FindStringSubmatch(line); m != nil {
		return Progress{Phase: PhaseMerge, File: m[1], ETA: -1}, true
	}
	if m := extractLine.FindStringSubmatch(line); m != nil {
		return Progress{Phase: PhaseExtract, File: m[1], ETA: -1}, true
	}
	return Progress{}, false
}

// parseNum reads a number from the template; "NA" or junk gives 0.
func parseNum(s string) float64 {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return n
}
