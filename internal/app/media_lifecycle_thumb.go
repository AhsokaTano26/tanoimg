package app

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) thumbnailPath(im Image) string {
	return filepath.Join(a.DataDir, "thumbnails", fmt.Sprintf("%s-r%d.jpg", im.UUID, im.Revision))
}

func thumbnailSupported(format string) bool {
	return format == "jpg" || format == "jpeg" || format == "png" || format == "gif" || format == "apng"
}

func (a *App) thumbnailFile(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if !safeFilename.MatchString(filename) {
		http.NotFound(w, r)
		return
	}
	a.mediaMu.Lock()
	locked := true
	defer func() {
		if locked {
			a.mediaMu.Unlock()
		}
	}()
	uuid := strings.TrimSuffix(filename, path.Ext(filename))
	im, err := a.getImageByUUID(uuid)
	if err != nil || im.Filename != filename || im.IsDeleted || (im.Visibility == "private" && a.userID(r) == "") {
		http.NotFound(w, r)
		return
	}
	if im.IsNsfw {
		http.Error(w, "图片不可访问", http.StatusForbidden)
		return
	}
	if !thumbnailSupported(im.Format) {
		http.Error(w, "该格式不支持缩略图", http.StatusUnsupportedMediaType)
		return
	}
	thumb := a.thumbnailPath(im)
	if _, err := os.Stat(thumb); os.IsNotExist(err) {
		select {
		case a.processing <- struct{}{}:
			defer func() { <-a.processing }()
		case <-r.Context().Done():
			return
		}
		if _, err := os.Stat(thumb); os.IsNotExist(err) {
			if err := a.generateThumbnail(im, thumb); err != nil {
				if err == errThumbnailTooLarge {
					http.Error(w, "图片尺寸超过缩略图处理上限", http.StatusUnsupportedMediaType)
				} else {
					http.Error(w, "生成缩略图失败", http.StatusUnprocessableEntity)
				}
				return
			}
		}
	} else if err != nil {
		http.Error(w, "读取缩略图失败", 500)
		return
	}
	f, err := os.Open(thumb)
	if err != nil {
		http.Error(w, "读取缩略图失败", 500)
		return
	}
	defer f.Close()
	_, err = f.Stat()
	if err != nil {
		http.Error(w, "读取缩略图失败", 500)
		return
	}
	a.mediaMu.Unlock()
	locked = false
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("ETag", fmt.Sprintf(`"thumb-%s-%d"`, im.UUID, im.Revision))
	http.ServeContent(w, r, filename, time.Time{}, f)
}

var errThumbnailTooLarge = fmt.Errorf("thumbnail pixel limit exceeded")

func (a *App) generateThumbnail(im Image, dest string) error {
	source, err := os.Open(filepath.Join(a.DataDir, "uploads", im.Filename))
	if err != nil {
		return err
	}
	defer source.Close()
	cfg, _, err := image.DecodeConfig(source)
	if err != nil {
		return err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxProcessingPixels {
		return errThumbnailTooLarge
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return err
	}
	decoded, _, err := image.Decode(source)
	if err != nil {
		return err
	}
	width, height := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	outW, outH := width, height
	if width > 320 || height > 320 {
		if width >= height {
			outW = 320
			outH = height * 320 / width
		} else {
			outH = 320
			outW = width * 320 / height
		}
	}
	if outW < 1 {
		outW = 1
	}
	if outH < 1 {
		outH = 1
	}
	thumb := image.NewRGBA(image.Rect(0, 0, outW, outH))
	for y := 0; y < outH; y++ {
		for x := 0; x < outW; x++ {
			point := color.NRGBAModel.Convert(decoded.At(decoded.Bounds().Min.X+x*width/outW, decoded.Bounds().Min.Y+y*height/outH)).(color.NRGBA)
			alpha := uint32(point.A)
			thumb.SetRGBA(x, y, color.RGBA{R: uint8((uint32(point.R)*alpha + 255*(255-alpha)) / 255), G: uint8((uint32(point.G)*alpha + 255*(255-alpha)) / 255), B: uint8((uint32(point.B)*alpha + 255*(255-alpha)) / 255), A: 255})
		}
	}
	temp, err := os.CreateTemp(filepath.Dir(dest), ".thumb-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := jpeg.Encode(temp, thumb, &jpeg.Options{Quality: 78}); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), dest)
}
