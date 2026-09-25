package app

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lifecycleUpload(t *testing.T, a *App, token, target, name string, data []byte) (Image, *httptest.ResponseRecorder) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	var result struct {
		Data Image `json:"data"`
	}
	if rec.Code == 200 && json.Unmarshal(rec.Body.Bytes(), &result) != nil {
		t.Fatalf("invalid response: %s", rec.Body.String())
	}
	return result.Data, rec
}

func anotherPNG(t *testing.T, shade uint8) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 3, 2))
	im.Set(1, 1, color.RGBA{R: shade, G: 10, B: 20, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestThumbnailsRespectVisibilityAndDecodeBound(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	public, rec := mediaUpload(t, a, token, "/api/upload/private?visibility=public", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	thumb := adminRequest(a, "", http.MethodGet, "/t/"+public.Filename, "")
	if thumb.Code != 200 || thumb.Header().Get("Content-Type") != "image/jpeg" || !bytes.HasPrefix(thumb.Body.Bytes(), []byte{0xff, 0xd8}) {
		t.Fatalf("thumbnail: %d %s", thumb.Code, thumb.Body.String())
	}
	private, rec := mediaUpload(t, a, token, "/api/upload/private?visibility=private", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if got := adminRequest(a, "", http.MethodGet, "/t/"+private.Filename, ""); got.Code != 404 {
		t.Fatalf("private thumbnail leaked: %d", got.Code)
	}
	if got := adminRequest(a, token, http.MethodGet, "/t/"+private.Filename, ""); got.Code != 200 {
		t.Fatalf("admin private thumbnail: %d", got.Code)
	}
	huge := tinyPNG(t)
	binary.BigEndian.PutUint32(huge[16:20], 10000)
	binary.BigEndian.PutUint32(huge[20:24], 10000)
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	id := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	name := id + ".png"
	if err := os.WriteFile(filepath.Join(a.DataDir, "uploads", name), huge, 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.saveImage(Image{ID: id, UUID: id, Filename: name, Format: "png", Size: int64(len(huge)), Visibility: "public", UploadedAt: now(), UpdatedAt: now()}); err != nil {
		t.Fatal(err)
	}
	if got := adminRequest(a, "", http.MethodGet, "/t/"+name, ""); got.Code != 415 {
		t.Fatalf("oversized decode: %d", got.Code)
	}
}

func TestReplaceAndRollbackKeepOriginalIDAndURL(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	first, rec := mediaUpload(t, a, token, "/api/upload/private?visibility=public", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	before := adminRequest(a, "", http.MethodGet, first.URL, "")
	if before.Code != 200 {
		t.Fatal(before.Code)
	}
	oldETag := before.Header().Get("ETag")
	replacement := anotherPNG(t, 99)
	after, rec := lifecycleUpload(t, a, token, "/api/admin/images/"+first.ID+"/replace", "new.png", replacement)
	if rec.Code != 200 || after.ID != first.ID || after.UUID != first.UUID || after.Filename != first.Filename {
		t.Fatalf("identity changed: %d %s", rec.Code, rec.Body.String())
	}
	if got := adminRequest(a, "", http.MethodGet, first.URL, ""); got.Code != 200 || !bytes.Equal(got.Body.Bytes(), replacement) || !strings.Contains(got.Header().Get("Cache-Control"), "no-cache") {
		t.Fatalf("replacement bytes/cache: %d", got.Code)
	}
	conditional := httptest.NewRequest(http.MethodGet, first.URL, nil)
	conditional.Header.Set("If-None-Match", oldETag)
	conditionalResponse := httptest.NewRecorder()
	a.Handler().ServeHTTP(conditionalResponse, conditional)
	if conditionalResponse.Code != 200 || !bytes.Equal(conditionalResponse.Body.Bytes(), replacement) {
		t.Fatalf("old ETag served stale bytes: %d", conditionalResponse.Code)
	}
	versions := adminRequest(a, token, http.MethodGet, "/api/admin/images/"+first.ID+"/versions", "")
	var list struct {
		Data struct {
			Versions []struct {
				ID string `json:"id"`
			} `json:"versions"`
		} `json:"data"`
	}
	if versions.Code != 200 || json.Unmarshal(versions.Body.Bytes(), &list) != nil || len(list.Data.Versions) != 1 {
		t.Fatalf("versions: %d %s", versions.Code, versions.Body.String())
	}
	if got := adminRequest(a, "", http.MethodGet, "/versions/"+first.UUID+"/"+list.Data.Versions[0].ID+".bin", ""); got.Code != 404 {
		t.Fatalf("version file leaked over static path: %d", got.Code)
	}
	rolled := adminRequest(a, token, http.MethodPost, "/api/admin/images/"+first.ID+"/rollback/"+list.Data.Versions[0].ID, "")
	if rolled.Code != 200 {
		t.Fatalf("rollback: %d %s", rolled.Code, rolled.Body.String())
	}
	if got := adminRequest(a, "", http.MethodGet, first.URL, ""); got.Code != 200 || !bytes.Equal(got.Body.Bytes(), before.Body.Bytes()) {
		t.Fatalf("rollback bytes: %d", got.Code)
	}
}

func TestImageVersionsRetainDefaultThree(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	settings := adminRequest(a, token, http.MethodGet, "/api/settings/image-lifecycle", "")
	if settings.Code != 200 || !strings.Contains(settings.Body.String(), `"retainVersions":3`) {
		t.Fatalf("default retention: %s", settings.Body.String())
	}
	im, rec := mediaUpload(t, a, token, "/api/upload/private?visibility=unlisted", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	for i := 0; i < 4; i++ {
		_, rec := lifecycleUpload(t, a, token, "/api/admin/images/"+im.ID+"/replace", "new.png", anotherPNG(t, uint8(10+i)))
		if rec.Code != 200 {
			t.Fatalf("replacement %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	versions := adminRequest(a, token, http.MethodGet, "/api/admin/images/"+im.ID+"/versions", "")
	var result struct {
		Data struct {
			Versions []imageVersion `json:"versions"`
		} `json:"data"`
	}
	if versions.Code != 200 || json.Unmarshal(versions.Body.Bytes(), &result) != nil || len(result.Data.Versions) != 3 {
		t.Fatalf("version cap: %d %s", versions.Code, versions.Body.String())
	}
}

func TestJPEGMetadataStripIsOptional(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	settings := adminRequest(a, token, http.MethodPut, "/api/settings/image-lifecycle", `{"retainVersions":3,"stripJPEGMetadata":true}`)
	if settings.Code != 200 {
		t.Fatalf("settings: %d %s", settings.Code, settings.Body.String())
	}
	var imageData bytes.Buffer
	if err := jpeg.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 3, 2)), nil); err != nil {
		t.Fatal(err)
	}
	marker := []byte("Exif\x00\x00GPS")
	segment := append([]byte{0xff, 0xe1, 0, byte(len(marker) + 2)}, marker...)
	jpegWithEXIF := append(append(append([]byte{}, imageData.Bytes()[:2]...), segment...), imageData.Bytes()[2:]...)
	late := []byte("Exif\x00\x00GPS-after-scan")
	lateSegment := append([]byte{0xff, 0xe1, 0, byte(len(late) + 2)}, late...)
	jpegWithEXIF = append(append(append([]byte{}, jpegWithEXIF[:len(jpegWithEXIF)-2]...), lateSegment...), jpegWithEXIF[len(jpegWithEXIF)-2:]...)
	im, rec := lifecycleUpload(t, a, token, "/api/upload/private?visibility=unlisted", "geo.jpg", jpegWithEXIF)
	if rec.Code != 200 {
		t.Fatalf("jpeg upload: %d %s", rec.Code, rec.Body.String())
	}
	stored := adminRequest(a, "", http.MethodGet, im.URL, "")
	if stored.Code != 200 || bytes.Contains(stored.Body.Bytes(), []byte("Exif")) || bytes.Contains(stored.Body.Bytes(), []byte("GPS")) {
		t.Fatalf("EXIF not stripped: %d", stored.Code)
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(stored.Body.Bytes())); err != nil {
		t.Fatalf("stripped JPEG invalid: %v", err)
	}
}

func TestReplacementSurvivesEasyImgRerun(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "db"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0700); err != nil {
		t.Fatal(err)
	}
	uuid := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	name := uuid + ".png"
	if err := os.WriteFile(filepath.Join(root, "uploads", name), tinyPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	legacy := `{"_id":"legacy-replace","uuid":"` + uuid + `","filename":"` + name + `","size":80,"uploadedByType":"private"}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "db", "images.db"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MigrateEasyImg(root); err != nil {
		t.Fatal(err)
	}
	token := adminToken(t, a)
	newBytes := anotherPNG(t, 77)
	_, rec := lifecycleUpload(t, a, token, "/api/admin/images/legacy-replace/replace", "fresh.png", newBytes)
	if rec.Code != 200 {
		t.Fatalf("replacement: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := a.MigrateEasyImg(root); err != nil {
		t.Fatal(err)
	}
	im, err := a.imageByID("legacy-replace")
	if err != nil || im.Size != int64(len(newBytes)) || im.OriginalName != "fresh.png" {
		t.Fatalf("reimport changed replacement metadata: %+v %v", im, err)
	}
	if got := adminRequest(a, "", http.MethodGet, "/i/"+name, ""); got.Code != 200 || !bytes.Equal(got.Body.Bytes(), newBytes) {
		t.Fatalf("reimport changed bytes: %d", got.Code)
	}
}
