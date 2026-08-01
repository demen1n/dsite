package db

import (
	"database/sql"
	"strings"
	"time"
)

// ───────────────────────── Users ─────────────────────────

func GetUserByLogin(login string) (id int, hash string, err error) {
	err = DB.QueryRow(`SELECT id, password FROM users WHERE login=?`, login).
		Scan(&id, &hash)
	return
}

func UserExists() (bool, error) {
	var count int
	err := DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count > 0, err
}

func CreateUser(login, hash string) error {
	_, err := DB.Exec(`INSERT INTO users (login, password) VALUES (?,?)`, login, hash)
	return err
}

func UpdatePassword(login, hash string) error {
	_, err := DB.Exec(`UPDATE users SET password=? WHERE login=?`, hash, login)
	return err
}

// GetAdmin возвращает логин и хеш пароля единственного администратора.
func GetAdmin() (login, hash string, err error) {
	err = DB.QueryRow(`SELECT login, password FROM users LIMIT 1`).Scan(&login, &hash)
	return
}

// ───────────────────────── Posts ─────────────────────────

type Post struct {
	ID         int
	Slug       string
	Title      string
	BodyMD     string
	BodyHTML   string
	Cover      string
	Published  bool
	Views      int
	Tags       []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	SeriesID   int // 0 = not part of a series
	SeriesName string
	SeriesSlug string
}

const PostsPerPage = 10

// ListPosts возвращает все посты (для admin-панели, без тегов).
func ListPosts(onlyPublished bool) ([]Post, error) {
	q := `SELECT id, slug, title, body_md, body_html, cover, published, views, created_at, updated_at
	      FROM posts`
	if onlyPublished {
		q += ` WHERE published=1`
	}
	q += ` ORDER BY created_at DESC`
	rows, err := DB.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		var p Post
		var pub int
		var ca, ua string
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.BodyMD, &p.BodyHTML,
			&p.Cover, &pub, &p.Views, &ca, &ua); err != nil {
			return nil, err
		}
		p.Published = pub == 1
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
		p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ua)
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// ListPostsMeta is like ListPosts but leaves BodyMD/BodyHTML unset — for
// callers that only need metadata (sitemap, admin post list) and would
// otherwise pull every post's full markdown+HTML off disk just to read a
// slug and a date.
func ListPostsMeta(onlyPublished bool) ([]Post, error) {
	q := `SELECT id, slug, title, cover, published, views, created_at, updated_at
	      FROM posts`
	if onlyPublished {
		q += ` WHERE published=1`
	}
	q += ` ORDER BY created_at DESC`
	rows, err := DB.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		var p Post
		var pub int
		var ca, ua string
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Cover, &pub, &p.Views, &ca, &ua); err != nil {
			return nil, err
		}
		p.Published = pub == 1
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
		p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ua)
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// ListPostsPaginated возвращает посты с пагинацией и опциональным фильтром по тегу.
// Возвращает посты, суммарное количество и ошибку.
func ListPostsPaginated(tagSlug string, page, perPage int) ([]Post, int, error) {
	var total int
	args := []any{}
	countQ := `SELECT COUNT(DISTINCT p.id) FROM posts p`
	if tagSlug != "" {
		countQ += ` JOIN post_tags pt ON pt.post_id = p.id
		            JOIN tags t ON t.id = pt.tag_id AND t.slug = ?`
		args = append(args, tagSlug)
	}
	countQ += ` WHERE p.published=1`
	if err := DB.QueryRow(countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage
	args2 := []any{}
	q := `SELECT p.id, p.slug, p.title, p.body_md, p.body_html, p.cover,
	             p.published, p.views, p.created_at, p.updated_at,
	             COALESCE(GROUP_CONCAT(t.name, ','), '') as tags
	      FROM posts p
	      LEFT JOIN post_tags pt ON pt.post_id = p.id
	      LEFT JOIN tags t ON t.id = pt.tag_id`
	if tagSlug != "" {
		q += ` WHERE p.published=1 AND p.id IN (
		           SELECT pt2.post_id FROM post_tags pt2
		           JOIN tags t2 ON t2.id = pt2.tag_id AND t2.slug = ?)`
		args2 = append(args2, tagSlug)
	} else {
		q += ` WHERE p.published=1`
	}
	q += ` GROUP BY p.id ORDER BY p.created_at DESC LIMIT ? OFFSET ?`
	args2 = append(args2, perPage, offset)

	rows, err := DB.Query(q, args2...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		var p Post
		var pub int
		var ca, ua, tagsStr string
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.BodyMD, &p.BodyHTML,
			&p.Cover, &pub, &p.Views, &ca, &ua, &tagsStr); err != nil {
			return nil, 0, err
		}
		p.Published = pub == 1
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
		p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ua)
		if tagsStr != "" {
			p.Tags = strings.Split(tagsStr, ",")
		}
		posts = append(posts, p)
	}
	return posts, total, rows.Err()
}

