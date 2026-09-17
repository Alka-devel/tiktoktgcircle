package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
)

var imageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
}

var audioOnlyExts = map[string]bool{
	".mp3":  true,
	".m4a":  true,
	".aac":  true,
	".opus": true,
	".ogg":  true,
	".wav":  true,
}

func isImageFile(path string) bool {
	return imageExts[strings.ToLower(filepath.Ext(path))]
}

func isAudioOnlyFile(path string) bool {
	return audioOnlyExts[strings.ToLower(filepath.Ext(path))]
}

func getVideoResolution(ctx context.Context, path string) (width, height int, err error) {
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=s=x:p=0",
		path,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ffprobe failed: %w, output: %s", err, string(output))
	}

	dims := strings.TrimSpace(string(output))
	parts := strings.Split(dims, "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf(errUnexpectedFfprobeOutput, dims)
	}

	width, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf(errParseWidth, err)
	}
	height, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf(errParseHeight, err)
	}

	return width, height, nil
}

func detectAndCrop(outputPath string, botCtx *th.Context, id telego.ChatID) {
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer probeCancel()

	origW, origH, err := getVideoResolution(probeCtx, outputPath)
	if err != nil {
		botCtx.Bot().SendMessage(botCtx, tu.Message(id, errDetectVidRes))
		log.Printf(logFfprobeErr, outputPath, err)
		return
	}

	detectCtx, detectCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer detectCancel()

	crop, err := detectCrop(detectCtx, outputPath)
	if err != nil {
		botCtx.Bot().SendMessage(botCtx, tu.Message(id, errDetectCrop))
		return
	}

	cropParts := strings.Split(crop, ":")
	if len(cropParts) != 4 {
		botCtx.Bot().SendMessage(botCtx, tu.Message(id, errBadCropFormat))
		return
	}
	cropW, err1 := strconv.Atoi(cropParts[0])
	cropH, err2 := strconv.Atoi(cropParts[1])
	if err1 != nil || err2 != nil {
		botCtx.Bot().SendMessage(botCtx, tu.Message(id, errParseCropSize))
		return
	}

	if cropW == origW && cropH == origH {
		return
	}

	cropCtx, cropCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cropCancel()

	tmpPath := outputPath + ".cropped.mp4"
	if err := cropVideo(cropCtx, outputPath, tmpPath, crop); err != nil {
		botCtx.Bot().SendMessage(botCtx, tu.Message(id, errCropVideo))
		fmt.Println(err)
		return
	}

	if err := os.Rename(tmpPath, outputPath); err != nil {
		botCtx.Bot().SendMessage(botCtx, tu.Message(id, errReplaceFile))
	}
}

func downloadTikTok(url, outputBase string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	outputTemplate := outputBase + "_%(playlist_index)s.%(ext)s"

	cmd := exec.CommandContext(ctx,
		ytdlpPath,
		"--proxy", "socks5://192.168.0.6:1984",
		"-f", "bv*+ba/b",
		"--merge-output-format", "mp4",
		"--playlist-items", "1-10",
		"-o", outputTemplate,
		url,
	)

	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf(errDownloadTimeout)
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf(errYtdlpExit, exitErr.ExitCode(), string(output))
		}
		return nil, fmt.Errorf(errStartYtdlp, err)
	}

	matches, err := filepath.Glob(outputBase + "_*.*")
	if err != nil {
		return nil, fmt.Errorf(errFindFiles, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf(errNoFilesFound)
	}
	sort.Strings(matches)
	return matches, nil
}

func sendPhotoAlbum(ctx context.Context, bot *telego.Bot, chatID telego.ChatID, paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf(errNoPhotos)
	}
	if len(paths) > 10 {
		paths = paths[:10]
	}
	files := make([]*os.File, 0, len(paths))
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	medias := make([]telego.InputMedia, 0, len(paths))
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return fmt.Errorf(errOpenFileW, p, err)
		}
		files = append(files, f)
		medias = append(medias, tu.MediaPhoto(tu.File(f)))
	}
	_, err := bot.SendMediaGroup(ctx, tu.MediaGroup(chatID, medias...))
	if err != nil {
		return fmt.Errorf(errSendAlbum, err)
	}
	return nil
}

func detectCrop(ctx context.Context, inputPath string) (string, error) {
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-i", inputPath,
		"-vf", "cropdetect=24:16:0",
		"-f", "null", "-",
	)
	output, _ := cmd.CombinedOutput()

	matches := cropRegex.FindAllStringSubmatch(string(output), -1)
	if len(matches) == 0 {
		return "", fmt.Errorf(errDetectCrop)
	}
	return matches[len(matches)-1][1], nil
}

func cropVideo(ctx context.Context, inputPath, outputPath, cropParams string) error {
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-i", inputPath,
		"-vf", fmt.Sprintf("crop=%s", cropParams),
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-c:a", "copy",
		"-y",
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg crop failed: %v: %s", err, string(output))
	}
	return nil
}
