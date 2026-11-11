package jobs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/abneribeiro/dotdl/internal/config"
	"github.com/abneribeiro/dotdl/internal/downloader"
)

const (
	// archiveFileName records the videos already downloaded into the output
	// folder; repeating a request skips them and carries on with the rest.
	archiveFileName = ".dotdl-archive.txt"

	// audioArchiveFileName is the archive for audio-only downloads. It is a
	// separate file because the archive records the video ID, not the
	// format: sharing one would skip the MP3 of a video saved as MP4.
	audioArchiveFileName = ".dotdl-archive-audio.txt"

	// pendingFileName lists the files yt-dlp finished that have not been
	// through the MP4 conversion yet. It lives in the output folder, not in a
	// temp file, because the archive records a video BEFORE the conversion:
	// after a Ctrl+C in between, the next run skips the video and only this
	// list remembers its .webm. It is deleted once everything is converted.
	pendingFileName = ".dotdl-pending.txt"

	// maxH264Height is the resolution above which the big sites (YouTube in
	// particular) stop offering H.264: past it, an MP4 means re-encoding.
	maxH264Height = 1080

	// maxTitleBytes caps the title in a file name. Captions used as titles
	// (Instagram, TikTok, X) run past the 255 bytes a file name can have.
	maxTitleBytes = 150

	// concurrentFragments speeds up the sites that serve a video in pieces
	// (HLS/DASH).
	concurrentFragments = 4

	// audioFormat is the best audio. nativeAACFormat prefers a stream that
	// is already AAC, which becomes an M4A without re-encoding.
	audioFormat     = "ba/b"
	nativeAACFormat = "ba[acodec^=mp4a]/" + audioFormat
)

// Formats a Download accepts.
const (
	FormatMP4      = "mp4"
	FormatOriginal = "original"
	FormatMP3      = "mp3"
	FormatM4A      = "m4a"
)

// AudioFormats are the audio-only formats.
var AudioFormats = []string{FormatMP3, FormatM4A}

// DownloadInput is a download request as the user gave it, before validation.
type DownloadInput struct {
	URLs      []string
	BatchFile string
	// OutputDir is the -o value: empty uses the base folder, a relative path
	// goes inside it.
	OutputDir string
	// Quality is "best" (or empty) or the maximum resolution, e.g. "720".
	Quality string
	// Format: mp4 (default), original, mp3 or m4a.
	Format  string
	Start   string
	End     string
	Cookies string
}

// Download is a validated request, ready to run.
type Download struct {
	URLs       []string
	BatchFile  string
	OutputDir  string
	Format     string
	FormatSort string
	// Height is the requested resolution, 0 for "best".
	Height  int
	Section *downloader.Section
	Cookies string
}

// Audio reports whether the request keeps only the audio.
func (d *Download) Audio() bool {
	return slices.Contains(AudioFormats, d.Format)
}

// NewDownload validates the request. outputBase is DOTDL_OUTPUT_DIR.
func NewDownload(in DownloadInput, outputBase string) (*Download, error) {
	d := &Download{URLs: in.URLs, BatchFile: in.BatchFile, Cookies: in.Cookies}

	switch in.Format {
	case "", FormatMP4:
		d.Format = FormatMP4
	case FormatOriginal, FormatMP3, FormatM4A:
		d.Format = in.Format
	default:
		return nil, fmt.Errorf("invalid format %q (use mp3 or m4a)", in.Format)
	}

	var err error
	if d.FormatSort, d.Height, err = downloader.QualitySort(in.Quality); err != nil {
		return nil, err
	}
	if d.FormatSort != "" && d.Audio() {
		return nil, fmt.Errorf("the quality picks the video resolution and does not apply to %s", d.Format)
	}

	if d.OutputDir, err = ResolveOutputDir(in.OutputDir, outputBase); err != nil {
		return nil, err
	}

	if len(d.URLs) == 0 && d.BatchFile == "" {
		return nil, errors.New("give at least one URL or a batch file")
	}

	if (in.Start != "") != (in.End != "") {
		return nil, errors.New("a trim needs both --start and --end")
	}
	if in.Start != "" {
		if d.BatchFile != "" {
			return nil, errors.New("a trim cannot be combined with a batch file")
		}
		if len(d.URLs) != 1 {
			return nil, errors.New("a trim takes exactly one URL")
		}
		if err := downloader.ValidateRange(in.Start, in.End); err != nil {
			return nil, fmt.Errorf("invalid trim: %w", err)
		}
		d.Section = &downloader.Section{Start: in.Start, End: in.End}
	}

	for _, u := range d.URLs {
		if err := downloader.ValidateURL(u); err != nil {
			return nil, err
		}
	}
	if d.BatchFile != "" {
		if err := validateBatchFile(d.BatchFile); err != nil {
			return nil, err
		}
	}
	if d.Cookies != "" {
		if err := config.CheckCookiesFile(d.Cookies); err != nil {
			return nil, fmt.Errorf("cookies: %w", err)
		}
	}
	return d, nil
}

