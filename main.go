package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
)

const (
	//RU-locale
	startDownVid      = "Скачивание видео началось"
	errDownVid        = "Не удалось скачать видео"
	errSendPhoto      = "Не удалось отправить фото"
	errFindDownFile   = "Не удалось найти скачанный файл"
	errOpenFile       = "ошибка открытия файла"
	errSendVid        = "ошибка отправки видео"
	getSomeVidFiles   = "получено несколько видео-файлов"
	viaLink           = "по ссылке"
	useFirst          = "беру первое"
	downSucces        = "Скачивание закончено, обрезка чёрных границ"
	cutSucces         = "Обрезка успешна"
	shutting          = "Выключаюсь."
	user              = "Пользователь"
	turnOffBot        = "выключил(-а) бота"
	gotVid            = "Получено видео"
	recVid            = "Не дождался ответа"
	waitVid           = "Жду видео"
	gotten            = "Получено"
	startProc         = "Начало обработки"
	errGetFile        = "не удалось получить файл"
	fileDownSuc       = "Файл скачан"
	errGetSizesVid    = "не удалось определить размер исходного видео"
	errGetOffsets     = "не удалось получить смещение"
	vidPrep           = "Видео подготовлено"
	errGetSizesSucVid = "не удалось определить размер обработанного видео"
	warnVidNotSq      = "обработка не дала квадрат: получено"
	sending           = "Отправка"

	// downtiktok.go
	errDetectVidRes            = "Не удалось определить разрешение видео"
	logFfprobeErr              = "ошибка ffprobe для %s: %v"
	errDetectCrop              = "Не удалось определить границы обрезки"
	errBadCropFormat           = "Некорректный формат обрезки"
	errParseCropSize           = "Не удалось распарсить размеры обрезки"
	errCropVideo               = "Не удалось обрезать видео"
	errReplaceFile             = "Не удалось заменить исходный файл"
	errNoPhotos                = "нет фото для отправки"
	errOpenFileW               = "не удалось открыть файл %s: %w"
	errSendAlbum               = "не удалось отправить альбом: %w"
	errDownloadTimeout         = "скачивание превысило таймаут (30с)"
	errYtdlpExit               = "yt-dlp завершился с кодом %d: %s"
	errStartYtdlp              = "не удалось запустить yt-dlp: %w"
	errFindFiles               = "не удалось найти скачанные файлы: %w"
	errNoFilesFound            = "yt-dlp не сообщил об ошибке, но файлы не найдены"
	errUnexpectedFfprobeOutput = "неожиданный вывод ffprobe: %s"
	errParseWidth              = "не удалось распарсить ширину: %w"
	errParseHeight             = "не удалось распарсить высоту: %w"

	// videoHand.go
	askOffsetPrompt          = "Ой-ой, походу видео не квадратное.\nКакой отступ сделать сверху?"
	btnTop                   = "Верх"
	btnCenter                = "Центр"
	btnBottom                = "Низ"
	btnThird                 = "1/3"
	btnTwoThirds             = "2/3"
	btnManual                = "Вручную (в процентах)"
	askManualPercentPrompt   = "Введите отступ сверху в процентах (0-100)"
	errNoChoice              = "не дождались выбора: %w"
	errUnexpectedUpdateType  = "получен неожиданный тип апдейта вместо callback"
	errInvalidPercent        = "некорректный процент: %q"
	errUnknownOffsetChoice   = "неизвестный выбор смещения: %q"
	errNoResponse            = "не дождались ответа: %w"
	errExpectedTextMessage   = "ожидалось текстовое сообщение"
	errCreateRequest         = "не удалось создать запрос: %w"
	logDownloadAttemptFailed = "попытка %d скачать файл не удалась: %v"
	errUnexpectedStatus      = "неожиданный статус %d"
	errCreateFile            = "не удалось создать файл %s: %w"
	errWriteFile             = "не удалось записать файл: %w"
	errDownloadFailedRetries = "не удалось скачать файл после нескольких попыток: %w"
)

