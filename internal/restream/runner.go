package restream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ProcessRunner struct {
	YTDLPPath  string
	FFmpegPath string
	Transcode  bool
}

func NewProcessRunner(transcode bool) *ProcessRunner {
	return &ProcessRunner{
		YTDLPPath:  "yt-dlp",
		FFmpegPath: "ffmpeg",
		Transcode:  transcode,
	}
}

func (r *ProcessRunner) Start(ctx context.Context, session *Session, options RunOptions) {
	go r.run(ctx, session, options)
}

func (r *ProcessRunner) run(ctx context.Context, session *Session, options RunOptions) {
	if err := r.requireBinaries(); err != nil {
		session.SetStatus(StatusFailed, err.Error())
		return
	}
	cookieFile, cleanup, err := writeTempCookieFile(options.CookiesText)
	if err != nil {
		session.SetStatus(StatusFailed, err.Error())
		return
	}
	defer cleanup()

	session.SetStatus(StatusResolving, "Resolving media URL with yt-dlp")
	qualities, err := r.listQualities(ctx, session.URL, cookieFile)
	if err != nil {
		session.SetStatus(StatusFailed, err.Error())
		return
	}
	session.SetQualityOptions(qualities)
	qualityID := options.QualityID
	if !qualityExists(qualities, qualityID) {
		qualityID = "best"
	}

	mediaURL, err := r.resolveMediaURL(ctx, session.URL, qualityID, cookieFile)
	if err != nil {
		session.SetStatus(StatusFailed, err.Error())
		return
	}

	if err := prepareHLSDir(session.HLSDir); err != nil {
		session.SetStatus(StatusFailed, "Could not create HLS directory: "+err.Error())
		return
	}

	session.SetStatus(StatusStarting, "Starting ffmpeg restream")
	cmd := exec.CommandContext(ctx, r.FFmpegPath, r.ffmpegArgs(mediaURL, session.HLSDir)...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		session.SetStatus(StatusFailed, "Could not capture ffmpeg logs: "+err.Error())
		return
	}
	cmd.Stdout = nil

	if err := cmd.Start(); err != nil {
		session.SetStatus(StatusFailed, "Could not start ffmpeg: "+err.Error())
		return
	}

	logs := make(chan string, 1)
	go captureTail(stderr, logs)

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	playlistPath := filepath.Join(session.HLSDir, "index.m3u8")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(75 * time.Second)
	defer timeout.Stop()

	ready := false
	for {
		select {
		case <-ctx.Done():
			session.SetStatus(StatusStopped, "Restream stopped")
			return
		case err := <-waitCh:
			if ready && err == nil {
				session.SetStatus(StatusStopped, "ffmpeg exited")
				return
			}
			message := "ffmpeg exited before the stream became ready"
			if tail := latestLog(logs); tail != "" {
				message += ": " + tail
			}
			if err != nil {
				message += ": " + err.Error()
			}
			session.SetStatus(StatusFailed, message)
			return
		case <-ticker.C:
			if playlistReady(playlistPath) {
				session.SetStatus(StatusReady, "Stream ready")
				ready = true
				timeout.Stop()
				ticker.Stop()
				if err := <-waitCh; err != nil {
					session.SetStatus(StatusFailed, "ffmpeg exited after startup: "+err.Error())
				} else {
					session.SetStatus(StatusStopped, "ffmpeg exited")
				}
				return
			}
		case <-timeout.C:
			message := "Timed out waiting for HLS playlist"
			if tail := latestLog(logs); tail != "" {
				message += ": " + tail
			}
			_ = cmd.Process.Kill()
			session.SetStatus(StatusFailed, message)
			return
		}
	}
}

func (r *ProcessRunner) requireBinaries() error {
	if _, err := exec.LookPath(r.YTDLPPath); err != nil {
		return errors.New("yt-dlp is required and was not found in PATH")
	}
	if _, err := exec.LookPath(r.FFmpegPath); err != nil {
		return errors.New("ffmpeg is required and was not found in PATH")
	}
	return nil
}

func (r *ProcessRunner) resolveMediaURL(ctx context.Context, sourceURL, qualityID, cookieFile string) (string, error) {
	resolveCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	formatSelector := "best[protocol*=m3u8]/best[protocol*=https]/best"
	if qualityID != "" && qualityID != "best" {
		formatSelector = qualityID
	}

	args := []string{
		"--no-playlist",
		"--no-warnings",
		"-f", formatSelector,
		"--get-url",
		sourceURL,
	}
	args = withCookies(args, cookieFile)
	cmd := exec.CommandContext(resolveCtx, r.YTDLPPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.New("yt-dlp could not resolve the stream: " + compactOutput(output))
	}

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			return line, nil
		}
	}

	return "", errors.New("yt-dlp did not return a playable media URL")
}