// ResolveOutputDir applies the base folder (DOTDL_OUTPUT_DIR) to an -o
// value: empty uses the base, a relative path goes inside it, an absolute
// path ignores it. Without a base, a relative -o is relative to the current
// folder and no -o means the current folder. A leading "~" is expanded.
func ResolveOutputDir(dir, base string) (string, error) {
	dir, err := config.ExpandHome(dir, os.Getenv)
	if err != nil {
		return "", err
	}
	if dir == "" {
		dir = base
	} else if base != "" && !filepath.IsAbs(dir) {
		dir = filepath.Join(base, dir)
	}
	if dir != "" {
		return dir, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine the current folder: %w", err)
	}
	return wd, nil
}

// outputTemplate returns yt-dlp's -o. A trim puts the interval in the name:
// with the full video's name, yt-dlp would find that file and answer
// "already downloaded" instead of trimming.
func outputTemplate(dir string, sec *downloader.Section) string {
	name := fmt.Sprintf("%%(title).%dB [%%(id)s]", maxTitleBytes)
	if sec != nil {
		name += fmt.Sprintf(" (%s-%s)", clockForFilename(sec.Start), clockForFilename(sec.End))
	}
	return filepath.Join(dir, name+".%(ext)s")
}

// clockForFilename swaps ":" for ".": Windows and macOS Finder do not accept
// colons in file names.
func clockForFilename(t string) string {
	return strings.ReplaceAll(t, ":", ".")
}

// validateBatchFile checks that the batch file exists and that every line
// is a valid URL. Empty lines and comments (#) are skipped, as yt-dlp does.
func validateBatchFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("batch file: %w", err)
	}
	defer f.Close()

	urls := 0
	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if err := downloader.ValidateURL(text); err != nil {
			return fmt.Errorf("%s, line %d: %w", path, line, err)
		}
		urls++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if urls == 0 {
		return fmt.Errorf("%s has no URLs", path)
	}
	return nil
}

// reporterSink adapts a Reporter to the downloader's callbacks, in one place.
type reporterSink struct {
	mu     sync.Mutex
	report Reporter
}

func (r *reporterSink) send(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report(e)
}

func (r *reporterSink) log(line string) { r.send(Event{Log: line}) }

func (r *reporterSink) note(format string, args ...any) {
	r.send(Event{Note: fmt.Sprintf(format, args...)})
}

// progress turns a downloader.Progress into an Event.
func (r *reporterSink) progress(p downloader.Progress) {
	e := Event{Index: p.Index, Count: p.Count}
	if p.File != "" {
		e.Item = filepath.Base(p.File)
	}
	switch p.Phase {
	case downloader.PhaseInfo:
		e.Stage = StageInfo
	case downloader.PhaseDownload:
		e.Stage = StageDownload
		// A "Destination" line only announces the file; the progress comes in
		// the template lines, which always have Downloaded or Total.
		if p.File == "" {
			e.Progress, e.Fraction, e.Speed, e.ETA = true, p.Fraction(), p.Speed, p.ETA
		}
	case downloader.PhaseMerge:
		e.Stage, e.Progress, e.Fraction = StageMerge, true, -1
	case downloader.PhaseExtract:
		e.Stage, e.Progress, e.Fraction = StageExtract, true, -1
	}
	r.send(e)
}

