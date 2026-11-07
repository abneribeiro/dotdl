package downloader

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/abneribeiro/dotdl/internal/procio"
)

// The pair every iPhone, iPad, Android phone, Windows and macOS machine plays
// natively, with no extra app. Every video this package delivers goes
// through here first.
const (
	mp4Ext        = ".mp4"
	mp4VideoCodec = "h264"
	mp4AudioCodec = "aac"

	// mp4PixFmt is 8-bit 4:2:0. A 10-bit AV1/VP9 re-encoded without -pix_fmt
	// would become H.264 High10, which iPhones cannot decode either: the file
	// would have the right codecs and still not open.
	mp4PixFmt = "yuv420p"

	// compatCRF targets quality, not size: there is no byte budget, only the
	// duty not to spoil the video while converting it.
	compatCRF = "20"

	// compatPreset favors speed: the quality comes from the CRF, and the
	// preset only decides how long it takes to get there.
	compatPreset = "veryfast"

	// compatProfile pins the High profile, which every iPhone, Android,
	// Windows and macOS device decodes in hardware.
	compatProfile = "high"

	// evenDimensions rounds width and height to even numbers: libx264 in
	// yuv420p rejects odd sizes, which show up in hand-cropped videos
	// (TikTok, Instagram), and the conversion used to fail.
	evenDimensions = "scale=trunc(iw/2)*2:trunc(ih/2)*2"

	// compatAudioKbps is generous on purpose: audio is a small part of the
	// file, and re-encoding Opus to AAC already costs a generation.
	compatAudioKbps = "192k"
)

// mp4Compatible reports whether a file with these attributes already plays on
// any phone without conversion. It is pure: this is where "compatible" is
// defined, and what the tests pin down.
//
// An empty acodec means a silent video, which is compatible: there is no
// audio stream to be incompatible.
func mp4Compatible(ext, vcodec, acodec, pixFmt string) bool {
	if strings.ToLower(ext) != mp4Ext {
		return false
	}
	if !videoCompatible(vcodec, pixFmt) {
		return false
	}
	return audioCompatible(acodec)
}

// videoCompatible accepts 8-bit 4:2:0 H.264. yuvj420p is the full-range
// variant of the same format: it decodes the same, and re-encoding it would
// only lose quality.
func videoCompatible(vcodec, pixFmt string) bool {
	return vcodec == mp4VideoCodec && (pixFmt == mp4PixFmt || pixFmt == "yuvj420p")
}

func audioCompatible(acodec string) bool {
	return acodec == "" || acodec == mp4AudioCodec
}

// compatArgs builds the ffmpeg arguments that make the file compliant,
// copying every stream that is already right: a .webm with H.264/AAC is
// remuxed in seconds instead of re-encoded for minutes. Pure, like BuildArgs.
func compatArgs(inputPath, outputPath, vcodec, acodec, pixFmt string) []string {
	// The explicit -map drops what MP4 refuses (WebVTT subtitles, data
	// streams), which would otherwise make ffmpeg abort. The "?" on audio
	// tolerates silent videos.
	args := []string{"-i", inputPath, "-map", "0:v:0", "-map", "0:a:0?"}

	if videoCompatible(vcodec, pixFmt) {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args,
			"-vf", evenDimensions,
			"-c:v", "libx264",
			"-crf", compatCRF,
			"-preset", compatPreset,
			"-profile:v", compatProfile,
			"-pix_fmt", mp4PixFmt,
		)
	}

	if audioCompatible(acodec) {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", compatAudioKbps)
	}

	// +faststart moves the index to the front: it is what lets an iPhone
	// start playing before it has the whole file.
	return append(args, "-movflags", "+faststart", "-y", outputPath)
}

// moovFirst reports whether an MP4's index (the moov box) comes before its
// data (mdat), which +faststart guarantees and which lets an iPhone show the
// video without reading all of it first (previews, sharing, streaming). It
// only reads the top-level box headers.
func moovFirst(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	return moovFirstIn(f)
}

// moovFirstIn is moovFirst over an io.ReaderAt, for tests.
func moovFirstIn(r io.ReaderAt) (bool, error) {
	var offset int64
	header := make([]byte, 16)
	for {
		if _, err := r.ReadAt(header[:8], offset); err != nil {
			return false, fmt.Errorf("MP4 without moov or mdat: %w", err)
		}
		size := int64(binary.BigEndian.Uint32(header[:4]))
		switch string(header[4:8]) {
		case "moov":
			return true, nil
		case "mdat":
			return false, nil
		}
		if size == 1 { // a 64-bit size follows the type
			if _, err := r.ReadAt(header[8:16], offset+8); err != nil {
				return false, err
			}
			size = int64(binary.BigEndian.Uint64(header[8:16]))
		}
		if size < 8 {
			return false, fmt.Errorf("invalid MP4 box at offset %d", offset)
		}
		offset += size
	}
}

