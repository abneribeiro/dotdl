package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// mediaInfo is what dotdl needs to know about a file, read with a single
// ffprobe run. Missing streams leave their fields empty: an empty VCodec is a
// file without video (audio only), an empty ACodec a silent video.
type mediaInfo struct {
	VCodec string
	PixFmt string
	Width  int
	Height int
	ACodec string
	// Duration in seconds; 0 when ffprobe does not know it.
	Duration float64
}

// probeArgs asks ffprobe for every stream's type, codec, pixel format and
// size, whether it is a cover (attached_pic), and the duration, as JSON.
func probeArgs(path string) []string {
	return []string{"-v", "error",
		"-show_entries", "stream=codec_type,codec_name,pix_fmt,width,height:stream_disposition=attached_pic:format=duration",
		"-of", "json", path}
}

// probeMedia reads everything in a single ffprobe run, which adds up on a
// playlist.
func probeMedia(ctx context.Context, path string) (mediaInfo, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", probeArgs(path)...).Output()
	if err != nil {
		return mediaInfo{}, fmt.Errorf("ffprobe failed: %w", err)
	}
	return parseProbe(out)
}

// ffprobeOutput is the part of ffprobe's JSON that probeArgs asks for.
type ffprobeOutput struct {
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		PixFmt      string `json:"pix_fmt"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// parseProbe reads ffprobe's JSON. It keeps the first video stream that is
// not a cover (an MP3 or M4A shows its artwork as an mjpeg/png "video") and
// the first audio stream.
func parseProbe(data []byte) (mediaInfo, error) {
	var out ffprobeOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return mediaInfo{}, fmt.Errorf("unreadable ffprobe output: %w", err)
	}
	var info mediaInfo
	for _, s := range out.Streams {
		switch {
		case s.CodecType == "video" && s.Disposition.AttachedPic == 0 && info.VCodec == "":
			info.VCodec, info.PixFmt, info.Width, info.Height = s.CodecName, s.PixFmt, s.Width, s.Height
		case s.CodecType == "audio" && info.ACodec == "":
			info.ACodec = s.CodecName
		}
	}
	// "N/A" or an absent duration leaves 0: the conversion runs without a
	// progress bar.
	if d, err := strconv.ParseFloat(out.Format.Duration, 64); err == nil && d > 0 {
		info.Duration = d
	}
	return info, nil
}

// Describe summarizes a downloaded file for the user, e.g. "H.264 · 1920×1080"
// or "AAC" for audio. It returns "" when ffprobe cannot read the file.
func Describe(ctx context.Context, path string) string {
	info, err := probeMedia(ctx, path)
	if err != nil {
		return ""
	}
	switch {
	case info.VCodec != "":
		return fmt.Sprintf("%s · %d×%d", codecName(info.VCodec), info.Width, info.Height)
	case info.ACodec != "":
		return codecName(info.ACodec)
	}
	return ""
}

// codecName turns an ffprobe codec name into the one people know.
func codecName(c string) string {
	switch c {
	case "h264":
		return "H.264"
	case "hevc":
		return "HEVC"
	case "av1":
		return "AV1"
	case "vp9":
		return "VP9"
	case "aac":
		return "AAC"
	case "mp3":
		return "MP3"
	case "opus":
		return "Opus"
	default:
		return strings.ToUpper(c)
	}
}

// FFmpegFraction reads one line of ffmpeg's "-progress pipe:1" output and
// returns the processed fraction of a file durationSec seconds long. Only
// out_time_us lines count (ffmpeg's out_time_ms is, for historical reasons,
// also in microseconds).
func FFmpegFraction(line string, durationSec float64) (float64, bool) {
	value, ok := strings.CutPrefix(strings.TrimSpace(line), "out_time_us=")
	if !ok || durationSec <= 0 {
		return 0, false
	}
	us, err := strconv.ParseFloat(value, 64)
	if err != nil || us < 0 {
		return 0, false
	}
	return min(us/1e6/durationSec, 1), true
}