// IncrementViews атомарно увеличивает счётчик просмотров поста.
func IncrementViews(id int) {
	DB.Exec(`UPDATE posts SET views=views+1 WHERE id=?`, id)
}

func GetPostBySlug(slug string) (*Post, error) {
	var p Post
	var pub int
	var ca, ua, tagsStr string
	var seriesID sql.NullInt64
	var seriesName, seriesSlug sql.NullString
	err := DB.QueryRow(`
		SELECT p.id, p.slug, p.title, p.body_md, p.body_html, p.cover, p.published, p.views,
		       p.created_at, p.updated_at, p.series_id, s.name, s.slug,
		       COALESCE(GROUP_CONCAT(t.name, ','), '') as tags
		FROM posts p
		LEFT JOIN post_tags pt ON pt.post_id = p.id
		LEFT JOIN tags t ON t.id = pt.tag_id
		LEFT JOIN series s ON s.id = p.series_id
		WHERE p.slug=? GROUP BY p.id`, slug).
		Scan(&p.ID, &p.Slug, &p.Title, &p.BodyMD, &p.BodyHTML,
			&p.Cover, &pub, &p.Views, &ca, &ua, &seriesID, &seriesName, &seriesSlug, &tagsStr)
	if err != nil {
		return nil, err
	}
	p.Published = pub == 1
	p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
	p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ua)
	p.SeriesID = int(seriesID.Int64)
	p.SeriesName = seriesName.String
	p.SeriesSlug = seriesSlug.String
	if tagsStr != "" {
		p.Tags = strings.Split(tagsStr, ",")
	}
	return &p, nil
}

func GetPostByID(id int) (*Post, error) {
	var p Post
	var pub int
	var ca, ua, tagsStr string
	var seriesID sql.NullInt64
	var seriesName, seriesSlug sql.NullString
	err := DB.QueryRow(`
		SELECT p.id, p.slug, p.title, p.body_md, p.body_html, p.cover, p.published, p.views,
		       p.created_at, p.updated_at, p.series_id, s.name, s.slug,
		       COALESCE(GROUP_CONCAT(t.name, ','), '') as tags
		FROM posts p
		LEFT JOIN post_tags pt ON pt.post_id = p.id
		LEFT JOIN tags t ON t.id = pt.tag_id
		LEFT JOIN series s ON s.id = p.series_id
		WHERE p.id=? GROUP BY p.id`, id).
		Scan(&p.ID, &p.Slug, &p.Title, &p.BodyMD, &p.BodyHTML,
			&p.Cover, &pub, &p.Views, &ca, &ua, &seriesID, &seriesName, &seriesSlug, &tagsStr)
	if err != nil {
		return nil, err
	}
	p.Published = pub == 1
	p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
	p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ua)
	p.SeriesID = int(seriesID.Int64)
	p.SeriesName = seriesName.String
	p.SeriesSlug = seriesSlug.String
	if tagsStr != "" {
		p.Tags = strings.Split(tagsStr, ",")
	}
	return &p, nil
}

// seriesIDArg превращает 0 (нет серии) в NULL для записи в БД.
func seriesIDArg(seriesID int) any {
	if seriesID <= 0 {
		return nil
	}
	return seriesID
}

