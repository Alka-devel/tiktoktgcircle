package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

type vidSpec struct {
	Width  int
	Height int
}

func vidHand(bot *telego.Bot, ctx context.Context, vid *telego.Video, ChatID telego.ChatID, mes *telego.Message) error {
	bot.EditMessageText(ctx, tu.EditMessageText(ChatID, mes.MessageID, startProc))
	inputPath := fmt.Sprintf("circle_in_%s.mp4", vid.FileID)
	outputPath := fmt.Sprintf("circle_out_%s.mp4", vid.FileID)
	defer os.Remove(inputPath)
	defer os.Remove(outputPath)
	file, err := bot.GetFile(ctx, &telego.GetFileParams{FileID: vid.FileID})
	if err != nil {
		return fmt.Errorf("%s: %w", errGetFile, err)
	}
	if err := downloadFile(bot, file, inputPath); err != nil {
		return err
	}
	bot.EditMessageText(ctx, tu.EditMessageText(ChatID, mes.MessageID, fileDownSuc))
	cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	origW, origH, err := getVideoResolution(cctx, inputPath)
	if err != nil {
		return fmt.Errorf("%s: %w", errGetSizesVid, err)
	}
	if origW != origH {
		side := origW
		if origH < side {
			side = origH
		}
		offsetX, offsetY, err := askForOffset(ctx, cctx, bot, ChatID, mes, origW, origH, side)
		if err != nil {
			return fmt.Errorf("%s: %w", errGetOffsets, err)
		}
		if err := cropToSquare(cctx, inputPath, outputPath, offsetX, offsetY); err != nil {
			return err
		}
	} else {
		if err := squareToVideoNote(cctx, inputPath, outputPath); err != nil {
			return err
		}
	}
	bot.EditMessageText(ctx, tu.EditMessageText(ChatID, mes.MessageID, vidPrep))
	realW, realH, err := getVideoResolution(cctx, outputPath)
	if err != nil {
		return fmt.Errorf("%s: %w", errGetSizesSucVid, err)
	}
	if realW != realH {
		return fmt.Errorf("%s %dx%d", warnVidNotSq, realW, realH)
	}
	f, err := os.Open(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()
	bot.EditMessageText(ctx, tu.EditMessageText(ChatID, mes.MessageID, sending))
	_, err = bot.SendVideoNote(ctx, &telego.SendVideoNoteParams{
		ChatID:    ChatID,
		VideoNote: tu.File(f),
		Duration:  vid.Duration,
		Length:    realW,
	})
	return err
}
func squareToVideoNote(ctx context.Context, inputPath, outputPath string) error {
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-i", inputPath,
		"-vf", "scale=640:640,setsar=1",
		"-c:a", "copy",
		"-y",
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg square passthrough failed: %s", string(output))
	}
	return nil
}

func cropToSquare(ctx context.Context, inputPath, outputPath string, offsetX, offsetY int) error {
	side := `trunc(min(iw\,ih)/2)*2`
	vf := fmt.Sprintf(
		"crop=%s:%s:%d:%d,setsar=1,scale=640:640",
		side, side, offsetX, offsetY,
	)
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-i", inputPath,
		"-vf", vf,
		"-c:a", "copy",
		"-y",
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg square crop failed: %s", string(output))
	}
	return nil
}
func downloadFile(bot *telego.Bot, file *telego.File, inputPath string) error {
	url := bot.FileDownloadURL(file.FilePath)
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		httpCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req, err := http.NewRequestWithContext(httpCtx, http.MethodGet, url, nil)
		if err != nil {
			cancel()
			return fmt.Errorf(errCreateRequest, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			log.Printf(logDownloadAttemptFailed, attempt, err)
			time.Sleep(2 * time.Second)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			cancel()
			lastErr = fmt.Errorf(errUnexpectedStatus, resp.StatusCode)
			continue
		}
		out, err := os.Create(inputPath)
		if err != nil {
			cancel()
			return fmt.Errorf(errCreateFile, inputPath, err)
		}
		_, err = io.Copy(out, resp.Body)
		out.Close()
		cancel()
		if err != nil {
			lastErr = fmt.Errorf(errWriteFile, err)
			continue
		}
		return nil
	}
	return fmt.Errorf(errDownloadFailedRetries, lastErr)
}
func askForOffset(ctx, waitCtx context.Context, bot *telego.Bot, chatID telego.ChatID, mes *telego.Message, origW, origH, side int) (offsetX, offsetY int, err error) {
	waitDone := make(chan struct{})
	var upd telego.Update
	var waitErr error
	go func() {
		upd, waitErr = waiter.WaitForMessage(waitCtx, chatID)
		close(waitDone)
	}()
	bot.EditMessageText(ctx, tu.EditMessageText(chatID, mes.MessageID,
		askOffsetPrompt).WithReplyMarkup(
		tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(btnTop).WithCallbackData("up"),
				tu.InlineKeyboardButton(btnCenter).WithCallbackData("cent"),
				tu.InlineKeyboardButton(btnBottom).WithCallbackData("down"),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(btnThird).WithCallbackData("13"),
				tu.InlineKeyboardButton(btnTwoThirds).WithCallbackData("23"),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(btnManual).WithCallbackData("manual"),
			),
		),
	))
	<-waitDone
	if waitErr != nil {
		return 0, 0, fmt.Errorf(errNoChoice, waitErr)
	}
	cb := upd.CallbackQuery
	if cb == nil {
		return 0, 0, fmt.Errorf(errUnexpectedUpdateType)
	}
	bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
	if cb.Data == "manual" {
		return askManualPercent(ctx, waitCtx, bot, chatID, mes, origW, origH, side)
	}
	return resolveOffsetChoice(cb.Data, origW, origH, side)
}
func askManualPercent(ctx, waitCtx context.Context, bot *telego.Bot, chatID telego.ChatID, mes *telego.Message, origW, origH, side int) (offsetX, offsetY int, err error) {
	bot.EditMessageText(ctx, tu.EditMessageText(chatID, mes.MessageID, askManualPercentPrompt))
	upd, err := waiter.WaitForMessage(waitCtx, chatID)
	if err != nil {
		return 0, 0, fmt.Errorf(errNoResponse, err)
	}
	if upd.Message == nil {
		return 0, 0, fmt.Errorf(errExpectedTextMessage)
	}
	percent, err := strconv.Atoi(strings.TrimSpace(upd.Message.Text))
	if err != nil || percent < 0 || percent > 100 {
		return 0, 0, fmt.Errorf(errInvalidPercent, upd.Message.Text)
	}
	offsetX, offsetY = resolveManualOffset(percent, origW, origH, side)
	return offsetX, offsetY, nil
}
func resolveOffsetChoice(choice string, origW, origH, side int) (offsetX, offsetY int, err error) {
	freeW := origW - side
	freeH := origH - side
	var frac float64
	switch choice {
	case "up":
		frac = 0.0
	case "cent":
		frac = 0.5
	case "down":
		frac = 1.0
	case "13":
		frac = 1.0 / 3.0
	case "23":
		frac = 2.0 / 3.0
	default:
		return 0, 0, fmt.Errorf(errUnknownOffsetChoice, choice)
	}
	offsetX = int(float64(freeW) * frac)
	offsetY = int(float64(freeH) * frac)
	return offsetX, offsetY, nil
}
func resolveManualOffset(percent, origW, origH, side int) (offsetX, offsetY int) {
	frac := float64(percent) / 100.0
	freeW := origW - side
	freeH := origH - side
	offsetX = int(float64(freeW) * frac)
	offsetY = int(float64(freeH) * frac)
	return offsetX, offsetY
}
