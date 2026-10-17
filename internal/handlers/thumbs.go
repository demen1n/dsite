package handlers

import (
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/demen1n/dsite/internal/db"
	"github.com/demen1n/dsite/internal/imgproc"
)

// Превью картинок. Оригинал (до 1600px) нужен в лайтбоксе и на больших
// экранах, а в плитках, карточках и на телефоне он почти всегда избыточен:
// страницы тянули десятки мегабайт, что ощутимо медленно издалека и на
// мобильном. Превью не заменяет оригинал, а идёт в srcset рядом с ним —
// браузер сам выбирает файл под реальный размер картинки и плотность экрана.
//
// Превью есть у любого файла из uploads/ (картинки в постах, обложки, фото
// галереи); путь и размеры оригинала лежат в таблице thumbnails, а для
// рендера — в памяти (thumbIdx), чтобы не ходить в БД на каждую картинку.
const (
	thumbWidth   = 800
	thumbQuality = 80
	thumbsDir    = "thumbs" // подкаталог uploads/

	// proseImgSizes и coverImgSizes — sizes для картинок в тексте поста
	// (.prose img: колонка 652px + 200px вылета) и обложек (колонка 652px).
	// На узких экранах ширина занижена до 67vw: так 3x-телефон берёт превью
	// 800px, как 2x (390·0.67·3 ≈ 800) — то же ограничение плотности, что
	// gallery.js делает для плиток; на фото разницы с 3x не видно, а оригинал
	// весит в разы больше.
	proseImgSizes = "(max-width: 520px) 67vw, 852px"
	coverImgSizes = "(max-width: 700px) 67vw, 652px"

	// Заглушка: картинка шириной placeholderWidth, растянутая фоном под
	// <img>. Пока картинка грузится (издалека — секунды, и JPEG проявляется
	// сверху вниз), вместо белого прямоугольника видна её размытая копия.
	// Лежит прямо в HTML как data:-URI, ~1 КБ на картинку.
	placeholderWidth   = 16
	placeholderQuality = 50
)

var thumbIdx = struct {
	sync.RWMutex
	m map[string]db.Thumbnail
}{m: map[string]db.Thumbnail{}}

// loadThumbIndex перечитывает превью из БД в память.
func loadThumbIndex() {
	list, err := db.ListThumbnails()
	if err != nil {
		log.Printf("load thumbnails: %v", err)
		return
	}
	m := make(map[string]db.Thumbnail, len(list))
	for _, t := range list {
		m[t.Filename] = t
	}
	thumbIdx.Lock()
	thumbIdx.m = m
	thumbIdx.Unlock()
}

func thumbOf(filename string) (db.Thumbnail, bool) {
	thumbIdx.RLock()
	defer thumbIdx.RUnlock()
	t, ok := thumbIdx.m[filename]
	return t, ok
}

// makeThumb делает превью из байтов оригинала filename. Для GIF (анимация)
// и фото не шире thumbWidth превью не нужно — Thumb = сам filename.
// Вызывающий должен держать слот imgproc.Reserve.
func makeThumb(filename string, data []byte) (db.Thumbnail, error) {
	filename = filepath.Base(filename)
	t := db.Thumbnail{Filename: filename, Thumb: filename}
	w, h, err := imgproc.Dimensions(data)
	if err != nil {
		return t, err
	}
	t.Width, t.Height = w, h
	if isGIF(filename) {
		return t, nil
	}
	if w <= thumbWidth {
		t.Placeholder = placeholderOf(filename, data)
		return t, nil
	}
	out, _, _, err := imgproc.Process(data, thumbWidth, 0, thumbQuality)
	if err != nil {
		return t, err
	}
	t.Placeholder = placeholderOf(filename, out)
	if err := os.MkdirAll(filepath.Join(uploadsDir, thumbsDir), 0o750); err != nil {
		return t, err
	}
	name := strings.TrimSuffix(filename, filepath.Ext(filename)) + ".jpg"
	if err := os.WriteFile(filepath.Join(uploadsDir, thumbsDir, name), out, 0o600); err != nil { //nolint:gosec // name — из filepath.Base, за пределы thumbs/ не выйти
		return t, err
	}
	t.Thumb = thumbsDir + "/" + name
	return t, nil
}

func isGIF(filename string) bool { return strings.EqualFold(filepath.Ext(filename), ".gif") }

// needsPlaceholder — у превью нет заглушки, хотя она нужна (у GIF её нет
// намеренно: прозрачные места показывали бы фон).
func needsPlaceholder(t db.Thumbnail) bool { return t.Placeholder == "" && !isGIF(t.Filename) }

// makePlaceholder — data:-URI заглушки для картинки data (лучше передавать
// превью, а не оригинал: меньше декодировать). Вызывающий держит слот
// imgproc.Reserve.
func makePlaceholder(data []byte) (string, error) {
	out, _, _, err := imgproc.Process(data, placeholderWidth, 0, placeholderQuality)
	if err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out), nil
}

// placeholderOf — makePlaceholder, ошибка которого только логируется: без
// заглушки картинка просто грузится на белом фоне.
func placeholderOf(filename string, data []byte) string {
	ph, err := makePlaceholder(data)
	if err != nil {
		log.Printf("placeholder %s: %v", filename, err)
	}
	return ph
}