func CreatePost(slug, title, bodyMD, bodyHTML, cover string, published bool, seriesID int) (int64, error) {
	pub := 0
	if published {
		pub = 1
	}
	res, err := DB.Exec(`INSERT INTO posts (slug, title, body_md, body_html, cover, published, series_id)
	                      VALUES (?,?,?,?,?,?,?)`, slug, title, bodyMD, bodyHTML, cover, pub, seriesIDArg(seriesID))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdatePost(id int, slug, title, bodyMD, bodyHTML, cover string, published bool, seriesID int) error {
	pub := 0
	if published {
		pub = 1
	}
	_, err := DB.Exec(`UPDATE posts SET slug=?, title=?, body_md=?, body_html=?, cover=?,
	                   published=?, series_id=?, updated_at=datetime('now') WHERE id=?`,
		slug, title, bodyMD, bodyHTML, cover, pub, seriesIDArg(seriesID), id)
	return err
}

func DeletePost(id int) error {
	_, err := DB.Exec(`DELETE FROM posts WHERE id=?`, id)
	return err
}

// PostsInSeries возвращает опубликованные посты серии в хронологическом
// порядке (по дате создания) — так задаётся порядок серии: просто публикуй
// посты по порядку.
func PostsInSeries(seriesID int) ([]Post, error) {
	rows, err := DB.Query(`
		SELECT p.id, p.slug, p.title, p.cover, p.body_html, p.published, p.views, p.created_at, p.updated_at
		FROM posts p
		WHERE p.series_id=? AND p.published=1
		ORDER BY p.created_at ASC`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		var p Post
		var pub int
		var ca, ua string
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Cover, &p.BodyHTML, &pub, &p.Views, &ca, &ua); err != nil {
			return nil, err
		}
		p.Published = pub == 1
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
		p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ua)
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// ───────────────────────── Series ─────────────────────────

type Series struct {
	ID              int
	Name            string
	Slug            string
	DescriptionMD   string
	DescriptionHTML string
	Cover           string
	CollectPhotos   bool // true = страница серии показывает все фото из постов галереей, а не плитками постов
	ShowCover       bool // false = не показывать обложку-баннер на странице самой серии
	PostCount       int
	CreatedAt       time.Time
}

// ListSeries возвращает все серии с числом опубликованных постов в каждой,
// от новых к старым.
func ListSeries() ([]Series, error) {
	rows, err := DB.Query(`
		SELECT s.id, s.name, s.slug, s.description_md, s.description_html, s.cover, s.collect_photos, s.show_cover, s.created_at,
		       COUNT(p.id) FILTER (WHERE p.published=1) as post_count
		FROM series s
		LEFT JOIN posts p ON p.series_id = s.id
		GROUP BY s.id
		ORDER BY s.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Series
	for rows.Next() {
		var s Series
		var cp, sc int
		var ca string
		if err := rows.Scan(&s.ID, &s.Name, &s.Slug, &s.DescriptionMD, &s.DescriptionHTML, &s.Cover, &cp, &sc, &ca, &s.PostCount); err != nil {
			return nil, err
		}
		s.CollectPhotos = cp == 1
		s.ShowCover = sc == 1
		s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
		list = append(list, s)
	}
	return list, rows.Err()
}

func GetSeriesBySlug(slug string) (*Series, error) {
	var s Series
	var cp, sc int
	var ca string
	err := DB.QueryRow(`SELECT id, name, slug, description_md, description_html, cover, collect_photos, show_cover, created_at
	                     FROM series WHERE slug=?`, slug).
		Scan(&s.ID, &s.Name, &s.Slug, &s.DescriptionMD, &s.DescriptionHTML, &s.Cover, &cp, &sc, &ca)
	if err != nil {
		return nil, err
	}
	s.CollectPhotos = cp == 1
	s.ShowCover = sc == 1
	s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
	return &s, nil
}

func GetSeriesByID(id int) (*Series, error) {
	var s Series
	var cp, sc int
	var ca string
	err := DB.QueryRow(`SELECT id, name, slug, description_md, description_html, cover, collect_photos, show_cover, created_at
	                     FROM series WHERE id=?`, id).
		Scan(&s.ID, &s.Name, &s.Slug, &s.DescriptionMD, &s.DescriptionHTML, &s.Cover, &cp, &sc, &ca)
	if err != nil {
		return nil, err
	}
	s.CollectPhotos = cp == 1
	s.ShowCover = sc == 1
	s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
	return &s, nil
}

func CreateSeries(name, slug, descMD, descHTML, cover string, collectPhotos, showCover bool) (int64, error) {
	cp, sc := 0, 0
	if collectPhotos {
		cp = 1
	}
	if showCover {
		sc = 1
	}
	res, err := DB.Exec(`INSERT INTO series (name, slug, description_md, description_html, cover, collect_photos, show_cover)
	                      VALUES (?,?,?,?,?,?,?)`, name, slug, descMD, descHTML, cover, cp, sc)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateSeries(id int, name, slug, descMD, descHTML, cover string, collectPhotos, showCover bool) error {
	cp, sc := 0, 0
	if collectPhotos {
		cp = 1
	}
	if showCover {
		sc = 1
	}
	_, err := DB.Exec(`UPDATE series SET name=?, slug=?, description_md=?, description_html=?, cover=?, collect_photos=?, show_cover=? WHERE id=?`,
		name, slug, descMD, descHTML, cover, cp, sc, id)
	return err
}

func DeleteSeries(id int) error {
	_, err := DB.Exec(`DELETE FROM series WHERE id=?`, id)
	return err
}

// ───────────────────────── Categories ─────────────────────────

type Category struct {
	ID   int
	Name string
	Slug string
}

func ListCategories() ([]Category, error) {
	rows, err := DB.Query(`SELECT id, name, slug FROM categories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cats []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug); err != nil {
			return nil, err
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

func CreateCategory(name, slug string) error {
	_, err := DB.Exec(`INSERT INTO categories (name, slug) VALUES (?, ?)`, name, slug)
	return err
}

func DeleteCategory(id int) error {
	_, err := DB.Exec(`DELETE FROM categories WHERE id=?`, id)
	return err
}

// ───────────────────────── Photos ─────────────────────────

type Photo struct {
	ID           int
	Filename     string
	Caption      string
	SortOrder    int
	CategoryID   int
	CategoryName string
	PlaceID      int
	PlaceName    string
	Width        int
	Height       int
	CreatedAt    time.Time
}

// ListPhotos returns photos optionally filtered by category slug and/or place slug.
// Pass empty strings to return all photos.
func ListPhotos(categorySlug, placeSlug string) ([]Photo, error) {
	q := `SELECT p.id, p.filename, p.caption, p.sort_order,
	             COALESCE(p.category_id, 0), COALESCE(c.name, ''),
	             COALESCE(p.place_id, 0), COALESCE(pl.name, ''),
	             p.width, p.height, p.created_at
	      FROM photos p
	      LEFT JOIN categories c ON c.id = p.category_id
	      LEFT JOIN places pl ON pl.id = p.place_id`
	args := []any{}
	var conds []string
	if categorySlug != "" {
		conds = append(conds, `c.slug = ?`)
		args = append(args, categorySlug)
	}
	if placeSlug != "" {
		conds = append(conds, `pl.slug = ?`)
		args = append(args, placeSlug)
	}
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	q += ` ORDER BY p.sort_order, p.id`

	rows, err := DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var photos []Photo
	for rows.Next() {
		var p Photo
		var ca string
		if err := rows.Scan(&p.ID, &p.Filename, &p.Caption, &p.SortOrder,
			&p.CategoryID, &p.CategoryName, &p.PlaceID, &p.PlaceName,
			&p.Width, &p.Height, &ca); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
		photos = append(photos, p)
	}
	return photos, rows.Err()
}

func SearchPosts(q string) ([]Post, error) {
	var terms []string
	for _, w := range strings.Fields(q) {
		// Strip FTS5 special chars to prevent query syntax errors
		clean := strings.Map(func(r rune) rune {
			switch r {
			case '"', '\'', '(', ')', '*', '^', '-', '+', ':', '.':
				return -1
			}
			return r
		}, w)
		if clean != "" {
			terms = append(terms, clean+"*")
		}
	}
	if len(terms) == 0 {
		return nil, nil
	}
	rows, err := DB.Query(`
		SELECT p.id, p.slug, p.title, p.body_html, p.cover, p.created_at
		FROM posts_fts f
		JOIN posts p ON p.id = f.rowid
		WHERE posts_fts MATCH ? AND p.published = 1
		ORDER BY rank
		LIMIT 20
	`, strings.Join(terms, " "))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		var p Post
		var ca string
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.BodyHTML, &p.Cover, &ca); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

func UpdatePhotoDimensions(id, width, height int) error {
	_, err := DB.Exec(`UPDATE photos SET width=?, height=? WHERE id=?`, width, height, id)
	return err
}

func UpdatePhotoCategory(id, categoryID int) error {
	if categoryID == 0 {
		_, err := DB.Exec(`UPDATE photos SET category_id=NULL WHERE id=?`, id)
		return err
	}
	_, err := DB.Exec(`UPDATE photos SET category_id=? WHERE id=?`, categoryID, id)
	return err
}

func UpdatePhotoOrder(ids []int) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE photos SET sort_order=? WHERE id=?`, i+1, id); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func AddPhoto(filename, caption string, categoryID, placeID, width, height int) error {
	var catArg, placeArg any
	if categoryID != 0 {
		catArg = categoryID
	}
	if placeID != 0 {
		placeArg = placeID
	}
	_, err := DB.Exec(`INSERT INTO photos (filename, caption, category_id, place_id, width, height, sort_order)
	                   VALUES (?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM photos))`,
		filename, caption, catArg, placeArg, width, height)
	return err
}

func UpdatePhotoPlace(id, placeID int) error {
	if placeID == 0 {
		_, err := DB.Exec(`UPDATE photos SET place_id=NULL WHERE id=?`, id)
		return err
	}
	_, err := DB.Exec(`UPDATE photos SET place_id=? WHERE id=?`, placeID, id)
	return err
}

func DeletePhoto(id int) (string, error) {
	var filename string
	err := DB.QueryRow(`SELECT filename FROM photos WHERE id=?`, id).Scan(&filename)
	if err != nil {
		return "", err
	}
	_, err = DB.Exec(`DELETE FROM photos WHERE id=?`, id)
	return filename, err
}

// ───────────────────────── Places ─────────────────────────

type Place struct {
	ID   int
	Name string
	Slug string
}

func ListPlaces() ([]Place, error) {
	rows, err := DB.Query(`SELECT id, name, slug FROM places ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var places []Place
	for rows.Next() {
		var p Place
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug); err != nil {
			return nil, err
		}
		places = append(places, p)
	}
	return places, rows.Err()
}

