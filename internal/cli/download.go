package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os/exec"
	"slices"

	"github.com/abneribeiro/dotdl/internal/downloader"
	"github.com/abneribeiro/dotdl/internal/jobs"
	"github.com/abneribeiro/dotdl/internal/ui"
)

// kind is what a download command keeps: the video or only the audio.
type kind string

const (
	kindVideo kind = "video"
	kindAudio kind = "audio"
)

// requiredTools are the binaries every download needs: yt-dlp downloads,
// ffmpeg merges and converts, ffprobe checks the format.
var requiredTools = []string{"yt-dlp", "ffmpeg", "ffprobe"}

var videoHelp = ui.Help{
	Usage:       "dotdl video [flags] <url>...",
	Description: "download videos as MP4 (H.264 + AAC) that plays on iPhone, Android, Windows and macOS",
	Sections: []ui.HelpSection{
		{Title: "Flags", Rows: [][2]string{
			{"-q, --quality <res>", "best (default) or a maximum: 2160, 1440, 1080, 720, 480"},
			{"-o, --output <dir>", "Output folder (relative to the configured one)"},
			{"--original", "Keep the site's format (may be .webm or .mkv) instead of MP4"},
			{"--start <time>", "Trim start, MM:SS or HH:MM:SS (needs --end and a single URL)"},
			{"--end <time>", "Trim end"},
			{"-a, --batch <file>", "Read URLs from a file, one per line"},
			{"--cookies <file>", "Netscape cookies for videos behind a login or a bot check"},
		}},
		{Title: "Examples", Rows: [][2]string{
			{"dotdl video https://youtu.be/xxxx", ""},
			{"dotdl video -q 1080 -o ~/Videos https://youtube.com/playlist?list=yyyy", ""},
			{"dotdl video --start 01:30 --end 02:00 https://x.com/user/status/123", ""},
		}},
	},
	Footer: "The quality is the short side, so 720 on a vertical video gives 720x1280.\n" +
		"1080p is YouTube's sweet spot: above it videos only exist as VP9/AV1 and are converted to H.264.",
}

var audioHelp = ui.Help{
	Usage:       "dotdl audio [flags] <url>...",
	Description: "download audio as MP3 or M4A with title, artist and cover art",
	Sections: []ui.HelpSection{
		{Title: "Flags", Rows: [][2]string{
			{"-f, --format <fmt>", "mp3 (default) or m4a (AAC, better quality at the same size)"},
			{"-o, --output <dir>", "Output folder (relative to the configured one)"},
			{"--start <time>", "Trim start, MM:SS or HH:MM:SS (needs --end and a single URL)"},
			{"--end <time>", "Trim end"},
			{"-a, --batch <file>", "Read URLs from a file, one per line"},
			{"--cookies <file>", "Netscape cookies for content behind a login or a bot check"},
		}},
		{Title: "Examples", Rows: [][2]string{
			{"dotdl audio https://youtu.be/xxxx", ""},
			{"dotdl audio -f m4a -o music https://soundcloud.com/artist/track", ""},
		}},
	},
}

// parseDownload reads a video or audio command's flags and validates the
// request. outputBase is DOTDL_OUTPUT_DIR. It returns flag.ErrHelp when
// the help was asked for.
func parseDownload(k kind, args []string, outputBase string) (*jobs.Download, error) {
	var in jobs.DownloadInput
	var original bool

	fs := newFlagSet(string(k))
	stringFlag(fs, &in.OutputDir, "o,output")
	stringFlag(fs, &in.BatchFile, "a,batch")
	stringFlag(fs, &in.Cookies, "cookies")
	stringFlag(fs, &in.Start, "start")
	stringFlag(fs, &in.End, "end")
	if k == kindVideo {
		stringFlag(fs, &in.Quality, "q,quality")
		boolFlag(fs, &original, "original")
	} else {
		stringFlag(fs, &in.Format, "f,format")
	}

	urls, err := parseInterspersed(fs, args)
	if err != nil {
		return nil, err
	}
	in.URLs = urls

	switch {
	case k == kindVideo && original:
		in.Format = jobs.FormatOriginal
	case k == kindVideo:
		in.Format = jobs.FormatMP4
	case in.Format == "":
		in.Format = jobs.FormatMP3
	case !slices.Contains(jobs.AudioFormats, in.Format):
		return nil, fmt.Errorf("invalid format %q (use mp3 or m4a)", in.Format)
	}
	return jobs.NewDownload(in, outputBase)
}

// download runs the video or audio command.
func (a *App) download(ctx context.Context, k kind, args []string) int {
	d, err := parseDownload(k, args, a.Config.OutputDir)
	if errors.Is(err, flag.ErrHelp) {
		ui.PrintHelp(a.Stdout, commandHelp[string(k)])
		return ExitOK
	}
	if err != nil {
		ui.PrintUsageError(a.Stderr, err, "dotdl help "+string(k))
		return ExitUsage
	}
	for _, tool := range requiredTools {
		if _, err := exec.LookPath(tool); err != nil {
			ui.PrintError(a.Stderr, fmt.Errorf("%s is not installed (run dotdl doctor to see what is missing)", tool))
			return ExitFailure
		}
	}

	env := jobs.EnvFrom(a.Config)
	if env.JSRuntimes == "" {
		env.JSRuntimes = pickJSRuntime(exec.LookPath)
	}
	run := func(ctx context.Context, report jobs.Reporter) (jobs.Result, error) {
		return d.Run(ctx, env, report)
	}
	var res jobs.Result
	if a.Interactive {
		res, err = ui.RunWithProgress(ctx, a.Stdout, title(d), run)
	} else {
		res, err = ui.RunPlain(ctx, a.Stdout, run)
	}

	// Describe runs after the job, even when it was canceled: it only reads
	// the finished files.
	describe := func(path string) string { return downloader.Describe(context.Background(), path) }
	switch {
	case errors.Is(err, context.Canceled):
		if len(res.Files) > 0 {
			ui.PrintResult(a.Stdout, res, describe)
		}
		ui.PrintCanceled(a.Stderr)
		return ExitInterrupted
	case err != nil && !errors.Is(err, jobs.ErrPartial):
		ui.PrintError(a.Stderr, err)
		return ExitFailure
	}
	ui.PrintResult(a.Stdout, res, describe)
	if err != nil {
		ui.PrintError(a.Stderr, err)
		return ExitFailure
	}
	return ExitOK
}

// title is the progress view's header: the command and what was asked.
func title(d *jobs.Download) string {
	switch {
	case d.Audio():
		return "audio · " + d.Format
	case d.Format == jobs.FormatOriginal:
		return "video · original format"
	case d.Height > 0:
		return fmt.Sprintf("video · up to %dp", d.Height)
	}
	return "video · best quality"
}

// pickJSRuntime returns the --js-runtimes value for yt-dlp. Deno is yt-dlp's
// default and needs no flag (""); node is only used when asked for, so it
// is enabled when it is the only one. Without either, dotdl doctor reports
// it.
func pickJSRuntime(lookPath func(string) (string, error)) string {
	if _, err := lookPath("deno"); err == nil {
		return ""
	}
	if _, err := lookPath("node"); err == nil {
		return "node"
	}
	return ""
}