func (r *ProcessRunner) listQualities(ctx context.Context, sourceURL, cookieFile string) ([]QualityOption, error) {
	resolveCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	args := []string{
		"--no-playlist",
		"--no-warnings",
		"--dump-single-json",
		sourceURL,
	}
	args = withCookies(args, cookieFile)
	cmd := exec.CommandContext(resolveCtx, r.YTDLPPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errors.New("yt-dlp could not inspect stream formats: " + compactOutput(output))
	}

	var payload struct {
		Formats []struct {
			ID       string  `json:"format_id"`
			Protocol string  `json:"protocol"`
			Ext      string  `json:"ext"`
			VCodec   string  `json:"vcodec"`
			Height   int     `json:"height"`
			FPS      float64 `json:"fps"`
			TBR      float64 `json:"tbr"`
			URL      string  `json:"url"`
		} `json:"formats"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return nil, errors.New("yt-dlp returned invalid format JSON: " + err.Error())
	}

	options := []QualityOption{{ID: "best", Label: "Auto"}}
	seen := map[string]bool{"best": true}
	for _, format := range payload.Formats {
		if format.ID == "" || format.URL == "" || seen[format.ID] || format.VCodec == "none" || format.Height == 0 {
			continue
		}
		if !strings.Contains(format.Protocol, "m3u8") && !strings.HasPrefix(format.URL, "http") {
			continue
		}
		fps := int(format.FPS + 0.5)
		label := fmt.Sprintf("%dp", format.Height)
		if fps > 30 {
			label += strconv.Itoa(fps)
		}
		options = append(options, QualityOption{
			ID:      format.ID,
			Label:   label,
			Height:  format.Height,
			FPS:     fps,
			Bitrate: int(format.TBR + 0.5),
		})
		seen[format.ID] = true
	}

	sort.SliceStable(options[1:], func(i, j int) bool {
		left := options[i+1]
		right := options[j+1]
		if left.Height != right.Height {
			return left.Height > right.Height
		}
		if left.FPS != right.FPS {
			return left.FPS > right.FPS
		}
		return left.Bitrate > right.Bitrate
	})

	return options, nil
}

func withCookies(args []string, cookieFile string) []string {
	if cookieFile == "" {
		return args
	}
	with := append([]string{"--cookies", cookieFile}, args...)
	return with
}

func writeTempCookieFile(cookiesText string) (string, func(), error) {
	cookiesText = strings.TrimSpace(cookiesText)
	if cookiesText == "" {
		return "", func() {}, nil
	}
	file, err := os.CreateTemp("", "live2gether-youtube-cookies-*.txt")
	if err != nil {
		return "", nil, errors.New("could not create temporary cookies file: " + err.Error())
	}
	path := file.Name()
	cleanup := func() {
		_ = os.Remove(path)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, errors.New("could not secure temporary cookies file: " + err.Error())
	}
	if _, err := file.WriteString(cookiesText + "\n"); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, errors.New("could not write temporary cookies file: " + err.Error())
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, errors.New("could not close temporary cookies file: " + err.Error())
	}
	return path, cleanup, nil
}

func (r *ProcessRunner) ffmpegArgs(mediaURL, hlsDir string) []string {
	args := []string{
		"-hide_banner",
		"-nostdin",
		"-loglevel", "warning",
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-i", mediaURL,
	}

	if r.Transcode {
		args = append(args,
			"-vf", "scale=w='min(1280,iw)':h=-2",
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-tune", "zerolatency",
			"-c:a", "aac",
			"-b:a", "128k",
		)
	} else {
		args = append(args, "-c", "copy")
	}

	return append(args,
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "8",
		"-hls_flags", "delete_segments+append_list+omit_endlist+program_date_time",
		"-hls_segment_filename", filepath.Join(hlsDir, "segment_%05d.ts"),
		filepath.Join(hlsDir, "index.m3u8"),
	)
}

func playlistReady(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && bytes.Contains(data, []byte("#EXTM3U")) && bytes.Contains(data, []byte("#EXTINF"))
}

func captureTail(pipe interface{ Read([]byte) (int, error) }, out chan<- string) {
	scanner := bufio.NewScanner(pipe)
	var tail string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			tail = line
		}
	}
	if tail != "" {
		out <- tail
	}
	close(out)
}

func latestLog(logs <-chan string) string {
	select {
	case tail, ok := <-logs:
		if ok {
			return tail
		}
	default:
	}
	return ""
}

func compactOutput(output []byte) string {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return "no output"
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 500 {
		return text[:500] + "..."
	}
	return text
}
