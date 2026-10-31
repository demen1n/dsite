package db

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	if err := Init(":memory:"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { DB.Close() })
}

// ───── Users ─────

func TestUserCRUD(t *testing.T) {
	setupTestDB(t)

	exists, err := UserExists()
	if err != nil || exists {
		t.Fatalf("UserExists initially: got %v, %v", exists, err)
	}

	if err := CreateUser("admin", "hash123"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	exists, err = UserExists()
	if err != nil || !exists {
		t.Errorf("UserExists after create: got %v, %v", exists, err)
	}

	id, hash, err := GetUserByLogin("admin")
	if err != nil || id == 0 || hash != "hash123" {
		t.Errorf("GetUserByLogin: id=%d hash=%q err=%v", id, hash, err)
	}

	if err := UpdatePassword("admin", "newhash"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	_, hash, _ = GetUserByLogin("admin")
	if hash != "newhash" {
		t.Errorf("UpdatePassword: got %q, want newhash", hash)
	}
}

// ───── Posts ─────

func TestPostCRUD(t *testing.T) {
	setupTestDB(t)

	id, err := CreatePost("test-post", "Test Post", "# Hello", "<h1>Hello</h1>", "", true, 0)
	if err != nil || id == 0 {
		t.Fatalf("CreatePost: id=%d err=%v", id, err)
	}

	p, err := GetPostBySlug("test-post")
	if err != nil || p.Title != "Test Post" || !p.Published {
		t.Fatalf("GetPostBySlug: %+v err=%v", p, err)
	}

	p2, err := GetPostByID(int(id))
	if err != nil || p2.Slug != "test-post" {
		t.Errorf("GetPostByID: slug=%q err=%v", p2.Slug, err)
	}

	if err := UpdatePost(int(id), "updated-slug", "Updated", "## Up", "<h2>Up</h2>", "", false, 0); err != nil {
		t.Fatalf("UpdatePost: %v", err)
	}
	p3, _ := GetPostByID(int(id))
	if p3.Slug != "updated-slug" || p3.Published {
		t.Errorf("UpdatePost: slug=%q published=%v", p3.Slug, p3.Published)
	}

	posts, err := ListPosts(false)
	if err != nil || len(posts) != 1 {
		t.Errorf("ListPosts: len=%d err=%v", len(posts), err)
	}

	if err := DeletePost(int(id)); err != nil {
		t.Fatalf("DeletePost: %v", err)
	}
	posts, _ = ListPosts(false)
	if len(posts) != 0 {
		t.Errorf("after delete: len=%d", len(posts))
	}
}

func TestListPostsMeta(t *testing.T) {
	setupTestDB(t)

	CreatePost("meta-test", "Meta Test", "full markdown body", "<p>full markdown body</p>", "", true, 0) //nolint
	CreatePost("meta-draft", "Meta Draft", "draft body", "<p>draft body</p>", "", false, 0)              //nolint

	published, err := ListPostsMeta(true)
	if err != nil || len(published) != 1 {
		t.Fatalf("ListPostsMeta(true): got %v err=%v", published, err)
	}
	if published[0].Title != "Meta Test" || published[0].Slug != "meta-test" {
		t.Errorf("ListPostsMeta(true): got %+v", published[0])
	}
	if published[0].BodyMD != "" || published[0].BodyHTML != "" {
		t.Errorf("ListPostsMeta should leave body fields unset, got MD=%q HTML=%q", published[0].BodyMD, published[0].BodyHTML)
	}

	all, err := ListPostsMeta(false)
	if err != nil || len(all) != 2 {
		t.Fatalf("ListPostsMeta(false): got %v err=%v", all, err)
	}
}

func TestListPostsPaginated(t *testing.T) {
	setupTestDB(t)

	for i := 1; i <= 3; i++ {
		if _, err := CreatePost(fmt.Sprintf("post-%d", i), fmt.Sprintf("Post %d", i), "", "", "", true, 0); err != nil {
			t.Fatal(err)
		}
	}
	CreatePost("draft", "Draft", "", "", "", false, 0) //nolint

	posts, total, err := ListPostsPaginated("", 1, 10)
	if err != nil {
		t.Fatalf("ListPostsPaginated: %v", err)
	}
	if total != 3 {
		t.Errorf("total: got %d, want 3", total)
	}
	if len(posts) != 3 {
		t.Errorf("posts: got %d, want 3", len(posts))
	}

	// page 2 with pageSize=2 should have 1 post
	posts, total, err = ListPostsPaginated("", 2, 2)
	if err != nil {
		t.Fatalf("ListPostsPaginated page2: %v", err)
	}
	if total != 3 || len(posts) != 1 {
		t.Errorf("page2: total=%d len=%d", total, len(posts))
	}
}

func TestIncrementViews(t *testing.T) {
	setupTestDB(t)

	id, _ := CreatePost("views-test", "Views", "", "", "", true, 0)
	IncrementViews(int(id))
	IncrementViews(int(id))

	p, _ := GetPostByID(int(id))
	if p.Views != 2 {
		t.Errorf("IncrementViews: got %d, want 2", p.Views)
	}
}

// A view-counter bump must not re-index the post in posts_fts: the update
// trigger has to be scoped to the indexed columns.
func TestFTSUpdateTriggerScoped(t *testing.T) {
	setupTestDB(t)
	var sql string
	if err := DB.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name='posts_fts_au'`).Scan(&sql); err != nil {
		t.Fatalf("posts_fts_au: %v", err)
	}
	if !strings.Contains(sql, "UPDATE OF title, body_md ON posts") {
		t.Errorf("posts_fts_au not scoped to title/body_md: %s", sql)
	}
}

// ───── Tags ─────

func TestSetPostTags(t *testing.T) {
	setupTestDB(t)

	id, _ := CreatePost("tagged", "Tagged", "", "", "", true, 0)

	if err := SetPostTags(int(id), []string{"Go", "Программирование", "Web"}); err != nil {
		t.Fatalf("SetPostTags: %v", err)
	}
	p, _ := GetPostBySlug("tagged")
	if len(p.Tags) != 3 {
		t.Errorf("after set: tags=%v", p.Tags)
	}

	// Replace tags — old ones are removed
	if err := SetPostTags(int(id), []string{"Go"}); err != nil {
		t.Fatalf("SetPostTags replace: %v", err)
	}
	p, _ = GetPostBySlug("tagged")
	if len(p.Tags) != 1 || p.Tags[0] != "Go" {
		t.Errorf("after replace: tags=%v", p.Tags)
	}

	tags, err := ListAllTags()
	if err != nil || len(tags) == 0 {
		t.Errorf("ListAllTags: %v err=%v", tags, err)
	}
}

func TestGetTagBySlug(t *testing.T) {
	setupTestDB(t)

	id, _ := CreatePost("p", "P", "", "", "", true, 0)
	SetPostTags(int(id), []string{"Комсомольск-на-Амуре"})

	tag, err := GetTagBySlug("комсомольск-на-амуре")
	if err != nil || tag.Name != "Комсомольск-на-Амуре" {
		t.Errorf("GetTagBySlug: %+v err=%v", tag, err)
	}
	if _, err := GetTagBySlug("nope"); err == nil {
		t.Error("GetTagBySlug(nope): want error")
	}
}

func TestListPublishedTags(t *testing.T) {
	setupTestDB(t)

	pub, _ := CreatePost("pub", "Pub", "", "", "", true, 0)
	draft, _ := CreatePost("draft", "Draft", "", "", "", false, 0)
	SetPostTags(int(pub), []string{"Go", "Shared"})
	SetPostTags(int(draft), []string{"DraftOnly", "Shared"})

	tags, err := ListPublishedTags()
	if err != nil {
		t.Fatalf("ListPublishedTags: %v", err)
	}
	var names []string
	for _, tg := range tags {
		names = append(names, tg.Name)
		if tg.LastMod.IsZero() {
			t.Errorf("tag %q: zero LastMod", tg.Name)
		}
	}
	if fmt.Sprint(names) != "[Go Shared]" {
		t.Errorf("ListPublishedTags names = %v, want [Go Shared]", names)
	}
}

func TestTagSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Go", "go"},
		{"Web Dev", "web-dev"},
		{"Программирование", "программирование"},
		{"C++", "c"},
		{"  trim  ", "trim"},
	}
	for _, tc := range cases {
		if got := TagSlug(tc.in); got != tc.want {
			t.Errorf("TagSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ───── Categories ─────

func TestCategoryCRUD(t *testing.T) {
	setupTestDB(t)

	if err := CreateCategory("Nature", "nature"); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	cats, err := ListCategories()
	if err != nil || len(cats) != 1 || cats[0].Name != "Nature" {
		t.Errorf("ListCategories: %v err=%v", cats, err)
	}

	if err := DeleteCategory(cats[0].ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	cats, _ = ListCategories()
	if len(cats) != 0 {
		t.Errorf("after delete: %v", cats)
	}
}

// ───── Places ─────

func TestPlaceCRUD(t *testing.T) {
	setupTestDB(t)

	if err := CreatePlace("Moscow", "moscow"); err != nil {
		t.Fatalf("CreatePlace: %v", err)
	}

	places, err := ListPlaces()
	if err != nil || len(places) != 1 || places[0].Slug != "moscow" {
		t.Errorf("ListPlaces: %v err=%v", places, err)
	}

	if err := DeletePlace(places[0].ID); err != nil {
		t.Fatalf("DeletePlace: %v", err)
	}
	places, _ = ListPlaces()
	if len(places) != 0 {
		t.Errorf("after delete: %v", places)
	}
}

// ───── Photos ─────

func TestPhotoCRUD(t *testing.T) {
	setupTestDB(t)

	if err := AddPhoto("test.webp", "A photo", 0, 0, 1920, 1080); err != nil {
		t.Fatalf("AddPhoto: %v", err)
	}

	photos, err := ListPhotos("", "")
	if err != nil || len(photos) != 1 {
		t.Fatalf("ListPhotos: len=%d err=%v", len(photos), err)
	}
	if photos[0].Width != 1920 || photos[0].Height != 1080 {
		t.Errorf("dimensions: got %dx%d", photos[0].Width, photos[0].Height)
	}

	id := photos[0].ID
	if err := UpdatePhotoDimensions(id, 800, 600); err != nil {
		t.Fatalf("UpdatePhotoDimensions: %v", err)
	}
	photos, _ = ListPhotos("", "")
	if photos[0].Width != 800 {
		t.Errorf("after update: width=%d", photos[0].Width)
	}

	filename, _, err := DeletePhoto(id)
	if err != nil || filename != "test.webp" {
		t.Errorf("DeletePhoto: filename=%q err=%v", filename, err)
	}
	photos, _ = ListPhotos("", "")
	if len(photos) != 0 {
		t.Errorf("after delete: len=%d", len(photos))
	}
}

func TestPhotoOrder(t *testing.T) {
	setupTestDB(t)

	AddPhoto("a.webp", "", 0, 0, 0, 0) //nolint
	AddPhoto("b.webp", "", 0, 0, 0, 0) //nolint
	AddPhoto("c.webp", "", 0, 0, 0, 0) //nolint

	photos, _ := ListPhotos("", "")
	// Reverse the order
	ids := []int{photos[2].ID, photos[0].ID, photos[1].ID}
	if err := UpdatePhotoOrder(ids); err != nil {
		t.Fatalf("UpdatePhotoOrder: %v", err)
	}

	photos, _ = ListPhotos("", "")
	for i, wantID := range ids {
		if photos[i].ID != wantID {
			t.Errorf("position %d: got ID %d, want %d", i, photos[i].ID, wantID)
		}
	}
}

func TestPhotoFilterByCategory(t *testing.T) {
	setupTestDB(t)

	CreateCategory("Travel", "travel") //nolint
	cats, _ := ListCategories()
	catID := cats[0].ID

	AddPhoto("cat.webp", "", catID, 0, 0, 0) //nolint
	AddPhoto("any.webp", "", 0, 0, 0, 0)     //nolint

	photos, err := ListPhotos("travel", "")
	if err != nil || len(photos) != 1 || photos[0].Filename != "cat.webp" {
		t.Errorf("filter by category: %v err=%v", photos, err)
	}
}

// ───── Sessions ─────

func TestSessions(t *testing.T) {
	setupTestDB(t)

	if err := CreateSession("tok1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if !SessionValid("tok1") {
		t.Error("should be valid for fresh session")
	}

	DeleteSession("tok1")
	if SessionValid("tok1") {
		t.Error("should be invalid after delete")
	}
}

func TestExpiredSession(t *testing.T) {
	setupTestDB(t)

	if err := CreateSession("expired", time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if SessionValid("expired") {
		t.Error("should be invalid for expired session")
	}
}

func TestCleanExpiredSessions(t *testing.T) {
	setupTestDB(t)

	CreateSession("alive", time.Now().Add(time.Hour)) //nolint
	CreateSession("dead", time.Now().Add(-time.Hour)) //nolint

	CleanExpiredSessions()

	if !SessionValid("alive") {
		t.Error("alive session should survive cleanup")
	}
	// dead was already expired so SessionValid returns false; just confirm it doesn't panic
}

// ───── Settings ─────

func TestSettings(t *testing.T) {
	setupTestDB(t)

	if err := SetSetting("key1", "val1"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := SetSetting("key1", "val2"); err != nil { // upsert
		t.Fatalf("SetSetting upsert: %v", err)
	}

	s := GetAllSettings()
	if s["key1"] != "val2" {
		t.Errorf("GetAllSettings: key1=%q, want val2", s["key1"])
	}
}

func TestSeedSettings(t *testing.T) {
	setupTestDB(t)

	SeedSettings(map[string]string{"foo": "bar", "baz": "qux"})
	s := GetAllSettings()
	if s["foo"] != "bar" || s["baz"] != "qux" {
		t.Errorf("SeedSettings: %v", s)
	}

	// Second seed must not override existing values
	SeedSettings(map[string]string{"foo": "overridden"})
	s = GetAllSettings()
	if s["foo"] != "bar" {
		t.Errorf("SeedSettings idempotent: got %q, want bar", s["foo"])
	}
}

// ───── Resume ─────

func TestResume(t *testing.T) {
	setupTestDB(t)

	r, err := GetResume()
	if err != nil || r.BodyMD != "" {
		t.Fatalf("GetResume initial: %+v err=%v", r, err)
	}

	if err := UpdateResume("# CV", "<h1>CV</h1>"); err != nil {
		t.Fatalf("UpdateResume: %v", err)
	}

	r, _ = GetResume()
	if r.BodyMD != "# CV" || r.BodyHTML != "<h1>CV</h1>" {
		t.Errorf("GetResume after update: %+v", r)
	}
}

// ───── Search (FTS5) ─────

func TestSearchPosts(t *testing.T) {
	setupTestDB(t)

	CreatePost("search-test", "Search Test Post", "findme unique keyword", "<p>findme</p>", "", true, 0) //nolint
	CreatePost("other", "Other Post", "unrelated content", "<p>other</p>", "", true, 0)                  //nolint

	results, err := SearchPosts("findme")
	if err != nil {
		t.Fatalf("SearchPosts: %v", err)
	}
	if len(results) != 1 || results[0].Slug != "search-test" {
		t.Errorf("SearchPosts: got %v", results)
	}

	// Empty query returns nil, no error
	results, err = SearchPosts("")
	if err != nil || results != nil {
		t.Errorf("SearchPosts empty: got %v err=%v", results, err)
	}

	// Special chars must not crash
	if _, err := SearchPosts(`"special" (chars) *`); err != nil {
		t.Errorf("SearchPosts special chars: %v", err)
	}
}

// TestSearchPostsIndexStaysInSync guards against the FTS5 external-content
// trigger bug where UPDATE/DELETE on posts left stale/phantom entries in
// posts_fts (see AUDIT.md §1.1): edits must drop old tokens, deletes must
// remove the row from the index.
func TestSearchPostsIndexStaysInSync(t *testing.T) {
	setupTestDB(t)

	id, err := CreatePost("sync-test", "Sync Test", "alphaword unique", "<p>alphaword</p>", "", true, 0)
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}

	results, err := SearchPosts("alphaword")
	if err != nil || len(results) != 1 {
		t.Fatalf("SearchPosts before update: got %v err=%v", results, err)
	}

	if err := UpdatePost(int(id), "sync-test", "Sync Test", "betaword unique", "<p>betaword</p>", "", true, 0); err != nil {
		t.Fatalf("UpdatePost: %v", err)
	}

	results, err = SearchPosts("alphaword")
	if err != nil || len(results) != 0 {
		t.Errorf("SearchPosts after update should not find old text, got %v err=%v", results, err)
	}
	results, err = SearchPosts("betaword")
	if err != nil || len(results) != 1 {
		t.Errorf("SearchPosts after update should find new text, got %v err=%v", results, err)
	}

	if err := DeletePost(int(id)); err != nil {
		t.Fatalf("DeletePost: %v", err)
	}

	results, err = SearchPosts("betaword")
	if err != nil || len(results) != 0 {
		t.Errorf("SearchPosts after delete should be empty, got %v err=%v", results, err)
	}
}

func TestSearchPostsDraft(t *testing.T) {
	setupTestDB(t)

	CreatePost("draft-search", "Draft Post", "secretword", "", "", false, 0) //nolint

	results, err := SearchPosts("secretword")
	if err != nil {
		t.Fatalf("SearchPosts: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("SearchPosts should not return drafts, got %v", results)
	}
}

// ───── GetFileUsage ─────

func TestGetFileUsage(t *testing.T) {
	setupTestDB(t)

	AddPhoto("used.webp", "", 0, 0, 0, 0) //nolint

	usage, err := GetFileUsage("used.webp")
	if err != nil {
		t.Fatalf("GetFileUsage: %v", err)
	}
	if !usage.InGallery {
		t.Error("InGallery: want true")
	}

	usage, err = GetFileUsage("unused.webp")
	if err != nil {
		t.Fatalf("GetFileUsage unused: %v", err)
	}
	if usage.InGallery {
		t.Error("InGallery: want false for unused file")
	}
}

func TestGetAllFileUsage(t *testing.T) {
	setupTestDB(t)

	AddPhoto("gallery.webp", "", 0, 0, 0, 0)                                                                     //nolint
	CreatePost("cover-post", "Cover Post", "no mention here", "", "cover.jpg", true, 0)                          //nolint
	CreatePost("body-post", "Body Post", "see inline.jpg embedded here", "", "", true, 0)                        //nolint
	CreateSeries("Trip", "trip", "no mention here", "", "series-cover.jpg", false, true, false, false)           //nolint
	CreateSeries("Journal", "journal", "see series-inline.jpg embedded here", "", "", false, true, false, false) //nolint
	SetSetting("home_avatar", "avatar.jpg")                                                                      //nolint
	UpdateResume("see resume-inline.jpg embedded here", "")                                                      //nolint

	usage, err := GetAllFileUsage([]string{
		"gallery.webp", "cover.jpg", "inline.jpg",
		"series-cover.jpg", "series-inline.jpg", "avatar.jpg", "resume-inline.jpg",
		"orphan.webp",
	})
	if err != nil {
		t.Fatalf("GetAllFileUsage: %v", err)
	}

	if !usage["gallery.webp"].InGallery {
		t.Error("gallery.webp: want InGallery true")
	}
	if len(usage["cover.jpg"].PostTitles) != 1 || usage["cover.jpg"].PostTitles[0] != "Cover Post" {
		t.Errorf("cover.jpg: got %v", usage["cover.jpg"].PostTitles)
	}
	if len(usage["inline.jpg"].PostTitles) != 1 || usage["inline.jpg"].PostTitles[0] != "Body Post" {
		t.Errorf("inline.jpg: got %v", usage["inline.jpg"].PostTitles)
	}
	if len(usage["series-cover.jpg"].SeriesTitles) != 1 || usage["series-cover.jpg"].SeriesTitles[0] != "Trip" {
		t.Errorf("series-cover.jpg: got %v", usage["series-cover.jpg"].SeriesTitles)
	}
	if len(usage["series-inline.jpg"].SeriesTitles) != 1 || usage["series-inline.jpg"].SeriesTitles[0] != "Journal" {
		t.Errorf("series-inline.jpg: got %v", usage["series-inline.jpg"].SeriesTitles)
	}
	if !usage["avatar.jpg"].IsAvatar {
		t.Error("avatar.jpg: want IsAvatar true")
	}
	if !usage["resume-inline.jpg"].InResume {
		t.Error("resume-inline.jpg: want InResume true")
	}
	if usage["orphan.webp"].InUse() {
		t.Errorf("orphan.webp: want unused, got %+v", usage["orphan.webp"])
	}
	for name, u := range usage {
		if name == "orphan.webp" {
			continue
		}
		if !u.InUse() {
			t.Errorf("%s: want InUse true, got %+v", name, u)
		}
	}
}

func TestFileUsage_SeriesAvatarResume_GetFileUsage(t *testing.T) {
	setupTestDB(t)

	CreateSeries("Trip", "trip", "", "", "series-cover.jpg", false, true, false, false) //nolint
	SetSetting("home_avatar", "avatar.jpg")                                             //nolint
	UpdateResume("see resume-inline.jpg here", "")                                      //nolint

	seriesUsage, err := GetFileUsage("series-cover.jpg")
	if err != nil {
		t.Fatalf("GetFileUsage series cover: %v", err)
	}
	if len(seriesUsage.SeriesTitles) != 1 || seriesUsage.SeriesTitles[0] != "Trip" || !seriesUsage.InUse() {
		t.Errorf("series-cover.jpg: got %+v", seriesUsage)
	}

	avatarUsage, err := GetFileUsage("avatar.jpg")
	if err != nil {
		t.Fatalf("GetFileUsage avatar: %v", err)
	}
	if !avatarUsage.IsAvatar || !avatarUsage.InUse() {
		t.Errorf("avatar.jpg: got %+v", avatarUsage)
	}

	resumeUsage, err := GetFileUsage("resume-inline.jpg")
	if err != nil {
		t.Fatalf("GetFileUsage resume: %v", err)
	}
	if !resumeUsage.InResume || !resumeUsage.InUse() {
		t.Errorf("resume-inline.jpg: got %+v", resumeUsage)
	}
}

// ───── Hidden series ─────

func TestHiddenSeries(t *testing.T) {
	setupTestDB(t)

	visibleID, err := CreateSeries("Seoul", "seoul", "", "", "", false, true, false, false)
	if err != nil {
		t.Fatalf("CreateSeries visible: %v", err)
	}
	hiddenID, err := CreateSeries("Osaka", "osaka", "", "", "", false, true, true, false)
	if err != nil {
		t.Fatalf("CreateSeries hidden: %v", err)
	}

	s, err := GetSeriesBySlug("osaka")
	if err != nil || !s.Hidden {
		t.Fatalf("GetSeriesBySlug osaka: hidden=%v, err=%v", s != nil && s.Hidden, err)
	}

	all, _ := ListSeries()
	if len(all) != 2 {
		t.Errorf("ListSeries: got %d, want 2 (admin sees hidden too)", len(all))
	}
	visible, err := ListVisibleSeries()
	if err != nil {
		t.Fatalf("ListVisibleSeries: %v", err)
	}
	if len(visible) != 1 || visible[0].ID != int(visibleID) {
		t.Errorf("ListVisibleSeries: got %+v, want only Seoul", visible)
	}

	postID, err := CreatePost("day-1", "Day 1", "b", "<p>b</p>", "", true, int(hiddenID))
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	p, err := GetPostByID(int(postID))
	if err != nil || !p.SeriesHidden {
		t.Errorf("post in hidden series: SeriesHidden=%v, err=%v", p != nil && p.SeriesHidden, err)
	}

	// Снимаем флаг — серия снова видна.
	if err := UpdateSeries(int(hiddenID), "Osaka", "osaka", "", "", "", false, true, false, false); err != nil {
		t.Fatalf("UpdateSeries: %v", err)
	}
	if visible, _ := ListVisibleSeries(); len(visible) != 2 {
		t.Errorf("after unhide: got %d visible, want 2", len(visible))
	}
}

func TestSeriesShufflePhotosFlag(t *testing.T) {
	setupTestDB(t)

	id, err := CreateSeries("Japan", "japan", "", "", "", true, true, false, true)
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	if s, err := GetSeriesByID(int(id)); err != nil || !s.ShufflePhotos {
		t.Fatalf("after create: ShufflePhotos=%v, err=%v", s != nil && s.ShufflePhotos, err)
	}
	if err := UpdateSeries(int(id), "Japan", "japan", "", "", "", true, true, false, false); err != nil {
		t.Fatalf("UpdateSeries: %v", err)
	}
	if s, err := GetSeriesBySlug("japan"); err != nil || s.ShufflePhotos {
		t.Errorf("after update: ShufflePhotos=%v, err=%v", s != nil && s.ShufflePhotos, err)
	}
	if list, _ := ListSeries(); len(list) != 1 || list[0].ShufflePhotos {
		t.Errorf("ListSeries: got %+v", list)
	}
}

// ───── Published at ─────

func TestPostPublishedAtStamping(t *testing.T) {
	setupTestDB(t)

	draftID, _ := CreatePost("draft", "Draft", "", "", "", false, 0)
	pubID, _ := CreatePost("pub", "Pub", "", "", "", true, 0)

	if p, _ := GetPostByID(int(draftID)); !p.PublishedAt.IsZero() {
		t.Errorf("draft: PublishedAt = %v, want zero", p.PublishedAt)
	}
	if p, _ := GetPostByID(int(pubID)); p.PublishedAt.IsZero() {
		t.Error("published on create: PublishedAt not set")
	}

	// Первая публикация черновика ставит дату.
	if err := UpdatePost(int(draftID), "draft", "Draft", "", "", "", true, 0); err != nil {
		t.Fatalf("UpdatePost publish: %v", err)
	}
	if p, _ := GetPostByID(int(draftID)); p.PublishedAt.IsZero() {
		t.Error("first publish: PublishedAt not set")
	}

	// Снятие с публикации и повторная публикация дату не меняют.
	old := time.Date(2020, 5, 1, 10, 30, 0, 0, time.UTC)
	if err := SetPostPublishedAt(int(draftID), old); err != nil {
		t.Fatalf("SetPostPublishedAt: %v", err)
	}
	UpdatePost(int(draftID), "draft", "Draft", "", "", "", false, 0) //nolint
	UpdatePost(int(draftID), "draft", "Draft", "", "", "", true, 0)  //nolint
	if p, _ := GetPostByID(int(draftID)); !p.PublishedAt.Equal(old) {
		t.Errorf("after unpublish/republish: PublishedAt = %v, want %v", p.PublishedAt, old)
	}
}

func TestPostsOrderedByPublishedAt(t *testing.T) {
	setupTestDB(t)
	sid, _ := CreateSeries("Trip", "trip", "", "", "", false, true, false, false)

	// Создаём a, b, c по порядку, но дату публикации a ставим самой поздней.
	ids := map[string]int{}
	for _, slug := range []string{"a", "b", "c"} {
		id, err := CreatePost(slug, slug, "", "", "", true, int(sid))
		if err != nil {
			t.Fatalf("CreatePost %s: %v", slug, err)
		}
		ids[slug] = int(id)
	}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	SetPostPublishedAt(ids["b"], base)                  //nolint
	SetPostPublishedAt(ids["c"], base.AddDate(0, 0, 1)) //nolint
	SetPostPublishedAt(ids["a"], base.AddDate(0, 0, 2)) //nolint

	slugs := func(posts []Post) string {
		var s string
		for _, p := range posts {
			s += p.Slug
		}
		return s
	}
	if posts, _, _ := ListPostsPaginated("", 1, 10); slugs(posts) != "acb" {
		t.Errorf("feed order = %q, want acb (newest published first)", slugs(posts))
	}
	if posts, _ := ListPostsMeta(true); slugs(posts) != "acb" {
		t.Errorf("meta order = %q, want acb", slugs(posts))
	}
	if posts, _ := PostsInSeries(int(sid)); slugs(posts) != "bca" {
		t.Errorf("series order = %q, want bca (oldest published first)", slugs(posts))
	}
}

func TestMigratePostsPublishedAtBackfill(t *testing.T) {
	setupTestDB(t)
	id, _ := CreatePost("old", "Old", "", "", "", true, 0)
	draftID, _ := CreatePost("old-draft", "Old draft", "", "", "", false, 0)
	// Как в базе до миграции: даты публикации нет, created_at в прошлом.
	DB.Exec(`UPDATE posts SET published_at = NULL, created_at = '2024-03-04 05:06:07'`) //nolint

	if err := migratePostsPublishedAt(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if p, _ := GetPostByID(int(id)); p.PublishedAt != time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC) {
		t.Errorf("backfilled PublishedAt = %v, want created_at", p.PublishedAt)
	}
	if p, _ := GetPostByID(int(draftID)); !p.PublishedAt.IsZero() {
		t.Errorf("draft PublishedAt = %v, want zero", p.PublishedAt)
	}
}

func TestPostDate(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	published := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
	if got := (Post{CreatedAt: created, PublishedAt: published}).Date(); !got.Equal(published) {
		t.Errorf("published post: Date = %v, want PublishedAt", got)
	}
	if got := (Post{CreatedAt: created}).Date(); !got.Equal(created) {
		t.Errorf("never-published draft: Date = %v, want CreatedAt", got)
	}
}

// ───── Bulk series ─────

func TestBulkSeries(t *testing.T) {
	setupTestDB(t)
	a, _ := CreateSeries("A", "a", "", "", "", false, true, false, false)
	b, _ := CreateSeries("B", "b", "", "", "", false, true, false, false)
	p1, _ := CreatePost("p1", "P1", "", "", "", true, 0)
	p2, _ := CreatePost("p2", "P2", "", "", "", false, int(b))
	p3, _ := CreatePost("p3", "P3", "", "", "", true, 0)

	seriesOf := func(id int64) int {
		p, err := GetPostByID(int(id))
		if err != nil {
			t.Fatalf("GetPostByID %d: %v", id, err)
		}
		return p.SeriesID
	}

	// Добавление: p1 без серии, p2 переезжает из B; p3 не выбран.
	if err := SetPostsSeries([]int{int(p1), int(p2)}, int(a)); err != nil {
		t.Fatalf("SetPostsSeries: %v", err)
	}
	if seriesOf(p1) != int(a) || seriesOf(p2) != int(a) || seriesOf(p3) != 0 {
		t.Errorf("after add: p1=%d p2=%d p3=%d, want %d %d 0", seriesOf(p1), seriesOf(p2), seriesOf(p3), a, a)
	}

	// Удаление из A трогает только посты A: p3 переносим в B и тоже «удаляем».
	SetPostsSeries([]int{int(p3)}, int(b)) //nolint
	if err := RemovePostsFromSeries(int(a), []int{int(p1), int(p3)}); err != nil {
		t.Fatalf("RemovePostsFromSeries: %v", err)
	}
	if seriesOf(p1) != 0 || seriesOf(p2) != int(a) || seriesOf(p3) != int(b) {
		t.Errorf("after remove: p1=%d p2=%d p3=%d, want 0 %d %d", seriesOf(p1), seriesOf(p2), seriesOf(p3), a, b)
	}

	// seriesID=0 — убрать из любой серии.
	if err := SetPostsSeries([]int{int(p2), int(p3)}, 0); err != nil {
		t.Fatalf("SetPostsSeries 0: %v", err)
	}
	if seriesOf(p2) != 0 || seriesOf(p3) != 0 {
		t.Errorf("after clear: p2=%d p3=%d, want 0 0", seriesOf(p2), seriesOf(p3))
	}

	// Пустой список — no-op без ошибки.
	if err := SetPostsSeries(nil, int(a)); err != nil {
		t.Errorf("SetPostsSeries(nil): %v", err)
	}
	if err := RemovePostsFromSeries(int(a), nil); err != nil {
		t.Errorf("RemovePostsFromSeries(nil): %v", err)
	}

	// ListPostsMeta отдаёт серию поста для админ-списка.
	SetPostsSeries([]int{int(p1)}, int(b)) //nolint
	posts, _ := ListPostsMeta(false)
	for _, p := range posts {
		if p.ID == int(p1) && (p.SeriesID != int(b) || p.SeriesName != "B") {
			t.Errorf("ListPostsMeta p1: series %d %q, want %d B", p.SeriesID, p.SeriesName, b)
		}
	}
}

func TestPhotoThumb(t *testing.T) {
	setupTestDB(t)
	AddPhoto("big.jpg", "", 0, 0, 1600, 1067) //nolint
	AddPhoto("small.jpg", "", 0, 0, 640, 480) //nolint

	pending, err := PhotosWithoutThumb()
	if err != nil || len(pending) != 2 {
		t.Fatalf("PhotosWithoutThumb: %d, %v; want 2", len(pending), err)
	}
	if pending[0].Filename != "big.jpg" || pending[0].Width != 1600 {
		t.Errorf("pending[0] = %+v", pending[0])
	}

	SetPhotoThumb("big.jpg", "thumbs/big.jpg") //nolint
	SetPhotoThumb("small.jpg", "small.jpg")    //nolint
	if pending, _ := PhotosWithoutThumb(); len(pending) != 0 {
		t.Errorf("after SetPhotoThumb: %d pending, want 0", len(pending))
	}
	photos, _ := ListPhotos("", "")
	if photos[0].Thumb != "thumbs/big.jpg" || photos[1].Thumb != "small.jpg" {
		t.Errorf("ListPhotos thumbs: %q %q", photos[0].Thumb, photos[1].Thumb)
	}

	filename, thumb, err := DeletePhoto(photos[0].ID)
	if err != nil || filename != "big.jpg" || thumb != "thumbs/big.jpg" {
		t.Errorf("DeletePhoto = %q, %q, %v", filename, thumb, err)
	}
}

// ───── Markdown re-render ─────

func TestRerenderMarkdown(t *testing.T) {
	setupTestDB(t)
	postID, err := CreatePost("p", "P", "post md", "<old>", "", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := GetPostByID(int(postID))
	seriesID, err := CreateSeries("S", "s", "series md", "<old>", "", false, true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateResume("resume md", "<old>"); err != nil {
		t.Fatal(err)
	}

	render := func(md string) (string, error) { return "<new>" + md, nil }
	n, err := RerenderMarkdown(render)
	if err != nil || n != 3 {
		t.Fatalf("RerenderMarkdown = %d, %v; want 3, nil", n, err)
	}
	p, _ := GetPostByID(int(postID))
	if p.BodyHTML != "<new>post md" {
		t.Errorf("post html = %q", p.BodyHTML)
	}
	if !p.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updated_at changed: %v → %v", before.UpdatedAt, p.UpdatedAt)
	}
	s, _ := GetSeriesBySlug("s")
	if s.ID != int(seriesID) || s.DescriptionHTML != "<new>series md" {
		t.Errorf("series html = %q", s.DescriptionHTML)
	}
	r, _ := GetResume()
	if r.BodyHTML != "<new>resume md" {
		t.Errorf("resume html = %q", r.BodyHTML)
	}

	// Повторный прогон ничего не меняет.
	if n, err := RerenderMarkdown(render); err != nil || n != 0 {
		t.Errorf("second run = %d, %v; want 0, nil", n, err)
	}

	// Ошибка рендера откатывает всё.
	failing := func(md string) (string, error) {
		if md == "series md" {
			return "", fmt.Errorf("boom")
		}
		return "<fail>", nil
	}
	if _, err := RerenderMarkdown(failing); err == nil {
		t.Fatal("want error")
	}
	if p, _ := GetPostByID(int(postID)); p.BodyHTML != "<new>post md" {
		t.Errorf("post html after rollback = %q", p.BodyHTML)
	}

	DB.Close()
	if _, err := RerenderMarkdown(render); err == nil {
		t.Error("want an error with the database closed")
	}
}

// ───── Slug history ─────

func TestSlugHistory(t *testing.T) {
	setupTestDB(t)
	id, err := CreatePost("old", "T", "", "", "", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	rename := func(slug string) {
		t.Helper()
		if err := UpdatePost(int(id), slug, "T", "", "", "", true, 0); err != nil {
			t.Fatal(err)
		}
	}
	rename("mid")
	rename("new")
	for _, s := range []string{"old", "mid"} {
		if got, ok := OldSlugTarget("post", s); !ok || got != int(id) {
			t.Errorf("OldSlugTarget(%q) = %d, %v", s, got, ok)
		}
	}
	if _, ok := OldSlugTarget("post", "new"); ok {
		t.Error("live slug is in history")
	}
	if _, ok := OldSlugTarget("series", "old"); ok {
		t.Error("post slug leaked into series history")
	}

	// Возврат к старому slug убирает его из истории.
	rename("old")
	if _, ok := OldSlugTarget("post", "old"); ok {
		t.Error("slug renamed back is still in history")
	}

	// Новый пост со slug из истории — запись истории удаляется.
	if _, err := CreatePost("mid", "Other", "", "", "", true, 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := OldSlugTarget("post", "mid"); ok {
		t.Error("slug reused by a new post is still in history")
	}

	// Удаление поста чистит его историю.
	if err := DeletePost(int(id)); err != nil {
		t.Fatal(err)
	}
	if _, ok := OldSlugTarget("post", "new"); ok {
		t.Error("history of a deleted post survived")
	}

	sid, err := CreateSeries("S", "s-old", "", "", "", false, true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateSeries(int(sid), "S", "s-new", "", "", "", false, true, false, false); err != nil {
		t.Fatal(err)
	}
	if got, ok := OldSlugTarget("series", "s-old"); !ok || got != int(sid) {
		t.Errorf("series OldSlugTarget = %d, %v", got, ok)
	}
	if err := DeleteSeries(int(sid)); err != nil {
		t.Fatal(err)
	}
	if _, ok := OldSlugTarget("series", "s-old"); ok {
		t.Error("history of a deleted series survived")
	}
}

// ───── Scheduled posts ─────

func TestScheduledPostsHidden(t *testing.T) {
	setupTestDB(t)
	sid, err := CreateSeries("S", "s", "", "", "", false, true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(slug string, at time.Time) int {
		t.Helper()
		id, err := CreatePost(slug, "Сеул "+slug, "сеул", "<p>сеул</p>", "", true, int(sid))
		if err != nil {
			t.Fatal(err)
		}
		if err := SetPostPublishedAt(int(id), at); err != nil {
			t.Fatal(err)
		}
		if err := SetPostTags(int(id), []string{"tag-" + slug}); err != nil {
			t.Fatal(err)
		}
		return int(id)
	}
	mk("past", time.Now().Add(-time.Hour))
	futureID := mk("future", time.Now().Add(time.Hour))

	p, err := GetPostByID(futureID)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Scheduled() || p.Live() {
		t.Errorf("future post: Scheduled=%v Live=%v", p.Scheduled(), p.Live())
	}

	slugs := func(posts []Post) []string {
		var s []string
		for _, p := range posts {
			s = append(s, p.Slug)
		}
		return s
	}
	want := "[past]"
	if got, _ := ListPosts(true); fmt.Sprint(slugs(got)) != want {
		t.Errorf("ListPosts(true) = %v", slugs(got))
	}
	if got, _ := ListPosts(false); len(got) != 2 {
		t.Errorf("ListPosts(false) = %v, want both", slugs(got))
	}
	if got, _ := ListPostsMeta(true); fmt.Sprint(slugs(got)) != want {
		t.Errorf("ListPostsMeta(true) = %v", slugs(got))
	}
	if got, _ := ListPostsMeta(false); len(got) != 2 {
		t.Errorf("ListPostsMeta(false) = %v, want both", slugs(got))
	}
	if got, total, _ := ListPostsPaginated("", 1, 10); fmt.Sprint(slugs(got)) != want || total != 1 {
		t.Errorf("ListPostsPaginated = %v, %d", slugs(got), total)
	}
	if got, total, _ := ListPostsPaginated("tag-future", 1, 10); len(got) != 0 || total != 0 {
		t.Errorf("ListPostsPaginated(tag-future) = %v, %d", slugs(got), total)
	}
	if got, _ := PostsInSeries(int(sid)); fmt.Sprint(slugs(got)) != want {
		t.Errorf("PostsInSeries = %v", slugs(got))
	}
	if series, _ := ListSeries(); len(series) != 1 || series[0].PostCount != 1 {
		t.Errorf("ListSeries post count = %+v", series)
	}
	if got, _ := SearchPosts("сеул"); fmt.Sprint(slugs(got)) != want {
		t.Errorf("SearchPosts = %v", slugs(got))
	}
	tags, _ := ListPublishedTags()
	for _, tg := range tags {
		if tg.Name == "tag-future" {
			t.Error("ListPublishedTags includes a tag used only by a scheduled post")
		}
	}

	// Дата наступила — пост появляется.
	if err := SetPostPublishedAt(futureID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _ := ListPosts(true); len(got) != 2 {
		t.Errorf("after the date: ListPosts(true) = %v", slugs(got))
	}
}

// ───── Related posts ─────

func TestRelatedPosts(t *testing.T) {
	setupTestDB(t)
	sid, err := CreateSeries("S", "s", "", "", "", false, true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	day := 0
	mk := func(slug string, series int, published bool, tags ...string) int {
		t.Helper()
		id, err := CreatePost(slug, slug, "", "", "", published, series)
		if err != nil {
			t.Fatal(err)
		}
		day++
		if err := SetPostPublishedAt(int(id), time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if err := SetPostTags(int(id), tags); err != nil {
			t.Fatal(err)
		}
		return int(id)
	}
	me := mk("me", int(sid), true, "a", "b", "c")
	mk("one-old", 0, true, "a")
	mk("two", 0, true, "a", "b")
	mk("one-new", 0, true, "c")
	mk("same-series", int(sid), true, "a", "b", "c")
	mk("draft", 0, false, "a", "b", "c")
	mk("unrelated", 0, true, "z")
	future, err := CreatePost("future", "future", "", "", "", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	SetPostPublishedAt(int(future), time.Now().Add(time.Hour))
	SetPostTags(int(future), []string{"a", "b", "c"})

	slugs := func(posts []Post) string {
		var s []string
		for _, p := range posts {
			s = append(s, p.Slug)
		}
		return fmt.Sprint(s)
	}
	got, err := RelatedPosts(me, int(sid), 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[two one-new one-old]"; slugs(got) != want {
		t.Errorf("RelatedPosts = %s, want %s", slugs(got), want)
	}
	if got, _ := RelatedPosts(me, int(sid), 1); slugs(got) != "[two]" {
		t.Errorf("limit 1 = %s", slugs(got))
	}
	// Без серии (или серия скрыта) — посты той же серии тоже предлагаются.
	if got, _ := RelatedPosts(me, 0, 1); slugs(got) != "[same-series]" {
		t.Errorf("no series = %s", slugs(got))
	}

	DB.Close()
	if _, err := RelatedPosts(me, 0, 3); err == nil {
		t.Error("want an error with the database closed")
	}
}

func TestThumbnails(t *testing.T) {
	setupTestDB(t)
	th := Thumbnail{Filename: "a.jpg", Thumb: "thumbs/a.jpg", Width: 1600, Height: 900, Placeholder: "data:image/jpeg;base64,AAA="}
	if err := UpsertThumbnail(th); err != nil {
		t.Fatal(err)
	}
	// Повторный upsert обновляет запись, а не дублирует её.
	th.Placeholder = "data:image/jpeg;base64,BBB="
	if err := UpsertThumbnail(th); err != nil {
		t.Fatal(err)
	}
	list, err := ListThumbnails()
	if err != nil || len(list) != 1 || list[0] != th {
		t.Fatalf("ListThumbnails = %+v, %v; want [%+v]", list, err, th)
	}
	if err := DeleteThumbnail("a.jpg"); err != nil {
		t.Fatal(err)
	}
	if list, _ := ListThumbnails(); len(list) != 0 {
		t.Errorf("after delete: %+v", list)
	}
}

func TestFileUsage_HomeBio(t *testing.T) {
	setupTestDB(t)
	SetSetting("home_bio", "Привет! ![я](/uploads/bio.jpg)") //nolint

	u, err := GetFileUsage("bio.jpg")
	if err != nil {
		t.Fatalf("GetFileUsage: %v", err)
	}
	if !u.InHomeBio || !u.InUse() {
		t.Errorf("GetFileUsage(bio.jpg) = %+v, want InHomeBio", u)
	}

	all, err := GetAllFileUsage([]string{"bio.jpg", "other.jpg"})
	if err != nil {
		t.Fatalf("GetAllFileUsage: %v", err)
	}
	if !all["bio.jpg"].InHomeBio {
		t.Errorf("GetAllFileUsage[bio.jpg] = %+v, want InHomeBio", all["bio.jpg"])
	}
	if all["other.jpg"].InUse() {
		t.Errorf("GetAllFileUsage[other.jpg] = %+v, want unused", all["other.jpg"])
	}
}

func TestSavePost(t *testing.T) {
	setupTestDB(t)
	at := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	id, err := SavePost(0, PostFields{Slug: "a", Title: "A", Published: true, PublishedAt: at, Tags: []string{"x", "y"}})
	if err != nil {
		t.Fatalf("SavePost create: %v", err)
	}
	p, _ := GetPostByID(id)
	if p.Title != "A" || !p.PublishedAt.Equal(at) || len(p.Tags) != 2 {
		t.Errorf("created post = %+v", p)
	}

	if _, err := SavePost(id, PostFields{Slug: "a", Title: "A2", Published: true, Tags: []string{"z"}}); err != nil {
		t.Fatalf("SavePost update: %v", err)
	}
	p, _ = GetPostByID(id)
	if p.Title != "A2" || !p.PublishedAt.Equal(at) || len(p.Tags) != 1 || p.Tags[0] != "z" {
		t.Errorf("updated post = %+v (date must stay %v)", p, at)
	}

	other, _ := SavePost(0, PostFields{Slug: "b", Title: "B"})
	if _, err := SavePost(other, PostFields{Slug: "a", Title: "B2", Tags: []string{"q"}}); !errors.Is(err, ErrSlugTaken) {
		t.Errorf("SavePost to a taken slug: err = %v, want ErrSlugTaken", err)
	}
	if _, err := SavePost(0, PostFields{Slug: "a", Title: "C"}); !errors.Is(err, ErrSlugTaken) {
		t.Errorf("SavePost create with a taken slug: err = %v, want ErrSlugTaken", err)
	}

	// Anything failing midway rolls the whole save back: a nonexistent
	// series violates the foreign key after the tags would've been written.
	if _, err := SavePost(other, PostFields{Slug: "b", Title: "B3", SeriesID: 999, Tags: []string{"q"}}); err == nil {
		t.Fatal("SavePost with a bad series: want error")
	}
	p, _ = GetPostByID(other)
	if p.Title != "B" || len(p.Tags) != 0 {
		t.Errorf("failed save left changes: title %q tags %v", p.Title, p.Tags)
	}
}

func TestListLiveTags(t *testing.T) {
	setupTestDB(t)
	live, _ := CreatePost("live", "Live", "", "", "", true, 0)
	draft, _ := CreatePost("draft", "Draft", "", "", "", false, 0)
	sched, _ := CreatePost("sched", "Sched", "", "", "", true, 0)
	SetPostPublishedAt(int(sched), time.Now().Add(48*time.Hour)) //nolint
	SetPostTags(int(live), []string{"shared", "public"})         //nolint
	SetPostTags(int(draft), []string{"shared", "secret"})        //nolint
	SetPostTags(int(sched), []string{"soon"})                    //nolint

	tags, err := ListLiveTags()
	if err != nil {
		t.Fatalf("ListLiveTags: %v", err)
	}
	var names []string
	for _, tg := range tags {
		names = append(names, tg.Name)
	}
	if got := strings.Join(names, ","); got != "public,shared" {
		t.Errorf("ListLiveTags = %s, want public,shared", got)
	}
}

func TestCreateFirstUser(t *testing.T) {
	// A file DB: with :memory: each pool connection would get its own empty
	// database, and the goroutines below use several connections.
	if err := Init(filepath.Join(t.TempDir(), "t.db")); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { DB.Close() })
	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) { errs <- CreateFirstUser(fmt.Sprintf("admin%d", i), "hash") }(i)
	}
	ok := 0
	for i := 0; i < n; i++ {
		switch err := <-errs; {
		case err == nil:
			ok++
		case !errors.Is(err, ErrAdminExists):
			t.Errorf("CreateFirstUser: %v", err)
		}
	}
	var users int
	DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users) //nolint
	if ok != 1 || users != 1 {
		t.Errorf("concurrent setup: %d succeeded, %d users; want exactly 1", ok, users)
	}
}

func TestSetSettings(t *testing.T) {
	setupTestDB(t)
	if err := SetSettings(map[string]string{"a": "1", "b": "2"}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	s := GetAllSettings()
	if s["a"] != "1" || s["b"] != "2" {
		t.Errorf("settings = %v", s)
	}
}