func CreatePlace(name, slug string) error {
	_, err := DB.Exec(`INSERT INTO places (name, slug) VALUES (?, ?)`, name, slug)
	return err
}

func DeletePlace(id int) error {
	_, err := DB.Exec(`DELETE FROM places WHERE id=?`, id)
	return err
}

// ───────────────────────── Resume ─────────────────────────

type Resume struct {
	BodyMD   string
	BodyHTML string
}

func GetResume() (*Resume, error) {
	var r Resume
	err := DB.QueryRow(`SELECT body_md, body_html FROM resume WHERE id=1`).
		Scan(&r.BodyMD, &r.BodyHTML)
	return &r, err
}

func UpdateResume(bodyMD, bodyHTML string) error {
	_, err := DB.Exec(`UPDATE resume SET body_md=?, body_html=? WHERE id=1`, bodyMD, bodyHTML)
	return err
}

// ───────────────────────── Sessions ─────────────────────────

func CreateSession(token string, expiresAt time.Time) error {
	_, err := DB.Exec(`INSERT INTO sessions (token, expires_at) VALUES (?, ?)`,
		token, expiresAt.UTC().Format("2006-01-02 15:04:05"))
	return err
}

func SessionValid(token string) bool {
	var exp string
	err := DB.QueryRow(`SELECT expires_at FROM sessions WHERE token=?`, token).Scan(&exp)
	if err != nil {
		return false
	}
	t, err := time.Parse("2006-01-02 15:04:05", exp)
	return err == nil && time.Now().Before(t)
}

