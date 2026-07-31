package db

import (
	"fmt"
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

func TestTagSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Go", "go"},
		{"Web Dev", "web-dev"},
		{"Программирование", "программирование"},
		{"C++", "c"},
		{"  trim  ", "trim"},
	}
	for _, tc := range cases {
		if got := tagSlug(tc.in); got != tc.want {
			t.Errorf("tagSlug(%q) = %q, want %q", tc.in, got, tc.want)
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

	filename, err := DeletePhoto(id)
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

	CreateSession("alive", time.Now().Add(time.Hour))    //nolint
	CreateSession("dead", time.Now().Add(-time.Hour))    //nolint

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