// Run executes the download. It sets no timeout, since big playlists can
// take hours; canceling the context sends SIGINT to yt-dlp and the .part
// files stay ready to resume on the next run. A canceled context returns
// context.Canceled; partly failed items return ErrPartial with the Result of
// what was done.
func (d *Download) Run(ctx context.Context, env Env, report Reporter) (Result, error) {
	res := Result{OutputDir: d.OutputDir}
	sink := &reporterSink{report: report}

	if err := PrepareOutputDir(d.OutputDir); err != nil {
		return res, err
	}
	cookies := d.Cookies
	if cookies == "" {
		cookies = env.CookiesFile
	}

	opts := downloader.Options{
		URLs:           d.URLs,
		BatchFile:      d.BatchFile,
		FormatSort:     d.FormatSort,
		OutputTemplate: outputTemplate(d.OutputDir, d.Section),
		Continue:       true,
		IgnoreErrors:   true,
		CookiesFile:    cookies,
		JSRuntimes:     env.JSRuntimes,
		Progress:       sink.progress,
		Log:            sink.log,

		ConcurrentFragments: concurrentFragments,
	}

	archive := archiveFileName
	switch {
	case d.Audio():
		opts.Format = audioFormat
		if d.Format == FormatM4A {
			opts.Format = nativeAACFormat
		}
		opts.ExtractAudio = d.Format
		opts.EmbedMetadata = true
		archive = audioArchiveFileName

	case d.Format == FormatMP4:
		opts.MergeFormat = "mp4"
		opts.FormatSort = downloader.MP4Sort(d.FormatSort)
		if d.Height > maxH264Height {
			sink.note("above %dp most sites only serve AV1/VP9, so the video will be converted to H.264, "+
				"which takes a while. %dp downloads directly.", maxH264Height, maxH264Height)
		}
	}

	// yt-dlp appends one line per finished file here, playlists included.
	// In MP4 mode it is the persistent pending list; otherwise a temp file.
	if d.Format == FormatMP4 {
		opts.PrintToFile = filepath.Join(d.OutputDir, pendingFileName)
	} else {
		tmp, err := os.CreateTemp(env.TempDir, "dotdl-files-*.txt")
		if err != nil {
			return res, fmt.Errorf("cannot create the file list: %w", err)
		}
		tmp.Close()
		defer os.Remove(tmp.Name())
		opts.PrintToFile = tmp.Name()
	}

	if d.Section != nil {
		// A trim applies to a single video (never a whole playlist) and does
		// not use the archive: a trim must not mark the full video as done.
		opts.Section = d.Section
		opts.NoPlaylist = true
	} else {
		opts.ArchiveFile = filepath.Join(d.OutputDir, archive)
	}

	sink.send(Event{Stage: StageInfo, Progress: true, Fraction: -1})

	// The archive gains a line per successfully downloaded video: comparing
	// before and after tells "the whole playlist failed" from "one private
	// video in the middle", which give the same exit code with
	// --ignore-errors.
	before := countArchive(opts.ArchiveFile)
	runErr := downloader.Run(ctx, opts)

	var convErr error
	if d.Format == FormatMP4 {
		// The conversion runs even when yt-dlp failed: a playlist may have
		// downloaded almost everything before tripping on a private video.
		// It also handles what an interrupted run left pending.
		res.Files, res.Converted, convErr = ensurePlayable(ctx, opts.PrintToFile, sink)
	} else {
		res.Files = printedPaths(opts.PrintToFile)
	}

	if ctx.Err() != nil {
		return res, context.Canceled
	}
	if runErr != nil {
		if countArchive(opts.ArchiveFile)-before > 0 || len(res.Files) > 0 {
			return res, fmt.Errorf("%w: %w", ErrPartial, runErr)
		}
		return res, runErr
	}
	return res, convErr
}

// ensurePlayable converts the files listed in pathFile to MP4/H.264/AAC
// (see downloader.EnsureMP4) and returns the final paths, plus the files
// that needed a re-encode mapped to their source codec. When everything is
// converted it deletes the list; after failures or an interruption it keeps
// it, and the next run starts there.
func ensurePlayable(ctx context.Context, pathFile string, sink *reporterSink) ([]string, map[string]string, error) {
	paths := printedPaths(pathFile)
	var files []string
	converted := make(map[string]string)
	var failed int
	for i, path := range paths {
		if ctx.Err() != nil {
			sink.note("conversion interrupted: %s and the next ones convert on the next run", filepath.Base(path))
			return files, converted, context.Canceled
		}
		var source string
		final, err := downloader.EnsureMP4(ctx, path, downloader.ConvertHooks{
			Start: func(reencode bool, vcodec string) {
				msg := "remuxing to MP4 (lossless)"
				if reencode {
					source = vcodec
					msg = fmt.Sprintf("converting %s to H.264, this can take a few minutes", strings.ToUpper(vcodec))
				}
				sink.send(Event{Stage: StageConvert, Item: filepath.Base(path), Index: i + 1, Count: len(paths),
					Progress: true, Fraction: 0, ETA: -1, Note: filepath.Base(path) + ": " + msg})
			},
			Progress: func(f float64) {
				sink.send(Event{Progress: true, Fraction: f, ETA: -1})
			},
		})
		if err != nil {
			if ctx.Err() != nil {
				return files, converted, context.Canceled
			}
			sink.log("WARNING: " + err.Error())
			failed++
			files = append(files, path)
			continue
		}
		if source != "" {
			converted[final] = source
		}
		files = append(files, final)
	}

	if failed > 0 {
		// The download itself worked: the files exist, just not as MP4.
		return files, converted, fmt.Errorf("%w: %d of %d file(s) could not be converted to MP4 "+
			"(listed in %s and retried on the next run)", ErrPartial, failed, len(paths), pendingFileName)
	}
	if err := os.Remove(pathFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return files, converted, fmt.Errorf("cannot clean up %s: %w", pathFile, err)
	}
	return files, converted, nil
}

// printedPaths reads the paths yt-dlp wrote with --print-to-file, one per
// line and without repeats (the pending list builds up across runs). A
// missing or empty file (nothing downloaded, everything already in the
// archive) returns an empty list, not an error.
func printedPaths(pathFile string) []string {
	data, err := os.ReadFile(pathFile)
	if err != nil {
		return nil
	}

	var paths []string
	seen := make(map[string]bool)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		// The file may have been removed or converted (.webm to .mp4) since.
		if _, err := os.Stat(line); err != nil {
			continue
		}
		paths = append(paths, line)
	}
	return paths
}

// countArchive counts the archive entries. It returns 0 when the file does
// not exist (first run) or is not in use (trims).
func countArchive(path string) int {
	if path == "" {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return bytes.Count(data, []byte("\n"))
}