func DeleteSession(token string) {
	DB.Exec(`DELETE FROM sessions WHERE token=?`, token)
}

// DeleteAllSessions разлогинивает все устройства (после смены пароля).
func DeleteAllSessions() {
	DB.Exec(`DELETE FROM sessions`)
}

func CleanExpiredSessions() {
	DB.Exec(`DELETE FROM sessions WHERE expires_at < datetime('now')`)
}

// ───────────────────────── Tags ─────────────────────────

type Tag struct {
	ID   int
	Name string
	Slug string
}

// DeleteTagByName удаляет тег по имени вместе со всеми его привязками к
// постам (post_tags каскадно) — используется для чистки опечаток из
// автодополнения тегов.
func DeleteTagByName(name string) error {
	_, err := DB.Exec(`DELETE FROM tags WHERE slug=?`, tagSlug(name))
	return err
}

// ListAllTags возвращает все теги отсортированные по имени.
func ListAllTags() ([]Tag, error) {
	rows, err := DB.Query(`SELECT id, name, slug FROM tags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// SetPostTags заменяет теги поста. names — список имён тегов (могут быть новые).
func SetPostTags(postID int, names []string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM post_tags WHERE post_id=?`, postID); err != nil {
		tx.Rollback()
		return err
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		slug := tagSlug(name)
		if _, err := tx.Exec(`INSERT OR IGNORE INTO tags (name, slug) VALUES (?,?)`, name, slug); err != nil {
			tx.Rollback()
			return err
		}
		var tagID int
		if err := tx.QueryRow(`SELECT id FROM tags WHERE slug=?`, slug).Scan(&tagID); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO post_tags (post_id, tag_id) VALUES (?,?)`, postID, tagID); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// tagSlug делает slug из имени тега (lowercase, пробелы → дефис).
func tagSlug(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			(r >= 'а' && r <= 'я') || r == 'ё':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	res := b.String()
	for strings.Contains(res, "--") {
		res = strings.ReplaceAll(res, "--", "-")
	}
	return strings.Trim(res, "-")
}

// ───────────────────────── Uploads ─────────────────────────

type FileUsage struct {
	InGallery  bool
	PostTitles []string
}

func GetFileUsage(filename string) (FileUsage, error) {
	var u FileUsage
	var count int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM photos WHERE filename=?`, filename).Scan(&count); err != nil {
		return u, err
	}
	u.InGallery = count > 0

	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(filename)
	rows, err := DB.Query(`SELECT title FROM posts WHERE cover=? OR body_md LIKE ? ESCAPE '\'`,
		filename, "%"+escaped+"%")
	if err != nil {
		return u, err
	}
	defer rows.Close()
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return u, err
		}
		u.PostTitles = append(u.PostTitles, title)
	}
	return u, rows.Err()
}