// ConvertHooks lets the caller follow an EnsureMP4 conversion.
type ConvertHooks struct {
	// Start is called before converting. reencode is true when the video is
	// re-encoded (slow: minutes) and false for a plain remux (seconds).
	Start func(reencode bool, vcodec string)
	// Progress receives the converted fraction, between 0 and 1.
	Progress func(fraction float64)
}

// EnsureMP4 makes sure path is an MP4 with 8-bit H.264 video, AAC audio and
// the index at the front. It returns the path to use: path itself when it was
// already compliant, or the converted file, which replaces the original.
//
// It is the second of the two layers that guarantee the format. The first
// asks yt-dlp for H.264/AAC (see MP4Sort), which handles the common case for
// free; this one catches the rest. YouTube above 1080p only has VP9/AV1, and
// no -S changes that.
func EnsureMP4(ctx context.Context, path string, hooks ConvertHooks) (string, error) {
	info, err := probeMedia(ctx, path)
	if err != nil {
		return path, fmt.Errorf("cannot inspect %s: %w", filepath.Base(path), err)
	}
	vcodec, acodec, pixFmt := info.VCodec, info.ACodec, info.PixFmt

	// Without a video stream there is nothing to guarantee, and converting
	// would produce an empty MP4. The file is left as it is.
	if vcodec == "" {
		slog.Debug("file has no video stream, left as is", "path", path)
		return path, nil
	}

	ext := filepath.Ext(path)
	if mp4Compatible(ext, vcodec, acodec, pixFmt) {
		// The codecs are right, but the index may be at the end: that happens
		// with trims, which yt-dlp itself re-encodes to H.264. A remux moves
		// it to the front.
		fast, err := moovFirst(path)
		if err != nil || fast {
			slog.Debug("file is already a compatible MP4", "path", path, "vcodec", vcodec, "acodec", acodec)
			return path, nil
		}
		slog.Debug("compatible MP4 with the index at the end: remuxing with +faststart", "path", path)
	}

	reencoding := !videoCompatible(vcodec, pixFmt)
	slog.Debug("converting to a compatible MP4",
		"path", path, "vcodec", vcodec, "acodec", acodec, "pix_fmt", pixFmt, "reencode", reencoding)
	if hooks.Start != nil {
		hooks.Start(reencoding, vcodec)
	}

	base := strings.TrimSuffix(path, ext)
	finalPath := base + mp4Ext
	// Write to a temporary name first: an interruption leaves the original
	// intact instead of a truncated MP4.
	tempPath := base + ".dotdl-compat" + mp4Ext

	args := compatArgs(path, tempPath, vcodec, acodec, pixFmt)
	// Without the duration there is no fraction: convert anyway, without a
	// progress bar.
	var duration float64
	if hooks.Progress != nil && info.Duration > 0 {
		duration = info.Duration
		args = append([]string{"-progress", "pipe:1", "-nostats"}, args...)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	stderr := &procio.TailBuffer{Max: 4096}
	cmd.Stderr = stderr
	stdout := procio.NewLineWriter(func(line string) {
		if f, ok := FFmpegFraction(line, duration); ok {
			hooks.Progress(f)
		}
	})
	cmd.Stdout = stdout

	err = cmd.Run()
	stdout.Flush()
	if err != nil {
		_ = os.Remove(tempPath) // a partial output is useless
		slog.Debug("MP4 conversion failed", "path", path, "err", err, "output", stderr.String())
		return path, fmt.Errorf("converting %s to MP4 failed: %w", filepath.Base(path), err)
	}

	// Renaming over the target is atomic: there is never a moment without a
	// complete file.
	if err := os.Rename(tempPath, finalPath); err != nil {
		_ = os.Remove(tempPath) // a partial output is useless
		return path, fmt.Errorf("replacing %s failed: %w", filepath.Base(path), err)
	}
	// When the extension changed (.webm/.mkv to .mp4) the original remains.
	if finalPath != path {
		_ = os.Remove(path) // the MP4 replaces it; a leftover is harmless
	}

	slog.Debug("MP4 conversion done", "path", finalPath)
	return finalPath, nil
}