// registerThumb делает превью и запоминает его (БД, память, photos.thumb
// для галереи). Ошибки только логирует — без превью показывается оригинал.
// Вызывающий держит слот imgproc.Reserve.
func registerThumb(filename string, data []byte) {
	t, err := makeThumb(filename, data)
	if err != nil {
		log.Printf("thumb %s: %v", filename, err)
		return
	}
	rememberThumb(t)
}

func rememberThumb(t db.Thumbnail) {
	if err := db.UpsertThumbnail(t); err != nil {
		log.Printf("UpsertThumbnail %s: %v", t.Filename, err)
		return
	}
	if err := db.SetPhotoThumb(t.Filename, t.Thumb); err != nil {
		log.Printf("SetPhotoThumb %s: %v", t.Filename, err)
	}
	thumbIdx.Lock()
	thumbIdx.m[t.Filename] = t
	thumbIdx.Unlock()
}

// thumbFileOK — превью из индекса на месте (его файл не удалили руками).
func thumbFileOK(t db.Thumbnail) bool {
	if t.Thumb == t.Filename {
		return true
	}
	_, err := os.Stat(filepath.Join(uploadsDir, filepath.FromSlash(t.Thumb)))
	return err == nil
}

// processThumbFile делает превью для файла из uploads/, если его ещё нет.
func processThumbFile(filename string) {
	filename = filepath.Base(filename)
	if t, ok := thumbOf(filename); ok && thumbFileOK(t) {
		if needsPlaceholder(t) {
			// Превью сделано до появления заглушек — её делаем из превью.
			addPlaceholder(t)
			return
		}
		// Превью уже есть (например, файл из поста добавили в галерею) —
		// достаточно проставить его фото галереи.
		if err := db.SetPhotoThumb(filename, t.Thumb); err != nil {
			log.Printf("SetPhotoThumb %s: %v", filename, err)
		}
		return
	}
	release := imgproc.Reserve()
	defer release()
	data, err := os.ReadFile(filepath.Join(uploadsDir, filename))
	if err != nil {
		log.Printf("thumb %s: %v", filename, err)
		return
	}
	registerThumb(filename, data)
}

// addPlaceholder дополняет готовое превью t заглушкой.
func addPlaceholder(t db.Thumbnail) {
	release := imgproc.Reserve()
	defer release()
	data, err := os.ReadFile(filepath.Join(uploadsDir, filepath.FromSlash(t.Thumb)))
	if err != nil {
		log.Printf("placeholder %s: %v", t.Filename, err)
		return
	}
	if t.Placeholder = placeholderOf(t.Filename, data); t.Placeholder != "" {
		rememberThumb(t)
	}
}

// thumbQueue — файлы, ждущие превью. Загрузки кладут сюда имя и не ждут
// генерации; разбирает очередь StartThumbWorker.
var thumbQueue = make(chan string, 512)

func queueThumb(filename string) {
	select {
	case thumbQueue <- filename:
	default:
		// Очередь переполнена — не страшно: превью догенерит BackfillThumbs
		// при следующем старте.
		log.Printf("thumb queue full, skipping %s", filename)
	}
}

// StartThumbWorker запускает фоновую генерацию превью: разбор очереди
// загрузок и догенерацию для уже лежащих в uploads/ файлов.
func StartThumbWorker() {
	go func() {
		for name := range thumbQueue {
			processThumbFile(name)
		}
	}()
	go BackfillThumbs()
}

// forgetThumb удаляет превью файла (файл превью, запись в БД и в памяти) —
// когда удаляют сам файл.
func forgetThumb(filename string) {
	filename = filepath.Base(filename)
	if t, ok := thumbOf(filename); ok && t.Thumb != t.Filename {
		if err := os.Remove(filepath.Join(uploadsDir, filepath.FromSlash(t.Thumb))); err != nil && !os.IsNotExist(err) {
			log.Printf("remove thumb %s: %v", t.Thumb, err)
		}
	}
	if err := db.DeleteThumbnail(filename); err != nil {
		log.Printf("DeleteThumbnail %s: %v", filename, err)
	}
	thumbIdx.Lock()
	delete(thumbIdx.m, filename)
	thumbIdx.Unlock()
}

// BackfillThumbs догенерирует превью для всех картинок в uploads/, у которых
// его нет (загружены до появления превью, или генерация не удалась).
// Идемпотентно; запускается в фоне при старте сервера.
func BackfillThumbs() {
	entries, err := os.ReadDir(uploadsDir)
	if err != nil {
		log.Printf("thumbs backfill: %v", err)
		return
	}
	done := 0
	for _, e := range entries {
		if e.IsDir() || !isAllowedImageExt(filepath.Ext(e.Name())) {
			continue
		}
		if t, ok := thumbOf(e.Name()); ok && thumbFileOK(t) && !needsPlaceholder(t) {
			continue
		}
		processThumbFile(e.Name())
		done++
	}
	// Фото галереи, чьё превью уже было, но не проставлено в photos.thumb.
	if photos, err := db.PhotosWithoutThumb(); err == nil {
		for _, p := range photos {
			if t, ok := thumbOf(p.Filename); ok {
				db.SetPhotoThumb(p.Filename, t.Thumb) //nolint:errcheck // best effort, повторится на следующем старте
			}
		}
	}
	if done > 0 {
		log.Printf("thumbs backfill: processed %d files", done)
	}
}

