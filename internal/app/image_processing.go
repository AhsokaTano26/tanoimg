package app

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"

	webp "github.com/SeriousBug/webp-go-pure/std"
)

// Processing is deliberately serialized: decoders and encoders retain multiple
// full-size pixel buffers, while ordinary uploads keep using the streaming path.
const maxProcessingPixels = 8_000_000

func (a *App) processImageFile(ctx context.Context, source *os.File, format string, size int64, config uploadConfig, compressionThreshold int64) (*os.File, string, int64, error) {
	target := format
	switch {
	case config.ConvertToWebp:
		target = "webp"
	case config.ConvertToPng:
		target = "png"
	case config.ConvertToJpg:
		target = "jpg"
	}
	if format == "gif" || (target == format && (!config.EnableCompression || size <= compressionThreshold)) {
		return source, format, size, nil
	}
	if format != "jpg" && format != "png" && format != "webp" {
		return nil, "", 0, fmt.Errorf("该图片格式暂不支持压缩或转换")
	}
	select {
	case a.processing <- struct{}{}:
		defer func() { <-a.processing }()
	case <-ctx.Done():
		return nil, "", 0, ctx.Err()
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, "", 0, err
	}
	configInfo, _, err := image.DecodeConfig(source)
	if err != nil || configInfo.Width <= 0 || configInfo.Height <= 0 || int64(configInfo.Width)*int64(configInfo.Height) > maxProcessingPixels {
		return nil, "", 0, fmt.Errorf("图片尺寸过大或无法解码")
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, "", 0, err
	}
	img, _, err := image.Decode(source)
	if err != nil {
		return nil, "", 0, fmt.Errorf("图片解码失败: %w", err)
	}
	out, err := os.CreateTemp(filepath.Join(a.DataDir, "uploads"), ".tanoimg-process-*")
	if err != nil {
		return nil, "", 0, err
	}
	defer func() {
		if err != nil {
			out.Close()
			os.Remove(out.Name())
		}
	}()
	quality := config.CompressionQuality
	if quality < 1 || quality > 100 {
		quality = 80
	}
	switch target {
	case "jpg":
		// JPEG has no alpha channel; use a white background for transparent images.
		canvas := image.NewRGBA(img.Bounds())
		draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
		draw.Draw(canvas, canvas.Bounds(), img, img.Bounds().Min, draw.Over)
		err = jpeg.Encode(out, canvas, &jpeg.Options{Quality: quality})
	case "png":
		level := png.DefaultCompression
		if config.EnableCompression {
			level = png.BestSpeed
		}
		err = (&png.Encoder{CompressionLevel: level}).Encode(out, img)
	case "webp":
		err = webp.Encode(out, img, &webp.Options{Quality: quality, Effort: webp.EffortFastest})
	default:
		err = fmt.Errorf("不支持的转换格式")
	}
	if err != nil {
		return nil, "", 0, fmt.Errorf("图片编码失败: %w", err)
	}
	stat, err := out.Stat()
	if err != nil {
		return nil, "", 0, err
	}
	if stat.Size() > config.MaxFileSize {
		err = fmt.Errorf("转换后的图片超过大小限制")
		return nil, "", 0, err
	}
	if _, err = out.Seek(0, io.SeekStart); err != nil {
		return nil, "", 0, err
	}
	return out, target, stat.Size(), nil
}
