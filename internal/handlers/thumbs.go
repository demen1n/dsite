package handlers

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/demen1n/dsite/internal/db"
	"github.com/demen1n/dsite/internal/imgproc"
)

// Превью для сетки галереи. Оригинал (до 1600px) нужен в лайтбоксе, а в
// плитке сетки он почти всегда избыточен: страница галереи тянула десятки
// мегабайт, что ощутимо медленно издалека и на мобильном. Плитки
// «резиновые» (высота ряда — ширина/3.5), поэтому превью не заменяет
// оригинал, а идёт в srcset рядом с ним: gallery.js проставляет sizes по
// реальной ширине плитки, и браузер сам берёт превью или оригинал с учётом
// плотности экрана.
const (
	thumbWidth   = 800
	thumbQuality = 80
	thumbsDir    = "thumbs" // подкаталог uploads/
)

// makeThumb делает превью из байтов оригинала filename (width — его ширина,
// 0 — неизвестна) и возвращает путь превью относительно uploads/. Для GIF
// (анимация) и фото не шире thumbWidth превью не нужно — возвращается сам
// filename. Вызывающий должен держать слот imgproc.Reserve.
func makeThumb(filename string, data []byte, width int) (string, error) {
	if strings.EqualFold(filepath.Ext(filename), ".gif") {
		return filename, nil
	}
	if width == 0 {
		width, _, _ = imgproc.Dimensions(data)
	}
	if width > 0 && width <= thumbWidth {
		return filename, nil
	}
	out, _, _, err := imgproc.Process(data, thumbWidth, 0, thumbQuality)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(uploadsDir, thumbsDir), 0o750); err != nil {
		return "", err
	}
	// Base — превью всегда ложится прямо в uploads/thumbs/, как бы ни
	// выглядело имя оригинала.
	base := filepath.Base(filename)
	name := strings.TrimSuffix(base, filepath.Ext(base)) + ".jpg"
	if err := os.WriteFile(filepath.Join(uploadsDir, thumbsDir, name), out, 0o600); err != nil { //nolint:gosec // name — filepath.Base, за пределы thumbs/ не выйти
		return "", err
	}
	return thumbsDir + "/" + name, nil
}

// saveThumb делает превью и записывает его в БД; ошибки только логирует —
// без превью галерея просто покажет оригинал. Вызывающий держит слот
// imgproc.Reserve.
func saveThumb(filename string, data []byte, width int) {
	thumb, err := makeThumb(filename, data, width)
	if err != nil {
		log.Printf("thumb %s: %v", filename, err)
		return
	}
	if err := db.SetPhotoThumb(filename, thumb); err != nil {
		log.Printf("SetPhotoThumb %s: %v", filename, err)
	}
}

// removeThumb удаляет файл превью, если он отдельный от оригинала.
func removeThumb(filename, thumb string) {
	if thumb == "" || thumb == filename {
		return
	}
	if err := os.Remove(filepath.Join(uploadsDir, filepath.FromSlash(thumb))); err != nil && !os.IsNotExist(err) {
		log.Printf("remove thumb %s: %v", thumb, err)
	}
}

// BackfillThumbs догенерирует превью для фото, загруженных до их появления
// (или если генерация когда-то не удалась). Запускается в фоне при старте
// сервера; идемпотентно — берёт только фото с пустым thumb.
func BackfillThumbs() {
	photos, err := db.PhotosWithoutThumb()
	if err != nil {
		log.Printf("thumbs backfill: %v", err)
		return
	}
	if len(photos) == 0 {
		return
	}
	done := 0
	for _, p := range photos {
		release := imgproc.Reserve()
		data, err := os.ReadFile(filepath.Join(uploadsDir, p.Filename))
		if err == nil {
			saveThumb(p.Filename, data, p.Width)
			done++
		} else {
			log.Printf("thumbs backfill %s: %v", p.Filename, err)
		}
		release()
	}
	log.Printf("thumbs backfill: processed %d/%d photos", done, len(photos))
}