// ───── Рендер ─────

// uploadName достаёт имя файла из "/uploads/x.jpg" или "x.jpg"; "" — не наш
// файл (внешняя ссылка, подкаталог и т.п.).
func uploadName(src string) string {
	name := strings.TrimPrefix(src, "/uploads/")
	if name == "" || strings.ContainsAny(name, "/?#") || strings.Contains(src, "://") {
		return ""
	}
	return name
}

// thumbSrcset — srcset «превью + оригинал» для картинки из uploads/; ""
// если превью нет или оно не нужно.
func thumbSrcset(src string) string {
	name := uploadName(src)
	if name == "" {
		return ""
	}
	t, ok := thumbOf(name)
	if !ok || t.Thumb == t.Filename || t.Width == 0 {
		return ""
	}
	return fmt.Sprintf("/uploads/%s %dw, /uploads/%s %dw", t.Thumb, thumbWidth, t.Filename, t.Width)
}

// thumbSrc — адрес превью (или исходный src, если превью нет).
func thumbSrc(src string) string {
	name := uploadName(src)
	if name == "" {
		return src
	}
	if t, ok := thumbOf(name); ok {
		return "/uploads/" + t.Thumb
	}
	return "/uploads/" + name
}

// thumbRatio — "ширина/высота" оригинала для data-ratio плитки (раскладка
// сетки сразу, не дожидаясь загрузки картинки); "" — размеры неизвестны.
func thumbRatio(src string) string {
	if t, ok := thumbOf(uploadName(src)); ok && t.Width > 0 && t.Height > 0 {
		return fmt.Sprintf("%d/%d", t.Width, t.Height)
	}
	return ""
}

// placeholderStyle — style с заглушкой картинки src фоном; "" — заглушки нет.
func placeholderStyle(src string) string {
	t, ok := thumbOf(uploadName(src))
	if !ok || t.Placeholder == "" {
		return ""
	}
	return "background: url(" + t.Placeholder + ") center / cover no-repeat"
}

// thumbAttrs — атрибуты width/height и style с заглушкой для <img> обложки:
// место под неё резервируется до загрузки, и до загрузки видна заглушка.
func thumbAttrs(src string) template.HTMLAttr {
	t, ok := thumbOf(uploadName(src))
	if !ok {
		return ""
	}
	var b strings.Builder
	if t.Width > 0 && t.Height > 0 {
		fmt.Fprintf(&b, ` width="%d" height="%d"`, t.Width, t.Height)
	}
	if st := placeholderStyle(src); st != "" {
		fmt.Fprintf(&b, ` style="%s"`, st)
	}
	return template.HTMLAttr(b.String()) //nolint:gosec // числа и data:-URI из base64, сгенерированные нами
}

var (
	reImgTag     = regexp.MustCompile(`<img\s[^>]*>`)
	reImgTagSrc  = regexp.MustCompile(`\ssrc="([^"]*)"`)
	reImgTagAttr = regexp.MustCompile(`\s(srcset|loading|decoding|width|height|style)=`)
)

// responsiveImages дополняет картинки в HTML поста: srcset/sizes для файлов
// с превью, width/height (место под картинку резервируется до загрузки и
// страница не прыгает), заглушку фоном, loading="lazy" и decoding="async". Атрибуты, уже
// заданные в markdown/HTML поста, не трогает.
func responsiveImages(html string) string {
	return reImgTag.ReplaceAllStringFunc(html, func(tag string) string {
		m := reImgTagSrc.FindStringSubmatch(tag)
		if m == nil {
			return tag
		}
		have := map[string]bool{}
		for _, a := range reImgTagAttr.FindAllStringSubmatch(tag, -1) {
			have[a[1]] = true
		}
		var add strings.Builder
		if name := uploadName(m[1]); name != "" {
			if ss := thumbSrcset(name); ss != "" && !have["srcset"] {
				fmt.Fprintf(&add, ` srcset="%s" sizes="%s"`, ss, proseImgSizes)
			}
			if t, ok := thumbOf(name); ok && t.Width > 0 && t.Height > 0 && !have["width"] && !have["height"] {
				fmt.Fprintf(&add, ` width="%d" height="%d"`, t.Width, t.Height)
			}
			if st := placeholderStyle(name); st != "" && !have["style"] {
				fmt.Fprintf(&add, ` style="%s"`, st)
			}
		}
		if !have["loading"] {
			add.WriteString(` loading="lazy"`)
		}
		if !have["decoding"] {
			add.WriteString(` decoding="async"`)
		}
		end := len(tag) - 1
		if strings.HasSuffix(tag, "/>") {
			end = len(tag) - 2
		}
		return strings.TrimRight(tag[:end], " ") + add.String() + tag[end:]
	})
}
