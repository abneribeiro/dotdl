package downloader

import (
	"fmt"
	"strconv"
)

// Section is a trim with start and end times as MM:SS or HH:MM:SS, already
// validated by the caller (see ValidateRange).
type Section struct {
	Start string
	End   string
}

// Options describes one yt-dlp invocation (see BuildArgs). Empty fields omit
// their flag.
type Options struct {
	// URLs are passed as the last arguments.
	URLs []string
	// BatchFile is a file with one URL per line (--batch-file).
	BatchFile string
	// OutputTemplate is the full -o value.
	OutputTemplate string
	// Format is the -f value.
	Format string
	// FormatSort is the -S value (see QualitySort and MP4Sort).
	FormatSort string
	// ExtractAudio keeps only the audio, in that format ("mp3", "m4a").
	ExtractAudio string
	// EmbedMetadata embeds title, artist and cover art.
	EmbedMetadata bool
	// CookiesFile is a Netscape cookies file (--cookies).
	CookiesFile string
	// JSRuntimes is the --js-runtimes value (e.g. "node").
	JSRuntimes string
	// NoPlaylist downloads only the video when the link points at a video
	// inside a playlist.
	NoPlaylist bool
	// Section trims the download (--download-sections).
	Section *Section
	// MergeFormat forces the output container (e.g. "mp4").
	MergeFormat string
	// PrintToFile makes yt-dlp append each final file path to that file.
	PrintToFile string
	// ArchiveFile records the downloaded videos, which later runs skip
	// (--download-archive).
	ArchiveFile string
	// Continue resumes the .part files of interrupted runs.
	Continue bool
	// IgnoreErrors keeps going through a playlist when one video fails.
	IgnoreErrors bool
	// ConcurrentFragments downloads that many fragments of an HLS/DASH
	// stream at once.
	ConcurrentFragments int

	// Progress receives yt-dlp's progress and phase changes (see ParseLine).
	Progress func(Progress)
	// Log receives every other line yt-dlp prints, on stdout and stderr.
	Log func(line string)
}

// QualitySort turns a requested quality into yt-dlp's -S and the resolution
// asked for. "" or "best" return "" and 0 (yt-dlp already defaults to the
// best quality). An integer such as "720" returns "res:720", a cap with a
// fallback: the highest resolution not above 720 and, only when there is
// none, the smallest one above.
//
// yt-dlp's "res" is the SHORT side of the video, which is why there is no
// -f [height<=N] filter: a vertical 720p video (Shorts, TikTok, Reels) is
// 720x1280, and filtering on height would deliver 360x640 instead. Sites
// that do not expose the resolution still work: the field just does not
// break ties.
func QualitySort(q string) (sort string, height int, err error) {
	if q == "" || q == "best" {
		return "", 0, nil
	}
	n, err := strconv.Atoi(q)
	if err != nil || n < 1 {
		return "", 0, fmt.Errorf("invalid quality %q (use \"best\" or a resolution such as 1080 or 720)", q)
	}
	return fmt.Sprintf("res:%d", n), n, nil
}

// mp4CodecSort makes yt-dlp prefer SDR and H.264 + AAC. hdr:sdr is there
// because yt-dlp prefers HDR by default, and HDR converted to 8-bit H.264
// looks washed out.
const mp4CodecSort = "hdr:sdr,vcodec:h264,acodec:aac"

// MP4Sort adds the codec preference to a resolution sort; an empty resSort
// means the highest resolution available.
//
// The resolution comes first: with vcodec first, a 480p H.264 would beat a
// 2160p AV1 and the requested quality would be silently ignored (EnsureMP4
// converts the AV1 afterwards). As a tie-breaker, vcodec picks the H.264 of
// that size, which needs no re-encoding.
func MP4Sort(resSort string) string {
	if resSort == "" {
		resSort = "res"
	}
	return resSort + "," + mp4CodecSort
}

// BuildArgs builds yt-dlp's argument list from the options.
func BuildArgs(o Options) []string {
	var args []string

	if o.Format != "" {
		args = append(args, "-f", o.Format)
	}
	if o.FormatSort != "" {
		args = append(args, "-S", o.FormatSort)
	}
	if o.MergeFormat != "" {
		args = append(args, "--merge-output-format", o.MergeFormat)
	}
	if o.ExtractAudio != "" {
		// --audio-quality 0 is the codec's best VBR setting.
		args = append(args, "-x", "--audio-format", o.ExtractAudio, "--audio-quality", "0")
	}
	if o.EmbedMetadata {
		args = append(args, "--embed-metadata", "--embed-thumbnail")
	}
	if o.CookiesFile != "" {
		args = append(args, "--cookies", o.CookiesFile)
	}
	if o.JSRuntimes != "" {
		args = append(args, "--js-runtimes", o.JSRuntimes)
	}
	if o.NoPlaylist {
		args = append(args, "--no-playlist")
	}
	if o.OutputTemplate != "" {
		args = append(args, "-o", o.OutputTemplate)
	}
	if o.ArchiveFile != "" {
		args = append(args, "--download-archive", o.ArchiveFile)
	}
	if o.Continue {
		args = append(args, "--continue")
	}
	if o.IgnoreErrors {
		args = append(args, "--ignore-errors")
	}
	if o.ConcurrentFragments > 0 {
		args = append(args, "--concurrent-fragments", strconv.Itoa(o.ConcurrentFragments))
	}
	// One line per progress update instead of "\r", for ParseLine.
	args = append(args, "--newline")
	if o.Progress != nil {
		args = append(args, "--progress-template", progressTemplate)
	}
	if o.BatchFile != "" {
		args = append(args, "--batch-file", o.BatchFile)
	}
	if o.Section != nil {
		section := fmt.Sprintf("*%s-%s", o.Section.Start, o.Section.End)
		args = append(args, "--download-sections", section, "--force-keyframes-at-cuts")
	}
	// --print-to-file implies --simulate: without --no-simulate yt-dlp would
	// write the path and download nothing.
	if o.PrintToFile != "" {
		args = append(args, "--print-to-file", "after_move:filepath", o.PrintToFile, "--no-simulate")
	}

	args = append(args, o.URLs...)
	return args
}