// GetAllFileUsage reports gallery/post usage for every name in filenames in
// one pass over photos and posts, instead of running GetFileUsage's two
// queries per file — on an uploads folder with hundreds of files that was
// O(files × posts) in round-trips alone.
func GetAllFileUsage(filenames []string) (map[string]FileUsage, error) {
	usage := make(map[string]FileUsage, len(filenames))
	for _, f := range filenames {
		usage[f] = FileUsage{}
	}

	galleryRows, err := DB.Query(`SELECT filename FROM photos`)
	if err != nil {
		return nil, err
	}
	defer galleryRows.Close()
	for galleryRows.Next() {
		var fn string
		if err := galleryRows.Scan(&fn); err != nil {
			return nil, err
		}
		if u, ok := usage[fn]; ok {
			u.InGallery = true
			usage[fn] = u
		}
	}
	if err := galleryRows.Err(); err != nil {
		return nil, err
	}

	postRows, err := DB.Query(`SELECT title, cover, body_md FROM posts`)
	if err != nil {
		return nil, err
	}
	defer postRows.Close()
	for postRows.Next() {
		var title, cover, bodyMD string
		if err := postRows.Scan(&title, &cover, &bodyMD); err != nil {
			return nil, err
		}
		for fn, u := range usage {
			if fn == cover || strings.Contains(bodyMD, fn) {
				u.PostTitles = append(u.PostTitles, title)
				usage[fn] = u
			}
		}
	}
	return usage, postRows.Err()
}

// ───────────────────────── Settings ─────────────────────────

func GetAllSettings() map[string]string {
	rows, err := DB.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			m[k] = v
		}
	}
	return m
}

func SetSetting(key, value string) error {
	_, err := DB.Exec(`INSERT INTO settings (key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// SeedSettings inserts defaults only if keys don't already exist.
func SeedSettings(defaults map[string]string) {
	for k, v := range defaults {
		DB.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES (?,?)`, k, v)
	}
}