var (
	ytdlpPath   = "yt-dlp"
	ffmpegPath  = "ffmpeg"
	ffprobePath = "ffprobe"
	botToken    = ""
	exitPass    = "0000"
	exitAbil    = false
	proxyIp     = "127.0.0.1"
	proxyPort   = 1984
	cropRegex   = regexp.MustCompile(`crop=(\d+:\d+:\d+:\d+)`)
	waiter      = NewWaiter()
)

func load() (*telego.Bot, context.Context) {
	ctx := context.Background()
	bot, err := telego.NewBot(botToken, telego.WithExtendedDefaultLogger(false, true, nil), telego.WithHTTPClient(&http.Client{}))

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	return bot, ctx
}
func main() {
	flag.StringVar(&botToken, "token", "", "Token from BotFather")
	flag.BoolVar(&exitAbil, "exit-ability", exitAbil, "The ability of disabling the bot")
	flag.StringVar(&exitPass, "exit-pass", exitPass, "Password for stopping bot")
	flag.StringVar(&proxyIp, "proxy-ip", proxyIp, "Proxy IP-adress")
	flag.IntVar(&proxyPort, "proxy-port", proxyPort, "Proxy port")
	flag.StringVar(&ytdlpPath, "ytdlp-path", ytdlpPath, "Path to yt-dlp binary")
	flag.StringVar(&ffmpegPath, "ffmpeg-path", ffmpegPath, "Path to ffmpeg binary")
	flag.StringVar(&ffprobePath, "ffprobe-path", ffprobePath, "Path to ffprobe binary")
	flag.Parse()
	bot, ctx := load()
	updates, _ := bot.UpdatesViaLongPolling(ctx, nil)
	bh, _ := th.NewBotHandler(bot, updates)
	defer func() { _ = bh.Stop() }()
	initComs(bh)
	_ = bh.Start()
}
func initComs(bh *th.BotHandler) {
	if exitAbil {
		exCom(bh)
	}
	ttCom(bh)
	cirCom(bh)
	videoDispatchCom(bh)
	callbackDispatchCom(bh)
	textDispatchCom(bh)
}
func ttCom(bh *th.BotHandler) {
	bh.Handle(func(ctx *th.Context, update telego.Update) error {
		ChatID := tu.ID(update.Message.Chat.ID)
		url := update.Message.Text
		outputBase := fmt.Sprintf("output_%d_%d", update.Message.From.ID, update.Message.MessageID)
		mes, _ := ctx.Bot().SendMessage(ctx, tu.Message(ChatID, startDownVid))
		files, err := downloadTikTok(url, outputBase)
		if err != nil {
			log.Printf("%s %s: %v", errSendVid, url, err)
			ctx.Bot().SendMessage(ctx, tu.Message(ChatID, errDownVid))
			return err
		}
		defer func() {
			for _, f := range files {
				os.Remove(f)
			}
		}()
		var imageFiles []string
		var videoFiles []string
		for _, f := range files {
			switch {
			case isImageFile(f):
				imageFiles = append(imageFiles, f)
			case isAudioOnlyFile(f):
			default:
				videoFiles = append(videoFiles, f)
			}
		}
		if len(imageFiles) > 0 {
			ctx.Bot().DeleteMessage(ctx, tu.Delete(ChatID, mes.MessageID))
			if err := sendPhotoAlbum(ctx, ctx.Bot(), ChatID, imageFiles); err != nil {
				log.Printf("ошибка отправки альбома: %v", err)
				ctx.Bot().SendMessage(ctx, tu.Message(ChatID, errSendPhoto))
				return err
			}
			return nil
		}

		if len(videoFiles) == 0 {
			ctx.Bot().SendMessage(ctx, tu.Message(ChatID, errFindDownFile))
			return fmt.Errorf("после скачивания не найдено ни одного пригодного файла")
		}

		outputPath := videoFiles[0]
		if len(videoFiles) > 1 {
			log.Printf("%s (%d) %s %s, %s: %v", getSomeVidFiles, len(videoFiles), viaLink, url, useFirst, videoFiles)
		}

		mes, _ = ctx.Bot().EditMessageText(ctx, tu.EditMessageText(ChatID, mes.MessageID, downSucces))
		detectAndCrop(outputPath, ctx, ChatID)
		mes, _ = ctx.Bot().EditMessageText(ctx, tu.EditMessageText(ChatID, mes.MessageID, cutSucces))
		f, err := os.Open(outputPath)
		if err != nil {
			log.Printf("%s %s: %v", errOpenFile, outputPath, err)
			return err
		}
		defer f.Close()
		ctx.Bot().DeleteMessage(ctx, tu.Delete(ChatID, mes.MessageID))
		if _, err := ctx.Bot().SendVideo(ctx, tu.Video(ChatID, tu.File(f))); err != nil {
			log.Printf("%s: %v", errSendVid, err)
			ctx.Bot().SendMessage(ctx, tu.Message(ChatID, errSendVid))
			return err
		}

		return nil
	},
		th.TextPrefix("https://"),
		th.TextContains("tiktok."),
	)
}
func exCom(bh *th.BotHandler) {
	bh.Handle(func(ctx *th.Context, update telego.Update) error {
		ctx.Bot().SendMessage(ctx, tu.Message(update.Message.Chat.ChatID(), shutting))
		fmt.Printf("%s %s %s", user, update.Message.From.Username, turnOffBot)
		os.Exit(0)
		return nil
	},
		th.CommandPrefix("exit"),
		th.TextSuffix(exitPass),
	)
}
func cirCom(bh *th.BotHandler) {
	bh.Handle(func(ctx *th.Context, update telego.Update) error {
		chatID := tu.ID(update.Message.Chat.ID)
		bot := ctx.Bot()
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			mes, _ := bot.SendMessage(bgCtx, tu.Message(chatID, waitVid))
			upd, err := waiter.WaitForMessage(bgCtx, chatID)
			if err != nil {
				bot.SendMessage(context.Background(), tu.Message(chatID, recVid))
				return
			}
			bot.DeleteMessage(bgCtx, tu.Delete(chatID, mes.MessageID))
			mes, _ = bot.SendMessage(bgCtx, tu.Message(chatID, gotten))
			vid := upd.Message.Video
			if vid != nil {
				bot.EditMessageText(bgCtx, tu.EditMessageText(chatID, mes.MessageID, gotVid))
				if err := vidHand(bot, bgCtx, vid, upd.Message.Chat.ChatID(), mes); err != nil {
					fmt.Println(err)
				}
			}
		}()
		return nil
	},
		th.Or(
			th.CommandEqual("circle"),
			th.TextPrefix("кружок"),
		),
	)
}
func videoDispatchCom(bh *th.BotHandler) {
	bh.Handle(func(ctx *th.Context, update telego.Update) error {
		if update.Message == nil || update.Message.Video == nil {
			return nil
		}
		chatID := tu.ID(update.Message.Chat.ID)
		waiter.Dispatch(chatID, update)
		return nil
	}, th.AnyMessageWithMedia())
}
func textDispatchCom(bh *th.BotHandler) {
	bh.Handle(func(ctx *th.Context, update telego.Update) error {
		chatID := tu.ID(update.Message.Chat.ID)
		waiter.Dispatch(chatID, update)
		ctx.Next(update)
		return nil
	}, th.AnyMessage())
}
func callbackDispatchCom(bh *th.BotHandler) {
	bh.HandleCallbackQuery(func(ctx *th.Context, query telego.CallbackQuery) error {
		if query.Data == "" {
			return nil
		}
		chatID := tu.ID(query.Message.GetChat().ID)
		waiter.Dispatch(chatID, telego.Update{CallbackQuery: &query})
		return nil
	})
}
